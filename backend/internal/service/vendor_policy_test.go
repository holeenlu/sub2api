package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

type vendorPolicySettings struct {
	SettingRepository
	values map[string]string
}

func (r vendorPolicySettings) GetValue(_ context.Context, key string) (string, error) {
	return r.values[key], nil
}
func (r vendorPolicySettings) GetMultiple(_ context.Context, keys []string) (map[string]string, error) {
	values := map[string]string{}
	for _, key := range keys {
		values[key] = r.values[key]
	}
	return values, nil
}

func TestVendorSettingsPermissionsAndIndependentOrigin(t *testing.T) {
	stored := map[string]string{"vendor_console_enabled": "true", "vendor_portal_origin": "https://supplier.example.invalid", SettingKeyFrontendURL: "https://app.example.invalid"}
	s := &SettingService{settingRepo: vendorPolicySettings{values: stored}}
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 2, Role: authz.Admin})
	root := authz.WithSubject(context.Background(), authz.Subject{UserID: 1, Role: authz.SuperAdmin})
	unchanged := map[string]string{"vendor_console_enabled": "true", "vendor_portal_origin": stored["vendor_portal_origin"]}
	require.NoError(t, s.validateVendorSettingsWrite(admin, unchanged))
	require.Empty(t, unchanged)
	require.ErrorIs(t, s.validateVendorSettingsWrite(admin, map[string]string{"vendor_console_enabled": "false"}), authz.ErrPermissionDenied)
	for _, raw := range []string{"http://supplier.example.invalid", "https://name:secret@supplier.example.invalid", "https://supplier.example.invalid/path", "https://supplier.example.invalid?x=1", "https://supplier.example.invalid#fragment", "https://APP.example.invalid:443/"} {
		require.ErrorIs(t, s.validateVendorSettingsWrite(root, map[string]string{"vendor_portal_origin": raw}), ErrVendorInput, raw)
	}
	require.ErrorIs(t, s.validateVendorSettingsWrite(root, map[string]string{"vendor_portal_origin": ""}), ErrVendorInput)
	require.ErrorIs(t, s.validateVendorSettingsWrite(root, map[string]string{SettingKeyFrontendURL: stored["vendor_portal_origin"]}), ErrVendorInput)
	require.NoError(t, s.validateVendorSettingsWrite(root, map[string]string{"vendor_portal_origin": "https://suppliers.example.invalid"}))
	// Partial unrelated saves preserve both supplier settings.
	require.NoError(t, s.validateVendorSettingsWrite(admin, map[string]string{"site_name": "Example"}))
}

func TestVendorPolicyPostgresConcurrentUpdateAndLiveRevocation(t *testing.T) {
	s, supplier := vendorAccountPostgres(t)
	_, err := s.db.Exec("ALTER TABLE vendor_groups ADD UNIQUE(vendor_id,group_id)")
	require.NoError(t, err)
	root := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1, ManagementRequest: true})
	admin := authz.WithSubject(context.Background(), authz.Subject{UserID: 4, Role: authz.Admin, SessionGeneration: 1, ManagementRequest: true})
	input, err := s.loadVendor(root, 1)
	require.NoError(t, err)
	require.ErrorIs(t, func() error { _, e := s.SaveVendor(admin, 1, *input); return e }(), ErrVendorDenied)
	input.Status = StatusDisabled
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.SaveVendor(root, 1, *input); results <- e }()
	}
	wg.Wait()
	close(results)
	var successes, conflicts int
	for e := range results {
		if e == nil {
			successes++
		} else if errors.Is(e, ErrVendorConflict) {
			conflicts++
		} else {
			require.NoError(t, e)
		}
	}
	require.Equal(t, 1, successes)
	require.Equal(t, 1, conflicts)
	_, err = s.CurrentVendor(supplier)
	require.ErrorIs(t, err, ErrVendorDenied)
	updated, err := s.RequireVendor(root, 1)
	require.NoError(t, err)
	require.Equal(t, int64(2), updated.PolicyVersion)
	require.Equal(t, input.ContractVersion, updated.ContractVersion, "nonfinancial changes must not change the contract")
	updated.Status = StatusActive
	updated.DefaultCoefficient = "1.9"
	updated, err = s.SaveVendor(root, 1, *updated)
	require.NoError(t, err)
	require.Equal(t, input.ContractVersion+1, updated.ContractVersion)
	_, err = s.CurrentVendor(supplier)
	require.ErrorIs(t, err, ErrVendorConflict, "stale supplier request must reload policy")
	_, err = s.db.Exec("UPDATE users SET session_generation=2 WHERE id=9")
	require.NoError(t, err)
	_, err = s.SaveVendor(root, 1, *updated)
	require.ErrorIs(t, err, ErrVendorDenied, "revoked root session cannot mutate suppliers")
}

