//go:build unit

package repository

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func TestLegacyBatchReceiptsReadOnlyMatchHotArchiveAndOwnership(t *testing.T) {
	ctx := context.Background()
	db := batchMoneyTestDB(t)
	repo := &usageBillingRepository{db: db}
	for _, archive := range []bool{false, true} {
		for _, prefix := range []func(string) string{service.BatchImageHoldRequestID, service.BatchImageCaptureRequestID, service.BatchImageReleaseRequestID} {
			batch := uuid.NewString()
			cmd := &service.BatchImageBalanceHoldCommand{BatchID: batch, RequestID: prefix(batch), UserID: 31, APIKeyID: 41,
				HoldAmount: 0.0761995125, RequestPayloadHash: "original"}
			cmd.Normalize()
			confirmed, err := repo.ConfirmBatchImageBillingReceipt(ctx, cmd)
			require.NoError(t, err)
			require.False(t, confirmed)
			table := "usage_billing_dedup"
			if archive {
				table = "usage_billing_dedup_archive"
			}
			_, err = db.ExecContext(ctx, "INSERT INTO "+table+"(request_id,api_key_id,request_fingerprint,created_at) VALUES($1,$2,$3,NOW())", cmd.RequestID, cmd.APIKeyID, cmd.RequestFingerprint)
			require.NoError(t, err)
			confirmed, err = repo.ConfirmBatchImageBillingReceipt(ctx, cmd)
			require.NoError(t, err)
			require.True(t, confirmed)
			wrong := *cmd
			wrong.UserID++
			confirmed, err = repo.ConfirmBatchImageBillingReceipt(ctx, &wrong)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			require.False(t, confirmed)
			wrong = *cmd
			wrong.RequestPayloadHash = "changed"
			_, err = repo.ConfirmBatchImageBillingReceipt(ctx, &wrong)
			require.ErrorIs(t, err, service.ErrUsageBillingRequestConflict)
			var ownedRows int
			require.NoError(t, db.QueryRowContext(ctx, "SELECT count(*) FROM batch_image_money_holds WHERE batch_id=$1", batch).Scan(&ownedRows))
			require.Zero(t, ownedRows, "receipt confirmation must not install historical owned holds")
		}
	}
}

func TestLegacyBatchReceiptCannotProveUnstoredPrecision(t *testing.T) {
	repo := &usageBillingRepository{}
	_, err := repo.ConfirmBatchImageBillingReceipt(context.Background(), &service.BatchImageBalanceHoldCommand{UserID: 31, APIKeyID: 41, BatchID: "legacy", RequestID: service.BatchImageHoldRequestID("legacy"), HoldAmount: 0.1234567850001})
	require.ErrorIs(t, err, service.ErrBatchImageLegacyMoneyPrecision)
}
