//go:build unit

package service

import (
	"context"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type securityNoticeRepo struct {
	UserRepository
	records map[int64][]UserAuthIdentityRecord
}

func (r *securityNoticeRepo) ListUserAuthIdentities(_ context.Context, id int64) ([]UserAuthIdentityRecord, error) {
	return r.records[id], nil
}
func (r *securityNoticeRepo) ListWithFilters(context.Context, pagination.PaginationParams, UserListFilters) ([]User, *pagination.PaginationResult, error) {
	return []User{{ID: 9, Email: "owner@example.com", Role: RoleSuperAdmin}, {ID: 10, Email: "owner2@example.com", Role: RoleSuperAdmin}}, &pagination.PaginationResult{Total: 2}, nil
}
func TestManagementSecurityNoticeUsesVerifiedOriginalAddress(t *testing.T) {
	now := time.Now()
	repo := &securityNoticeRepo{records: map[int64][]UserAuthIdentityRecord{
		2:  {{ProviderType: "email", ProviderSubject: "original@example.com", VerifiedAt: &now}},
		10: {{ProviderType: "email", ProviderSubject: "owner2@example.com", VerifiedAt: &now}},
		9:  {{ProviderType: "email", ProviderSubject: "owner@example.com", VerifiedAt: &now}},
	}}
	queue := &EmailQueueService{taskChan: make(chan EmailTask, 4)}
	svc := &adminServiceImpl{userRepo: repo, emailQueue: queue}
	ctx := authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: RoleAdmin, PolicyVersion: 1, ManagementRequest: true})
	original := svc.verifiedManagementEmail(ctx, 2, "original@example.com")
	require.Equal(t, "original@example.com", original)
	// The address and proof are captured before the transaction replaces it.
	repo.records[2] = []UserAuthIdentityRecord{{ProviderType: "email", ProviderSubject: "replacement@example.com"}}
	require.Empty(t, svc.verifiedManagementEmail(ctx, 2, "replacement@example.com"))
	svc.notifyManagementSecurityChange(ctx, 2, original)
	recipients := []string{}
	for len(queue.taskChan) > 0 {
		task := <-queue.taskChan
		recipients = append(recipients, task.Email)
		require.Equal(t, TaskTypeAdminSecurityChange, task.TaskType)
		require.EqualValues(t, 2, task.SecurityUserID)
		require.EqualValues(t, 1, task.SecurityActorID)
		require.Empty(t, task.ResetURL)
	}
	require.ElementsMatch(t, []string{"original@example.com", "owner@example.com", "owner2@example.com"}, recipients)
}

func TestManagementEffectsWaitForCommitAndNeverRunAfterRollback(t *testing.T) {
	client := newAdminServiceAuthIdentityBindingTestClient(t)
	actorCtx := authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: RoleSuperAdmin, ManagementRequest: true})
	for _, commit := range []bool{false, true} {
		tx, err := client.Tx(actorCtx)
		require.NoError(t, err)
		ctx := dbent.NewTxContext(actorCtx, tx)
		called := false
		require.True(t, DeferManagementCommit(ctx, func(effectCtx context.Context) { called = true; require.Nil(t, dbent.TxFromContext(effectCtx)) }))
		require.False(t, called)
		if commit {
			require.NoError(t, tx.Commit())
		} else {
			require.NoError(t, tx.Rollback())
		}
		require.Equal(t, commit, called)
	}
}
