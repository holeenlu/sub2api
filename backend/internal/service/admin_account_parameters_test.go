//go:build unit

package service

import (
	"context"
	"github.com/stretchr/testify/require"
	"maps"
	"testing"
	"time"
)

type accountParametersRepo struct {
	AccountRepository
	rows   map[int64]*Account
	writes int
}

func (r *accountParametersRepo) GetByIDs(_ context.Context, ids []int64) ([]*Account, error) {
	result := []*Account{}
	for _, id := range ids {
		if a := r.rows[id]; a != nil {
			result = append(result, a)
		}
	}
	return result, nil
}
func (r *accountParametersRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	a := r.rows[id]
	if a == nil {
		return nil, ErrAccountNotFound
	}
	copy := *a
	copy.Credentials = maps.Clone(a.Credentials)
	copy.Extra = maps.Clone(a.Extra)
	return &copy, nil
}
func (r *accountParametersRepo) Update(_ context.Context, a *Account) error {
	r.rows[a.ID] = a
	r.writes++
	return nil
}

func TestBulkAccountParametersReuseSingleEditAndPreserveEachTarget(t *testing.T) {
	expiry := time.Now().Add(time.Hour)
	repo := &accountParametersRepo{rows: map[int64]*Account{}}
	for _, id := range []int64{1, 2} {
		repo.rows[id] = &Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeOAuth, Status: StatusActive,
			Concurrency: 10, Priority: 1, AutoPauseOnExpired: true, ExpiresAt: &expiry, CreatedByRole: "super_admin",
			Credentials: map[string]any{"access_token": id, "model_mapping": map[string]any{"a": "a"}},
			Extra:       map[string]any{"own_setting": id, "cache_ttl_override_target": "1h"}}
	}
	svc := &adminServiceImpl{accountRepo: repo}
	note := "shared note"
	priority := 7
	clear := int64(0)
	pause := false
	result, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 2}, UseSingleAccountValidation: true,
		Notes: &note, Priority: &priority, ExpiresAt: &clear, AutoPauseOnExpired: &pause,
		Extra:       map[string]any{"session_id_masking_enabled": true, "cache_ttl_override_target": nil},
		Credentials: map[string]any{"model_mapping": map[string]any{"a": "b"}}})
	require.NoError(t, err)
	require.Equal(t, 2, result.Success)
	require.Zero(t, result.Failed)
	for _, id := range []int64{1, 2} {
		a := repo.rows[id]
		require.Equal(t, id, a.Credentials["access_token"])
		require.Equal(t, id, a.Extra["own_setting"])
		require.Equal(t, "super_admin", a.CreatedByRole)
		require.Nil(t, a.ExpiresAt)
		require.False(t, a.AutoPauseOnExpired)
		require.Equal(t, 7, a.Priority)
		require.Equal(t, note, *a.Notes)
		require.NotContains(t, a.Extra, "cache_ttl_override_target")
	}
}

func TestBulkAccountParametersRejectMissingScopeBeforeWrites(t *testing.T) {
	repo := &accountParametersRepo{rows: map[int64]*Account{1: {ID: 1}}}
	svc := &adminServiceImpl{accountRepo: repo}
	_, err := svc.BulkUpdateAccounts(context.Background(), &BulkUpdateAccountsInput{AccountIDs: []int64{1, 99}, UseSingleAccountValidation: true})
	require.ErrorIs(t, err, ErrAccountNotFound)
	require.Zero(t, repo.writes)
}
