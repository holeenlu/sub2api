package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/stretchr/testify/require"
)

type supplyValidationRepo struct {
	AccountRepository
	allowed     bool
	err         error
	calls       int
	nativeCalls int
	nativeErr   error
	reads       int
	group       *int64
	account     *Account
}

func (r *supplyValidationRepo) ValidateVendorSupply(_ context.Context, account *Account, group *int64) error {
	if account.VendorID == nil {
		r.nativeCalls++
		return r.nativeErr
	}
	r.calls++
	r.group = group
	if r.err != nil {
		return r.err
	}
	if !r.allowed {
		return ErrVendorSupplyUnavailable
	}
	return nil
}
func (r *supplyValidationRepo) ValidateVendorAuthorizationMode(ctx context.Context, account *Account, group *int64, _ string) error {
	return r.ValidateVendorSupply(ctx, account, group)
}

func (r *supplyValidationRepo) GetByID(context.Context, int64) (*Account, error) {
	r.reads++
	return r.account, nil
}

func TestDispatchRecheckUsesCurrentOwnershipAndPreservesSendGuard(t *testing.T) {
	owner := int64(7)
	cached := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true}
	for _, tc := range []struct {
		name    string
		owner   *int64
		deleted bool
		allowed bool
	}{
		{name: "native", allowed: true},
		{name: "missing_owner_in_cache", owner: &owner},
		{name: "deleted", deleted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fresh := *cached
			fresh.VendorID = tc.owner
			repo := &supplyValidationRepo{account: &fresh}
			if tc.deleted {
				repo.account = nil
			}
			svc := &OpenAIGatewayService{accountRepo: repo, schedulerSnapshot: &SchedulerSnapshotService{}}
			got, err := svc.recheckSelectedOpenAIAccountFromDBBeforeProfit(context.Background(), cached, nil, PlatformOpenAI, "gpt-5.1", false, "")
			require.Equal(t, 1, repo.reads)
			require.Zero(t, repo.nativeCalls, "实时读取已确认普通账号存在，无需重复存在性查询")
			if !tc.allowed {
				require.Nil(t, got)
				if tc.owner != nil {
					require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
					require.Equal(t, 1, repo.calls)
				}
				return
			}
			require.NoError(t, err)
			require.NotNil(t, got)
			repo.nativeErr = ErrAccountNotFound
			transport := &supplyValidationHTTP{}
			svc.httpUpstream = transport
			req, err := http.NewRequest(http.MethodPost, "https://provider.invalid/v1/responses", nil)
			require.NoError(t, err)
			_, err = svc.doOpenAIUpstream(req, "", got)
			require.ErrorIs(t, err, ErrAccountNotFound)
			require.Equal(t, 1, repo.nativeCalls)
			require.Zero(t, transport.calls, "准备之后删除账号仍不得发送")
		})
	}
}

type supplyValidationCache struct {
	SchedulerCache
	accounts []*Account
}

func (c *supplyValidationCache) GetSnapshot(context.Context, SchedulerBucket) ([]*Account, bool, error) {
	return c.accounts, true, nil
}
func (c *supplyValidationCache) GetAccount(context.Context, int64) (*Account, error) {
	return c.accounts[0], nil
}

type supplyValidationHTTP struct{ calls int }

func (h *supplyValidationHTTP) Do(*http.Request, string, int64, int) (*http.Response, error) {
	h.calls++
	return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("existing stream"))}, nil
}
func (h *supplyValidationHTTP) DoWithTLS(r *http.Request, p string, id int64, n int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return h.Do(r, p, id, n)
}

func TestVendorDispatchFailsClosedWithoutRepositoryCapability(t *testing.T) {
	id := int64(7)
	a := &Account{ID: 1, VendorID: &id}
	require.ErrorIs(t, validateVendorDispatch(context.Background(), nil, a, nil), ErrVendorSupplyUnavailable)
	a.VendorID = nil
	require.NoError(t, validateVendorDispatch(context.Background(), nil, a, nil))
}

