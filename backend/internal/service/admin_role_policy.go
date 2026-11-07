package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/logger"
)

var (
	ErrAdminPolicyUnavailable = authz.ErrPolicyUnavailable
	ErrAdminPolicyConflict    = infraerrors.Conflict("ADMIN_POLICY_CONFLICT", "Administrator permissions changed; reload before saving")
	ErrAdminPermissionDenied  = authz.ErrPermissionDenied
	ErrLastSuperAdmin         = authz.ErrLastSuperAdmin
)

type AdminRolePolicyUpdater interface {
	UpdateAdminRolePolicy(ctx context.Context, expected int64, policy *authz.Policy, trace *AuditLog) error
}

func (s *SettingService) GetAdminRolePolicy(ctx context.Context) (*authz.Policy, error) {
	if s == nil || s.settingRepo == nil {
		return nil, ErrAdminPolicyUnavailable
	}
	raw, err := s.settingRepo.GetValue(ctx, authz.PolicyKey)
	if err != nil {
		return nil, ErrAdminPolicyUnavailable
	}
	var policy authz.Policy
	if json.Unmarshal([]byte(raw), &policy) != nil || policy.Validate() != nil {
		return nil, ErrAdminPolicyUnavailable
	}
	var ignored []string
	policy.Permissions, ignored = authz.ReadPermissions(policy.Permissions)
	if len(ignored) > 0 {
		logger.LegacyPrintf("service.authz", "administrator policy version=%d ignored_permissions=%q", policy.Version, ignored)
	}
	return &policy, nil
}

// Editing needs an explicit recovery state; ordinary authorization must never
// replace an absent or corrupt policy with default grants.
func (s *SettingService) GetAdminRolePolicyForEdit(ctx context.Context) (*authz.Policy, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.Role != RoleSuperAdmin {
		return nil, ErrAdminPermissionDenied
	}
	if s == nil || s.settingRepo == nil {
		return nil, ErrAdminPolicyUnavailable
	}
	raw, err := s.settingRepo.GetValue(ctx, authz.PolicyKey)
	if err != nil && !errors.Is(err, ErrSettingNotFound) {
		return nil, ErrAdminPolicyUnavailable
	}
	var policy authz.Policy
	if err != nil || json.Unmarshal([]byte(raw), &policy) != nil || policy.Validate() != nil {
		return &authz.Policy{Permissions: []string{}}, nil
	}
	policy.Permissions, _ = authz.ReadPermissions(policy.Permissions)
	return &policy, nil
}

func (s *SettingService) UpdateAdminRolePolicy(ctx context.Context, expected int64, permissions []string, trace *AuditLog) (*authz.Policy, error) {
	actor, ok := authz.FromContext(ctx)
	if !ok || actor.Role != RoleSuperAdmin || trace == nil {
		return nil, ErrAdminPermissionDenied
	}
	if expected < 0 {
		return nil, ErrAdminPolicyConflict
	}
	normalized, err := authz.NormalizePermissions(permissions)
	if err != nil {
		return nil, infraerrors.BadRequest("INVALID_ADMIN_PERMISSIONS", err.Error())
	}
	updater, ok := s.settingRepo.(AdminRolePolicyUpdater)
	if !ok {
		return nil, ErrAdminPolicyUnavailable
	}
	policy := &authz.Policy{Version: expected + 1, Permissions: normalized, UpdatedBy: actor.UserID, UpdatedAt: time.Now().UTC()}
	trace.ActorRole = RoleSuperAdmin
	trace.Visibility = RoleSuperAdmin
	trace.Action = "admin.roles.permissions.update"
	if trace.Extra == nil {
		trace.Extra = map[string]any{}
	}
	trace.Extra["previous_version"] = expected
	trace.Extra["version"] = policy.Version
	trace.Extra["permissions"] = normalized
	if err := updater.UpdateAdminRolePolicy(ctx, expected, policy, trace); err != nil {
		return nil, err
	}
	return policy, nil
}

// ManagementSubject always resolves the shared policy from authoritative
// storage. Root access is independent of the presence of limited admins.
func (s *SettingService) ManagementSubject(ctx context.Context, user *User) (authz.Subject, error) {
	if user == nil || !user.IsActive() {
		return authz.Subject{}, ErrUserNotActive
	}
	subject := authz.Subject{UserID: user.ID, Role: user.Role, SessionGeneration: user.SessionGeneration}
	if user.Role == RoleAdmin {
		policy, err := s.GetAdminRolePolicy(ctx)
		if err != nil {
			return authz.Subject{}, err
		}
		subject.PolicyVersion = policy.Version
		subject.Permissions = policy.Permissions
	}
	return subject, nil
}

func (s *SettingService) ManagementSnapshot(ctx context.Context, user *User) (authz.Snapshot, error) {
	actor, err := s.ManagementSubject(ctx, user)
	if err != nil {
		if !errors.Is(err, ErrAdminPolicyUnavailable) {
			return authz.Snapshot{}, err
		}
		actor = authz.Subject{UserID: user.ID, Role: user.Role}
	}
	snapshot := actor.Snapshot()
	if user.IsStaff() && s != nil {
		snapshot.Features = map[string]bool{}
		if settings, err := s.GetAllSettings(ctx); err == nil {
			snapshot.Features["ops_monitoring_enabled"] = settings.OpsMonitoringEnabled
			snapshot.Features["ops_realtime_monitoring_enabled"] = settings.OpsRealtimeMonitoringEnabled
		}
	}
	return snapshot, nil
}
