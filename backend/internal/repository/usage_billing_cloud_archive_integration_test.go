//go:build integration

package repository

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestCloudLateUsageDurableArchivePreservesSettledLedger(t *testing.T) {
	for _, phase := range []string{"pending", "acked", "archived"} {
		t.Run(phase, func(t *testing.T) {
			ctx := t.Context()
			a, b := newCloudRealRedisStores(t)
			repo := &usageBillingRepository{db: integrationDB}
			client := testEntClient(t)
			user := mustCreateUser(t, client, &service.User{Balance: 100})
			key := mustCreateApiKey(t, client, &service.APIKey{UserID: user.ID, Key: uuid.NewString(), Name: "cloud-late"})
			account := mustCreateAccount(t, client, &service.Account{Name: uuid.NewString()})
			r := cloudStoreRecord("late-archive-"+phase, user.ID, account.ID)
			r.RootRequestID, r.APIKeyID = uuid.NewString(), key.ID
			r.ThreadID, r.TurnID = "thread", "turn"
			t.Cleanup(func() {
				_, err := integrationDB.ExecContext(context.Background(), "DELETE FROM openai_cloud_settlement_archive WHERE root_id=$1", r.RootRequestID)
				require.NoError(t, err)
			})
			require.NoError(t, a.CreateCloudSession(ctx, r))
			r.Sequence++
			r.Status, r.Usage = "completed", &service.OpenAICloudUsage{InputTokens: 10, TotalTokens: 10}
			if phase == "archived" {
				r.Status, r.Usage = "unknown", nil
			}
			original := cloudStoreSettlement(r, r.Usage == nil)
			require.NoError(t, a.SaveCloudSession(ctx, r, nil, original))
			before, err := b.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			claimed, err := b.ClaimCloudSettlement(ctx, r.RootRequestID, "worker", time.Minute)
			require.NoError(t, err)
			require.True(t, claimed)
			requestID := "cloud:" + r.RootRequestID
			if phase == "acked" {
				result, err := repo.applyUsageBillingTransaction(ctx, &service.UsageBillingCommand{RequestID: requestID, UserID: user.ID, APIKeyID: key.ID, AccountID: account.ID, BalanceCost: 1.25})
				require.NoError(t, err)
				require.True(t, result.Applied)
				require.NoError(t, b.AckCloudSettlement(ctx, r.RootRequestID, "worker", before.ArchivePayload))
			}
			if phase == "archived" {
				require.NoError(t, repo.StoreCloudSettlementArchive(ctx, r.RootRequestID, "unknown_usage", before.ArchivePayload))
				require.NoError(t, b.ArchiveCloudSettlement(ctx, r.RootRequestID, "worker", before.ArchivePayload))
			}
			next := cloudStoreLateRecord(t, r, 20)
			require.NoError(t, a.PreserveCloudLateUsage(ctx, next, original))
			late, err := b.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			require.True(t, late.ReconciliationOnly)
			require.Equal(t, before.ArchivePayload, late.OriginalPayload)
			claimed, err = b.ClaimCloudSettlement(ctx, r.RootRequestID, "worker", time.Minute)
			require.NoError(t, err)
			require.True(t, claimed)
			require.Error(t, b.AckCloudSettlement(ctx, r.RootRequestID, "worker", before.ArchivePayload))
			for range 2 {
				require.NoError(t, repo.StoreCloudSettlementArchive(ctx, r.RootRequestID, "billing_conflict", late.ArchivePayload))
			}
			var exact int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM openai_cloud_settlement_archive WHERE root_id=$1 AND payload=$2", r.RootRequestID, late.ArchivePayload).Scan(&exact))
			require.Equal(t, 1, exact, "archive retries retain one immutable payload")
			require.NoError(t, b.ArchiveCloudSettlement(ctx, r.RootRequestID, "worker", late.ArchivePayload))
			require.NoError(t, a.PreserveCloudLateUsage(ctx, next, original))
			pending, err := b.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			require.Nil(t, pending)
			// A contradictory later counter is another immutable fact in the
			// same archive, never an adjusted or second money transaction.
			next = cloudStoreLateRecord(t, next, 5)
			require.NoError(t, a.PreserveCloudLateUsage(ctx, next, original))
			changed, err := b.GetCloudSettlement(ctx, r.RootRequestID)
			require.NoError(t, err)
			require.NoError(t, repo.StoreCloudSettlementArchive(ctx, r.RootRequestID, "billing_conflict", changed.ArchivePayload))
			var balance float64
			var ledgerCount int
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT balance FROM users WHERE id=$1", user.ID).Scan(&balance))
			require.NoError(t, integrationDB.QueryRowContext(ctx, "SELECT count(*) FROM usage_billing_dedup WHERE request_id=$1 AND api_key_id=$2", requestID, key.ID).Scan(&ledgerCount))
			wantBalance, wantCount := 100.0, 0
			if phase == "acked" {
				wantBalance, wantCount = 98.75, 1
			}
			require.Equal(t, wantBalance, balance, fmt.Sprintf("late evidence changed balance in %s", phase))
			require.Equal(t, wantCount, ledgerCount)
		})
	}
}
