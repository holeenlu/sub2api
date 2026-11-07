package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/access"
	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type nativeAuthorityKeys struct {
	APIKeyRepository
	key   *APIKey
	err   error
	reads int
}

func (r *nativeAuthorityKeys) GetByID(ctx context.Context, id int64) (*APIKey, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	if r.key == nil {
		return nil, nil
	}
	clone := *r.key
	return &clone, nil
}

type nativeAuthorityAccounts struct {
	AccountRepository
	accounts                      map[int64]*Account
	err, supplyErr, modeErr       error
	reads, supplyCalls, modeCalls int
	groups                        []*int64
}

func (r *nativeAuthorityAccounts) GetByID(ctx context.Context, id int64) (*Account, error) {
	r.reads++
	if r.err != nil {
		return nil, r.err
	}
	if a := r.accounts[id]; a != nil {
		clone := *a
		return &clone, nil
	}
	return nil, ErrAccountNotFound
}
func (r *nativeAuthorityAccounts) ValidateVendorSupply(_ context.Context, _ *Account, group *int64) error {
	r.supplyCalls++
	r.groups = append(r.groups, group)
	return r.supplyErr
}
func (r *nativeAuthorityAccounts) ValidateVendorAuthorizationMode(_ context.Context, _ *Account, _ *int64, mode string) error {
	r.modeCalls++
	if mode != VendorOpenAIModeNative {
		return errors.New("unexpected mode")
	}
	return r.modeErr
}

type nativeAuthoritySubscriptions struct {
	sub   *UserSubscription
	err   error
	reads int
}

func (r *nativeAuthoritySubscriptions) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	r.reads++
	return r.sub, r.err
}

type nativeAuthorityLeases struct {
	proof NativeDispatchLeaseProof
	err   error
	calls int
}

func (l *nativeAuthorityLeases) RevalidateNativeDispatchLeases(context.Context, model.PreparedIdentity) (NativeDispatchLeaseProof, error) {
	l.calls++
	return l.proof, l.err
}

type nativeAuthorityTokenFunc func(context.Context, *Account) (string, error)

func (f nativeAuthorityTokenFunc) GetAccessToken(ctx context.Context, a *Account) (string, error) {
	return f(ctx, a)
}

type nativeAuthorityCache struct {
	OpenAITokenCache
	token string
}

func (c *nativeAuthorityCache) GetAccessToken(context.Context, string) (string, error) {
	return c.token, nil
}

type nativeAuthorityFixture struct {
	gate          *NativeDispatchAuthority
	keys          *nativeAuthorityKeys
	accounts      *nativeAuthorityAccounts
	subscriptions *nativeAuthoritySubscriptions
	leases        *nativeAuthorityLeases
	resource      *Account
	key           *APIKey
}

