package access

import (
	"errors"
	"testing"
	"time"
)

func TestKeyAuthenticationStatus(t *testing.T) {
	for _, status := range []string{"active", "expired", "quota_exhausted", "disabled", "", "ACTIVE", "future"} {
		t.Run(status, func(t *testing.T) {
			want := status == "active" || status == "expired" || status == "quota_exhausted"
			if KeyStatusPermitsAuthentication(status) != want {
				t.Fatal("authentication status changed")
			}
			if KeyUserIsActive(status) != (status == "active") {
				t.Fatal("user status changed")
			}
		})
	}
}

func TestKeyQuotaOrderAndSimpleMode(t *testing.T) {
	now := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	past, future := now.Add(-time.Second), now.Add(time.Second)
	for _, tc := range []struct {
		name string
		key  KeyState
		want error
	}{
		{name: "active", key: KeyState{Status: "active"}},
		{name: "stored quota precedes actual expiry", key: KeyState{Status: "quota_exhausted", ExpiresAt: &past}, want: ErrKeyQuotaExhausted},
		{name: "stored expiry precedes actual quota", key: KeyState{Status: "expired", Quota: 1, QuotaUsed: 2}, want: ErrKeyExpired},
		{name: "runtime expiry precedes quota", key: KeyState{Status: "active", ExpiresAt: &past, Quota: 1, QuotaUsed: 2}, want: ErrKeyExpired},
		{name: "quota exact limit", key: KeyState{Status: "active", Quota: 1, QuotaUsed: 1}, want: ErrKeyQuotaExhausted},
		{name: "quota below limit", key: KeyState{Status: "active", Quota: 2, QuotaUsed: 1}},
		{name: "zero unlimited", key: KeyState{Status: "active", QuotaUsed: 10}},
		{name: "negative unlimited", key: KeyState{Status: "active", Quota: -1, QuotaUsed: 10}},
		{name: "expiry equality", key: KeyState{Status: "active", ExpiresAt: &now}},
		{name: "future expiry", key: KeyState{Status: "active", ExpiresAt: &future}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := KeyQuotaError(tc.key, false, now); !errors.Is(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
			if got := KeyQuotaError(tc.key, true, now); got != nil {
				t.Fatal(got)
			}
		})
	}
}

func TestValidateKeyActor(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*KeyActor)
		want   error
	}{
		{name: "valid"},
		{name: "zero expected key", change: func(a *KeyActor) { a.ExpectedKeyID = 0 }, want: ErrKeyActorChanged},
		{name: "zero expected user", change: func(a *KeyActor) { a.ExpectedUserID = 0 }, want: ErrKeyActorChanged},
		{name: "key changed", change: func(a *KeyActor) { a.KeyID++ }, want: ErrKeyActorChanged},
		{name: "key ownership changed", change: func(a *KeyActor) { a.KeyUserID++ }, want: ErrKeyActorChanged},
		{name: "missing user", change: func(a *KeyActor) { a.UserPresent = false }, want: ErrKeyUserInactive},
		{name: "different loaded user", change: func(a *KeyActor) { a.UserID++ }, want: ErrKeyUserInactive},
		{name: "inactive user", change: func(a *KeyActor) { a.UserStatus = "disabled" }, want: ErrKeyUserInactive},
		{name: "deleted user", change: func(a *KeyActor) { a.UserDeleted = true }, want: ErrKeyUserInactive},
		{name: "vendor", change: func(a *KeyActor) { a.Vendor = true }, want: ErrKeyVendorDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := KeyActor{ExpectedKeyID: 2, ExpectedUserID: 3, KeyID: 2, KeyUserID: 3, UserID: 3, UserPresent: true, UserStatus: "active"}
			if tc.change != nil {
				tc.change(&actor)
			}
			if got := ValidateKeyActor(actor); !errors.Is(got, tc.want) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}
