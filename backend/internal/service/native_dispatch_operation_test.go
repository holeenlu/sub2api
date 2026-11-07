package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

type nativeOperationUsers struct {
	UserRepository
	user *User
}

func (r *nativeOperationUsers) GetByID(context.Context, int64) (*User, error) { return r.user, nil }

type nativeOperationSubscriptions struct{ UserSubscriptionRepository }

func (*nativeOperationSubscriptions) GetActiveByUserIDAndGroupID(context.Context, int64, int64) (*UserSubscription, error) {
	return nil, nil
}

type nativeOperationTransport struct {
	sends    atomic.Int32
	legacy   atomic.Int32
	inspect  func(*http.Request, native.TransportOptions)
	reply    func(*http.Request, native.TransportOptions) (*http.Response, error)
	redirect func(*http.Request, []*http.Request, string) error
}

func (u *nativeOperationTransport) PrepareNativeRequest(req *http.Request, _ int64) (*http.Request, error) {
	return req, nil
}

func (u *nativeOperationTransport) ValidateNativeRedirect(request *http.Request, via []*http.Request, proxy string) error {
	if u.redirect != nil {
		return u.redirect(request, via, proxy)
	}
	return nil
}
func (u *nativeOperationTransport) RoundTripOnce(req *http.Request, options native.TransportOptions) (*http.Response, error) {
	u.sends.Add(1)
	if u.inspect != nil {
		u.inspect(req, options)
	}
	if u.reply != nil {
		return u.reply(req, options)
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("fixture response"))}, nil
}
func (u *nativeOperationTransport) Do(*http.Request, string, int64, int) (*http.Response, error) {
	u.legacy.Add(1)
	return nil, errors.New("unexpected legacy send")
}
func (u *nativeOperationTransport) DoWithTLS(*http.Request, string, int64, int, *tlsfingerprint.Profile) (*http.Response, error) {
	u.legacy.Add(1)
	return nil, errors.New("unexpected legacy TLS send")
}

type nativeOperationFixture struct {
	*nativeAuthorityFixture
	service               *OpenAIGatewayService
	ctx                   context.Context
	op                    *nativeDispatchOperation
	transport             *nativeOperationTransport
	userSlot, accountSlot *routing.AcquireResult
}

const nativeOperationFixtureBody = `{"model":"mapped-provider-model"}`

func newNativeOperationFixture(t *testing.T, slots bool, limits ...int) *nativeOperationFixture {
	t.Helper()
	return newNativeOperationFixtureWithInput(t, slots, model.ProtocolResponses, []byte(nativeOperationFixtureBody), limits...)
}

func newNativeOperationFixtureWithInput(t *testing.T, slots bool, protocol model.ClientProtocol, originalWire []byte, limits ...int) *nativeOperationFixture {
	t.Helper()
	a := newNativeAuthorityFixture(t)
	transport := &nativeOperationTransport{}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: 1}}
	if len(limits) > 0 {
		cfg.Gateway.OpenAITurnMaxAttempts = limits[0]
	}
	s := &OpenAIGatewayService{cfg: cfg, accountRepo: a.accounts, userRepo: &nativeOperationUsers{user: a.key.User}, userSubRepo: &nativeOperationSubscriptions{}, httpUpstream: transport}
	ctx, err := s.BindNativeHTTPDispatch(EnsureOpenAITurnBudget(t.Context(), cfg), &APIKeyService{apiKeyRepo: a.keys}, a.gate.request)
	require.NoError(t, err)
	ctx, err = s.BindNativeHTTPOperation(ctx, protocol, sha256.Sum256(originalWire))
	require.NoError(t, err)
	f := &nativeOperationFixture{nativeAuthorityFixture: a, service: s, ctx: ctx, op: nativeDispatchFromContext(ctx), transport: transport}
	bindNativeRuntimeFixture(t, f)
	if slots {
		// These are actual routing receipts for the fixture's explicit unlimited
		// policies, not synthetic fingerprints injected into the authority.
		f.userSlot, err = routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: a.key.UserID, User: true, Limit: 0}, nil)
		require.NoError(t, err)
		f.accountSlot, err = routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: a.resource.ID, Limit: 0}, nil)
		require.NoError(t, err)
		t.Cleanup(f.userSlot.ReleaseFunc)
		t.Cleanup(f.accountSlot.ReleaseFunc)
	}
	return f
}

func (f *nativeOperationFixture) request(t *testing.T) *http.Request {
	t.Helper()
	token, _, err := f.service.GetAccessToken(f.ctx, f.resource)
	require.NoError(t, err)
	req, err := http.NewRequestWithContext(WithHTTPUpstreamProfile(f.ctx, HTTPUpstreamProfileOpenAI), http.MethodPost, "https://api.openai.com/v1/responses", strings.NewReader(nativeOperationFixtureBody))
	require.NoError(t, err)
	req.Header.Set("Authorization", "Bearer "+token)
	return req
}

func TestNativeOperationActualTransportUsesPermitAndOriginalBudget(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	f.transport.inspect = func(req *http.Request, options native.TransportOptions) {
		body, err := io.ReadAll(req.Body)
		require.NoError(t, err)
		require.Equal(t, nativeOperationFixtureBody, string(body))
		require.Equal(t, native.ProfileOpenAI, options.Profile)
		require.Equal(t, f.resource.ID, options.AccountID)
	}
	response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	require.NoError(t, err)
	require.Equal(t, "fixture response", string(body))
	require.NoError(t, response.Body.Close())
	require.EqualValues(t, 1, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load())
	require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
	_, err = f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
	require.ErrorIs(t, err, execution.ErrAttemptBudgetExhausted)
	require.EqualValues(t, 1, f.transport.sends.Load())
}