func newNativeAuthorityFixture(t *testing.T) *nativeAuthorityFixture {
	t.Helper()
	group := &Group{ID: 9, Status: StatusActive, Hydrated: true, ModelAllowlist: catalog.ModelAllowlist{Enabled: true, Models: []string{"client-model"}}}
	key := &APIKey{ID: 2, UserID: 3, Key: "synthetic-downstream-key", Status: StatusActive, GroupID: &group.ID, Group: group, Quota: 10, QuotaUsed: 1, User: &User{ID: 3, Role: RoleUser, Status: StatusActive}}
	resource := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Status: StatusActive, Schedulable: true, GroupIDs: []int64{9}, Credentials: map[string]any{"api_key": "synthetic-upstream-key", "model_mapping": map[string]any{"client-model": "mapped-provider-model"}}}
	f := &nativeAuthorityFixture{keys: &nativeAuthorityKeys{key: key}, accounts: &nativeAuthorityAccounts{accounts: map[int64]*Account{7: resource}}, subscriptions: &nativeAuthoritySubscriptions{}, key: key, resource: resource}
	// Synthetic lease fixture only; production routing must supply real owned tokens.
	f.leases = &nativeAuthorityLeases{proof: NativeDispatchLeaseProof{Fingerprint: sha256.Sum256([]byte("synthetic-owned-token")), ValidUntil: time.Now().Add(time.Hour)}}
	request := NativeDispatchRequest{OperationID: "native-operation", UserID: 3, APIKeyID: 2, APIKeyDigest: sha256.Sum256([]byte(key.Key)), GroupBound: true, GroupID: 9, RequestedModel: "client-model", ClientIP: "192.0.2.8", Transport: "http"}
	gate, err := NewNativeDispatchAuthority(&APIKeyService{apiKeyRepo: f.keys}, f.accounts, f.subscriptions, nil, f.leases, request)
	require.NoError(t, err)
	f.gate = gate
	return f
}
func (f *nativeAuthorityFixture) identity(t *testing.T) model.PreparedIdentity {
	t.Helper()
	credential, err := f.gate.ResolveCredential(t.Context(), f.resource.ID)
	require.NoError(t, err)
	token, kind := credential.Bearer()
	require.NotEmpty(t, token)
	require.NotEmpty(t, kind)
	r := f.gate.request
	return model.PreparedIdentity{OperationID: r.OperationID, ResourceID: f.resource.ID, Owner: credential.Owner, UserID: r.UserID, APIKeyID: r.APIKeyID, GroupID: r.GroupID, GroupBound: r.GroupBound, Mode: VendorOpenAIModeNative, Transport: r.Transport, GrantDigest: credential.GrantDigest, TargetDigest: sha256.Sum256([]byte("target")), WireDigest: sha256.Sum256([]byte("wire")), BodyKind: model.BodyReplayable, BodyDigest: sha256.Sum256([]byte("body"))}
}
func (f *nativeAuthorityFixture) dispatcher(t *testing.T) (*execution.Dispatcher, *execution.AttemptBudget) {
	t.Helper()
	budget := execution.NewAttemptBudget(execution.AttemptLimits{Total: 3, PerOwner: 3, Preparation: 2, RetryWait: time.Second})
	d, err := execution.NewDispatcher(t.Context(), f.gate.request.OperationID, budget, execution.NewFirstOutput(time.Now()), f.gate)
	require.NoError(t, err)
	return d, budget
}

