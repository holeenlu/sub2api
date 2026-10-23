package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/imroc/req/v3"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type cloudContinuationKey struct{}

func cloudContinuationFromContext(ctx context.Context) *OpenAICloudBinding {
	if ctx == nil {
		return nil
	}
	b, _ := ctx.Value(cloudContinuationKey{}).(*OpenAICloudBinding)
	return b
}

func HasOpenAICloudContinuation(ctx context.Context) bool {
	return cloudContinuationFromContext(ctx) != nil
}

func cloudModelAllowed(a *Account, model string) bool {
	if a == nil {
		return false
	}
	for _, allowed := range a.OpenAICloudModels() {
		if allowed == model {
			return true
		}
	}
	return false
}

// Credentials rotate independently of execution settings. A setting change
// invalidates pending work without accidentally making every refresh a logout.
func cloudSettingsDigest(a *Account) string {
	if a == nil {
		return ""
	}
	models := a.OpenAICloudModels()
	sort.Strings(models)
	b, _ := json.Marshal(struct {
		Protocol string
		Enabled  bool
		Models   []string
		Mapping  map[string]string
		ProxyID  *int64
		Identity string
	}{OpenAICloudProtocolVersion, a.IsOpenAICloudEnabled(), models, a.GetModelMapping(), a.ProxyID, a.GetChatGPTAccountID() + "/" + a.GetChatGPTUserID()})
	digest := sha256.Sum256(b)
	return hex.EncodeToString(digest[:])
}

func (s *OpenAIGatewayService) SetCloudService(cloud *OpenAICloudService) {
	if s == nil {
		return
	}
	s.cloudService = cloud
	if cloud == nil {
		return
	}
	cloud.SetModelDiscovery(s.discoverCloudModels)
	store, ok := s.cache.(OpenAICloudSessionStore)
	if !ok {
		return
	}
	s.cloudSessions = NewOpenAICloudSessions(store, s.connectCloudSession, s.validateCloudSessionOwner)
	s.StartCloudBilling(store)
}

func (s *OpenAIGatewayService) validateCloudSessionOwner(ctx context.Context, owner OpenAICloudOwner) error {
	if s == nil || s.cloudService == nil {
		return errors.New("Cloud authorization service is unavailable")
	}
	store, ok := s.cache.(OpenAICloudSessionStore)
	if !ok {
		return errors.New("Cloud shared state is unavailable")
	}
	blocked, blockErr := store.CloudAccountBlocked(ctx, owner.AccountID)
	if blockErr != nil {
		return errors.New("Cloud settlement admission is unavailable")
	}
	if blocked {
		return errors.New("Cloud account has unresolved consumption and cannot run new work")
	}
	access, err := s.cloudService.GetAccessToken(ctx, owner.AccountID)
	if err != nil {
		return err
	}
	if access.Generation != owner.GrantGeneration || cloudSettingsDigest(access.Account) != owner.SettingsDigest {
		return errors.New("Cloud authorization or account settings changed")
	}
	if !owner.Probe {
		if !access.Account.IsOpenAICloudEnabled() || !cloudModelAllowed(access.Account, owner.Model) {
			return errors.New("Cloud is disabled for this account or model")
		}
		binding, err := s.cloudService.repo.GetCloudAuthorization(ctx, owner.AccountID)
		if err != nil || !cloudModelsVerified(access.Account, binding) {
			return errors.New("Cloud model capabilities must be verified again")
		}
	}
	return nil
}

func (s *OpenAIGatewayService) connectCloudSession(ctx context.Context, owner OpenAICloudOwner) (*cloudRPC, error) {
	if err := s.validateCloudSessionOwner(ctx, owner); err != nil {
		return nil, err
	}
	access, err := s.cloudService.GetAccessToken(ctx, owner.AccountID)
	if err != nil {
		return nil, err
	}
	access, err = s.checkCloudUsage(ctx, access)
	if err != nil {
		return nil, err
	}
	if access.Generation != owner.GrantGeneration {
		return nil, ErrOpenAICloudAuthorization
	}
	return dialOpenAICloud(ctx, access.AccessToken, access.Account.GetChatGPTAccountID(), cloudAccountProxy(access.Account))
}