func TestVendorSnapshotRechecksCachedListAndStickyOnEachRead(t *testing.T) {
	id, group := int64(7), int64(13)
	vendor := &Account{ID: 1, VendorID: &id, Status: StatusActive, Schedulable: true}
	platform := &Account{ID: 2, Status: StatusActive, Schedulable: true}
	repo := &supplyValidationRepo{allowed: true}
	cache := &supplyValidationCache{accounts: []*Account{vendor, platform}}
	s := NewSchedulerSnapshotService(cache, nil, repo, nil, nil)
	accounts, _, err := s.ListSchedulableAccounts(context.Background(), &group, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	require.Equal(t, group, *repo.group)
	require.False(t, vendor.VendorSupplyBlocked, "must not mutate the shared cached pointer")
	repo.allowed = false
	accounts, _, err = s.ListSchedulableAccounts(context.Background(), &group, PlatformOpenAI, false)
	require.NoError(t, err)
	require.Len(t, accounts, 1)
	require.Equal(t, int64(2), accounts[0].ID)
	got, err := s.GetAccount(context.Background(), 1)
	require.Nil(t, got)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	repo.allowed = true
	got, err = s.GetAccount(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, int64(1), got.ID)
}

func TestVendorHTTPDispatchRechecksEveryRetryAndDoesNotInterruptStream(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "HTTP", true: "TLS"}[tls], func(t *testing.T) {
			id, group := int64(7), int64(13)
			a := &Account{ID: 1, VendorID: &id}
			repo := &supplyValidationRepo{allowed: true}
			transport := &supplyValidationHTTP{}
			upstream := qualifiedAccountUpstream(repo, transport, a, &group)
			req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://provider.invalid/v1/messages", nil)
			require.NoError(t, err)
			send := func() (*http.Response, error) {
				if tls {
					return upstream.DoWithTLS(req, "", a.ID, 1, nil)
				}
				return upstream.Do(req, "", a.ID, 1)
			}
			response, err := send()
			require.NoError(t, err)
			require.Equal(t, group, *repo.group)
			repo.allowed = false
			rejected, err := send()
			require.Nil(t, rejected)
			require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
			require.Equal(t, 1, transport.calls)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "existing stream", string(body))
			require.NoError(t, response.Body.Close())
		})
	}
}

func TestVendorHTTPDispatchRejectsProxySwapAndDatabaseOutage(t *testing.T) {
	id, proxy := int64(7), int64(9)
	a := &Account{ID: 1, VendorID: &id, ProxyID: &proxy, Proxy: &Proxy{ID: proxy, VendorID: &id, Protocol: "http", Host: "proxy.invalid", Port: 8080, ConfigVersion: 3}}
	repo := &supplyValidationRepo{allowed: true}
	transport := &supplyValidationHTTP{}
	req, _ := http.NewRequest(http.MethodGet, "https://provider.invalid", nil)
	upstream := qualifiedAccountUpstream(repo, transport, a, nil)
	_, err := upstream.Do(req, "", a.ID, 1)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	_, err = upstream.Do(req, a.Proxy.URL(), 99, 1)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	repo.err = errors.New("database unavailable")
	_, err = upstream.Do(req, a.Proxy.URL(), a.ID, 1)
	require.Error(t, err)
	require.Zero(t, transport.calls)
}

func TestVendorOpenAISharedDispatchChecksGroupAndPolicyBeforePluginOrHTTP(t *testing.T) {
	id, group := int64(7), int64(13)
	a := &Account{ID: 1, VendorID: &id, Platform: PlatformOpenAI}
	repo := &supplyValidationRepo{allowed: false}
	transport := &supplyValidationHTTP{}
	s := &OpenAIGatewayService{accountRepo: repo, httpUpstream: transport}
	req, _ := http.NewRequest(http.MethodPost, "https://provider.invalid/v1/responses", nil)
	_, err := s.doOpenAIUpstream(req, "", a, &group)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	require.Equal(t, group, *repo.group)
	require.Zero(t, transport.calls)
}

func TestVendorDirectForwardRejectsBeforeTokenOrDial(t *testing.T) {
	id := int64(7)
	a := &Account{ID: 1, VendorID: &id, Platform: PlatformAnthropic}
	_, err := (&GatewayService{}).Forward(context.Background(), nil, a, &ParsedRequest{})
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	a.Platform = PlatformOpenAI
	_, err = (&OpenAIGatewayService{}).Forward(context.Background(), nil, a, []byte("{}"))
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
	_, err = (&OpenAIGatewayService{}).forwardOpenAIWSV2(context.Background(), nil, a, nil, "", "", "", OpenAIWSProtocolDecision{}, false, false, "", "", time.Time{}, 0, "", nil)
	require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
}

func TestVendorNativeBackgroundProbeRequiresLiveWorkbenchScope(t *testing.T) {
	owner := int64(7)
	a := &Account{ID: 1, VendorID: &owner, Platform: PlatformAnthropic, Extra: map[string]any{"synthetic_ui_test": true}}
	s := &AccountTestService{accountRepo: &supplyValidationRepo{account: a}}
	valid := authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: 7, PolicyVersion: 1, ActorUserID: 3})
	for _, ctx := range []context.Context{context.Background(), authz.WithVendorScope(context.Background(), authz.VendorScope{VendorID: 8, PolicyVersion: 1, ActorUserID: 3}), authz.WithRevalidation(valid, func(context.Context) error { return authz.ErrAuthorizationExpired })} {
		result, err := s.RunTestBackground(ctx, 1, "")
		require.NoError(t, err)
		require.Equal(t, "failed", result.Status)
		require.Contains(t, result.ErrorMessage, "supplier workbench")
	}
	result, err := s.RunTestBackground(valid, 1, "")
	require.NoError(t, err)
	require.Equal(t, "success", result.Status, "scoped workbench probe may reuse native protocol")
}
