//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type keyDataRepo struct {
	*createLimitAPIKeyRepoStub
	keys        map[string]*APIKey
	saveErr     error
	allowedIDs  []int64
	verifiedIDs []int64
}

func (r *keyDataRepo) GetByKey(_ context.Context, value string) (*APIKey, error) {
	if key := r.keys[value]; key != nil {
		return key, nil
	}
	return nil, ErrAPIKeyNotFound
}
func (r *keyDataRepo) GetByID(_ context.Context, id int64) (*APIKey, error) {
	for _, key := range r.keys {
		if key.ID == id {
			return key, nil
		}
	}
	return nil, ErrAPIKeyNotFound
}
func (r *keyDataRepo) VerifyOwnership(_ context.Context, _ int64, ids []int64) ([]int64, error) {
	r.verifiedIDs = append([]int64(nil), ids...)
	return r.allowedIDs, nil
}
func (r *keyDataRepo) ExistsByKey(_ context.Context, value string) (bool, error) {
	return r.keys[value] != nil, nil
}
func (r *keyDataRepo) Create(_ context.Context, key *APIKey) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	if r.keys[key.Key] != nil {
		return ErrAPIKeyExists
	}
	key.ID = int64(len(r.keys) + 1)
	r.created = append(r.created, key)
	r.keys[key.Key] = key
	return nil
}

type keyDataGroups struct{ *visibilityGroupRepo }

func (r *keyDataGroups) GetByID(_ context.Context, id int64) (*Group, error) {
	for _, group := range r.groups {
		if group.ID == id {
			return &group, nil
		}
	}
	return nil, ErrGroupNotAllowed
}
func newKeyDataService(userID int64) (*APIKeyService, *keyDataRepo) {
	repo, _ := newCreateLimitStubs()
	r := &keyDataRepo{createLimitAPIKeyRepoStub: repo, keys: map[string]*APIKey{}}
	return &APIKeyService{apiKeyRepo: r, userRepo: &userRepoStub{user: &User{ID: userID}}, cfg: &config.Config{},
		groupRepo: &keyDataGroups{&visibilityGroupRepo{}}, userSubRepo: &visibilitySubRepo{}}, r
}
func keyDataFixture() APIKeyData {
	return APIKeyData{Type: "sub2api-keys", Version: 1, Keys: []APIKeyDataRow{{Name: "restored", Key: "sk-import-0123456789", Status: "inactive", Quota: 20, QuotaUsed: 12}}}
}

func TestAPIKeyDataRoundTripPreservesStateAndRemapsGroup(t *testing.T) {
	ctx := context.Background()
	source, repo := newKeyDataService(7)
	expired := time.Now().UTC().Add(-time.Hour).Truncate(time.Second)
	groupID := int64(21)
	original := &APIKey{ID: 9, UserID: 7, Key: "sk-roundtrip-0123456789", Name: "A &amp; B", Status: "inactive", GroupID: &groupID,
		Group:       &Group{ID: groupID, Name: "same name", Platform: PlatformOpenAI},
		IPWhitelist: []string{"10.0.0.0/8"}, IPBlacklist: []string{"10.0.0.1"},
		Quota: 20, QuotaUsed: 12.5, ExpiresAt: &expired, RateLimit5h: 2, RateLimit1d: 4, RateLimit7d: 8,
		Usage5h: 1, Usage1d: 2, Usage7d: 3, Window5hStart: &expired, Window1dStart: &expired, Window7dStart: &expired}
	repo.keys[original.Key] = original
	backup, err := source.ExportData(ctx, 7, []int64{9, 9}, APIKeyListFilters{})
	require.NoError(t, err)
	require.Len(t, backup.Keys, 1)
	raw, err := json.Marshal(backup)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "user_id")
	require.NotContains(t, string(raw), "group_id")
	var parsed APIKeyData
	require.NoError(t, json.Unmarshal(raw, &parsed))
	target, targetRepo := newKeyDataService(42)
	target.groupRepo = &keyDataGroups{&visibilityGroupRepo{groups: []Group{{ID: 81, Name: "same name", Platform: PlatformOpenAI, Status: StatusActive}}}}
	result, err := target.ImportData(ctx, 42, parsed)
	require.NoError(t, err)
	require.Equal(t, 1, result.Created)
	require.Empty(t, result.Errors)
	restored := targetRepo.created[0]
	require.Equal(t, int64(42), restored.UserID)
	require.Equal(t, int64(81), *restored.GroupID)
	original.ID, original.UserID, original.GroupID, original.Group = restored.ID, 42, restored.GroupID, nil
	restored.CompiledIPWhitelist, restored.CompiledIPBlacklist = nil, nil
	require.Equal(t, original, restored)
	again, err := target.ImportData(ctx, 42, parsed)
	require.NoError(t, err)
	require.Equal(t, 1, again.Skipped)
	require.Len(t, targetRepo.created, 1)
}

