package service

import (
	"context"
	"encoding/json"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"strings"
	"time"
)

// Reuse existing transactions for target identity protection and atomic batches.
// This does not lock the administrator policy or recheck its version.
func WithManagementWrite[T any](ctx context.Context, client *dbent.Client, ids []int64, fn func(context.Context) (T, error)) (out T, err error) {
	if !authz.IsManagementRequest(ctx) {
		return fn(ctx)
	}
	actor, _ := authz.FromContext(ctx)
	if len(ids) == 0 && len(actor.TargetUserIDs) == 0 && actor.PersonnelAction == "" {
		return fn(ctx)
	}
	tx := dbent.TxFromContext(ctx)
	owned := tx == nil
	if owned {
		if client == nil {
			return out, ErrAdminPolicyUnavailable
		}
		tx, err = client.Tx(ctx)
		if err != nil {
			return out, err
		}
		defer func() { _ = tx.Rollback() }()
		ctx = dbent.NewTxContext(ctx, tx)
	}
	if err = authz.LockManagementWrite(ctx, tx.Client(), ids, "", "", false); err != nil {
		return out, err
	}
	out, err = fn(ctx)
	if err == nil && owned {
		err = tx.Commit()
	}
	return out, err
}

func withPersonnelAction(ctx context.Context, action string) context.Context {
	actor, ok := authz.FromContext(ctx)
	if !ok {
		return ctx
	}
	actor.PersonnelAction = action
	return authz.WithSubject(ctx, actor)
}

func RunManagementWrite(ctx context.Context, client *dbent.Client, ids []int64, fn func(context.Context) error) error {
	_, err := WithManagementWrite(ctx, client, ids, func(ctx context.Context) (struct{}, error) {
		return struct{}{}, fn(ctx)
	})
	return err
}

// Identity events share the business transaction. The asynchronous HTTP audit
// remains useful for denied requests, but cannot be the sole change receipt.
func RecordManagementUserChange(ctx context.Context, client *dbent.Client, target int64, action, previousRole, nextRole string) error {
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || !authz.IsManagementRole(actor.Role) {
		return nil
	}
	visibility := "staff"
	if actor.Role == authz.SuperAdmin || previousRole == authz.SuperAdmin || nextRole == authz.SuperAdmin {
		visibility = authz.SuperAdmin
	}
	extra, err := json.Marshal(map[string]any{"target_user_id": target, "previous_role": previousRole, "role": nextRole, "policy_version": actor.PolicyVersion})
	if err != nil {
		return err
	}
	method := actor.AuthMethod
	if method == "" {
		method = AuditAuthMethodJWT
	}
	_, err = client.ExecContext(ctx, `INSERT INTO audit_logs(actor_user_id,actor_role,auth_method,action,status_code,extra,visibility) VALUES($1,$2,$3,$4,200,$5::jsonb,$6)`, actor.UserID, actor.Role, method, action, string(extra), visibility)
	return err
}

func (s *adminServiceImpl) verifiedManagementEmail(ctx context.Context, userID int64, email string) string {
	if s.emailQueue == nil || strings.TrimSpace(email) == "" {
		return ""
	}
	identities, err := s.userRepo.ListUserAuthIdentities(ctx, userID)
	if err != nil {
		logger.LegacyPrintf("service.admin", "security notification identity lookup failed user=%d: %v", userID, err)
		return ""
	}
	for _, identity := range identities {
		if identity.ProviderType == "email" && identity.VerifiedAt != nil && strings.EqualFold(strings.TrimSpace(identity.ProviderSubject), strings.TrimSpace(email)) {
			return email
		}
	}
	return ""
}
func (s *adminServiceImpl) notifyManagementSecurityChange(ctx context.Context, userID int64, previousEmail string) {
	actor, ok := authz.FromContext(ctx)
	if !ok || !actor.ManagementRequest || s.emailQueue == nil {
		return
	}
	if DeferManagementCommit(ctx, func(committedCtx context.Context) {
		s.notifyManagementSecurityChange(committedCtx, userID, previousEmail)
	}) {
		return
	}

	noticeCtx, cancel := context.WithTimeout(authz.WithSubject(context.Background(), actor), 5*time.Second)
	defer cancel()
	ctx = noticeCtx
	recipients := map[string]bool{}
	if previousEmail != "" {
		recipients[previousEmail] = true
	}
	for page := 1; ; page++ {
		roots, result, err := s.userRepo.ListWithFilters(ctx, pagination.PaginationParams{Page: page, PageSize: 200}, UserListFilters{Role: RoleSuperAdmin})
		if err != nil {
			logger.LegacyPrintf("service.admin", "security notification root lookup failed: %v", err)
			break
		}
		for _, root := range roots {
			if email := s.verifiedManagementEmail(ctx, root.ID, root.Email); email != "" {
				recipients[email] = true
			}
		}
		if len(roots) == 0 || result == nil || int64(page*200) >= result.Total {
			break
		}
	}
	siteName := "Sub2API"
	if s.settingService != nil {
		siteName = s.settingService.GetSiteName(ctx)
	}
	for email := range recipients {
		if err := s.emailQueue.EnqueueManagementSecurityChange(email, siteName, userID, actor.UserID); err != nil {
			logger.LegacyPrintf("service.admin", "security notification enqueue failed user=%d: %v", userID, err)
		}
	}
}

// Defer management cache/notification effects until the outermost transaction
// succeeds. A failed write must not publish a partial snapshot or send a notice.
func DeferManagementCommit(ctx context.Context, effect func(context.Context)) bool {
	if !authz.IsManagementRequest(ctx) {
		return false
	}
	tx := dbent.TxFromContext(ctx)
	if tx == nil {
		return false
	}
	actor, _ := authz.FromContext(ctx)
	tx.OnCommit(func(next dbent.Committer) dbent.Committer {
		return dbent.CommitFunc(func(commitCtx context.Context, committedTx *dbent.Tx) error {
			if err := next.Commit(commitCtx, committedTx); err != nil {
				return err
			}
			effectCtx, cancel := context.WithTimeout(authz.WithSubject(context.Background(), actor), 5*time.Second)
			defer cancel()
			effect(effectCtx)
			return nil
		})
	})
	return true
}