func TestNativeDispatchAuthorityRechecksAtAdmitAndConsume(t *testing.T) {
	for _, tc := range []struct {
		name      string
		change    func(*nativeAuthorityFixture)
		cause     error
		principal bool
	}{
		{name: "key disabled", change: func(f *nativeAuthorityFixture) { f.key.Status = StatusDisabled }, principal: true, cause: access.ErrKeyDisabled},
		{name: "key removed", change: func(f *nativeAuthorityFixture) { f.keys.key = nil }, principal: true, cause: ErrAPIKeyNotFound},
		{name: "key owner changed", change: func(f *nativeAuthorityFixture) { f.key.UserID++ }, principal: true, cause: access.ErrKeyActorChanged},
		{name: "loaded user changed", change: func(f *nativeAuthorityFixture) { f.key.User.ID++ }, principal: true, cause: access.ErrKeyUserInactive},
		{name: "user missing", change: func(f *nativeAuthorityFixture) { f.key.User = nil }, principal: true, cause: access.ErrKeyUserInactive},
		{name: "user inactive", change: func(f *nativeAuthorityFixture) { f.key.User.Status = StatusDisabled }, principal: true, cause: access.ErrKeyUserInactive},
		{name: "user deleted", change: func(f *nativeAuthorityFixture) { now := time.Now(); f.key.User.DeletedAt = &now }, principal: true, cause: access.ErrKeyUserInactive},
		{name: "vendor user", change: func(f *nativeAuthorityFixture) { f.key.User.Role = RoleVendorLevel1 }, principal: true, cause: access.ErrKeyVendorDenied},
		{name: "key secret replaced", change: func(f *nativeAuthorityFixture) { f.key.Key = "replacement" }, principal: true, cause: ErrNativeDispatchScopeChanged},
		{name: "key quota", change: func(f *nativeAuthorityFixture) { f.key.QuotaUsed = 10 }, principal: true, cause: access.ErrKeyQuotaExhausted},
		{name: "key expired", change: func(f *nativeAuthorityFixture) { at := time.Now().Add(-time.Minute); f.key.ExpiresAt = &at }, principal: true, cause: access.ErrKeyExpired},
		{name: "key group rebound", change: func(f *nativeAuthorityFixture) { f.key.GroupID = nil }, principal: true, cause: ErrNativeDispatchScopeChanged},
		{name: "group deleted", change: func(f *nativeAuthorityFixture) { f.key.Group = nil }, principal: true, cause: ErrGroupNotAllowed},
		{name: "group disabled", change: func(f *nativeAuthorityFixture) { f.key.Group.Status = StatusDisabled }, principal: true, cause: ErrGroupNotAllowed},
		{name: "exclusive revoked", change: func(f *nativeAuthorityFixture) { f.key.Group.IsExclusive = true }, principal: true, cause: ErrGroupNotAllowed},
		{name: "public restriction", change: func(f *nativeAuthorityFixture) { f.key.User.RestrictPublicGroups = true }, principal: true, cause: ErrGroupNotAllowed},
		{name: "model revoked", change: func(f *nativeAuthorityFixture) { f.key.Group.ModelAllowlist.Models = []string{"mapped-provider-model"} }, principal: true, cause: ErrGroupNotAllowed},
		{name: "IP revoked", change: func(f *nativeAuthorityFixture) { f.key.IPBlacklist = []string{"192.0.2.8"} }, principal: true, cause: ErrNativeDispatchScopeChanged},
		{name: "invalid IP allowlist", change: func(f *nativeAuthorityFixture) { f.key.IPWhitelist = []string{"not-an-address"} }, principal: true, cause: ErrNativeDispatchScopeChanged},
		{name: "resource membership removed", change: func(f *nativeAuthorityFixture) { f.resource.GroupIDs = nil }, cause: ErrAccountNotFound},
		{name: "resource disabled", change: func(f *nativeAuthorityFixture) { f.resource.Status = StatusDisabled }, cause: ErrAccountNotFound},
		{name: "resource removed", change: func(f *nativeAuthorityFixture) { delete(f.accounts.accounts, 7) }, cause: ErrAccountNotFound},
		{name: "resource owner changed", change: func(f *nativeAuthorityFixture) { id := int64(22); f.resource.VendorID = &id }, cause: ErrNativeDispatchScopeChanged},
		{name: "native mode revoked", change: func(f *nativeAuthorityFixture) { f.accounts.modeErr = ErrVendorSupplyUnavailable }, cause: ErrVendorSupplyUnavailable},
		{name: "credential rotated", change: func(f *nativeAuthorityFixture) { f.resource.Credentials["api_key"] = "rotated" }, cause: ErrNativeDispatchGrantUnproven},
		{name: "full credential identity changed", change: func(f *nativeAuthorityFixture) { f.resource.Credentials["organization"] = "replacement" }, cause: ErrNativeDispatchGrantUnproven},
		{name: "lease missing", change: func(f *nativeAuthorityFixture) { f.leases.proof = NativeDispatchLeaseProof{} }, cause: ErrNativeDispatchLeaseUnproven},
		{name: "lease expired", change: func(f *nativeAuthorityFixture) { f.leases.proof.ValidUntil = time.Now().Add(-time.Minute) }, cause: ErrNativeDispatchLeaseUnproven},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			identity := f.identity(t)
			d, budget := f.dispatcher(t)
			permit, err := d.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Mode: identity.Mode, Limit: 2})
			require.NoError(t, err)
			before := f.keys.reads
			tc.change(f)
			err = permit.Consume(t.Context(), identity)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
			require.ErrorIs(t, err, tc.cause)
			require.Equal(t, tc.principal, IsNativeDispatchPrincipalFailure(err), "resource and principal failures must retain distinct provenance")
			require.Greater(t, f.keys.reads, before, "Consume must fresh-load by ID")
			require.Equal(t, 1, budget.Snapshot().Attempts)
			require.ErrorIs(t, permit.Consume(t.Context(), identity), execution.ErrSendPermitConsumed)
		})
	}
}