func TestAPIKeyDataExportOwnershipFiltersAndBound(t *testing.T) {
	svc, repo := newKeyDataService(7)
	repo.keys["foreign"] = &APIKey{ID: 2, UserID: 8}
	_, err := svc.ExportData(context.Background(), 7, []int64{2}, APIKeyListFilters{})
	require.ErrorIs(t, err, ErrAPIKeyNotFound)
	repo.allowListByUserID = true
	repo.listByUserIDKeys = []APIKey{{UserID: 7, Name: "mine", Key: "sk-owned-0123456789"}}
	groupID := int64(4)
	filters := APIKeyListFilters{Search: "mine", Status: "inactive", GroupID: &groupID}
	data, err := svc.ExportData(context.Background(), 7, nil, filters)
	require.NoError(t, err)
	require.Len(t, data.Keys, 1)
	require.Equal(t, []int64{7}, repo.listByUserIDCalls)
	require.Equal(t, filters, repo.listByUserIDFilters[0])
	repo.listByUserIDKeys = make([]APIKey, APIKeyDataLimit+1)
	_, err = svc.ExportData(context.Background(), 7, nil, filters)
	require.Error(t, err)
}

func TestAPIKeyDataImportDuplicatesPermissionsAndSafeErrors(t *testing.T) {
	svc, repo := newKeyDataService(7)
	data := keyDataFixture()
	repo.keys[data.Keys[0].Key] = &APIKey{UserID: 8, Key: data.Keys[0].Key}
	result, err := svc.ImportData(context.Background(), 7, data)
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Empty(t, repo.created)
	repo.keys = map[string]*APIKey{}
	data.Keys[0].Group = &APIKeyDataGroup{Name: "not allowed", Platform: PlatformOpenAI}
	result, err = svc.ImportData(context.Background(), 7, data)
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Empty(t, repo.created)
	svc.groupRepo = &keyDataGroups{&visibilityGroupRepo{groups: []Group{{ID: 1, Name: "not allowed", Platform: PlatformOpenAI}, {ID: 2, Name: "not allowed", Platform: PlatformOpenAI}}}}
	result, err = svc.ImportData(context.Background(), 7, data)
	require.NoError(t, err)
	require.Contains(t, result.Errors[0].Message, "ambiguous")
	data.Keys[0].Group = nil
	repo.saveErr = errors.New("database failure containing sk-import-0123456789")
	result, err = svc.ImportData(context.Background(), 7, data)
	require.NoError(t, err)
	require.Equal(t, "internal error", result.Errors[0].Message)
	require.Empty(t, repo.created)
}

func TestAPIKeyDataImportValidationAndCreateLimit(t *testing.T) {
	svc, repo := newKeyDataService(7)
	for _, mutate := range []func(*APIKeyDataRow){
		func(r *APIKeyDataRow) { r.Quota = -1 }, func(r *APIKeyDataRow) { r.QuotaUsed = math.NaN() },
		func(r *APIKeyDataRow) { r.Status = "unknown" }, func(r *APIKeyDataRow) { r.Key = strings.Repeat("x", 129) },
		func(r *APIKeyDataRow) { r.IPWhitelist = []string{"invalid"} }, func(r *APIKeyDataRow) { r.Name = "" },
	} {
		data := keyDataFixture()
		mutate(&data.Keys[0])
		result, err := svc.ImportData(context.Background(), 7, data)
		require.NoError(t, err)
		require.Equal(t, 1, result.Failed)
		require.Empty(t, repo.created)
	}
	data := keyDataFixture()
	svc.cfg.APIKeyCreate.MaxActivePerUser = 1
	repo.activeCount = 1
	result, err := svc.ImportData(context.Background(), 7, data)
	require.NoError(t, err)
	require.Equal(t, 1, result.Failed)
	require.Empty(t, repo.created)
	data.Version = 2
	_, err = svc.ImportData(context.Background(), 7, data)
	require.Error(t, err)
	data = keyDataFixture()
	data.Keys = make([]APIKeyDataRow, APIKeyDataLimit+1)
	_, err = svc.ImportData(context.Background(), 7, data)
	require.Error(t, err)
}

func TestAPIKeyDataExportUsesLiveManagementScope(t *testing.T) {
	for _, selected := range []bool{true, false} {
		t.Run(map[bool]string{true: "selected", false: "filtered"}[selected], func(t *testing.T) {
			svc, repo := newKeyDataService(7)
			svc.userRepo = &userRepoStub{user: &User{ID: 7, Role: RoleSuperAdmin}}
			peer := APIKey{ID: 2, UserID: 8, Name: "peer", Key: "sk-peer"}
			repo.keys[peer.Key] = &peer
			repo.allowListByUserID = true
			repo.listByUserIDKeys = []APIKey{peer}
			repo.allowedIDs = []int64{2}
			var ids []int64
			if selected {
				ids = []int64{2, 2}
			}
			data, err := svc.ExportData(context.Background(), 7, ids, APIKeyListFilters{})
			require.NoError(t, err)
			require.Len(t, data.Keys, 1)
			require.Equal(t, peer.Key, data.Keys[0].Key)
			require.Equal(t, []int64{2}, repo.verifiedIDs)
			if !selected {
				require.True(t, repo.listByUserIDFilters[0].IncludeSuperAdminKeys)
			}
			// A stale caller role must not override a repository denial, e.g.
			// after either superadmin has been demoted or deleted.
			repo.allowedIDs = nil
			data, err = svc.ExportData(context.Background(), 7, ids, APIKeyListFilters{})
			require.ErrorIs(t, err, ErrAPIKeyNotFound)
			require.Nil(t, data)
		})
	}
	svc, repo := newKeyDataService(7)
	repo.allowListByUserID = true
	_, err := svc.ExportData(context.Background(), 7, nil, APIKeyListFilters{IncludeSuperAdminKeys: true})
	require.NoError(t, err)
	require.False(t, repo.listByUserIDFilters[0].IncludeSuperAdminKeys)
}