// wham/usage is an admission check, never the token meter. Credentials are
// explicit and the shared native authority owns the sole 401 refresh attempt.
func (s *OpenAIGatewayService) checkCloudUsage(ctx context.Context, access *OpenAICloudAccess) (*OpenAICloudAccess, error) {
	if access == nil || s.cloudService == nil || s.cloudService.oauth == nil || s.cloudService.oauth.privacyClientFactory == nil {
		return nil, errors.New("Cloud usage client is unavailable")
	}
	for attempt := 0; attempt < 2; attempt++ {
		client, err := s.cloudService.oauth.privacyClientFactory(cloudAccountProxy(access.Account))
		if err != nil {
			return nil, errors.New("Cloud usage client is unavailable")
		}
		client.SetRedirectPolicy(req.NoRedirectPolicy())
		callCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
		var usage OpenAIQuotaUsage
		response, err := client.R().SetContext(callCtx).SetHeaders(map[string]string{
			"Authorization": "Bearer " + access.AccessToken, "ChatGPT-Account-Id": access.Account.GetChatGPTAccountID(),
			"originator": "sub2api", "User-Agent": "sub2api-cloud/0.1",
		}).SetSuccessResult(&usage).Get(chatGPTUsageURL)
		cancel()
		if err != nil {
			return nil, errors.New("Cloud usage check failed")
		}
		if response.StatusCode == http.StatusUnauthorized && attempt == 0 {
			provider := s.cloudService.tokens
			if provider == nil || provider.refreshAPI == nil || provider.executor == nil {
				return nil, ErrOpenAICloudAuthorization
			}
			if _, err := provider.refreshAPI.RefreshRejected(ctx, access.Account, provider.executor, access.AccessToken); err != nil {
				return nil, ErrOpenAICloudAuthorization
			}
			access, err = s.cloudService.GetAccessToken(ctx, access.Account.ID)
			if err != nil {
				return nil, err
			}
			continue
		}
		if response.StatusCode != http.StatusOK {
			return nil, &cloudGatewayError{status: mapCloudHTTPStatus(response.StatusCode), code: "cloud_usage_rejected", message: "Cloud usage admission was rejected"}
		}
		if usage.AccountID != "" && usage.AccountID != access.Account.GetChatGPTAccountID() {
			return nil, ErrOpenAICloudAuthorization
		}
		if usage.RateLimit == nil {
			return nil, errors.New("Cloud usage response is incomplete")
		}
		if !usage.RateLimit.Allowed || usage.RateLimit.LimitReached {
			return nil, &cloudGatewayError{status: http.StatusTooManyRequests, code: "cloud_quota_exhausted", message: "Cloud account quota is exhausted"}
		}
		return access, nil
	}
	return nil, ErrOpenAICloudAuthorization
}

func (s *OpenAIGatewayService) discoverCloudModels(ctx context.Context, access *OpenAICloudAccess) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	access, err := s.checkCloudUsage(ctx, access)
	if err != nil {
		return nil, err
	}
	conn, err := dialOpenAICloud(ctx, access.AccessToken, access.Account.GetChatGPTAccountID(), cloudAccountProxy(access.Account))
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	result, err := conn.Call(ctx, "model/list", map[string]any{"includeHidden": false})
	if err != nil {
		return nil, err
	}
	var payload struct {
		Data []struct {
			Model string `json:"model"`
			ID    string `json:"id"`
		} `json:"data"`
		NextCursor *string `json:"nextCursor"`
	}
	if json.Unmarshal(result, &payload) != nil {
		return nil, errors.New("Cloud model catalog is invalid")
	}
	models := make([]string, 0, len(payload.Data))
	for _, m := range payload.Data {
		if m.Model != "" && len(m.Model) <= 200 {
			models = append(models, m.Model)
		}
	}
	return models, nil
}