func TestNativeOperationMissingOrReleasedCapacityNeverSends(t *testing.T) {
	for _, name := range []string{"missing", "released", "policy_changed"} {
		t.Run(name, func(t *testing.T) {
			f := newNativeOperationFixture(t, name != "missing")
			req := f.request(t)
			switch name {
			case "released":
				f.accountSlot.ReleaseFunc()
			case "policy_changed":
				f.key.User.Concurrency = 1
			}
			_, err := f.service.doOpenAIUpstream(req, "", f.resource, f.key.GroupID)
			require.Error(t, err)
			require.True(t, IsNativeDispatchDenied(err), "%v", err)
			require.Zero(t, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
		})
	}
}

func TestNativeOperationPreparedPolicyAndPrincipalMustRemainCurrent(t *testing.T) {
	for _, name := range []string{"key_revoked", "target_changed", "proxy_changed"} {
		t.Run(name, func(t *testing.T) {
			f := newNativeOperationFixture(t, true)
			prepared, target, err := f.op.prepare(f.request(t), f.resource, "", false)
			require.NoError(t, err)
			defer prepared.Close()
			switch name {
			case "key_revoked":
				f.key.Status = StatusDisabled
			case "target_changed":
				f.resource.Credentials["base_url"] = "https://changed.invalid"
			case "proxy_changed":
				id := int64(8)
				f.resource.ProxyID = &id
			}
			_, err = f.op.dispatcher.Admit(f.ctx, prepared.Identity(), target)
			require.Error(t, err)
			require.True(t, IsNativeDispatchDenied(err), "%v", err)
			require.Zero(t, f.transport.sends.Load())
			require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
		})
	}
}

func TestNativeOperationBindDoesNotInventMissingLifetime(t *testing.T) {
	f := newNativeOperationFixture(t, false)
	_, err := f.service.BindNativeHTTPDispatch(context.Background(), &APIKeyService{apiKeyRepo: f.keys}, f.gate.request)
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	_, err = f.service.BindNativeHTTPDispatch(f.ctx, &APIKeyService{apiKeyRepo: f.keys}, f.gate.request)
	require.ErrorIs(t, err, execution.ErrDispatchChanged)
	_, err = f.op.authority.leases.RevalidateNativeDispatchLeases(f.ctx, model.PreparedIdentity{UserID: f.key.UserID, ResourceID: f.resource.ID})
	require.ErrorIs(t, err, ErrNativeDispatchLeaseUnproven)
}

func TestNativeOperationRedirectHopsUseSealedBodyAndLiveAdmission(t *testing.T) {
	for _, tc := range []struct {
		name                   string
		status                 int
		location, method, body string
		auth                   bool
	}{
		{"same_origin_307", 307, "/continued", http.MethodPost, nativeOperationFixtureBody, true},
		{"same_origin_303", 303, "/continued", http.MethodGet, "", true},
		{"cross_origin_strips_auth", 307, "https://redirected.invalid/continued", http.MethodPost, nativeOperationFixtureBody, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newNativeOperationFixture(t, true, 3)
			var methods, bodies, auth []string
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				require.NoError(t, err)
				methods = append(methods, request.Method)
				bodies = append(bodies, string(body))
				auth = append(auth, request.Header.Get("Authorization"))
				if len(methods) == 1 {
					return &http.Response{StatusCode: tc.status, Header: http.Header{"Location": []string{tc.location}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("final body"))}, nil
			}
			request := f.request(t)
			request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("untrusted replay")), nil }
			response, err := f.service.doOpenAIUpstream(request, "", f.resource, f.key.GroupID)
			require.NoError(t, err)
			body, err := io.ReadAll(response.Body)
			require.NoError(t, err)
			require.Equal(t, "final body", string(body))
			require.NoError(t, response.Body.Close())
			require.Equal(t, []string{http.MethodPost, tc.method}, methods)
			require.Equal(t, []string{nativeOperationFixtureBody, tc.body}, bodies)
			if tc.auth {
				require.Equal(t, auth[0], auth[1])
			} else {
				require.Empty(t, auth[1])
			}
			require.Equal(t, 2, f.op.budget.attemptBudget.Snapshot().Attempts)
			require.Zero(t, f.transport.legacy.Load())
		})
	}
}

func TestNativeOperationRedirectDoesNotEscapeRevocationBudgetOrPolicy(t *testing.T) {
	for _, name := range []string{"revoked", "budget", "disabled"} {
		t.Run(name, func(t *testing.T) {
			limit := 3
			if name == "budget" {
				limit = 1
			}
			f := newNativeOperationFixture(t, true, limit)
			f.transport.reply = func(request *http.Request, _ native.TransportOptions) (*http.Response, error) {
				if name == "revoked" {
					f.key.Status = StatusDisabled
				}
				return &http.Response{StatusCode: 307, Header: http.Header{"Location": []string{"/next"}}, Body: io.NopCloser(strings.NewReader("redirect"))}, nil
			}
			if name == "disabled" {
				f.transport.redirect = func(*http.Request, []*http.Request, string) error { return http.ErrUseLastResponse }
			}
			response, err := f.service.doOpenAIUpstream(f.request(t), "", f.resource, f.key.GroupID)
			if name == "disabled" {
				require.NoError(t, err)
				require.Equal(t, 307, response.StatusCode)
				_ = response.Body.Close()
			} else {
				require.Error(t, err)
			}
			if name == "revoked" {
				require.True(t, IsNativeDispatchDenied(err))
			}
			if name == "budget" {
				require.ErrorIs(t, err, execution.ErrAttemptBudgetExhausted)
			}
			require.EqualValues(t, 1, f.transport.sends.Load())
			require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
		})
	}
}
