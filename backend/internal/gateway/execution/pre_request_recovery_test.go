package execution

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
)

func TestPreRequestRecoveryRequiresEveryOriginalSafetyCondition(t *testing.T) {
	valid := PreRequestFailure{SendState: model.SendNotSent, BodyKind: model.BodyReplayable,
		OpenAIProfile: true, HTTPS: true, ConfiguredProxy: true, TLSHandshakeFailed: true,
		TransientTLSHandshakeFailure: true, TransientTransportFailure: true}
	if !PreRequestRetryAllowed(t.Context(), valid, 0) || PreRequestRetryDelay != 150*time.Millisecond {
		t.Fatal("existing TLS-before-HTTP recovery was lost")
	}
	cases := []struct {
		name   string
		change func(*PreRequestFailure)
	}{
		{"default_profile", func(f *PreRequestFailure) { f.OpenAIProfile = false }},
		{"http", func(f *PreRequestFailure) { f.HTTPS = false }},
		{"direct", func(f *PreRequestFailure) { f.ConfiguredProxy = false }},
		{"unknown_send", func(f *PreRequestFailure) { f.SendState = model.SendUnknown }},
		{"sent", func(f *PreRequestFailure) { f.SendState = model.SendSent }},
		{"ambiguous", func(f *PreRequestFailure) { f.SendState = model.SendAmbiguous }},
		{"stream", func(f *PreRequestFailure) { f.BodyKind = model.BodyExclusiveStream }},
		{"unknown_body", func(f *PreRequestFailure) { f.BodyKind = model.BodyUnknown }},
		{"no_handshake_failure", func(f *PreRequestFailure) { f.TLSHandshakeFailed = false }},
		{"permanent_handshake_failure", func(f *PreRequestFailure) { f.TransientTLSHandshakeFailure = false }},
		{"different_final_error", func(f *PreRequestFailure) { f.TransientTransportFailure = false }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := valid
			tc.change(&f)
			if PreRequestRetryAllowed(t.Context(), f, 0) {
				t.Fatal("unsafe recovery admitted")
			}
		})
	}
	for _, count := range []int{-1, 1, 2} {
		if PreRequestRetryAllowed(t.Context(), valid, count) {
			t.Fatalf("invalid repeat count %d admitted", count)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if PreRequestRetryAllowed(ctx, valid, 0) || PreRequestRetryAllowed(nil, valid, 0) || PreRequestRetryAllowed(t.Context(), PreRequestFailure{}, 0) {
		t.Fatal("missing/canceled evidence admitted")
	}
}