func TestNativeDispatchAuthorityAdmissionFailurePreservesCauses(t *testing.T) {
	dependency := errors.New("synthetic authoritative store failure")
	for _, stage := range []string{"key", "account", "supply", "mode", "lease"} {
		t.Run(stage, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			identity := f.identity(t)
			d, budget := f.dispatcher(t)
			switch stage {
			case "key":
				f.keys.err = dependency
			case "account":
				f.accounts.err = dependency
			case "supply":
				f.accounts.supplyErr = dependency
			case "mode":
				f.accounts.modeErr = dependency
			case "lease":
				f.leases.err = dependency
			}
			permit, err := d.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
			require.Nil(t, permit)
			require.ErrorIs(t, err, dependency)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
			require.Zero(t, budget.Snapshot().Attempts)
		})
	}
}

func TestNativeDispatchAuthorityProofsAndReadOnlyUsage(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	identity := f.identity(t)
	original, err := f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, original.Authorization)
	require.Equal(t, f.leases.proof.Fingerprint, original.Leases)
	require.Equal(t, f.leases.proof.ValidUntil, original.ValidUntil)
	f.key.QuotaUsed++
	f.key.User.Balance = -100 // billing admission is not invoked or duplicated here.
	f.resource.UpdatedAt = time.Now().Add(time.Hour)
	current, err := f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	require.Equal(t, original, current)
	require.Positive(t, f.accounts.supplyCalls)
	require.Positive(t, f.accounts.modeCalls)
	d, budget := f.dispatcher(t)
	permit, err := d.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.NoError(t, err)
	f.leases.proof.Fingerprint = sha256.Sum256([]byte("different owned lease"))
	require.ErrorIs(t, permit.Consume(t.Context(), identity), execution.ErrDispatchChanged)
	require.Equal(t, 1, budget.Snapshot().Attempts)
}

func TestNativeDispatchAuthoritySubscriptionAndSimpleMode(t *testing.T) {
	for _, simple := range []bool{false, true} {
		t.Run(fmt.Sprint(simple), func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			f.gate.request.SimpleMode = simple
			f.key.Group.IsExclusive = true
			f.key.Group.SubscriptionType = SubscriptionTypeSubscription
			expiry := time.Now().Add(30 * time.Minute)
			f.subscriptions.sub = &UserSubscription{ID: 6, UserID: 3, GroupID: 9, Status: SubscriptionStatusActive, ExpiresAt: expiry}
			if simple {
				f.key.Status = StatusAPIKeyExpired
				past := time.Now().Add(-time.Hour)
				f.key.ExpiresAt = &past
				f.key.QuotaUsed = 100
				f.subscriptions.err = errors.New("must not read")
			}
			identity := f.identity(t)
			proof, err := f.gate.RevalidateDispatch(t.Context(), identity)
			require.NoError(t, err)
			if simple {
				require.Zero(t, f.subscriptions.reads)
				require.Equal(t, f.leases.proof.ValidUntil, proof.ValidUntil)
			} else {
				require.Positive(t, f.subscriptions.reads)
				require.Equal(t, expiry, proof.ValidUntil)
				f.subscriptions.sub.Status = SubscriptionStatusExpired
				_, err = f.gate.RevalidateDispatch(t.Context(), identity)
				require.ErrorIs(t, err, ErrGroupNotAllowed)
			}
			f.key.User.Role = RoleVendorLevel2
			_, err = f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, access.ErrKeyVendorDenied)
		})
	}
}

func TestNativeDispatchAuthorityNilAndZeroGroup(t *testing.T) {
	for _, bound := range []bool{false, true} {
		t.Run(fmt.Sprint(bound), func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			f.gate.request.GroupBound = bound
			f.gate.request.GroupID = 0
			f.key.GroupID = nil
			f.key.Group = nil
			f.resource.GroupIDs = nil
			if bound {
				id := int64(0)
				f.key.GroupID = &id
				f.key.Group = &Group{ID: 0, Status: StatusActive}
				f.resource.GroupIDs = []int64{0}
			}
			identity := f.identity(t)
			_, err := f.gate.RevalidateDispatch(t.Context(), identity)
			if bound {
				require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity, "explicit zero is not an unbound customer scope")
				return
			}
			require.NoError(t, err)
			for _, id := range f.accounts.groups {
				if bound {
					require.NotNil(t, id)
					require.Zero(t, *id)
				} else {
					require.Nil(t, id)
				}
			}
			f.resource.GroupIDs = []int64{100}
			_, err = f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, ErrAccountNotFound)
		})
	}
}

