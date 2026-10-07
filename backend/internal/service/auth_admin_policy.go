package service

import (
	"context"
	"github.com/Wei-Shaw/sub2api/internal/authz"
)

// Authorization is informational on the personal authentication surface.
// A missing policy closes management access without locking out personal use.
func (s *AuthService) GetUserAuthorization(ctx context.Context, user *User) (authz.Snapshot, error) {
	return s.settingService.ManagementSnapshot(ctx, user)
}
