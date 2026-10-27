package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
)

func captureScheduledAuthorization(ctx context.Context) *authz.Lease {
	lease := authz.CaptureLease(ctx)
	if lease == nil {
		return &authz.Lease{System: true}
	}
	// A saved schedule survives a routine logout, but not a role/credential
	// generation change or revocation of its required permission.
	lease.SessionID = ""
	return lease
}

func (s *ScheduledTestService) CheckScheduledAuthorization(ctx context.Context, lease *authz.Lease) error {
	if lease == nil {
		return ErrAdminPermissionDenied
	}
	if lease.System {
		return nil
	}
	if s.users == nil {
		return ErrAdminPolicyUnavailable
	}
	owner, err := s.users.GetByID(ctx, lease.UserID)
	if err != nil || owner == nil || !owner.IsActive() {
		return ErrAdminPermissionDenied
	}
	var settings *SettingService
	if s.gateway != nil {
		settings = s.gateway.settingService
	}
	actor, err := settings.ManagementSubject(ctx, owner)
	if err != nil {
		return err
	}
	if !lease.Allows(actor, "accounts.diagnostics") {
		return ErrAdminPermissionDenied
	}
	return nil
}
