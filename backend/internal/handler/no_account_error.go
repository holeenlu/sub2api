package handler

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// noAccountErrorClassification describes the HTTP response to emit when
// account selection failed with ErrNoAvailableAccounts. Handlers obtain it
// via classifyNoAccountError and choose between:
//
//   - 404 model_not_found — the group has accounts, but none of them are
//     configured to serve the requested model (config / typo / unsupported
//     model). Returning 503 here misleads operators and trips reverse-proxy
//     health checks; 404 lets the client surface the real problem.
//
//   - 429 rate_limit_error — every account whose model mapping admits the
//     requested model (and that would be schedulable once its cooldown lifts)
//     is currently inside a rate-limit cooldown. This is the one temporary
//     exhaustion case where we can hand the client an actionable Retry-After
//     instead of a bare 503.
//
//   - 503 api_error — accounts that could serve the model exist but are
//     temporarily exhausted for another reason (overload, quota auto-pause,
//     runtime block) OR the group has no accounts at all. Both stay on 503
//     because retrying after a backoff can plausibly succeed (or, in the
//     empty-pool case, the operator may be in the middle of adding accounts).
type noAccountErrorClassification struct {
	Status        int
	ErrType       string
	Message       string
	ModelNotFound bool // true when this is a 404 model_not_found classification
	// RetryAfter is the hint to emit as the Retry-After header. Non-zero only
	// on 429 classifications.
	RetryAfter time.Duration
	// cooldownRetryAfter is the earliest rate-limit reset the diagnosis saw
	// among model-capable accounts when that alone did not justify a 429
	// (only part of the pool is cooling). It is both the precondition and the
	// payload of the scheduler-summary upgrade in classifySelectionFailureError:
	// no observed cooldown, no 429.
	cooldownRetryAfter time.Duration
}

const (
	noAccountRateLimitedMessage = "All available accounts are currently rate-limited. Please retry later."

	poolCooldownRetryAfterMin = time.Second
	poolCooldownRetryAfterMax = 5 * time.Minute
)

// retryAfterFromReset converts the earliest pool reset into a Retry-After
// duration. Retry-After carries whole seconds, so the remaining time is
// rounded up: a client that honours the header must not come back while the
// cooldown is still running and eat another 429. The result is clamped so an
// absent or absurd upstream reset cannot produce a zero (retry-storm) or
// multi-hour (client-stalling) hint.
func retryAfterFromReset(resetAt *time.Time) time.Duration {
	if resetAt == nil {
		return poolCooldownRetryAfterMin
	}
	d := time.Duration(math.Ceil(time.Until(*resetAt).Seconds())) * time.Second
	switch {
	case d < poolCooldownRetryAfterMin:
		return poolCooldownRetryAfterMin
	case d > poolCooldownRetryAfterMax:
		return poolCooldownRetryAfterMax
	default:
		return d
	}
}

var selectionModelRateLimitedPattern = regexp.MustCompile(`(?:model_rate_limited|rate_limited)=(\d+)`)

