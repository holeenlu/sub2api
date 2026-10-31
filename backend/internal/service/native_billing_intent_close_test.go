package service

import (
	"context"
	"errors"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func nativeNotSentFixture(t *testing.T) (*NativeUsageIntentRecord, NativeIntentNotSentInput) {
	t.Helper()
	intent := nativeIntentFixture()
	record, err := MergeNativeUsageIntent(nil, intent)
	require.NoError(t, err)
	ref, err := intent.NativeIntentReference()
	require.NoError(t, err)
	return record, NativeIntentNotSentInput{Key: intent.Key, IntentFingerprint: ref.IntentFingerprint,
		Operation: model.OperationResult{OperationID: intent.Key.OperationID, APIKeyID: intent.Key.APIKeyID, UserID: intent.UserID, OriginalWireDigest: intent.OriginalWireDigest,
			State: model.OperationCanceled, Attempts: []model.AttemptFact{{Sequence: 1, Identity: intent.Attempt.Prepared, Send: model.SendNotSent}}}}
}

func TestNativeIntentNotSentRequiresCompleteActualEvidence(t *testing.T) {
	for _, sample := range []string{"valid", "unknown", "sent", "ambiguous", "running", "response", "zero_usage", "business_output", "delegated", "missing_attempt", "changed_scope", "wrong_wire", "older_sent"} {
		t.Run(sample, func(t *testing.T) {
			record, in := nativeNotSentFixture(t)
			switch sample {
			case "unknown":
				in.Operation.Attempts[0].Send = model.SendUnknown
			case "sent":
				in.Operation.Attempts[0].Send = model.SendSent
			case "ambiguous":
				in.Operation.Attempts[0].Send = model.SendAmbiguous
			case "running":
				in.Operation.State = model.OperationDispatching
			case "response":
				in.Operation.Attempts[0].ResponseID = "observed-response"
			case "zero_usage":
				in.Operation.Attempts[0].Usage = &model.UsageFact{Input: &model.TokenMeasurement{Value: 0}}
			case "business_output":
				in.Operation.Delivery.PossiblyCommitted = true
			case "delegated":
				in.Operation.DelegatedCycles = 1
			case "missing_attempt":
				in.Operation.Attempts = nil
			case "changed_scope":
				in.Operation.Attempts[0].Identity.TargetDigest = [32]byte{9}
			case "wrong_wire":
				in.Operation.OriginalWireDigest = [32]byte{8}
			case "older_sent":
				earlier := in.Operation.Attempts[0]
				earlier.Sequence, earlier.Send = 2, model.SendSent
				in.Operation.Attempts = append([]model.AttemptFact{earlier}, in.Operation.Attempts...)
			}
			proof, err := ValidateNativeIntentNotSent(record, in)
			if sample == "valid" {
				require.NoError(t, err)
				require.Len(t, proof.Attempts, 1)
			} else {
				require.Error(t, err)
				require.Nil(t, proof)
			}
		})
	}
}

func TestNativeIntentNotSentCannotOmitPreservedAttempt(t *testing.T) {
	record, in := nativeNotSentFixture(t)
	second := nativeIntentFixture()
	second.Attempt.Sequence = 2
	var err error
	record, err = MergeNativeUsageIntent(record, second)
	require.NoError(t, err)
	_, err = ValidateNativeIntentNotSent(record, in)
	require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
	in.Operation.Attempts = append(in.Operation.Attempts, model.AttemptFact{Sequence: 2, Identity: second.Attempt.Prepared, Send: model.SendNotSent})
	_, err = ValidateNativeIntentNotSent(record, in)
	require.NoError(t, err)
}

func TestNativeIntentCloseServiceCannotInventDurability(t *testing.T) {
	_, in := nativeNotSentFixture(t)
	repo := &intentServiceRepo{err: errors.New("commit acknowledgement lost")}
	svc, err := NewNativeBillingIntentService(repo)
	require.NoError(t, err)
	got, err := svc.CloseNotSent(t.Context(), in)
	require.ErrorIs(t, err, repo.err)
	require.False(t, got.Durable())
	canceled, cancel := context.WithCancel(t.Context())
	cancel()
	_, err = svc.CloseNotSent(canceled, in)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 1, repo.calls)
}

func TestNativeIntentNotSentRetainsAttemptWhoseScopeWasNotPreserved(t *testing.T) {
	for _, send := range []model.SendState{model.SendNotSent, model.SendUnknown, model.SendSent, model.SendAmbiguous} {
		record, in := nativeNotSentFixture(t)
		unsaved := in.Operation.Attempts[0]
		unsaved.Sequence, unsaved.Send = 2, send
		unsaved.Identity.TargetDigest = [32]byte{42}
		in.Operation.Attempts = append(in.Operation.Attempts, unsaved)
		proof, err := ValidateNativeIntentNotSent(record, in)
		if send != model.SendNotSent {
			require.ErrorIs(t, err, billing.ErrNativeIntentConflict)
			require.Nil(t, proof)
			continue
		}
		require.NoError(t, err)
		require.Len(t, proof.Attempts, 2)
		require.Equal(t, unsaved.Identity, proof.Attempts[1].Prepared)
		require.Len(t, record.Attempts, 1, "closure must not manufacture a captured price scope")
		in.Operation.Attempts[0].Send = model.SendSent
		_, err = ValidateNativeIntentNotSent(record, in)
		require.ErrorIs(t, err, billing.ErrNativeIntentConflict, "later capture failure cannot erase an earlier send")
	}
}