// Resolve before generic bootstrap normalization: a real Cloud tool result
// must never be turned into a user message or sent with a different account.
func (s *OpenAIGatewayService) PrepareCloudContinuation(ctx context.Context, userID, keyID, groupID int64, body []byte) (context.Context, error) {
	hasCloudMarker := cloudIsResponseID(gjson.GetBytes(body, "previous_response_id").String())
	for _, item := range gjson.GetBytes(body, "input").Array() {
		if item.Get("type").String() == "function_call_output" && strings.HasPrefix(item.Get("call_id").String(), "call_cloud_") {
			hasCloudMarker = true
			break
		}
	}
	if !hasCloudMarker {
		return ctx, nil
	}
	previous, calls, err := cloudResponseReferences(body)
	if err != nil {
		return nil, err
	}
	hasCloud := cloudIsResponseID(previous)
	for _, id := range calls {
		hasCloud = hasCloud || strings.HasPrefix(id, "call_cloud_")
	}
	if !hasCloud {
		return ctx, nil
	}
	if s == nil || s.cloudSessions == nil {
		return nil, errors.New("Cloud continuation is unavailable")
	}
	binding, err := s.cloudSessions.Resolve(ctx, userID, keyID, groupID, body)
	if err != nil {
		return nil, err
	}
	if binding == nil {
		return nil, errors.New("Cloud continuation is unknown or expired")
	}
	return context.WithValue(ctx, cloudContinuationKey{}, binding), nil
}

type cloudGatewayError struct {
	status        int
	code, message string
}

func (e *cloudGatewayError) Error() string { return e.message }
func mapCloudHTTPStatus(status int) int {
	switch status {
	case http.StatusUnauthorized, http.StatusForbidden:
		return http.StatusForbidden
	case http.StatusTooManyRequests:
		return status
	default:
		return http.StatusBadGateway
	}
}

func writeCloudGatewayError(c *gin.Context, err error) {
	if c == nil || c.Writer.Written() {
		return
	}
	status, code, message := http.StatusBadGateway, "cloud_execution_failed", "Cloud execution failed; the request was not replayed"
	if mappedStatus, mappedCode, mappedMessage, ok := cloudPublicError(err); ok {
		status, code, message = mappedStatus, mappedCode, mappedMessage
	}
	var upstream *cloudGatewayError
	if errors.As(err, &upstream) {
		status, code, message = upstream.status, upstream.code, upstream.message
	}
	if errors.Is(err, ErrOpenAICloudAuthorization) || errors.Is(err, ErrOpenAICloudSettings) {
		status, code, message = http.StatusConflict, "cloud_authorization_changed", "Cloud authorization or settings changed"
	}
	writeOpenAIResponsesErrorJSON(c, status, gin.H{"error": gin.H{"type": "api_error", "code": code, "message": message}})
}