// classifySelectionFailureError preserves the scheduler's compact reason when
// the selection error itself reports model-capable accounts in a rate-limit
// cooldown. It only ever upgrades a plain 503 fallback that the diagnosis
// backed with a cooldown hint: the diagnosis-based verdicts (404 and 429) rank
// above the scheduler's summary, and a summary alone cannot date the retry.
func classifySelectionFailureError(err error, fallback noAccountErrorClassification) noAccountErrorClassification {
	if err == nil {
		return fallback
	}
	// A 404 model_not_found fallback is authoritative and must not be downgraded
	// to a rate-limit verdict. classifyNoAccountError only reaches it through
	// DiagnoseModelAvailabilityForPlatform, a dedicated database query over
	// persistent eligibility (active + schedulable + model_mapping) that already
	// established no account in the group can serve this model at all. A transient
	// per-model cooldown on one of the remaining candidates does not make "all
	// available accounts are rate-limited" true.
	//
	// Reporting 429 here is actively harmful: retrying can never succeed, and
	// clients that treat 429 as a rate limit retry hard and swallow the body
	// (Codex surfaces only "exceeded retry limit, last status: 429"), losing the
	// one message that names the real problem. It also flips the ops attribution
	// from a local model-configuration issue to routing capacity, because call
	// sites gate markOpsRoutingCapacityLimitedIfNoAvailable on ModelNotFound.
	if fallback.ModelNotFound {
		return fallback
	}
	// A 429 the diagnosis already produced carries a Retry-After computed from
	// the pool's actual reset times; rebuilding it from the error text would
	// throw that away.
	if fallback.Status == http.StatusTooManyRequests {
		return fallback
	}
	match := selectionModelRateLimitedPattern.FindStringSubmatch(strings.ToLower(err.Error()))
	if len(match) != 2 {
		return fallback
	}
	count, parseErr := strconv.Atoi(match[1])
	if parseErr != nil || count <= 0 {
		return fallback
	}
	// The summary names no reset time, only a count. The upgrade is worth doing
	// solely because a 429 can say "come back in N seconds"; when the diagnosis
	// saw no cooling model-capable account (no diagnoser wired, the candidate
	// query failed, or its view disagrees with the scheduler snapshot) there is
	// no honest N. Inventing one invites a retry storm and a bare 429 is the
	// very shape this path was meant to remove — so the verdict stays on the
	// diagnosis' own 503, which at least carries the scheduler summary.
	if fallback.cooldownRetryAfter <= 0 {
		return fallback
	}
	return noAccountErrorClassification{
		Status:     http.StatusTooManyRequests,
		ErrType:    "rate_limit_error",
		Message:    noAccountRateLimitedMessage,
		RetryAfter: fallback.cooldownRetryAfter,
	}
}

// classifySelectionFailureErrorFromGin chains classifySelectionFailureError
// after classifyNoAccountErrorFromGin and writes the Retry-After header for a
// 429 the scheduler summary produced. Call sites that feed the selection error
// into the classification must use this so the header is set once the final
// verdict is known.
func classifySelectionFailureErrorFromGin(c *gin.Context, err error, fallback noAccountErrorClassification) noAccountErrorClassification {
	classification := classifySelectionFailureError(err, fallback)
	if classification.Status != fallback.Status {
		setNoAccountRetryAfterHeader(c, classification)
	}
	return classification
}

// selectionErrorIsPoolExhaustion reports whether err belongs to the "the pool
// came up empty" family — the only selection failure the classification below
// is allowed to reinterpret. Every other error names its own cause
// (ErrClaudeCodeOnly, a group lookup failure, a database error); rewriting it
// into 404 model_not_found or 429 rate_limit_error answers a question nobody
// asked and drops the one message that explains the failure. A nil error is
// the "selection returned no account without saying why" case, which is the
// same empty-pool situation.
//
// The list matches isNoAccountFallbackTriggerError in the scheduling layer:
// exactly the errors that make the scheduler retry in a fallback group are
// the ones a pool diagnosis can speak to.
func selectionErrorIsPoolExhaustion(err error) bool {
	return err == nil ||
		errors.Is(err, service.ErrNoAvailableAccounts) ||
		errors.Is(err, service.ErrNoAvailableCompactAccounts)
}

// noAccountUnavailableClassification is the untouched 503 every "no available
// accounts" path started from. Call sites append the selection error to it via
// messageWithSelectionDetail, which is how a non-capacity failure keeps its
// own wording.
func noAccountUnavailableClassification() noAccountErrorClassification {
	return noAccountErrorClassification{
		Status:  http.StatusServiceUnavailable,
		ErrType: "api_error",
		Message: "Service temporarily unavailable",
	}
}

// classifySelectionError is the entry point for handlers that hold the account
// selection error: it gates the diagnosis on the error actually being pool
// exhaustion and then runs both stages (pool diagnosis, then the scheduler's
// own summary) so every protocol entry reaches the same verdict.
func classifySelectionError(
	ctx context.Context,
	err error,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	if !selectionErrorIsPoolExhaustion(err) {
		return noAccountUnavailableClassification()
	}
	return classifySelectionFailureError(err, classifyNoAccountError(ctx, diag, apiKey, routingModel, displayModel, platform))
}