func TestVendorPolicyPostgresEnrollmentAndDisable(t *testing.T) {
	s, root := vendorHierarchyPostgres(t, false)
	policy := NewVendorPolicy()
	policy.Name = "New supplier"
	policy.Platforms = []string{"openai"}
	policy.GroupIDs = []int64{1}
	created, err := s.CreateVendor(root, VendorCreateInput{Email: "enrollment@example.invalid", Password: "Password123!", Vendor: policy})
	require.NoError(t, err)
	_, err = s.SaveVendor(root, 0, *created)
	require.ErrorIs(t, err, ErrVendorInput, "existing users cannot be converted by the old endpoint")
	user := hierarchyActor(created)
	_, err = s.CurrentVendor(user)
	require.NoError(t, err)
	s.settings = &SettingService{settingRepo: vendorPolicySettings{values: map[string]string{"vendor_console_enabled": "false"}}}
	_, err = s.CurrentVendor(user)
	require.ErrorIs(t, err, ErrVendorDenied)
	_, err = s.RequireVendor(root, created.ID)
	require.NoError(t, err, "administrative history survives portal disable")
}

func TestVendorPolicyPostgresWaitDoesNotLockOwner(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	_, err := s.db.Exec("ALTER TABLE vendor_groups ADD UNIQUE(vendor_id,group_id)")
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	root := authz.WithSubject(ctx, authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1, ManagementRequest: true})
	input, err := s.loadVendor(root, 1)
	require.NoError(t, err)
	blocker, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer blocker.Rollback()
	require.NoError(t, AcquireVendorProxyLock(ctx, blocker, input.ID))
	var blockerPID int
	require.NoError(t, blocker.QueryRowContext(ctx, "SELECT pg_backend_pid()").Scan(&blockerPID))
	result := make(chan error, 1)
	go func() { _, e := s.SaveVendor(root, input.ID, *input); result <- e }()
	require.Eventually(t, func() bool {
		var waiting bool
		e := s.db.QueryRowContext(ctx, `SELECT EXISTS (
			SELECT 1 FROM pg_locks held JOIN pg_locks waiting
			ON waiting.locktype=held.locktype AND waiting.database=held.database
			AND waiting.classid=held.classid AND waiting.objid=held.objid
			AND waiting.objsubid=held.objsubid
			WHERE held.pid=$1 AND held.locktype='advisory' AND NOT waiting.granted)`, blockerPID).Scan(&waiting)
		return e == nil && waiting
	}, 3*time.Second, 10*time.Millisecond)
	// A policy update queued behind account/proxy work must not already hold
	// its owner lock: another vendor-first mutation may need that same user.
	probe, err := s.db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer probe.Rollback()
	var owner int64
	require.NoError(t, probe.QueryRowContext(ctx, "SELECT id FROM users WHERE id=$1 FOR UPDATE NOWAIT", input.UserID).Scan(&owner))
	require.NoError(t, probe.Rollback())
	require.NoError(t, blocker.Rollback())
	require.NoError(t, <-result)
}

func TestVendorPolicyPostgresOwnerAndGroupsCannotBeReassigned(t *testing.T) {
	s, _ := vendorAccountPostgres(t)
	root := authz.WithSubject(context.Background(), authz.Subject{UserID: 9, Role: authz.SuperAdmin, SessionGeneration: 1})
	v, err := s.loadVendor(root, 1)
	require.NoError(t, err)
	v.UserID = 4
	_, err = s.SaveVendor(root, 1, *v)
	require.ErrorIs(t, err, ErrVendorConflict)
	v.UserID = 3
	v.GroupIDs = []int64{1, 999}
	v.GroupCoefficients = map[string]string{}
	_, err = s.SaveVendor(root, 1, *v)
	require.ErrorIs(t, err, ErrVendorInput)
	fresh, err := s.loadVendor(root, 1)
	require.NoError(t, err)
	require.ElementsMatch(t, []int64{1, 2}, fresh.GroupIDs)
	require.Equal(t, int64(1), fresh.PolicyVersion, "rejected updates must roll back")
}