func (s *OpenAIGatewayService) forwardOpenAICloud(ctx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
	start := time.Now()
	fail := func(err error) (*OpenAIForwardResult, error) { writeCloudGatewayError(c, err); return nil, err }
	if s.cloudSessions == nil || s.cloudService == nil || account == nil {
		return fail(errors.New("Cloud gateway is unavailable"))
	}
	if !account.IsOpenAICloudEnabled() {
		return fail(ErrOpenAICloudSettings)
	}
	restriction := s.detectCodexClientRestriction(c, account, body)
	if restriction.Enabled && !restriction.Matched {
		return fail(&cloudGatewayError{http.StatusForbidden, "client_restricted", CodexClientRestrictionMessage(restriction)})
	}
	access, err := s.cloudService.GetAccessToken(ctx, account.ID)
	if err != nil {
		return fail(err)
	}
	account = access.Account
	model := gjson.GetBytes(body, "model").String()
	upstreamModel := account.GetMappedModel(model)
	if !account.IsOpenAICloudEnabled() || !cloudModelAllowed(account, upstreamModel) {
		return fail(ErrOpenAICloudSettings)
	}
	if s.IsOpenAICloudRequestUnsupported(c, body) {
		return fail(&cloudGatewayError{http.StatusBadRequest, "cloud_unsupported_endpoint", "Cloud supports the HTTP Responses endpoint; this transport or operation is unsupported"})
	}
	if body, err = sjson.SetBytes(body, "model", upstreamModel); err != nil {
		return fail(err)
	}
	if _, err = parseCloudProtocolRequest(body); err != nil {
		return fail(&cloudGatewayError{http.StatusBadRequest, "cloud_unsupported_parameter", err.Error()})
	}
	raw, ok := c.Get(openAIHTTPResponseOwnerContextKey)
	if !ok {
		return fail(errors.New("Cloud request owner is missing"))
	}
	httpOwner, ok := raw.(openAIHTTPResponseOwner)
	if !ok || httpOwner.userID <= 0 || httpOwner.apiKeyID <= 0 {
		return fail(errors.New("Cloud request owner is invalid"))
	}
	owner := OpenAICloudOwner{UserID: httpOwner.userID, APIKeyID: httpOwner.apiKeyID, GroupID: getOpenAIGroupIDFromContext(c), AccountID: account.ID, GrantGeneration: access.Generation, Model: upstreamModel, SettingsDigest: cloudSettingsDigest(account)}
	ctx = EnsureOpenAITurnBudget(ctx, s.cfg)
	ctx = withTurnRecoveryTarget(ctx, account, UsageAuthorizationModeCloud, "/", cloudAccountProxy(account), access.Generation)
	if err = TakeOpenAITurnAttempt(ctx); err != nil {
		return fail(err)
	}
	rootID := "cloud_" + uuid.NewString()
	newBillingRoot := true
	if binding := cloudContinuationFromContext(ctx); binding != nil && binding.Status != "completed" {
		rootID = binding.RootRequestID
		newBillingRoot = false
		if binding.OpenAICloudOwner != owner {
			return fail(ErrOpenAICloudSettings)
		}
	} else {
		var effectiveEffort string
		body, effectiveEffort, err = s.inheritCloudBillingEffort(ctx, cloudContinuationFromContext(ctx), owner, body)
		if err != nil {
			return fail(err)
		}
		ctx, err = s.FinalizeCloudBillingSnapshot(ctx, upstreamModel, gjson.GetBytes(body, "service_tier").String(), effectiveEffort)
		if err != nil {
			return fail(err)
		}
		if err = s.HoldCloudBillingReservation(ctx, rootID); err != nil {
			return fail(err)
		}
	}
	setActualOpenAIUsageAuthorizationMode(c, UsageAuthorizationModeCloud)
	SetActualOpenAIUpstreamEndpoint(c, "wss://codex-cloud-backend.chatgpt.com/")
	stream := gjson.GetBytes(body, "stream").Bool()
	var first *int
	begun := false
	emit := func(event json.RawMessage) error {
		if !stream {
			return nil
		}
		if !begun {
			c.Header("Content-Type", "text/event-stream")
			c.Header("Cache-Control", "no-cache")
			c.Header("X-Accel-Buffering", "no")
			begun = true
		}
		if first == nil && gjson.GetBytes(event, "type").String() == "response.output_text.delta" {
			ms := int(time.Since(start).Milliseconds())
			first = &ms
		}
		if _, err := fmt.Fprintf(c.Writer, "data: %s\n\n", event); err != nil {
			return err
		}
		c.Writer.Flush()
		return nil
	}
	result, err := s.cloudSessions.Run(ctx, OpenAICloudRequest{Owner: owner, Body: body, RootRequestID: rootID, BillingSnapshot: CloudBillingSnapshotFromContext(ctx)}, emit)
	if result == nil {
		if newBillingRoot && errors.Is(err, ErrOpenAICloudNotStarted) {
			s.ReleaseCloudBillingReservation(rootID)
		}
		return fail(err)
	}
	if newBillingRoot && result.RootRequestID != rootID {
		// The actor returned an already completed idempotent tool result. This
		// generated root never started and will never have a settlement to ACK.
		s.ReleaseCloudBillingReservation(rootID)
	}
	responseID := gjson.GetBytes(result.Response, "id").String()
	if responseID != "" {
		s.bindHTTPResponseAccount(ctx, c, account, responseID)
	}
	if !stream && len(result.Response) > 0 {
		status := http.StatusOK
		if result.ErrorStatus >= 400 && result.ErrorStatus <= 599 {
			status = result.ErrorStatus
		}
		c.Data(status, "application/json", result.Response)
	}
	forward := &OpenAIForwardResult{RequestID: result.RootRequestID, Model: model, UpstreamModel: upstreamModel, UpstreamResponseModel: upstreamModel, AuthorizationMode: UsageAuthorizationModeCloud, Stream: stream, Duration: time.Since(start), FirstTokenMs: first, CloudManagedBilling: true, UpstreamEndpoint: "wss://codex-cloud-backend.chatgpt.com/"}
	if result.Usage != nil {
		forward.Usage = OpenAIUsage{InputTokens: result.Usage.InputTokens, OutputTokens: result.Usage.OutputTokens, CacheReadInputTokens: result.Usage.CachedInputTokens, CacheCreationInputTokens: result.Usage.CacheWriteInputTokens}
	}
	return forward, err
}