// classifySelectionErrorFromGin is classifySelectionError for the gin call
// sites; it also writes the Retry-After header and the ops attribution.
func classifySelectionErrorFromGin(
	c *gin.Context,
	err error,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	if !selectionErrorIsPoolExhaustion(err) {
		return noAccountUnavailableClassification()
	}
	return classifySelectionFailureErrorFromGin(c, err,
		classifyNoAccountErrorFromGin(c, diag, apiKey, routingModel, displayModel, platform))
}

// classifyOpenAICompatibleSelectionErrorFromGin is classifySelectionErrorFromGin
// with the platform derived from the API key's group (OpenAI vs. Grok).
func classifyOpenAICompatibleSelectionErrorFromGin(
	c *gin.Context,
	err error,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
) noAccountErrorClassification {
	if !selectionErrorIsPoolExhaustion(err) {
		return noAccountUnavailableClassification()
	}
	return classifySelectionFailureErrorFromGin(c, err,
		classifyOpenAICompatibleNoAccountErrorFromGin(c, diag, apiKey, routingModel, displayModel))
}

// classifyNoAccountError decides between 404 model_not_found, 429
// rate_limit_error and 503 api_error for "no available accounts" failures,
// in that order of precedence.
//
// The classifier intentionally does not consume the original error: the
// selection layer never tells us *why* the pool came up empty (rate-limited
// vs. unsupported model are both wrapped as ErrNoAvailableAccounts). Instead
// we re-check pool composition through DiagnoseModelAvailabilityForPlatform.
// Its dedicated database query considers only persistent eligibility
// (active status + schedulable setting) and model_mapping, bypassing scheduler
// snapshots and transient filters. That guarantees a 404 is only returned
// when persistent account/group/model configuration must change before the
// request can succeed. The same query keeps transient state visible, which
// is what lets the diagnosis recognise a pool whose every capable account is
// waiting out a rate limit and turn that into a 429 with a Retry-After.
//
// routingModel is the model name that account selection actually compared
// against (i.e. after group-level dispatch mapping). displayModel is the
// raw model the caller asked for; it is used only in the user-facing error
// message so that internal mapping details don't leak. Most callers pass
// the same value for both.
//
// platform is the platform the request was routed to (use
// service.PlatformOpenAI / PlatformAnthropic / PlatformGemini). It is
// required because Anthropic/Gemini routes additionally surface
// mixed-scheduled Antigravity accounts; passing the wrong platform would
// flip a legitimate 503 to a misleading 404 (or vice versa).
func classifyNoAccountError(
	ctx context.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	fallback := noAccountUnavailableClassification()

	routingModel = strings.TrimSpace(routingModel)
	displayModel = strings.TrimSpace(displayModel)
	if displayModel == "" {
		displayModel = routingModel
	}
	if diag == nil || apiKey == nil || apiKey.GroupID == nil || routingModel == "" {
		return fallback
	}

	result := diag.DiagnoseModelAvailabilityForPlatform(ctx, apiKey.GroupID, routingModel, platform)
	if result.HasAccountsInPool && !result.HasModelSupport {
		return noAccountErrorClassification{
			Status:        http.StatusNotFound,
			ErrType:       "model_not_found",
			Message:       fmt.Sprintf("Model %q is not supported by any configured account in this group", displayModel),
			ModelNotFound: true,
		}
	}
	// The scheduler's own account list is filtered in SQL, so a fully
	// rate-limited pool reaches us as an empty candidate set with no reason
	// attached. Only the diagnosis can see the cooldown, and only a rate limit
	// gives the client a trustworthy "retry after N seconds".
	if result.HasModelSupport && result.AllModelCapableRateLimited {
		return noAccountErrorClassification{
			Status:     http.StatusTooManyRequests,
			ErrType:    "rate_limit_error",
			Message:    noAccountRateLimitedMessage,
			RetryAfter: retryAfterFromReset(result.EarliestRateLimitResetAt),
		}
	}
	if result.EarliestRateLimitResetAt != nil {
		// Part of the pool is cooling. Not enough for a 429 on its own, but if
		// the scheduler's summary later upgrades this 503, that 429 should say
		// when the first rate-limited account returns.
		fallback.cooldownRetryAfter = retryAfterFromReset(result.EarliestRateLimitResetAt)
	}
	return fallback
}

