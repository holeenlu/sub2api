package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	provider "github.com/Wei-Shaw/sub2api/internal/providers/typesafe"
)

// Kept independently of retired ForwardSystemOne tests: account probe tests
// still consume these two legacy HTTP/account-policy fixtures.
type systemOneHTTPUpstream struct {
	do func(*http.Request) (*http.Response, error)
}

func (u *systemOneHTTPUpstream) Do(r *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	return u.do(r)
}
func (u *systemOneHTTPUpstream) DoWithTLS(r *http.Request, _ string, _ int64, _ int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.do(r)
}

type systemOnePolicyAccountRepo struct {
	AccountRepository
	account                                       *Account
	rateLimitedCalls, overloadedCalls, errorCalls int
}

func (r *systemOnePolicyAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, nil
}
func (r *systemOnePolicyAccountRepo) SetRateLimited(context.Context, int64, time.Time) error {
	r.rateLimitedCalls++
	return nil
}
func (r *systemOnePolicyAccountRepo) SetOverloaded(context.Context, int64, time.Time) error {
	r.overloadedCalls++
	return nil
}
func (r *systemOnePolicyAccountRepo) SetError(context.Context, int64, string) error {
	r.errorCalls++
	return nil
}

type systemOneExecutionAccounts struct {
	*nativeAuthorityAccounts
	lists                             int
	rateLimited, overloaded, disabled []int64
}

