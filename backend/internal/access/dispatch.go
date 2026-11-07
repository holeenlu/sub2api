// Package access owns authorization decisions at new-dispatch boundaries.
package access

import (
	"context"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// ErrVendorSupplyUnavailable denies a new dispatch, not an accepted stream.
var ErrVendorSupplyUnavailable = errors.New("supplier account is not eligible for new dispatch")

// DispatchSubject classifies the selected resource; it is not an authorization
// grant. VendorOwned records presence, including an explicitly zero vendor ID.
// The zero value describes an ordinary account on an unspecified platform.
type DispatchSubject struct {
	VendorOwned bool
	Platform    string
}

// DispatchChecks are narrow live-validation ports bound by the entity/storage
// adapter to the same selected account and group as DispatchSubject. The adapter
// must preserve an absent group versus an explicit zero group. Checks query the
// current authority; they must not substitute cached eligibility. Errors retain
// their identity and chain. Nil functions mean the capability is unavailable, as
// in lightweight repository fakes; the production repository supplies both.
type DispatchChecks struct {
	Supply func(context.Context) error
	Mode   func(context.Context, string) error
}

// RevalidateSupply checks current existence/ownership even for ordinary accounts
// when the capability exists. Supplier dispatch fails closed without it.
// Call on every new send/retry; this function owns no stream or cached grant.
func RevalidateSupply(ctx context.Context, subject DispatchSubject, checks DispatchChecks) error {
	if checks.Supply != nil {
		return checks.Supply(ctx)
	}
	if subject.VendorOwned {
		return ErrVendorSupplyUnavailable
	}
	return nil
}

// RevalidateMode requires the selected OpenAI supplier's actual transport mode
// to pass live authorization. Generic supply approval cannot substitute for it.
// The caller supplies the mode from its transport, never a context marker. Mode
// validity and eligibility remain in the authoritative storage validator; even
// empty/unknown modes reach that validator unchanged. Other resources use the
// ordinary supply check. This does not grant permission for a subsequent send.
func RevalidateMode(ctx context.Context, subject DispatchSubject, mode string, checks DispatchChecks) error {
	if !subject.VendorOwned || subject.Platform != domain.PlatformOpenAI {
		return RevalidateSupply(ctx, subject, checks)
	}
	if checks.Mode == nil {
		return ErrVendorSupplyUnavailable
	}
	return checks.Mode(ctx, mode)
}
