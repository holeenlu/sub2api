package access

import (
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

var (
	ErrKeyDisabled       = errors.New("API key is disabled")
	ErrKeyExpired        = errors.New("API key has expired")
	ErrKeyQuotaExhausted = errors.New("API key quota is exhausted")
	ErrKeyActorChanged   = errors.New("API key ownership changed")
	ErrKeyUserInactive   = errors.New("API key user is unavailable")
	ErrKeyVendorDenied   = errors.New("vendor identity cannot use consumer API keys")
)

// KeyState is a read-only value projection of the current key. ExpiresAt is
// borrowed only for this call; nil means no expiry. Quota <= 0 is unlimited.
type KeyState struct {
	Status    string
	ExpiresAt *time.Time
	Quota     float64
	QuotaUsed float64
}

// KeyStatusPermitsAuthentication preserves usage/billing-read and simple-mode
// access for expired/exhausted keys. Dispatch must also check KeyQuotaError.
func KeyStatusPermitsAuthentication(status string) bool {
	return status == domain.StatusActive || status == "expired" || status == "quota_exhausted"
}

// KeyUserIsActive interprets only the persisted active state, not role or owner.
func KeyUserIsActive(status string) bool { return status == domain.StatusActive }

// KeyQuotaError preserves the ingress order: stored exhaustion, stored expiry,
// actual expiry, actual exhaustion. Equality at the expiry instant is not yet
// expired, matching the existing API-key contract. Simple mode skips only this
// stage, never identity/group/IP checks. This function has no billing writes.
func KeyQuotaError(key KeyState, simple bool, now time.Time) error {
	if simple {
		return nil
	}
	if key.Status == "quota_exhausted" {
		return ErrKeyQuotaExhausted
	}
	if key.Status == "expired" {
		return ErrKeyExpired
	}
	if key.ExpiresAt != nil && now.After(*key.ExpiresAt) {
		return ErrKeyExpired
	}
	if key.Quota > 0 && key.QuotaUsed >= key.Quota {
		return ErrKeyQuotaExhausted
	}
	return nil
}

// KeyActor contains authoritative loaded identifiers and identity classification
// supplied by the existing identity boundary. It never derives roles itself.
type KeyActor struct {
	ExpectedKeyID, ExpectedUserID    int64
	KeyID, KeyUserID, UserID         int64
	UserPresent, UserDeleted, Vendor bool
	UserStatus                       string
}

// ValidateKeyActor binds the current key and loaded user to the operation's
// authenticated principal. Missing/zero identity never becomes permission.
func ValidateKeyActor(actor KeyActor) error {
	if actor.ExpectedKeyID <= 0 || actor.ExpectedUserID <= 0 || actor.KeyID != actor.ExpectedKeyID || actor.KeyUserID != actor.ExpectedUserID {
		return ErrKeyActorChanged
	}
	if !actor.UserPresent || actor.UserID != actor.ExpectedUserID || actor.UserDeleted || !KeyUserIsActive(actor.UserStatus) {
		return ErrKeyUserInactive
	}
	if actor.Vendor {
		return ErrKeyVendorDenied
	}
	return nil
}
