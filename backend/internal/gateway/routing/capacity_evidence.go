package routing

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

var ErrCapacityPolicyChanged = errors.New("capacity policy changed after acquisition")

// CapacityPolicy identifies the effective limit supplied by the authoritative
// user/account policy owner. Verify requires its current value again, including
// unlimited limits; an old snapshot alone is not a new-send authorization.
type CapacityPolicy struct {
	ResourceID int64
	User       bool
	Limit      int
}

// CapacityEvidence can only be produced after current-policy and token checks.
// The zero value has a zero fingerprint and cannot issue a SendPermit. No local
// counter, random marker or clock value is used as a substitute for ownership.
type CapacityEvidence struct {
	fingerprint [32]byte
	validUntil  time.Time
}

func (e CapacityEvidence) Fingerprint() [32]byte { return e.fingerprint }

// ValidUntil is the conservative Redis-derived deadline. Unlimited policies
// have no capacity expiry; the authority must still bound its policy/auth proof.
func (e CapacityEvidence) ValidUntil() time.Time { return e.validUntil }

func (r *AcquireResult) Policy() CapacityPolicy {
	if r == nil {
		return CapacityPolicy{}
	}
	return r.policy
}

// Verify consumes a freshly rechecked effective policy from the existing policy
// authority, then checks the real Redis member when limited. It does not impose
// SQL on every send: the policy owner retains its existing consistency contract.
func (r *AcquireResult) Verify(ctx context.Context, current CapacityPolicy) (CapacityEvidence, error) {
	if ctx == nil {
		return CapacityEvidence{}, ErrCapacityDependencyUnavailable
	}
	if err := ctx.Err(); err != nil {
		return CapacityEvidence{}, err
	}
	if r == nil || !r.Acquired || r.policy.ResourceID <= 0 || r.released == nil || r.released.Load() {
		return CapacityEvidence{}, ErrCapacityDependencyUnavailable
	}
	if current != r.policy {
		return CapacityEvidence{}, ErrCapacityPolicyChanged
	}
	token := ""
	until := time.Time{}
	if r.Unlimited {
		if current.Limit > 0 || r.Lease != nil {
			return CapacityEvidence{}, ErrCapacityDependencyUnavailable
		}
	} else {
		if current.Limit <= 0 || r.Lease == nil {
			return CapacityEvidence{}, ErrCapacityDependencyUnavailable
		}
		if err := r.Lease.Validate(ctx); err != nil {
			return CapacityEvidence{}, err
		}
		snapshot := r.Lease.Snapshot()
		if (current.User && (snapshot.Identity.UserID != current.ResourceID || snapshot.Identity.AccountID != 0)) || (!current.User && (snapshot.Identity.AccountID != current.ResourceID || snapshot.Identity.UserID != 0)) {
			return CapacityEvidence{}, ErrCapacityLeaseLost
		}
		token, until = snapshot.Identity.Token, snapshot.ExpiresAt
	}
	bytes := append([]byte("sub2api-capacity-proof-v1"), 0)
	if current.User {
		bytes = append(bytes, 1)
	} else {
		bytes = append(bytes, 0)
	}
	if r.Unlimited {
		bytes = append(bytes, 1)
	} else {
		bytes = append(bytes, 0)
	}
	var data [16]byte
	binary.BigEndian.PutUint64(data[:8], uint64(current.ResourceID))
	binary.BigEndian.PutUint64(data[8:], uint64(int64(current.Limit)))
	bytes = append(bytes, data[:]...)
	bytes = append(bytes, []byte(token)...)
	if r.released.Load() {
		return CapacityEvidence{}, ErrCapacityDependencyUnavailable
	}
	return CapacityEvidence{fingerprint: sha256.Sum256(bytes), validUntil: until}, nil
}
