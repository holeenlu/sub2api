package service

import (
	"context"
	"errors"
	"io"
	"maps"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/gateway/session"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/stretchr/testify/require"
)

func TestCPRWSReusePartitionsCredentialOwnerAndTarget(t *testing.T) {
	resource := &Account{ID: 7, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "selected-fixture"}}
	owner := &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{"api_key": "parent-fixture"}}
	scope, err := nativeWSReuseScope(resource, owner)
	require.NoError(t, err)
	request := openAIWSAcquireRequest{Account: resource, TenantScope: "tenant-fixture", WSURL: "wss://upstream.invalid/responses", CredentialScope: scope}
	first := normalizeOpenAIWSAcquireCompatibility(request, http.Header{"Authorization": {"AgentAssertion dial-one"}})
	require.Equal(t, first, normalizeOpenAIWSAcquireCompatibility(request, http.Header{"Authorization": {"AgentAssertion dial-two"}}), "ephemeral per-dial assertions do not redefine a durable credential generation")
	for _, change := range []string{"endpoint", "proxy", "parent", "selected"} {
		t.Run(change, func(t *testing.T) {
			next := request
			selected, parent := *resource, *owner
			selected.Credentials, parent.Credentials = maps.Clone(resource.Credentials), maps.Clone(owner.Credentials)
			switch change {
			case "endpoint":
				next.WSURL = "wss://other.invalid/responses"
			case "proxy":
				next.ProxyURL = "http://proxy.invalid:8080"
			case "parent":
				parent.Credentials["api_key"] = "rotated-parent-fixture"
			case "selected":
				selected.Credentials["api_key"] = "rotated-selected-fixture"
			}
			next.CredentialScope, err = nativeWSReuseScope(&selected, &parent)
			require.NoError(t, err)
			require.NotEqual(t, first, normalizeOpenAIWSAcquireCompatibility(next, nil))
		})
	}
}

func TestCPRNativePricingUsesOwnedBodyRatherThanCallerReplay(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	request := f.request(t)
	replayed := false
	request.GetBody = func() (io.ReadCloser, error) {
		replayed = true
		return nil, errors.New("caller replay must not supply model facts")
	}
	prepared, _, err := f.op.prepare(request, f.resource, "", false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prepared.Close()) })
	require.False(t, replayed)
	require.Equal(t, "mapped-provider-model", f.op.pricingFacts[prepared.Identity()].UpstreamModel)
	replay, err := native.HTTPControlRequest(f.ctx, prepared)
	require.NoError(t, err)
	body, err := io.ReadAll(replay.Body)
	require.NoError(t, err)
	require.NoError(t, replay.Body.Close())
	require.Equal(t, nativeOperationFixtureBody, string(body))
}

func TestCPRNativePricingBoundsActualBodyAndLeavesExclusiveStreamsUnread(t *testing.T) {
	ctx := WithNativeIntentModelFacts(t.Context(), NativeIntentModelFacts{Model: "client", UpstreamModel: "encoded", BillingModel: "priced"})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://unused.invalid", strings.NewReader(strings.Repeat("x", 17)))
	require.NoError(t, err)
	request.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(strings.NewReader("{}")), nil }
	_, err = nativeIntentHTTPModelFacts(request, 16)
	require.ErrorIs(t, err, native.ErrReplayBodyLimit)
	require.Equal(t, http.NoBody, request.Body)

	request, err = http.NewRequestWithContext(ctx, http.MethodPost, "https://unused.invalid", io.NopCloser(strings.NewReader("exclusive")))
	require.NoError(t, err)
	require.Nil(t, request.GetBody)
	facts, err := nativeIntentHTTPModelFacts(request, 16)
	require.NoError(t, err)
	require.Equal(t, "encoded", facts.UpstreamModel)
	body, err := io.ReadAll(request.Body)
	require.NoError(t, err)
	require.NoError(t, request.Body.Close())
	require.Equal(t, "exclusive", string(body))
}

