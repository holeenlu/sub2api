package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountModelReferencesCASPreservesSupplierCredentials(t *testing.T) {
	for _, changed := range []bool{true, false} {
		name := "saved"
		if !changed {
			name = "concurrent update or archived"
		}
		t.Run(name, func(t *testing.T) {
			repo, mock := supplyRepoMock(t)
			vendor := int64(7)
			account := &service.Account{ID: 11, VendorID: &vendor, Credentials: map[string]any{"access_token": "private-token", "model_allowlist": []string{"old"}, "model_mapping": map[string]any{}}}
			affected := int64(0)
			if changed {
				affected = 1
			}
			mock.ExpectExec(`WITH updated AS .*UPDATE accounts SET credentials=jsonb_set.*vendor_archived_at.*scheduler_outbox`).
				WithArgs(int64(11), `["new"]`, `{}`, `["old"]`, `{}`, service.SchedulerOutboxEventAccountChanged).WillReturnResult(sqlmock.NewResult(0, affected))
			err := repo.UpdateModelReferences(authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: authz.SuperAdmin}), account, []string{"new"}, map[string]any{})
			if changed {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, service.ErrModelPolicyConflict)
			}
			require.Equal(t, "private-token", account.Credentials["access_token"])
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