func TestNativeDispatchAuthorityRealTokenProviderAndStaleCache(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	expiry := time.Now().Add(2 * time.Hour)
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials = map[string]any{"access_token": "actual-provider-token", "refresh_token": "synthetic-refresh", "expires_at": expiry.Format(time.RFC3339Nano), "chatgpt_account_id": "synthetic-owner"}
	f.gate.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
	credential, err := f.gate.ResolveCredential(t.Context(), 7)
	require.NoError(t, err)
	token, kind := credential.Bearer()
	require.Equal(t, "actual-provider-token", token)
	require.Equal(t, "oauth", kind)
	require.True(t, expiry.Equal(credential.ValidUntil))
	require.NotContains(t, fmt.Sprintf("%+v", credential), token)
	require.NotContains(t, fmt.Sprintf("%#v", credential), token)
	identity := f.identity(t)
	_, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	f.gate.tokens = NewOpenAITokenProvider(f.accounts, &nativeAuthorityCache{token: "stale-cached-token"}, nil)
	_, err = f.gate.ResolveCredential(t.Context(), 7)
	require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
	// A stale token-cache entry cannot replace the already-prepared wire bearer.
	// Admit/Consume use only current durable credentials; no refresh or cache I/O.
	f.gate.tokens = nativeAuthorityTokenFunc(func(context.Context, *Account) (string, error) {
		t.Fatal("revalidation must not obtain/refresh another token")
		return "", errors.New("unexpected token lookup")
	})
	_, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
}

func TestNativeDispatchAuthorityUnknownGrantsFailClosed(t *testing.T) {
	for _, kind := range []string{"missing token", "unknown type", "agent identity", "Excel-only", "Cloud", "OAuth provider missing", "OAuth expiry unknown", "malformed full identity"} {
		t.Run(kind, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			switch kind {
			case "missing token":
				f.resource.Credentials = nil
			case "unknown type":
				f.resource.Type = "unknown"
			case "agent identity":
				f.resource.Type = AccountTypeOAuth
				f.resource.Credentials["auth_mode"] = OpenAIAuthModeAgentIdentity
			case "Excel-only":
				f.resource.Type = AccountTypeOAuth
				f.resource.Credentials["client_id"] = openai.ExcelClientID
			case "Cloud":
				f.resource.Extra = map[string]any{OpenAICloudEnabledKey: true}
			case "OAuth provider missing":
				f.resource.Type = AccountTypeOAuth
			case "OAuth expiry unknown":
				f.resource.Type = AccountTypeOAuth
				f.resource.Credentials["access_token"] = "synthetic"
				f.gate.tokens = nativeAuthorityTokenFunc(func(context.Context, *Account) (string, error) { return "synthetic", nil })
			case "malformed full identity":
				f.resource.Credentials["invalid"] = func() {}
			}
			_, err := f.gate.ResolveCredential(t.Context(), 7)
			require.Error(t, err)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
		})
	}
}

func TestNativeDispatchAuthorityPreparedPrincipalAndCancellation(t *testing.T) {
	for _, field := range []string{"user", "key", "group", "operation", "mode", "transport", "owner"} {
		t.Run(field, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			identity := f.identity(t)
			switch field {
			case "user":
				identity.UserID++
			case "key":
				identity.APIKeyID++
			case "group":
				identity.GroupBound = false
			case "operation":
				identity.OperationID = "other"
			case "mode":
				identity.Mode = "bps"
			case "transport":
				identity.Transport = "ws"
			case "owner":
				identity.Owner.AccountID++
			}
			_, err := f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
		})
	}
	f := newNativeAuthorityFixture(t)
	identity := f.identity(t)
	cause := errors.New("operation cancelled")
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(cause)
	before := f.keys.reads
	_, err := f.gate.RevalidateDispatch(ctx, identity)
	require.ErrorIs(t, err, cause)
	require.Equal(t, before, f.keys.reads)
	var zero NativeDispatchAuthority
	_, err = zero.RevalidateDispatch(t.Context(), identity)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	_, err = zero.ResolveCredential(t.Context(), 7)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
}