func TestCPRResponsePlanFreezesOnlyAdmittedCandidate(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	account := f.resource
	account.Credentials["model_mapping"] = map[string]any{"client-model": "first-model"}
	first, err := f.service.planResponseExecution(f.ctx, account, "client-model")
	require.NoError(t, err)
	require.Empty(t, f.op.responsePlans, "speculative planning does not claim the current WS turn")
	require.NoError(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", first))
	require.NoError(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", first))

	account.Credentials["model_mapping"] = map[string]any{"client-model": "second-model"}
	second, err := f.service.planResponseExecution(f.ctx, account, "client-model")
	require.NoError(t, err)
	require.NotEqual(t, first.upstreamModel, second.upstreamModel)
	var failure *UpstreamFailoverError
	require.ErrorAs(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", second), &failure)
	require.Equal(t, GatewayFailureReason("response_execution_plan_changed"), failure.Reason)
	require.Equal(t, NextAccountStop, failure.NextAccountAction)
	require.True(t, failure.RequestNotSent)
	require.Equal(t, first, f.op.responsePlans[account.ID])
	require.Zero(t, f.transport.sends.Load())

	other := second
	other.accountID++
	require.NoError(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", other), "another eligible candidate owns its own mapped model")
	next := newNativeOperationFixture(t, true)
	require.NoError(t, next.service.admitResponseExecutionPlan(next.ctx, 0, "", second), "new authenticated turn gets a new plan")
}

func TestCPRResponsePlanRejectsModeAndTransportDrift(t *testing.T) {
	for _, change := range []string{"cloud", "bps", "transport"} {
		t.Run(change, func(t *testing.T) {
			f := newNativeOperationFixture(t, true)
			plan, err := f.service.planResponseExecution(f.ctx, f.resource, "client-model")
			require.NoError(t, err)
			require.NoError(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", plan))
			switch change {
			case "cloud":
				plan.mode = session.ResponseModeCloud
			case "bps":
				plan.mode = session.ResponseModeBPS
			case "transport":
				plan.native.Transport = OpenAIUpstreamTransportResponsesWebsocketV2
			}
			require.ErrorIs(t, f.service.admitResponseExecutionPlan(f.ctx, 0, "", plan), ErrNativeDispatchScopeChanged)
			require.False(t, excelBPSExecutionPinned(f.ctx), "rejected plan must not pin a BPS execution")
		})
	}
}

func TestCPRResponsePlanConcurrentAdmissionKeepsOneValue(t *testing.T) {
	operation := &nativeDispatchOperation{}
	var wg sync.WaitGroup
	errorsSeen := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			plan := responseExecutionPlan{accountID: 7, mode: session.ResponseModeNative, upstreamModel: "first"}
			if i%2 == 1 {
				plan.upstreamModel = "second"
			}
			errorsSeen <- operation.admitResponsePlan(plan)
		}(i)
	}
	wg.Wait()
	close(errorsSeen)
	accepted, rejected := 0, 0
	for err := range errorsSeen {
		if err == nil {
			accepted++
		} else {
			require.ErrorIs(t, err, ErrNativeDispatchScopeChanged)
			rejected++
		}
	}
	require.Equal(t, 16, accepted)
	require.Equal(t, 16, rejected)
}

func TestCPRNativeHTTPGateUsesOneFreshResourceAndNeverCachesRevocation(t *testing.T) {
	f := newNativeOperationFixture(t, true)
	prepared, _, err := f.op.prepare(f.request(t), f.resource, "", false)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, prepared.Close()) })
	for i := 0; i < 2; i++ {
		reads := f.accounts.reads
		_, err = f.op.RevalidateDispatch(f.ctx, prepared.Identity())
		require.NoError(t, err)
		require.Equal(t, 2, f.accounts.reads-reads, "one policy/authority read plus the independent live capacity-policy read; no duplicate wire-policy lookup")
	}
	f.resource.Credentials["base_url"] = "https://different.invalid"
	_, err = f.op.RevalidateDispatch(f.ctx, prepared.Identity())
	require.ErrorIs(t, err, ErrNativeDispatchScopeChanged)
	require.Zero(t, f.transport.sends.Load())
	f.key.Status = StatusDisabled
	_, err = f.op.RevalidateDispatch(f.ctx, prepared.Identity())
	require.True(t, IsNativeDispatchPrincipalFailure(err))
}

func TestCPRNativeWSGateUsesOneFreshResourceAndChecksPolicy(t *testing.T) {
	f, handshake, id := nativeWSHeldFixture(t)
	op := &nativeDispatchOperation{authority: f.gate, request: f.gate.request, budget: NewOpenAITurnBudget(nil), policies: map[model.PreparedIdentity][32]byte{id: nativeDispatchWirePolicy(f.resource)}}
	ctx := context.WithValue(t.Context(), nativeWSRegistrationKey{}, &nativeWSRegistration{operation: op, identity: id, handshake: handshake, live: func() bool { return true }})
	reads := f.accounts.reads
	_, err := op.RevalidateDispatch(ctx, id)
	require.NoError(t, err)
	require.Equal(t, 1, f.accounts.reads-reads)
	delete(op.policies, id)
	_, err = op.RevalidateDispatch(ctx, id)
	require.ErrorIs(t, err, ErrNativeDispatchScopeChanged)
	require.False(t, errors.Is(err, ErrNativeDispatchGrantUnproven), "unregistered policy is denied before a grant can authorize it")
}
