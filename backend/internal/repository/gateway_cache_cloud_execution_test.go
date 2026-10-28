package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCloudExecutionRedisClaimFencesCrossInstanceOwnerAndCancellation(t *testing.T) {
	for _, change := range []string{"duplicate", "grant_digest", "command", "owner", "lease", "cancel"} {
		t.Run(change, func(t *testing.T) {
			cache, server := newCloudStoreFixture(t)
			record := cloudStoreRecord("execution", 1, 7)
			require.NoError(t, cache.CreateCloudSession(t.Context(), record))
			digest := [32]byte{1}
			require.NoError(t, cache.OfferCloudExecution(t.Context(), record.SessionID, "command", "operation", digest))
			claim := service.CloudExecutionClaim{SessionID: record.SessionID, OwnerToken: record.OwnerToken, CommandID: "command", OperationID: "operation", TransferDigest: digest}
			remaining, err := cache.ClaimCloudExecution(t.Context(), claim)
			require.NoError(t, err)
			require.Positive(t, remaining)
			// A second process has only the shared Redis state, not a Go pointer.
			other := &gatewayCache{rdb: cache.rdb}
			switch change {
			case "duplicate":
				_, err = other.ClaimCloudExecution(t.Context(), claim)
			case "grant_digest":
				claim.TransferDigest[0]++
				_, err = other.CheckCloudExecution(t.Context(), claim)
			case "command":
				claim.CommandID = "another"
				_, err = other.CheckCloudExecution(t.Context(), claim)
			case "owner":
				require.NoError(t, cache.rdb.Set(t.Context(), cloudKey("owner", record.SessionID), "replacement", time.Minute).Err())
				_, err = other.CheckCloudExecution(t.Context(), claim)
			case "lease":
				server.FastForward(service.OpenAICloudOwnerLease + time.Second)
				_, err = other.CheckCloudExecution(t.Context(), claim)
			case "cancel":
				require.NoError(t, cache.DeleteCloudEvents(t.Context(), claim.CommandID))
				_, err = other.CheckCloudExecution(t.Context(), claim)
			}
			require.Error(t, err)
		})
	}
}

func TestCloudExecutionRedisReturnSurvivesTerminalLeaseRetirementWithoutRegrant(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	record := cloudStoreRecord("return", 1, 7)
	require.NoError(t, cache.CreateCloudSession(ctx, record))
	digest := [32]byte{3}
	require.NoError(t, cache.OfferCloudExecution(ctx, record.SessionID, "command", "operation", digest))
	claim := service.CloudExecutionClaim{SessionID: record.SessionID, OwnerToken: record.OwnerToken, CommandID: "command", OperationID: "operation", TransferDigest: digest}
	_, err := cache.ClaimCloudExecution(ctx, claim)
	require.NoError(t, err)
	record.Status = "failed"
	record.Sequence++
	require.NoError(t, cache.SaveCloudSession(ctx, record, nil, nil))
	returned := execution.OperationReturn{CommandID: "command", TransferDigest: digest, Result: model.OperationResult{OperationID: "operation", State: model.OperationFailed}}
	require.NoError(t, cache.SaveCloudExecutionReturn(ctx, claim, returned))
	require.NoError(t, cache.SaveCloudExecutionReturn(ctx, claim, returned), "byte-identical acknowledgement is idempotent")
	other := &gatewayCache{rdb: cache.rdb}
	saved, err := other.LoadCloudExecutionReturn(ctx, record.SessionID, "command", digest)
	require.NoError(t, err)
	require.Equal(t, returned.Digest(), saved.Digest())
	_, err = other.CheckCloudExecution(ctx, claim)
	require.Error(t, err, "return cannot keep owner sends alive")
	require.NoError(t, other.ClaimCloudExecutionReturn(ctx, record.SessionID, "command", digest, returned.Digest()))
	require.Error(t, cache.ClaimCloudExecutionReturn(ctx, record.SessionID, "command", digest, returned.Digest()))
	returned.Result.State = model.OperationCompleted
	require.Error(t, cache.SaveCloudExecutionReturn(ctx, claim, returned))
}

func TestCloudExecutionRedisCanceledReturnRetainsFactsButCannotRecover(t *testing.T) {
	cache, _ := newCloudStoreFixture(t)
	ctx := t.Context()
	record := cloudStoreRecord("cancel-return", 1, 7)
	require.NoError(t, cache.CreateCloudSession(ctx, record))
	digest := [32]byte{4}
	require.NoError(t, cache.OfferCloudExecution(ctx, record.SessionID, "command", "operation", digest))
	claim := service.CloudExecutionClaim{SessionID: record.SessionID, OwnerToken: record.OwnerToken, CommandID: "command", OperationID: "operation", TransferDigest: digest}
	_, err := cache.ClaimCloudExecution(ctx, claim)
	require.NoError(t, err)
	require.NoError(t, cache.DeleteCloudEvents(ctx, "command"))
	returned := execution.OperationReturn{CommandID: "command", TransferDigest: digest, Result: model.OperationResult{OperationID: "operation", State: model.OperationCanceled, Billing: model.BillingReceipt{Disposition: model.BillingPendingUsage}}}
	require.NoError(t, cache.SaveCloudExecutionReturn(ctx, claim, returned))
	saved, err := cache.LoadCloudExecutionReturn(ctx, record.SessionID, "command", digest)
	require.NoError(t, err)
	require.Equal(t, model.BillingPendingUsage, saved.Result.Billing.Disposition)
	require.Error(t, cache.ClaimCloudExecutionReturn(ctx, record.SessionID, "command", digest, returned.Digest()))
}
