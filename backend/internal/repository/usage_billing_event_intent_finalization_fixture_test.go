//go:build unit || integration

package repository

import (
	"encoding/hex"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
	"testing"
)

func nativeI2RebindFixture(t *testing.T, intent service.NativeUsageIntent, input service.NativeIntentFinalizationInput) service.NativeIntentFinalizationInput {
	t.Helper()
	ref, err := intent.NativeIntentReference()
	require.NoError(t, err)
	scope, err := hex.DecodeString(ref.ScopeDigest)
	require.NoError(t, err)
	input.Intent = ref
	input.Evidence.Intent = &ref
	input.Evidence.FrozenPricing = intent.Attempt.FrozenPricing
	input.Evidence.Evidence.OperationID = intent.Key.OperationID
	input.Evidence.Evidence.Version=2
	input.Evidence.Evidence.AttemptSequence = intent.Attempt.Sequence
	input.Evidence.Evidence.AttemptScopeDigest = [32]byte(scope)
	input.Evidence.Evidence.PricingDigest = [32]byte{}
	input.Evidence.Evidence.Terminal = model.TerminalCompleted
	input.Evidence.Evidence.Prepared = &intent.Attempt.Prepared
	input.Evidence.Observations = nil
	prepared, err := service.PrepareNativeIntentFinalization(input)
	require.NoError(t, err)
	return *prepared
}