func TestNativeDispatchAuthorityFactoryRequiresActualDependencies(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	var nilLease *nativeAuthorityLeases
	_, err := NewNativeDispatchAuthority(f.gate.keys, f.accounts, f.subscriptions, nil, nilLease, f.gate.request)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	_, err = NewNativeDispatchAuthority(f.gate.keys, f.accounts, nil, nil, f.leases, f.gate.request)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	var missingChecks struct{ AccountRepository }
	_, err = NewNativeDispatchAuthority(f.gate.keys, missingChecks, f.subscriptions, nil, f.leases, f.gate.request)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	for _, change := range []func(*NativeDispatchRequest){func(r *NativeDispatchRequest) { r.APIKeyDigest = [32]byte{} }, func(r *NativeDispatchRequest) { r.ClientIP = "forwarded garbage" }, func(r *NativeDispatchRequest) { r.RequestedModel = "" }, func(r *NativeDispatchRequest) { r.Transport = "unknown" }, func(r *NativeDispatchRequest) { r.GroupBound = false }} {
		request := f.gate.request
		change(&request)
		_, err = NewNativeDispatchAuthority(f.gate.keys, f.accounts, f.subscriptions, nil, f.leases, request)
		require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity)
	}
}

func TestNativeDispatchAuthorityShadowUsesCurrentPhysicalOwner(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	parentID := int64(8)
	parent := &Account{ID: parentID, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Credentials: map[string]any{"access_token": "parent-token", "expires_at": time.Now().Add(2 * time.Hour).Format(time.RFC3339Nano)}}
	f.accounts.accounts[parentID] = parent
	f.resource.ParentAccountID = &parentID
	f.resource.QuotaDimension = "spark"
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials = nil
	f.gate.tokens = NewOpenAITokenProvider(f.accounts, nil, nil)
	identity := f.identity(t)
	require.Equal(t, int64(7), identity.ResourceID)
	require.Equal(t, parentID, identity.Owner.AccountID)
	_, err := f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	parent.Credentials["access_token"] = "replaced-parent-token"
	_, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.ErrorIs(t, err, ErrNativeDispatchGrantUnproven)
	parent.ParentAccountID = &parentID
	_, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.ErrorIs(t, err, ErrNativeDispatchDenied)
}

func TestNativeDispatchAuthorityReadOnlyDeadlineAndProofChanges(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	expiry := time.Now().Add(20 * time.Minute)
	f.key.ExpiresAt = &expiry
	identity := f.identity(t)
	proof, err := f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	require.Equal(t, expiry, proof.ValidUntil)
	d, budget := f.dispatcher(t)
	permit, err := d.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.NoError(t, err)
	f.key.Quota = 20 // still eligible, but permission was issued against another quota policy.
	require.ErrorIs(t, permit.Consume(t.Context(), identity), execution.ErrDispatchChanged)
	require.Equal(t, 1, budget.Snapshot().Attempts)
	f.leases.proof.ValidUntil = time.Time{}
	proof, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	require.Equal(t, expiry, proof.ValidUntil, "real key expiry still bounds unlimited capacity")
	f.leases.proof.Fingerprint = [32]byte{}
	_, err = f.gate.RevalidateDispatch(t.Context(), identity)
	require.ErrorIs(t, err, ErrNativeDispatchLeaseUnproven)
}

func TestNativeDispatchAuthoritySubscriptionLookupFailureAndOwnership(t *testing.T) {
	for _, mode := range []string{"failure", "missing", "different user", "different group", "deleted", "expired"} {
		t.Run(mode, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			identity := f.identity(t)
			f.key.Group.SubscriptionType = SubscriptionTypeSubscription
			f.key.Group.IsExclusive = true
			f.subscriptions.sub = &UserSubscription{ID: 11, UserID: 3, GroupID: 9, Status: SubscriptionStatusActive, ExpiresAt: time.Now().Add(time.Hour)}
			failure := errors.New("subscription authority unavailable")
			switch mode {
			case "failure":
				f.subscriptions.err = failure
			case "missing":
				f.subscriptions.sub = nil
			case "different user":
				f.subscriptions.sub.UserID++
			case "different group":
				f.subscriptions.sub.GroupID++
			case "deleted":
				now := time.Now()
				f.subscriptions.sub.DeletedAt = &now
			case "expired":
				f.subscriptions.sub.ExpiresAt = time.Now().Add(-time.Hour)
			}
			_, err := f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, ErrNativeDispatchDenied)
			if mode == "failure" {
				require.ErrorIs(t, err, failure)
			} else {
				require.ErrorIs(t, err, ErrGroupNotAllowed)
			}
		})
	}
}

