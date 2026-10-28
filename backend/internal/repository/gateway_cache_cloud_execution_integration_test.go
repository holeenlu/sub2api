//go:build integration

package repository

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestCloudExecutionRealRedisSingleClaimAndRetiredReturn(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		name := "retired"
		if canceled {
			name = "canceled"
		}
		t.Run(name, func(t *testing.T) {
			a, b := newCloudRealRedisStores(t)
			ctx := t.Context()
			record := cloudStoreRecord(name, 1, 7)
			require.NoError(t, a.CreateCloudSession(ctx, record))
			digest := [32]byte{7}
			require.NoError(t, a.OfferCloudExecution(ctx, name, "command", "operation", digest))
			claim := service.CloudExecutionClaim{SessionID: name, OwnerToken: record.OwnerToken, CommandID: "command", OperationID: "operation", TransferDigest: digest}
			var accepted atomic.Int32
			var group sync.WaitGroup
			for i := range 12 {
				group.Go(func() {
					store := a
					if i%2 == 1 {
						store = b
					}
					if _, err := store.ClaimCloudExecution(ctx, claim); err == nil {
						accepted.Add(1)
					}
				})
			}
			group.Wait()
			require.EqualValues(t, 1, accepted.Load(), "only one process may resume the transferred operation")
			_, err := b.CheckCloudExecution(ctx, claim)
			require.NoError(t, err)
			if canceled {
				require.NoError(t, b.DeleteCloudEvents(ctx, "command"))
			}
			record.Status, record.Sequence = "failed", record.Sequence+1
			require.NoError(t, a.SaveCloudSession(ctx, record, nil, nil))
			returned := execution.OperationReturn{CommandID: "command", TransferDigest: digest,
				Result: model.OperationResult{OperationID: "operation", State: model.OperationFailed,
					Billing: model.BillingReceipt{Disposition: model.BillingPendingUsage}}}
			require.NoError(t, a.SaveCloudExecutionReturn(ctx, claim, returned))
			saved, err := b.LoadCloudExecutionReturn(ctx, name, "command", digest)
			require.NoError(t, err)
			require.NotNil(t, saved)
			require.Equal(t, returned.Digest(), saved.Digest())
			_, err = b.CheckCloudExecution(ctx, claim)
			require.Error(t, err, "persisted evidence cannot restore send permission")
			err = b.ClaimCloudExecutionReturn(ctx, name, "command", digest, returned.Digest())
			if canceled {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Error(t, a.ClaimCloudExecutionReturn(ctx, name, "command", digest, returned.Digest()))
		})
	}
}