// classifyNoAccountErrorFromGin is a thin wrapper that forwards the gin
// context's underlying request context. Most call sites already have a
// *gin.Context handy, so this keeps the call sites uncluttered.
//
// Only for the "selection returned no account and no error" branch. Call sites
// that hold a selection error must go through classifySelectionErrorFromGin,
// which first checks the error is pool exhaustion at all — this classifier
// happily turns any failure into a 404 or 429.
func classifyNoAccountErrorFromGin(
	c *gin.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
	platform string,
) noAccountErrorClassification {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	classification := classifyNoAccountError(ctx, diag, apiKey, routingModel, displayModel, platform)
	if classification.ModelNotFound {
		service.MarkOpsClientBusinessLimited(c, service.OpsClientBusinessLimitedReasonLocalModelConfiguration)
	}
	setNoAccountRetryAfterHeader(c, classification)
	return classification
}

// setNoAccountRetryAfterHeader writes the Retry-After header for a 429
// classification. It lives here rather than at each call site: every handler
// on the "no available accounts" path already routes through the gin wrapper,
// so the 429 contract stays consistent without touching two dozen callers.
// Callers that already committed response headers (streaming) simply lose
// it, which is the best the protocol allows.
func setNoAccountRetryAfterHeader(c *gin.Context, cls noAccountErrorClassification) {
	if c == nil || cls.Status != http.StatusTooManyRequests || cls.RetryAfter <= 0 {
		return
	}
	seconds := int(cls.RetryAfter / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	c.Header("Retry-After", strconv.Itoa(seconds))
}

// noAccountWSCloseReason renders the classification as a WebSocket close
// reason. The close frame has no headers, so the retry hint travels inline
// as `retry_after=<seconds>`; the 404 and 503 keep the legacy wording.
func noAccountWSCloseReason(cls noAccountErrorClassification) string {
	if cls.Status != http.StatusTooManyRequests {
		return "no available account"
	}
	reason := "no available account: rate limited"
	if cls.RetryAfter > 0 {
		reason += fmt.Sprintf(", retry_after=%d", int(cls.RetryAfter/time.Second))
	}
	return reason
}

// messageWithSelectionDetail returns the client-facing message for this
// classification. The plain 503 gets the scheduler's selection summary
// appended (behind prefix) so operators can see why the pool came up empty;
// the 404 and 429 messages are contracts in their own right (they name the
// model / accompany a Retry-After) and are returned untouched.
func (cls noAccountErrorClassification) messageWithSelectionDetail(prefix string, err error) string {
	if cls.Status != http.StatusServiceUnavailable || err == nil {
		return cls.Message
	}
	return prefix + err.Error()
}

func classifyOpenAICompatibleNoAccountErrorFromGin(
	c *gin.Context,
	diag service.ModelAvailabilityDiagnoser,
	apiKey *service.APIKey,
	routingModel string,
	displayModel string,
) noAccountErrorClassification {
	ctx := context.Background()
	if c != nil && c.Request != nil {
		ctx = c.Request.Context()
	}
	return classifyNoAccountErrorFromGin(
		c,
		diag,
		apiKey,
		routingModel,
		displayModel,
		openAICompatibleRequestPlatform(ctx, apiKey),
	)
}

func openAICompatibleSelectionErrorForLog(err error, platform string) error {
	if err == nil || platform != service.PlatformGrok {
		return err
	}
	message := strings.ReplaceAll(err.Error(), "OpenAI accounts", "Grok accounts")
	if message == err.Error() {
		return err
	}
	return fmt.Errorf("%s", message)
}
