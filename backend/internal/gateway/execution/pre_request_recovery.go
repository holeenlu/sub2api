package execution

import (
	"context"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

// PreRequestFailure is transport evidence for the existing native HTTP TLS
// recovery. It is not permission, a budget, or proof that another target is safe.
// Both transient flags require the original wrapped error identities, not text.
// Missing trace/unknown state must remain ineligible. Providers report this from
// the same prepared attempt that actually failed, never from a later account.
type PreRequestFailure struct {
	SendState                    model.SendState
	BodyKind                     model.BodyKind
	OpenAIProfile                bool
	HTTPS                        bool
	ConfiguredProxy              bool
	TLSHandshakeFailed           bool
	TransientTLSHandshakeFailure bool
	TransientTransportFailure    bool
}

// PreRequestRetryDelay preserves the existing cancellable TLS-before-HTTP wait.
// The operation owner performs and accounts for waiting; this package does not
// start timers, allocate an allowance, or issue a network request here.
const PreRequestRetryDelay = 150 * time.Millisecond

// PreRequestRetryAllowed preserves the old one-repeat eligibility. retries is
// the existing owner-held count for this recovery sequence, not a fresh counter
// per failed send. A true result still requires new Prepare, live Admit and a
// fresh one-shot permit spending the SAME operation/owner/target AttemptBudget.
// Preserve the original sealed identity when preparing that retry; a changed
// grant, wire, proxy or target needs its own separately justified recovery action.
// Recheck cancellation/output/deadlines at admission; no permission is extended.
func PreRequestRetryAllowed(ctx context.Context, failure PreRequestFailure, retries int) bool {
	return ctx != nil && ctx.Err() == nil && retries == 0 &&
		failure.OpenAIProfile && failure.HTTPS && failure.ConfiguredProxy &&
		failure.SendState == model.SendNotSent && failure.BodyKind == model.BodyReplayable &&
		failure.TLSHandshakeFailed && failure.TransientTLSHandshakeFailure && failure.TransientTransportFailure
}