func (r *systemOneExecutionAccounts) ListSchedulableByGroupIDAndPlatform(_ context.Context, group int64, platform string) ([]Account, error) {
	r.lists++
	var out []Account
	for _, a := range r.accounts {
		for _, g := range a.GroupIDs {
			if g == group && a.Platform == platform {
				out = append(out, *a)
				break
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}
func (r *systemOneExecutionAccounts) SetRateLimited(_ context.Context, id int64, _ time.Time) error {
	r.rateLimited = append(r.rateLimited, id)
	return nil
}
func (r *systemOneExecutionAccounts) SetOverloaded(_ context.Context, id int64, _ time.Time) error {
	r.overloaded = append(r.overloaded, id)
	return nil
}
func (r *systemOneExecutionAccounts) SetError(_ context.Context, id int64, _ string) error {
	r.disabled = append(r.disabled, id)
	return nil
}

type systemOneExecutionGroups struct {
	GroupRepository
	group *Group
}

func (r *systemOneExecutionGroups) GetByIDLite(context.Context, int64) (*Group, error) {
	return r.group, nil
}

// Service-only in-memory journal. This exercises the shared intent/finalization
// validators and merges; its simulated receipts prove NO PostgreSQL commit,
// concurrency, crash recovery, or actual monetary debit.
type systemOneExecutionJournal struct {
	*nativeApplicationJournal
	projections   []SystemOneUsagePreparation
	contextErrors []error
}

func (j *systemOneExecutionJournal) RecordSystemOneUsage(ctx context.Context, p *SystemOneUsagePreparation) (SystemOneBillingReceipt, error) {
	var empty SystemOneBillingReceipt
	j.contextErrors = append(j.contextErrors, ctx.Err())
	if err := ValidateSystemOneUsagePreparation(*p); err != nil {
		return empty, err
	}
	stored, err := j.LoadNativeUsageIntent(ctx, p.Intent.Key)
	if err != nil {
		return empty, err
	}
	root := billing.NativeUsageEvidence{Version: 2, Identity: p.Identity, OperationID: p.Operation.OperationID, UserID: p.Operation.UserID, GroupID: p.Operation.GroupID, GroupBound: p.Operation.GroupBound, OriginalWireDigest: p.Operation.OriginalWireDigest}
	var observations []billing.NativeUsageEvidence
	var selected billing.NativeUsageEvidence
	for _, a := range p.Operation.Attempts {
		e, err := NativeUsageEvidenceFromAttempt(root, a)
		if err != nil {
			return empty, err
		}
		for _, scope := range stored.Attempts {
			if scope.Sequence != a.Sequence {
				continue
			}
			ref, err := (&NativeUsageIntent{Key: stored.Key, UserID: stored.UserID, OriginalWireDigest: stored.OriginalWireDigest, Request: stored.Request, Attempt: scope}).NativeIntentReference()
			if err != nil {
				return empty, err
			}
			digest, err := hex.DecodeString(ref.ScopeDigest)
			if err != nil {
				return empty, err
			}
			copy(e.AttemptScopeDigest[:], digest)
		}
		observations = append(observations, e)
		if a.Sequence == p.Intent.AttemptSequence {
			selected = e
		}
	}
	in := NativeIntentFinalizationInput{Intent: p.Intent, SystemOne: p, Reason: billing.NativeBuilderUnavailable, Evidence: NativeUsageEvidenceInput{Protocol: NativeIntentProtocolSystemOne, Intent: &p.Intent, Evidence: selected, Observations: observations, FrozenPricing: p.Scope.Attempt.FrozenPricing, Settlement: p.Settlement, SettlementUsage: p.SettlementUsage}}
	if p.Settlement != nil {
		in.Reason = billing.NativePreparationReady
	}
	result, err := j.FinalizeNativeUsageIntent(ctx, &in)
	if err != nil {
		return empty, err
	}
	j.projections = append(j.projections, *p)
	disposition := model.BillingPendingUsage
	if result.Intent.Disposition == billing.NativeIntentPendingPreparation {
		disposition = model.BillingRecoveryPending
	}
	if result.Canonical != nil {
		disposition = model.BillingSettled
	}
	return SystemOneBillingReceipt{Intent: p.Intent, Billing: model.BillingReceipt{Disposition: disposition, Durable: result.Intent.Durable(), EvidenceID: result.Intent.FactsFingerprint}}, nil
}

const systemOneExecutionBody = `{"model":"jev-latest","state":{"text":"sample"},"questions":{"q":{"type":"choice","instructions":"Pick","criteria":{"a":"A","b":"B"}}}}`
const systemOneExecutionResponse = `{"model":"jev-1.13.0","answers":{"q":{"type":"choice","choice":"a"}},"usage":{"input_tokens":123,"output_tokens":7},"provider_extension":{"kept":true}}`

type systemOneExecutionFixture struct {
	*nativeAuthorityFixture
	svc                          *GatewayService
	ctx                          context.Context
	cancel                       context.CancelFunc
	in                           SystemOneExecutionInput
	ports                        SystemOneIngressPorts
	transport                    *nativeOperationTransport
	repo                         *systemOneExecutionAccounts
	journal                      *systemOneExecutionJournal
	writer                       *httptest.ResponseRecorder
	selected, acquired, released []int64
	delivered                    int
}

func newSystemOneExecutionFixture(t *testing.T) *systemOneExecutionFixture {
	t.Helper()
	a := newNativeAuthorityFixture(t)
	a.key.Group.Platform = PlatformTypeSafe
	a.key.Group.RateMultiplier = 1
	a.key.Group.ModelAllowlist.Models = []string{typesafe.JevLatestModel}
	a.resource.Platform = PlatformTypeSafe
	a.resource.Credentials = map[string]any{"api_key": "ts-secret", "base_url": "http://127.0.0.1"}
	cfg := &config.Config{Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: 4}, Security: config.SecurityConfig{URLAllowlist: config.URLAllowlistConfig{AllowInsecureHTTP: true}}}
	cfg.Default.RateMultiplier = 1
	transport := &nativeOperationTransport{}
	repo := &systemOneExecutionAccounts{nativeAuthorityAccounts: a.accounts}
	journal := &systemOneExecutionJournal{nativeApplicationJournal: &nativeApplicationJournal{t: t}}
	prices := &PricingService{pricingData: map[string]*LiteLLMModelPricing{typesafe.JevLatestModel: {InputCostPerToken: 2e-6, OutputCostPerToken: 4e-6}}}
	svc := &GatewayService{cfg: cfg, accountRepo: repo, groupRepo: &systemOneExecutionGroups{group: a.key.Group}, userRepo: &nativeOperationUsers{user: a.key.User}, userSubRepo: &nativeOperationSubscriptions{}, httpUpstream: transport, usageBillingRepo: journal, billingService: NewBillingService(cfg, prices)}
	svc.resolver = NewModelPricingResolver(nil, svc.billingService)
	svc.rateLimitService = NewRateLimitService(repo, nil, &config.Config{}, nil, nil)
	ctx, cancel := context.WithCancel(context.WithValue(t.Context(), ctxkey.RequestID, "systemone-client-request"))
	t.Cleanup(cancel)
	ctx, err := svc.SystemOneOperationContext(ctx)
	require.NoError(t, err)
	userSlot, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: a.key.UserID, User: true, Limit: 0}, nil)
	require.NoError(t, err)
	t.Cleanup(userSlot.ReleaseFunc)
	f := &systemOneExecutionFixture{nativeAuthorityFixture: a, svc: svc, ctx: ctx, cancel: cancel, transport: transport, repo: repo, journal: journal, writer: httptest.NewRecorder()}
	f.in = SystemOneExecutionInput{OperationID: "systemone-service-operation", OriginalWireDigest: sha256.Sum256([]byte(systemOneExecutionBody)), Body: []byte(systemOneExecutionBody), RequestedModel: typesafe.JevLatestModel, APIKey: a.key, KeyService: &APIKeyService{apiKeyRepo: a.keys}, PricingAt: time.Now(), SecurityClientIP: "192.0.2.8", QuotaPlatform: PlatformTypeSafe, InboundEndpoint: typesafe.SystemOnePath, UpstreamEndpoint: typesafe.SystemOnePath, MaxAccountSwitches: 2, ChannelUsageFields: ChannelUsageFields{OriginalModel: typesafe.JevLatestModel}}
	f.ports = SystemOneIngressPorts{AcquireAccount: func(ctx context.Context, s *AccountSelectionResult) (func(), error) {
		require.True(t, s.Acquired)
		f.acquired = append(f.acquired, s.Account.ID)
		slot, err := routing.AcquireCapacity(ctx, nil, routing.CapacityRequest{ID: s.Account.ID, Limit: 0}, nil)
		if err != nil {
			return s.ReleaseFunc, err
		}
		t.Cleanup(slot.ReleaseFunc)
		return func() { slot.ReleaseFunc(); s.ReleaseFunc(); f.released = append(f.released, s.Account.ID) }, nil
	}, Selected: func(a *Account) { f.selected = append(f.selected, a.ID) }, Deliver: func(_ context.Context, r *provider.Response) (model.DeliveryReceipt, error) {
		f.delivered++
		f.writer.Header().Set("Content-Type", r.ContentType)
		f.writer.WriteHeader(r.StatusCode)
		n, err := f.writer.Write(r.Body)
		return model.DeliveryReceipt{BytesWritten: int64(n), HeadersCommitted: true, BusinessCommitted: n > 0, Closed: true}, err
	}}
	f.reply(http.StatusOK, systemOneExecutionResponse)
	t.Cleanup(func() {
		require.Zero(t, transport.legacy.Load())
		require.Equal(t, f.acquired, f.released, "every acquired account must be released")
	})
	return f
}
func (f *systemOneExecutionFixture) reply(status int, body string) {
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: status, Header: http.Header{"X-Request-Id": {"req-jev"}, "Content-Type": {"application/json; charset=utf-8"}}, Body: io.NopCloser(strings.NewReader(body))}, nil
	}
}
func (f *systemOneExecutionFixture) secondAccount() {
	second := *f.resource
	second.ID++
	second.Priority = f.resource.Priority + 1
	second.Credentials = map[string]any{"api_key": "second-secret", "base_url": f.resource.Credentials["base_url"]}
	f.repo.accounts[second.ID] = &second
}
func (f *systemOneExecutionFixture) execute() (model.OperationResult, error) {
	return f.svc.ExecuteSystemOne(f.ctx, f.in, f.ports)
}