// Uses actual routing acquisition/evidence for the unlimited case; only the
// current user/account records are fixtures. This is not a Redis integration test.
type nativeRoutingEvidenceVerifier struct {
	user, account  *routing.AcquireResult
	currentUser    *User
	currentAccount *Account
}

func (v *nativeRoutingEvidenceVerifier) RevalidateNativeDispatchLeases(ctx context.Context, prepared model.PreparedIdentity) (NativeDispatchLeaseProof, error) {
	user, err := v.user.Verify(ctx, routing.CapacityPolicy{ResourceID: prepared.UserID, User: true, Limit: v.currentUser.Concurrency})
	if err != nil {
		return NativeDispatchLeaseProof{}, err
	}
	account, err := v.account.Verify(ctx, routing.CapacityPolicy{ResourceID: prepared.ResourceID, Limit: v.currentAccount.Concurrency})
	if err != nil {
		return NativeDispatchLeaseProof{}, err
	}
	userHash, accountHash := user.Fingerprint(), account.Fingerprint()
	var combined [64]byte
	copy(combined[:32], userHash[:])
	copy(combined[32:], accountHash[:])
	return NativeDispatchLeaseProof{Fingerprint: sha256.Sum256(combined[:]), ValidUntil: nativeDispatchEarlier(user.ValidUntil(), account.ValidUntil())}, nil
}

func TestNativeDispatchAuthorityAllowsProvenUnlimitedWithoutExpiry(t *testing.T) {
	f := newNativeAuthorityFixture(t)
	user, err := routing.AcquireCapacity(t.Context(), nil, routing.CapacityRequest{ID: f.key.User.ID, User: true, Limit: 0}, nil)
	require.NoError(t, err)
	t.Cleanup(user.ReleaseFunc)
	account, err := routing.AcquireCapacity(t.Context(), nil, routing.CapacityRequest{ID: f.resource.ID, Limit: 0}, nil)
	require.NoError(t, err)
	t.Cleanup(account.ReleaseFunc)
	f.gate.leases = &nativeRoutingEvidenceVerifier{user: user, account: account, currentUser: f.key.User, currentAccount: f.resource}
	identity := f.identity(t)
	proof, err := f.gate.RevalidateDispatch(t.Context(), identity)
	require.NoError(t, err)
	require.NotEqual(t, [32]byte{}, proof.Leases)
	require.True(t, proof.ValidUntil.IsZero())
	dispatcher, budget := f.dispatcher(t)
	permit, err := dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.NoError(t, err)
	require.NoError(t, permit.Consume(t.Context(), identity))
	require.Equal(t, 1, budget.Snapshot().Attempts)
	// Current-policy verification still happens on Consume, even with no TTL.
	permit, err = dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.NoError(t, err)
	f.key.User.Concurrency = 1
	require.ErrorIs(t, permit.Consume(t.Context(), identity), routing.ErrCapacityPolicyChanged)
	f.key.User.Concurrency = 0
	// Unlimited capacity remains owned by the real operation lifetime.
	control, cancel := context.WithCancelCause(t.Context())
	scoped, err := execution.NewDispatcher(control, identity.OperationID, execution.NewAttemptBudget(execution.AttemptLimits{Total: 2, PerOwner: 2}), execution.NewFirstOutput(time.Now()), f.gate)
	require.NoError(t, err)
	permit, err = scoped.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.NoError(t, err)
	cause := errors.New("real operation cancelled")
	cancel(cause)
	require.ErrorIs(t, permit.Consume(t.Context(), identity), cause)
	// A configured first-output deadline also remains effective without TTLs.
	output := execution.NewFirstOutput(time.Now().Add(-time.Minute))
	output.Configure(time.Second, true, false)
	timed, err := execution.NewDispatcher(t.Context(), identity.OperationID, execution.NewAttemptBudget(execution.AttemptLimits{Total: 2, PerOwner: 2}), output, f.gate)
	require.NoError(t, err)
	_, err = timed.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
	require.ErrorIs(t, err, execution.ErrDispatchExpired)
}

