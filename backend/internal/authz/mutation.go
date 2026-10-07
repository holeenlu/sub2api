// Package-level persistence guard shared by repositories and transactional services.
package authz

import (
	"context"
	"database/sql"
	"github.com/Wei-Shaw/sub2api/internal/domain"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/lib/pq"
	"sort"
)

var (
	ErrPolicyUnavailable    = infraerrors.ServiceUnavailable("ADMIN_POLICY_UNAVAILABLE", "Administrator permissions are unavailable")
	ErrPermissionDenied     = infraerrors.Forbidden("ADMIN_PERMISSION_DENIED", "This operation is not permitted")
	ErrAuthorizationChanged = infraerrors.Unauthorized("TOKEN_REVOKED", "Administrator authorization changed")
	ErrLastSuperAdmin       = infraerrors.Forbidden("LAST_SUPER_ADMIN", "Cannot remove the last active super administrator")
)

type ManagementDB interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Protect current target identities inside their existing business transaction.
// No policy locks: a request already authorized may finish after policy changes.
// Only personnel changes serialize the last-root invariant, before row locks.
func LockManagementWrite(ctx context.Context, client ManagementDB, ids []int64, newRole, newStatus string, deleting bool) error {
	actor, present := FromContext(ctx)
	if !present || !actor.ManagementRequest || !IsManagementRole(actor.Role) {
		return nil
	}
	ids = append(append([]int64{}, ids...), actor.TargetUserIDs...)
	identityChange := actor.PersonnelAction != "" || newRole != "" || newStatus != "" || deleting
	if actor.Role == Admin && newRole == SuperAdmin {
		return ErrPermissionDenied
	}
	if actor.PersonnelAction == "create" && actor.Role == Admin && newRole != "" && !actor.Can(PersonnelPermission("create", newRole, "")) {
		return ErrPermissionDenied
	}
	if identityChange {
		if _, err := client.ExecContext(ctx, "SELECT pg_advisory_xact_lock(6820453791204)"); err != nil {
			return err
		}
	}
	if len(ids) == 0 && !identityChange {
		return nil
	}
	all := append([]int64{}, ids...)
	if identityChange {
		all = append(all, actor.UserID)
	}
	sort.Slice(all, func(i, j int) bool { return all[i] < all[j] })
	rows, err := client.QueryContext(ctx, "SELECT id,role,status,session_generation FROM users WHERE id=ANY($1) AND deleted_at IS NULL ORDER BY id FOR UPDATE", pq.Array(all))
	if err != nil {
		return err
	}
	type state struct {
		role, status string
		generation   int64
	}
	states := map[int64]state{}
	for rows.Next() {
		var id int64
		var current state
		if err := rows.Scan(&id, &current.role, &current.status, &current.generation); err != nil {
			_ = rows.Close()
			return err
		}
		states[id] = current
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if identityChange {
		current, exists := states[actor.UserID]
		if !exists || current.role != actor.Role || current.status != domain.StatusActive || current.generation != actor.SessionGeneration {
			return ErrAuthorizationChanged
		}
	}
	if actor.Role == Admin && newRole == SuperAdmin {
		return ErrPermissionDenied
	}
	for _, id := range ids {
		target, exists := states[id]
		if !exists {
			return ErrPermissionDenied
		}
		if actor.Role == Admin && target.role == SuperAdmin {
			return ErrPermissionDenied
		}
		if actor.Role == Admin && actor.PersonnelAction != "" && !actor.Can(PersonnelPermission(actor.PersonnelAction, target.role, newRole)) {
			return ErrPermissionDenied
		}
		removing := deleting || (newRole != "" && newRole != target.role) || (newStatus != "" && newStatus != domain.StatusActive)
		if id == actor.UserID && removing {
			return ErrPermissionDenied
		}
		if target.role == SuperAdmin && target.status == domain.StatusActive && (deleting || (newRole != "" && newRole != SuperAdmin) || newStatus == domain.StatusDisabled) {
			rows, err := client.QueryContext(ctx, "SELECT COUNT(*) FROM users WHERE role=$1 AND status=$2 AND deleted_at IS NULL", SuperAdmin, domain.StatusActive)
			if err != nil {
				return err
			}
			var count int64
			if !rows.Next() {
				_ = rows.Close()
				return ErrPermissionDenied
			}
			err = rows.Scan(&count)
			_ = rows.Close()
			if err != nil {
				return err
			}
			if count <= 1 {
				return ErrLastSuperAdmin
			}
		}
	}
	return nil
}