// The actor inherits omitted reasoning across a completed thread. Freeze the
// same effective effort before reserving the next billing root; otherwise a
// high-effort turn could be charged at the omitted/default effort rate.
func (s *OpenAIGatewayService) inheritCloudBillingEffort(ctx context.Context, binding *OpenAICloudBinding, owner OpenAICloudOwner, body []byte) ([]byte, string, error) {
	effort := gjson.GetBytes(body, "reasoning.effort").String()
	if binding == nil {
		return body, effort, nil
	}
	if binding.OpenAICloudOwner != owner || binding.Status != "completed" || s.cloudSessions == nil || s.cloudSessions.store == nil {
		return nil, "", ErrOpenAICloudSettings
	}
	if gjson.GetBytes(body, "reasoning").Exists() {
		return body, effort, nil
	}
	record, err := s.cloudSessions.store.LoadCloudSession(ctx, binding.SessionID)
	if err != nil {
		return nil, "", err
	}
	if record == nil || record.OpenAICloudOwner != owner || record.RootRequestID != binding.RootRequestID || record.Status != "completed" {
		return nil, "", ErrOpenAICloudSettings
	}
	var previous OpenAICloudBillingSnapshot
	if json.Unmarshal(record.BillingSnapshot, &previous) != nil || previous.Version != 1 || previous.UserID != owner.UserID || previous.APIKeyID != owner.APIKeyID || valueOrZero(previous.GroupID) != owner.GroupID || previous.AccountID != owner.AccountID || previous.UpstreamModel != owner.Model {
		return nil, "", errors.New("Cloud preceding turn has no matching billing settings")
	}
	effort = previous.ReasoningEffort
	if effort != "" {
		body, err = sjson.SetBytes(body, "reasoning.effort", effort)
		if err != nil {
			return nil, "", err
		}
	}
	return body, effort, nil
}

func (s *OpenAIGatewayService) IsOpenAICloudRequestUnsupported(c *gin.Context, body []byte) bool {
	return c == nil || c.Request == nil || strings.EqualFold(c.GetHeader("Upgrade"), "websocket") || IsOpenAIResponsesCompactPath(c) || gjson.GetBytes(body, "compaction_trigger").Exists()
}
