package access

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

func TestRevalidateSupplyRequiresLiveCheckWhenAvailable(t *testing.T) {
	t.Parallel()
	storageFailure := fmt.Errorf("current ownership: %w", ErrVendorSupplyUnavailable)
	for _, tc := range []struct {
		name       string
		vendor     bool
		available  bool
		checkError error
		wantError  error
	}{
		{name: "ordinary_fake_without_capability"},
		{name: "supplier_without_capability", vendor: true, wantError: ErrVendorSupplyUnavailable},
		{name: "ordinary_still_checks_current_existence", available: true},
		{name: "ordinary_stale_owner_is_denied", available: true, checkError: storageFailure, wantError: storageFailure},
		{name: "supplier_live_approval", vendor: true, available: true},
		{name: "supplier_live_rejection", vendor: true, available: true, checkError: storageFailure, wantError: storageFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			ctx := t.Context()
			var checks DispatchChecks
			if tc.available {
				checks.Supply = func(received context.Context) error {
					calls++
					if received != ctx {
						t.Error("live validation lost the caller context")
					}
					return tc.checkError
				}
			}
			err := RevalidateSupply(ctx, DispatchSubject{VendorOwned: tc.vendor}, checks)
			if err != tc.wantError {
				t.Fatalf("error = %v, want original error %v", err, tc.wantError)
			}
			if (calls == 1) != tc.available {
				t.Fatalf("live checks = %d, capability present = %v", calls, tc.available)
			}
		})
	}
}

func TestRevalidateModeSelectsRequiredAuthority(t *testing.T) {
	t.Parallel()
	storageFailure := fmt.Errorf("mode revoked: %w", ErrVendorSupplyUnavailable)
	for _, tc := range []struct {
		name      string
		subject   DispatchSubject
		mode      string
		supply    bool
		modeCheck bool
		wantCheck string
		wantError error
	}{
		{name: "supplier_native", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "native", supply: true, modeCheck: true, wantCheck: "mode", wantError: storageFailure},
		{name: "supplier_bps", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "bps", supply: true, modeCheck: true, wantCheck: "mode", wantError: storageFailure},
		{name: "generic_approval_cannot_permit_native", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "native", supply: true, wantError: ErrVendorSupplyUnavailable},
		{name: "missing_both_capabilities", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "bps", wantError: ErrVendorSupplyUnavailable},
		{name: "mode_only_capability", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "bps", modeCheck: true, wantCheck: "mode", wantError: storageFailure},
		{name: "ordinary_current_ownership", subject: DispatchSubject{Platform: domain.PlatformOpenAI}, mode: "native", supply: true, modeCheck: true, wantCheck: "supply", wantError: storageFailure},
		{name: "ordinary_fake", subject: DispatchSubject{Platform: domain.PlatformOpenAI}, mode: "native", modeCheck: true},
		{name: "other_supplier_platform", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformAnthropic}, mode: "native", supply: true, modeCheck: true, wantCheck: "supply", wantError: storageFailure},
		{name: "other_supplier_requires_supply", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformAnthropic}, mode: "native", modeCheck: true, wantError: ErrVendorSupplyUnavailable},
		{name: "unknown_mode_reaches_authority", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, mode: "unknown", supply: true, modeCheck: true, wantCheck: "mode", wantError: storageFailure},
		{name: "empty_mode_is_not_generic_approval", subject: DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}, supply: true, modeCheck: true, wantCheck: "mode", wantError: storageFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := t.Context()
			called := ""
			var checks DispatchChecks
			check := func(received context.Context, kind string) error {
				if received != ctx {
					t.Error("live validation lost the caller context")
				}
				called += kind
				return storageFailure
			}
			if tc.supply {
				checks.Supply = func(ctx context.Context) error { return check(ctx, "supply") }
			}
			if tc.modeCheck {
				checks.Mode = func(ctx context.Context, mode string) error {
					if mode != tc.mode {
						t.Errorf("mode = %q, want %q", mode, tc.mode)
					}
					return check(ctx, "mode")
				}
			}
			err := RevalidateMode(ctx, tc.subject, tc.mode, checks)
			if err != tc.wantError || called != tc.wantCheck {
				t.Fatalf("result = (%v, %q), want (%v, %q)", err, called, tc.wantError, tc.wantCheck)
			}
		})
	}
}

func TestRevalidatePreservesCancellationAndFailureChain(t *testing.T) {
	t.Parallel()
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded, ErrVendorSupplyUnavailable} {
		t.Run(cause.Error(), func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			failure := fmt.Errorf("authoritative recheck: %w", cause)
			checks := DispatchChecks{
				Supply: func(received context.Context) error {
					if received.Err() != context.Canceled {
						t.Error("supply check lost cancellation")
					}
					return failure
				},
				Mode: func(received context.Context, _ string) error {
					if received.Err() != context.Canceled {
						t.Error("mode check lost cancellation")
					}
					return failure
				},
			}
			subject := DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}
			for _, err := range []error{RevalidateSupply(ctx, subject, checks), RevalidateMode(ctx, subject, "native", checks)} {
				if err != failure || !errors.Is(err, cause) {
					t.Fatalf("error identity/chain changed: %v", err)
				}
			}
		})
	}
}

func TestRevalidateEveryRetryObservesRevocation(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"supply", "native", "bps"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var revoked atomic.Bool
			var calls atomic.Int64
			check := func(context.Context) error {
				calls.Add(1)
				if revoked.Load() {
					return ErrVendorSupplyUnavailable
				}
				return nil
			}
			checks := DispatchChecks{Supply: check, Mode: func(ctx context.Context, _ string) error { return check(ctx) }}
			subject := DispatchSubject{VendorOwned: true, Platform: domain.PlatformOpenAI}
			revalidate := func() error {
				if mode == "supply" {
					return RevalidateSupply(ctx, subject, checks)
				}
				return RevalidateMode(ctx, subject, mode, checks)
			}
			const senders = 16
			ready := make(chan struct{}, senders)
			retry := make(chan struct{})
			var workers sync.WaitGroup
			for range senders {
				workers.Add(1)
				go func() {
					defer workers.Done()
					if err := revalidate(); err != nil {
						t.Errorf("initial dispatch: %v", err)
					}
					ready <- struct{}{}
					select {
					case <-retry:
						if err := revalidate(); !errors.Is(err, ErrVendorSupplyUnavailable) {
							t.Errorf("retry reused old approval: %v", err)
						}
					case <-ctx.Done():
						t.Error("timed out waiting for revocation")
					}
				}()
			}
			for range senders {
				select {
				case <-ready:
				case <-ctx.Done():
					workers.Wait()
					t.Fatal("timed out waiting for initial dispatches")
				}
			}
			revoked.Store(true)
			close(retry)
			workers.Wait()
			if got := calls.Load(); got != 2*senders {
				t.Fatalf("live checks = %d, want %d", got, 2*senders)
			}
		})
	}
}