func TestNativeDispatchAuthorityRejectsExpiredFiniteProofAndMissingFingerprint(t *testing.T) {
	for _, tc := range []struct {
		name  string
		proof NativeDispatchLeaseProof
	}{
		{name: "expired finite lease", proof: NativeDispatchLeaseProof{Fingerprint: sha256.Sum256([]byte("finite-token-fixture")), ValidUntil: time.Now().Add(-time.Minute)}},
		{name: "missing proof"},
		{name: "deadline without fingerprint", proof: NativeDispatchLeaseProof{ValidUntil: time.Now().Add(time.Minute)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			identity := f.identity(t)
			f.leases.proof = tc.proof
			dispatcher, budget := f.dispatcher(t)
			permit, err := dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
			require.Nil(t, permit)
			require.ErrorIs(t, err, ErrNativeDispatchLeaseUnproven)
			require.Zero(t, budget.Snapshot().Attempts)
		})
	}
}

func TestNativeDispatchAuthoritySimpleModeUsesFullPoolWithoutWideningAccess(t *testing.T) {
	for _, group := range []struct {
		name  string
		bound bool
		id    int64
	}{
		{name: "unbound key"},
		{name: "bound key", bound: true, id: 9},
		{name: "explicit zero", bound: true, id: 0},
	} {
		t.Run(group.name, func(t *testing.T) {
			f := newNativeAuthorityFixture(t)
			f.gate.request.SimpleMode = true
			f.gate.request.GroupBound, f.gate.request.GroupID = group.bound, group.id
			if group.bound {
				id := group.id
				f.key.GroupID = &id
				f.key.Group.ID = id
			} else {
				f.key.GroupID = nil
				f.key.Group = nil
			}
			f.resource.GroupIDs = []int64{99}
			identity := f.identity(t)
			_, err := f.gate.RevalidateDispatch(t.Context(), identity)
			if group.bound && group.id == 0 {
				require.ErrorIs(t, err, execution.ErrInvalidPreparedIdentity, "simple mode cannot authorize an invalid customer scope")
				return
			}
			require.NoError(t, err)
			dispatcher, _ := f.dispatcher(t)
			permit, err := dispatcher.Admit(t.Context(), identity, model.AttemptTarget{Owner: identity.Owner, Limit: 2})
			require.NoError(t, err)
			f.resource.GroupIDs = []int64{101}
			require.NoError(t, permit.Consume(t.Context(), identity), "simple-mode membership changes do not revoke full-pool eligibility")
			// The same current resource is outside the standard-mode pool.
			f.gate.request.SimpleMode = false
			_, err = f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, ErrAccountNotFound)
			f.gate.request.SimpleMode = true
			// Full-pool selection never removes live supply/mode qualification.
			f.accounts.modeErr = ErrVendorSupplyUnavailable
			_, err = f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, ErrVendorSupplyUnavailable)
			f.accounts.modeErr = nil
			for _, seen := range f.accounts.groups {
				if group.bound {
					require.NotNil(t, seen)
					require.Equal(t, group.id, *seen)
				} else {
					require.Nil(t, seen)
				}
			}
			f.key.User.Status = StatusDisabled
			_, err = f.gate.RevalidateDispatch(t.Context(), identity)
			require.ErrorIs(t, err, access.ErrKeyUserInactive)
			f.key.User.Status = StatusActive
			if group.bound {
				f.key.User.RestrictPublicGroups = true
				_, err = f.gate.RevalidateDispatch(t.Context(), identity)
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				f.key.User.RestrictPublicGroups = false
				f.key.Group.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"other-model"}}
				_, err = f.gate.RevalidateDispatch(t.Context(), identity)
				require.ErrorIs(t, err, ErrGroupNotAllowed)
				f.key.Group.ModelAllowlist = catalog.ModelAllowlist{}
				f.key.Group.Status = StatusDisabled
				_, err = f.gate.RevalidateDispatch(t.Context(), identity)
				require.ErrorIs(t, err, ErrGroupNotAllowed)
			}
		})
	}
}
