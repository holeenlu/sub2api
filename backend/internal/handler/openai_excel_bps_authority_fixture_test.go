//go:build unit

package handler

import (
	"context"
	"fmt"
	"net/http"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func newExcelBPSHandlerAPIKey(id, groupID int64) *service.APIKey {
	key := newNativeHandlerAPIKey()
	key.ID = id
	key.Key = fmt.Sprintf("bps-handler-fixture-key-%d", id)
	key.GroupID = &groupID
	key.Group.ID = groupID
	return key
}

// Reuse the detached, live reads of the Native fixture. BPS adds only its
// explicitly provisioned mode and never treats an Excel bearer as Native.
type excelBPSHandlerAccounts struct{ *nativeHandlerAccounts }

func (r *excelBPSHandlerAccounts) ValidateExcelBPSDispatch(ctx context.Context, account *service.Account, group *int64, upstreamModel string) error {
	fresh, err := r.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	fingerprint, bound := service.ExcelBPSDispatchFingerprint(ctx, account.ID)
	if !bound || fingerprint != "primary" || !reflect.DeepEqual(fresh.Credentials, account.Credentials) || !fresh.IsExcelBPSEnabledForModel(upstreamModel) {
		return service.ErrNativeDispatchScopeChanged
	}
	return r.ValidateVendorAuthorizationMode(ctx, fresh, group, service.VendorOpenAIModeBPS)
}

func (r *excelBPSHandlerAccounts) ValidateVendorSupply(ctx context.Context, account *service.Account, groupID *int64) error {
	if err := r.nativeHandlerAccounts.ValidateVendorSupply(ctx, account, groupID); err != nil {
		return err
	}
	current, err := r.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	if groupID != nil && !slices.Contains(current.GroupIDs, *groupID) {
		return service.ErrGroupNotAllowed
	}
	return nil
}

func (r *excelBPSHandlerAccounts) ValidateVendorAuthorizationMode(ctx context.Context, account *service.Account, groupID *int64, mode string) error {
	if err := r.ValidateVendorSupply(ctx, account, groupID); err != nil {
		return err
	}
	current, err := r.GetByID(ctx, account.ID)
	if err != nil {
		return err
	}
	if current.Platform != service.PlatformOpenAI {
		return service.ErrVendorSupplyUnavailable
	}
	switch mode {
	case service.VendorOpenAIModeBPS:
		if !current.IsExcelBPSEnabled() || !current.IsExcelOAuth() || strings.TrimSpace(current.GetOpenAIAccessToken()) == "" || current.GetChatGPTAccountID() == "" {
			return service.ErrVendorSupplyUnavailable
		}
		switch models := current.Extra["openai_excel_bps_models"].(type) {
		case []string:
			for _, model := range models {
				if strings.TrimSpace(model) != "" {
					return nil
				}
			}
		case []any:
			for _, value := range models {
				if model, ok := value.(string); ok && strings.TrimSpace(model) != "" {
					return nil
				}
			}
		}
	case service.VendorOpenAIModeNative:
		if current.IsExcelOAuth() {
			return service.ErrVendorSupplyUnavailable
		}
		if current.Type == service.AccountTypeAPIKey && strings.TrimSpace(current.GetOpenAIProtocolAPIKey()) != "" {
			return nil
		}
		if current.Type == service.AccountTypeOAuth && strings.TrimSpace(current.GetOpenAIAccessToken()) != "" {
			return nil
		}
	}
	return service.ErrVendorSupplyUnavailable
}

func (r *excelBPSHandlerAccounts) ListModelAvailabilityCandidates(_ context.Context, groupID *int64, platforms []string, _ bool) ([]service.Account, error) {
	var accounts []service.Account
	for _, platform := range platforms {
		for _, account := range r.list(platform) {
			if groupID == nil || slices.Contains(account.GroupIDs, *groupID) {
				accounts = append(accounts, account)
			}
		}
	}
	return accounts, nil
}

type excelBPSHandlerKeys struct {
	*nativeHandlerKeys
	additional map[int64]*nativeHandlerKeys
}

func (r *excelBPSHandlerKeys) GetByID(ctx context.Context, id int64) (*service.APIKey, error) {
	if row := r.additional[id]; row != nil {
		return row.GetByID(ctx, id)
	}
	return r.nativeHandlerKeys.GetByID(ctx, id)
}

func (r *excelBPSHandlerKeys) GetByKeyForAuth(ctx context.Context, credential string) (*service.APIKey, error) {
	rows := []*nativeHandlerKeys{r.nativeHandlerKeys}
	for _, row := range r.additional {
		rows = append(rows, row)
	}
	for _, row := range rows {
		row.store.mu.RLock()
		var id int64
		if row.store.key != nil {
			id = row.store.key.ID
		}
		row.store.mu.RUnlock()
		if id == 0 {
			continue
		}
		key, err := row.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if key.Key == credential {
			return key, nil
		}
	}
	return nil, service.ErrAPIKeyNotFound
}

func (r *excelBPSHandlerKeys) GetByKey(ctx context.Context, credential string) (*service.APIKey, error) {
	return r.GetByKeyForAuth(ctx, credential)
}

type excelBPSHandlerAuthority struct {
	accounts *excelBPSHandlerAccounts
	users    *nativeHandlerUsers
	keys     *service.APIKeyService
}

func newExcelBPSHandlerAuthority(t *testing.T, cfg *config.Config, accounts []service.Account, groupID int64, additionalKeys ...*service.APIKey) excelBPSHandlerAuthority {
	t.Helper()
	store := &nativeHandlerStore{key: newExcelBPSHandlerAPIKey(99, groupID)}
	for _, account := range accounts {
		store.accounts = append(store.accounts, cloneNativeHandlerAccount(account))
	}
	keyRepo := &excelBPSHandlerKeys{nativeHandlerKeys: &nativeHandlerKeys{store: store}, additional: make(map[int64]*nativeHandlerKeys)}
	for _, key := range additionalKeys {
		require.NotNil(t, key)
		require.Equal(t, store.key.UserID, key.UserID)
		require.NotEqual(t, store.key.ID, key.ID)
		require.NotContains(t, keyRepo.additional, key.ID)
		keyRepo.additional[key.ID] = &nativeHandlerKeys{store: &nativeHandlerStore{key: key}}
	}
	users := &nativeHandlerUsers{store: store}
	return excelBPSHandlerAuthority{
		accounts: &excelBPSHandlerAccounts{nativeHandlerAccounts: &nativeHandlerAccounts{store: store}},
		users:    users, keys: service.NewAPIKeyService(keyRepo, users, nil, nil, nil, nil, cfg),
	}
}

// BPS uses the shared sealed HTTP port with its explicit profile. The scripted
// responder below is a test transport, not qualifiedAccountHTTPUpstream.Do.
type excelBPSHandlerUpstream struct {
	service.HTTPUpstream
	t *testing.T
}

var _ service.NativeHTTPUpstream = (*excelBPSHandlerUpstream)(nil)

func (u *excelBPSHandlerUpstream) rejectNative() error {
	u.t.Helper()
	u.t.Error("BPS handler fixture attempted Native dispatch")
	return fmt.Errorf("unexpected Native dispatch in BPS handler fixture")
}

func (u *excelBPSHandlerUpstream) PrepareNativeRequest(request *http.Request, _ int64) (*http.Request, error) {
	return request, nil
}

func (u *excelBPSHandlerUpstream) ValidateNativeRedirect(*http.Request, []*http.Request, string) error {
	return u.rejectNative()
}

func (u *excelBPSHandlerUpstream) RoundTripOnce(request *http.Request, options native.TransportOptions) (*http.Response, error) {
	if options.Profile != native.ProfileExcelBPS {
		return nil, u.rejectNative()
	}
	return u.HTTPUpstream.Do(request, options.ProxyURL, options.AccountID, options.AccountConcurrency)
}
