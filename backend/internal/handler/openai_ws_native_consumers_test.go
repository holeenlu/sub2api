package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/billing"
	"github.com/Wei-Shaw/sub2api/internal/catalog"
	"github.com/Wei-Shaw/sub2api/internal/gateway/routing"
	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsHandlerAcquisition struct {
	scope    *routing.CapacityScope
	user     bool
	bound    bool
	previous *routing.AcquireResult
}

// Observe real handler acquisitions and service authority reads. The test never
// binds an operation/scope, publishes a turn, or manufactures a lease receipt.
type wsHandlerScopeProbe struct {
	mu           sync.Mutex
	acquisitions []wsHandlerAcquisition
	reads        map[*routing.CapacityScope]int
}

func (p *wsHandlerScopeProbe) beforeAcquire(ctx context.Context, user bool) {
	scope := routing.CapacityScopeFrom(ctx)
	id := int64(9901)
	if user {
		id = 1701
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.acquisitions = append(p.acquisitions, wsHandlerAcquisition{scope, user, service.NativeHTTPOperationBound(ctx), scope.Lookup(user, id)})
}

func (p *wsHandlerScopeProbe) authorityRead(ctx context.Context) {
	scope := routing.CapacityScopeFrom(ctx)
	if scope == nil || !service.NativeHTTPOperationBound(ctx) {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.reads == nil {
		p.reads = make(map[*routing.CapacityScope]int)
	}
	p.reads[scope]++
}

func (p *wsHandlerScopeProbe) scopes() []*routing.CapacityScope {
	p.mu.Lock()
	defer p.mu.Unlock()
	var scopes []*routing.CapacityScope
	for _, a := range p.acquisitions {
		if a.user {
			scopes = append(scopes, a.scope)
		}
	}
	return scopes
}

func (p *wsHandlerScopeProbe) assertTurns(t *testing.T, turns int) {
	t.Helper()
	p.mu.Lock()
	defer p.mu.Unlock()
	counts := make(map[*routing.CapacityScope][2]int)
	for _, a := range p.acquisitions {
		require.NotNil(t, a.scope, "root must publish the real scope before Acquire")
		require.True(t, a.bound, "handler must bind its execution operation before Acquire")
		require.Nil(t, a.previous, "new turn/lane cannot inherit an old receipt")
		c := counts[a.scope]
		if a.user {
			c[0]++
		} else {
			c[1]++
		}
		counts[a.scope] = c
	}
	require.Len(t, counts, turns, "one fresh capacity scope per accepted turn")
	require.Len(t, p.reads, turns, "service must consume those same scopes")
	for scope, counts := range counts {
		require.Equal(t, [2]int{1, 1}, counts)
		require.Greater(t, p.reads[scope], 0, "service authority consumes the handler-bound context")
		for _, target := range []struct {
			user bool
			id   int64
		}{{true, 1701}, {false, 9901}} {
			receipt := scope.Lookup(target.user, target.id)
			require.NotNil(t, receipt)
			require.NotNil(t, receipt.Lease, "limited admission must use a real token")
			_, err := receipt.Verify(context.Background(), receipt.Policy())
			require.Error(t, err, "handler exit must release the original receipt")
		}
	}
}

func verifyWSHandlerLiveScope(scope *routing.CapacityScope) error {
	if scope == nil {
		return fmt.Errorf("missing handler scope at actual upstream frame")
	}
	for _, target := range []struct {
		user bool
		id   int64
	}{{true, 1701}, {false, 9901}} {
		r := scope.Lookup(target.user, target.id)
		if r == nil || r.Lease == nil || r.Unlimited {
			return fmt.Errorf("missing acquired lease: user=%t id=%d", target.user, target.id)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		evidence, err := r.Verify(ctx, r.Policy())
		cancel()
		if err != nil {
			return err
		}
		if evidence.Fingerprint() == ([32]byte{}) || evidence.ValidUntil().IsZero() {
			return fmt.Errorf("missing real token/deadline evidence")
		}
	}
	return nil
}

func wsHandlerCompleted(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return conn.Write(ctx, coderws.MessageText, []byte(fmt.Sprintf(
		`{"type":"response.completed","response":{"id":"resp_usage_e2e_%d","model":%q,"output":[],"usage":{"input_tokens":2,"output_tokens":1}}}`, turn, gjson.GetBytes(payload, "model").String())))
}

// Smallest real consumer selector: prove first Bind/Begin/Publish, both capacity
// acquisitions, provider write, downstream terminal and observed usage together.
func TestNativeWSHandlerFirstTurnReachesDispatch(t *testing.T) {
	probe := &wsHandlerScopeProbe{}
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.beforeDispatchRead = probe.authorityRead
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload: wsGroupPricingFirst, ingressMode: service.OpenAIWSIngressModeCtxPool,
		apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
		beforeCapacityAcquire: probe.beforeAcquire,
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
			scopes := probe.scopes()
			if turn != 1 || len(scopes) != 1 {
				return fmt.Errorf("first wire create=%d handler scopes=%d", turn, len(scopes))
			}
			if err := verifyWSHandlerLiveScope(scopes[0]); err != nil {
				return err
			}
			return wsHandlerCompleted(ctx, conn, turn, payload)
		},
	})
	require.Len(t, got.logs, 1)
	require.Equal(t, 2, got.logs[0].InputTokens)
	require.Equal(t, 1, got.logs[0].OutputTokens)
	require.Equal(t, 1, got.upstreamAttempts)
	require.Len(t, got.upstreamPayloads, 1)
	require.Len(t, got.clientEvents, 1)
	rows, writes, applies := got.nativeJournal.counts()
	require.Equal(t, 1, rows, "one native journal identity for this operation")
	require.Equal(t, 1, writes, "one synchronous evidence write")
	require.Zero(t, applies, "simple mode must never debit via Apply")
	record, err := got.nativeJournal.LoadNativeUsageEvidence(t.Context(), billing.Identity{RequestID: got.log.RequestID, APIKeyID: got.log.APIKeyID})
	require.NoError(t, err)
	require.Equal(t, billing.NativeSettled, record.Receipt.Disposition)
	require.NotNil(t, record.SettlementSequence, "the original zero-funding command owns the durable log transaction")
	require.NotNil(t, record.Evidence.Usage)
	require.NotNil(t, record.Evidence.Usage.InputTokens)
	require.NotNil(t, record.Evidence.Usage.OutputTokens)
	require.EqualValues(t, 2, *record.Evidence.Usage.InputTokens)
	require.EqualValues(t, 1, *record.Evidence.Usage.OutputTokens)
	require.Len(t, record.Observations, 1)
	require.Equal(t, "ws", record.Evidence.Prepared.Transport)
	probe.assertTurns(t, 1)
}

// Preserve metered-error measurements and non-replay under the native journal.
// A bare error is not response terminal proof: partial counts stay pending.
// Cache-only observations remain measured even with input/output both zero.
func TestNativeWSHandlerMeteredRateLimitPersistsPartialUsageOnce(t *testing.T) {
	for _, tc := range []struct {
		name, usage          string
		input, output, cache int
	}{
		{"input_output", `{"input_tokens":5,"output_tokens":1}`, 5, 1, 0},
		{"cache_only", `{"input_tokens":0,"output_tokens":0,"input_tokens_details":{"cached_tokens":3}}`, 0, 0, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			completedLogs := 0 // No confirmed response terminal was received.
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: `{"type":"response.create","model":"gpt-5.1","input":"hello"}`,
				ingressMode:  service.OpenAIWSIngressModePassthrough, meteredRateLimitUsage: tc.usage, expectedUsageCount: &completedLogs,
				afterNativeFinalization: func(t *testing.T, journal *wsNativeFixtureJournal) {
					records := nativeWSFixtureRecoveryRecords(t, journal)
					require.Len(t, records, 1)
					require.Empty(t, journal.identities(), "partial facts do not manufacture canonical rows")
					final := records[0].Finalization
					evidence := final.Evidence.Evidence
					require.Equal(t, records[0].Intent.Key, final.Intent.Key)
					require.Empty(t, evidence.Identity.RequestID)
					require.Empty(t, evidence.UpstreamRequestID)
					require.Empty(t, evidence.UpstreamResponseID)
					require.NotNil(t, evidence.Usage)
					require.Equal(t, billing.NativeUsagePartial, evidence.Confidence)
					require.False(t, evidence.TerminalConfirmed)
					require.Nil(t, final.Evidence.SettlementSequence)
					require.False(t, final.Evidence.Receipt.Durable(), "partial facts are not a canonical settlement")
					require.Nil(t, final.Evidence.SettlementUsage)
					require.EqualValues(t, tc.input, *evidence.Usage.InputTokens)
					require.EqualValues(t, tc.output, *evidence.Usage.OutputTokens)
					if tc.cache > 0 {
						require.NotNil(t, evidence.Usage.CacheReadTokens)
						require.EqualValues(t, tc.cache, *evidence.Usage.CacheReadTokens)
					}
					journal.assertNoLegacyApply(t)
				},
			})
			require.Empty(t, got.logs, "unconfirmed partial usage remains durable evidence, not a final zero bill")
			require.Nil(t, got.log)
			require.Equal(t, 1, got.upstreamAttempts)
			require.Len(t, got.upstreamPayloads, 1, "metered failure is never replayed")
		})
	}
}

func TestNativeWSHandlerMeteredRateLimitStreamsKeepSeparatePendingRoots(t *testing.T) {
	completedLogs := 0
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		firstPayload:  `{"type":"response.create","model":"gpt-5.1","stream_id":"a","input":"hello"}`,
		secondPayload: `{"type":"response.create","model":"gpt-5.1","stream_id":"b","input":"hello"}`,
		ingressMode:   service.OpenAIWSIngressModePassthrough, multiplexStreams: true, expectedUsageCount: &completedLogs,
		meteredRateLimitUsage: `{"input_tokens":5,"output_tokens":1}`,
		afterNativeFinalization: func(t *testing.T, journal *wsNativeFixtureJournal) {
			records := nativeWSFixtureRecoveryRecords(t, journal)
			require.Len(t, records, 2, "shared correlation must not collapse technical roots")
			require.Empty(t, journal.identities(), "partial usage must not invent canonical identities")
			operations := make(map[string]bool)
			for _, recovery := range records {
				final := recovery.Finalization
				evidence := final.Evidence.Evidence
				require.Equal(t, recovery.Intent.Key, final.Intent.Key)
				require.Empty(t, evidence.Identity.RequestID)
				require.Equal(t, billing.NativeUsagePartial, evidence.Confidence)
				require.False(t, evidence.TerminalConfirmed)
				require.Nil(t, final.Evidence.SettlementSequence)
				require.False(t, final.Evidence.Receipt.Durable(), "partial facts are not a canonical settlement")
				require.Nil(t, final.Evidence.SettlementUsage)
				require.NotNil(t, evidence.Usage)
				require.EqualValues(t, 5, *evidence.Usage.InputTokens)
				require.EqualValues(t, 1, *evidence.Usage.OutputTokens)
				require.Empty(t, evidence.UpstreamRequestID)
				require.Empty(t, evidence.UpstreamResponseID)
				operations[evidence.OperationID] = true
			}
			require.Len(t, operations, 2)
			journal.assertNoLegacyApply(t)
		},
	})
	require.Empty(t, got.logs, "two partial observations do not become two confirmed settlements")
	require.Nil(t, got.log)
	require.Equal(t, 2, got.upstreamAttempts)
	require.Len(t, got.upstreamPayloads, 2)
	for _, event := range got.clientEvents {
		require.False(t, gjson.GetBytes(event, "response.id").Exists(), "billing identity must not become a provider continuation ID")
	}
}

func TestNativeWSHandlerTurnsShareBoundScopeThroughDispatch(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			probe := &wsHandlerScopeProbe{}
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.beforeDispatchRead = probe.authorityRead
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
				apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
				beforeCapacityAcquire: probe.beforeAcquire,
				upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
					scopes := probe.scopes()
					if len(scopes) != turn {
						return fmt.Errorf("wire turn %d has %d handler scopes", turn, len(scopes))
					}
					if err := verifyWSHandlerLiveScope(scopes[turn-1]); err != nil {
						return err
					}
					return wsHandlerCompleted(ctx, conn, turn, payload)
				},
			})
			require.Len(t, got.logs, 2)
			require.Len(t, got.upstreamPayloads, 2)
			require.Len(t, got.clientEvents, 2)
			probe.assertTurns(t, 2)
		})
	}
}

func TestNativeWSHandlerConcurrentLanesOwnIndependentScopes(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			probe := &wsHandlerScopeProbe{}
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.apiKey.User.Concurrency = 2
			repo.beforeDispatchRead = probe.authorityRead
			// Keep both leases live until both server goroutines verify them.
			verified := make(chan struct{})
			var verifiedMu sync.Mutex
			verifiedCount := 0
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				multiplexStreams: true, ingressMode: mode,
				firstPayload:  `{"type":"response.create","model":"gpt-5.6-sol","stream_id":"parent","input":"parent"}`,
				secondPayload: `{"type":"response.create","model":"gpt-5.6-sol","stream_id":"child","input":"child"}`,
				apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
				beforeCapacityAcquire: probe.beforeAcquire,
				upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
					scopes := probe.scopes()
					if len(scopes) != 2 || scopes[0] == scopes[1] {
						return fmt.Errorf("concurrent lanes shared or lost their scope")
					}
					for _, scope := range scopes {
						if err := verifyWSHandlerLiveScope(scope); err != nil {
							return err
						}
					}
					verifiedMu.Lock()
					verifiedCount++
					if verifiedCount == 2 {
						close(verified)
					}
					verifiedMu.Unlock()
					select {
					case <-verified:
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(3 * time.Second):
						return fmt.Errorf("lane verification barrier timed out")
					}
					return wsHandlerCompleted(ctx, conn, turn, payload)
				},
			})
			require.Len(t, got.logs, 2)
			require.Equal(t, 2, got.upstreamAttempts)
			require.ElementsMatch(t, []string{"parent", "child"}, []string{gjson.GetBytes(got.clientEvents[0], "stream_id").String(), gjson.GetBytes(got.clientEvents[1], "stream_id").String()})
			for _, payload := range got.upstreamPayloads {
				require.False(t, gjson.GetBytes(payload, "stream_id").Exists())
			}
			probe.assertTurns(t, 2)
		})
	}
}

func TestNativeWSHandlerFreshRevocationKeepsPriorBillingAndSendsNoFrame(t *testing.T) {
	cases := []struct {
		name, reason string
		revoke       func(*service.APIKey, *service.Group)
	}{
		{"key_disabled", "authorization is no longer valid", func(k *service.APIKey, _ *service.Group) { k.Status = service.StatusDisabled }},
		{"key_replaced", "authorization is no longer valid", func(k *service.APIKey, _ *service.Group) { k.ID++ }},
		{"group_revoked", "authorization is no longer valid", func(_ *service.APIKey, g *service.Group) { g.Status = service.StatusDisabled }},
		{"group_rebound", "authorization is no longer valid", func(k *service.APIKey, g *service.Group) { id := g.ID + 1; k.GroupID = &id; g.ID = id }},
		{"model_revoked", "not available for this group", func(_ *service.APIKey, g *service.Group) {
			g.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"gpt-4.1"}}
		}},
	}
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		for _, tc := range cases {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				repo := newWSGroupPricingAPIKeyRepoStub(3)
				keys := newWSGroupPricingAPIKeyService(repo)
				probe := &wsHandlerScopeProbe{}
				got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
					firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
					apiKeyService: keys, apiKeyCredential: wsGroupPricingKey, beforeCapacityAcquire: probe.beforeAcquire,
					secondTurnCloseExpected: true, closeStatus: coderws.StatusPolicyViolation, closeReason: tc.reason,
					// Change authority after transmission, before first completion/billing.
					afterFirstUpstreamRequest: func(*service.ChannelService) error {
						return repo.adminUpdate(keys, func(k *service.APIKey, g *service.Group) { tc.revoke(k, g); g.RateMultiplier = 0.3 })
					},
				})
				require.Len(t, got.logs, 1)
				require.Len(t, got.upstreamPayloads, 1, "revoked followup emits zero data messages, including empty messages")
				require.Len(t, probe.scopes(), 1, "fresh rejection precedes followup Acquire")
				log := got.logs[0]
				require.EqualValues(t, 1801, log.APIKeyID)
				require.NotNil(t, log.GroupID)
				require.Equal(t, wsGroupPricingGroupID, *log.GroupID)
				require.InDelta(t, 3, log.RateMultiplier, 1e-12, "prior turn keeps its admitted price")
				require.Equal(t, 2, log.InputTokens)
				require.Equal(t, 1, log.OutputTokens)
				require.Greater(t, log.ActualCost, 0.0)
			})
		}
	}
}

func readWSHandlerEvent(t *testing.T, conn *coderws.Conn, event string) []byte {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	_, payload, err := conn.Read(ctx)
	require.NoError(t, err)
	require.Equal(t, event, gjson.GetBytes(payload, "type").String(), string(payload))
	return payload
}

// Regression for the real wait boundary: cached ingress auth remains valid,
// but the authoritative row changes while handler capacity acquisition runs.
// A generic recovery/503 event is deliberately NOT an acceptable denial.
func TestNativeWSHandlerRevokedDuringAcquireRejectsWithoutDispatch(t *testing.T) {
	cases := []struct {
		name   string
		revoke func(*service.APIKey, *service.Group)
	}{
		{"key", func(k *service.APIKey, _ *service.Group) { k.Status = service.StatusDisabled }},
		{"group", func(k *service.APIKey, g *service.Group) { id := g.ID + 1; k.GroupID = &id; g.ID = id }},
		{"model", func(_ *service.APIKey, g *service.Group) {
			g.ModelAllowlist = catalog.ModelAllowlist{Enabled: true, Models: []string{"gpt-4.1"}}
		}},
	}
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		for _, phase := range []string{"first", "followup"} {
			for _, mutation := range cases {
				t.Run(mode+"/"+phase+"/"+mutation.name, func(t *testing.T) {
					repo := newWSGroupPricingAPIKeyRepoStub(3)
					keys := newWSGroupPricingAPIKeyService(repo)
					var armed, revoked atomic.Bool
					armed.Store(phase == "first")
					tc := openAIResponsesWSUsageLogCase{
						firstPayload: wsGroupPricingFirst, ingressMode: mode,
						apiKeyService: keys, apiKeyCredential: wsGroupPricingKey,
						closeStatus: coderws.StatusPolicyViolation, closeReason: "authorization",
						beforeCapacityAcquire: func(ctx context.Context, user bool) {
							if user || !armed.Load() || !revoked.CompareAndSwap(false, true) {
								return
							}
							// Do not invalidate the ingress cache: dispatch must read by ID.
							repo.mu.Lock()
							mutation.revoke(&repo.apiKey, &repo.group)
							repo.mu.Unlock()
						},
					}
					if phase == "first" {
						tc.firstFrameCloseExpected = true
					} else {
						tc.secondPayload = wsGroupPricingSecond
						tc.secondTurnCloseExpected = true
						tc.afterFirstClientTurn = func() { armed.Store(true) }
					}
					got := runOpenAIResponsesWebSocketUsageLogCase(t, tc)
					require.True(t, revoked.Load(), "must reach actual Acquire, not fail fixture setup")
					want := 0
					if phase == "followup" {
						want = 1
					}
					require.Len(t, got.upstreamPayloads, want, "revocation after admission must emit zero extra frames")
					require.Len(t, got.logs, want, "no zero-usage bill for denied work")
				})
			}
		}
	}
}

func writeWSHandlerEvent(t *testing.T, conn *coderws.Conn, payload string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, []byte(payload)))
}

func TestNativeWSHandlerCanceledTurnStartsFreshFollowup(t *testing.T) {
	probe := &wsHandlerScopeProbe{}
	repo := newWSGroupPricingAPIKeyRepoStub(1)
	repo.beforeDispatchRead = probe.authorityRead
	knownUsage := 1 // First turn has no observed usage; do not invent a zero bill.
	got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
		ingressMode: service.OpenAIWSIngressModeCtxPool, turnPerConnection: true, expectedUsageCount: &knownUsage,
		firstPayload:  `{"type":"response.create","model":"gpt-5.6-sol","store":false,"input":[{"role":"user","content":"first"}]}`,
		secondPayload: `{"type":"response.create","model":"gpt-5.6-sol","store":false,"input":[{"role":"user","content":"first"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete item"}]},{"role":"user","content":"next"}]}`,
		apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
		beforeCapacityAcquire: probe.beforeAcquire,
		upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
			if turn == 2 {
				scopes := probe.scopes()
				if len(scopes) != 2 || scopes[0] == scopes[1] {
					return fmt.Errorf("canceled operation/scope leaked into followup")
				}
				if err := verifyWSHandlerLiveScope(scopes[1]); err != nil {
					return err
				}
				return wsHandlerCompleted(ctx, conn, turn, payload)
			}
			ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()
			for _, frame := range []string{
				`{"type":"response.created","response":{"id":"resp_cancelled"}}`,
				`{"type":"response.output_item.done","item":{"type":"message","id":"msg_done","role":"assistant","content":[{"type":"output_text","text":"complete item"}]}}`,
			} {
				if err := conn.Write(ctx, coderws.MessageText, []byte(frame)); err != nil {
					return err
				}
			}
			// Wait for actual retirement; no terminal or usage is fabricated by the peer.
			for {
				_, control, err := conn.Read(ctx)
				if err != nil {
					if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
						return fmt.Errorf("canceled upstream was not retired: %w", err)
					}
					if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) || coderws.CloseStatus(err) >= 0 {
						return nil
					}
					return err
				}
				if gjson.GetBytes(control, "type").String() != "response.cancel" {
					return fmt.Errorf("unexpected frame on canceled connection: %s", control)
				}
			}
		},
		clientScenario: func(t *testing.T, conn *coderws.Conn) [][]byte {
			readWSHandlerEvent(t, conn, "response.created")
			readWSHandlerEvent(t, conn, "response.output_item.done")
			writeWSHandlerEvent(t, conn, `{"type":"response.cancel","response_id":"resp_cancelled"}`)
			canceled := readWSHandlerEvent(t, conn, "response.cancelled")
			writeWSHandlerEvent(t, conn, `{"type":"response.create","model":"gpt-5.6-sol","store":false,"input":[{"role":"user","content":"first"},{"type":"message","role":"assistant","content":[{"type":"output_text","text":"complete item"}]},{"role":"user","content":"next"}]}`)
			completed := readWSHandlerEvent(t, conn, "response.completed")
			require.Equal(t, "resp_usage_e2e_2", gjson.GetBytes(completed, "response.id").String())
			return [][]byte{canceled, completed}
		},
	})
	require.Len(t, got.logs, 1)
	require.Equal(t, "resp_usage_e2e_2", got.logs[0].RequestID)
	require.Equal(t, 2, got.upstreamAttempts)
	require.Len(t, got.upstreamPayloads, 2)
	require.False(t, gjson.GetBytes(got.upstreamPayloads[1], "previous_response_id").Exists())
	require.Contains(t, string(got.upstreamPayloads[1]), "complete item")
	require.Contains(t, string(got.upstreamPayloads[1]), "next")
	probe.assertTurns(t, 2)
}

func TestNativeWSHandlerOldTurnControlAndReleaseDoNotCancelNextTurn(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			probe := &wsHandlerScopeProbe{}
			repo := newWSGroupPricingAPIKeyRepoStub(1)
			repo.beforeDispatchRead = probe.authorityRead
			controlRejected := make(chan struct{})
			var once sync.Once
			resume := func() { once.Do(func() { close(controlRejected) }) }
			defer resume()
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload: wsGroupPricingFirst, secondPayload: wsGroupPricingSecond, ingressMode: mode,
				apiKeyService: newWSGroupPricingAPIKeyService(repo), apiKeyCredential: wsGroupPricingKey,
				beforeCapacityAcquire: probe.beforeAcquire,
				upstreamScenario: func(ctx context.Context, conn *coderws.Conn, turn int, payload []byte) error {
					if turn == 1 {
						return wsHandlerCompleted(ctx, conn, turn, payload)
					}
					scopes := probe.scopes()
					if len(scopes) != 2 || scopes[0] == scopes[1] {
						return fmt.Errorf("followup inherited prior scope")
					}
					for _, target := range []struct {
						user bool
						id   int64
					}{{true, 1701}, {false, 9901}} {
						old := scopes[0].Lookup(target.user, target.id)
						if old == nil {
							return fmt.Errorf("missing first turn receipt")
						}
						old.ReleaseFunc()
					}
					if err := verifyWSHandlerLiveScope(scopes[1]); err != nil {
						return err
					}
					writeCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
					err := conn.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.created","response":{"id":"resp_usage_e2e_2"}}`))
					cancel()
					if err != nil {
						return err
					}
					select {
					case <-controlRejected:
					case <-ctx.Done():
						return ctx.Err()
					case <-time.After(3 * time.Second):
						return fmt.Errorf("old-turn control was not rejected")
					}
					if err := verifyWSHandlerLiveScope(scopes[1]); err != nil {
						return err
					}
					return wsHandlerCompleted(ctx, conn, turn, payload)
				},
				clientScenario: func(t *testing.T, conn *coderws.Conn) [][]byte {
					first := readWSHandlerEvent(t, conn, "response.completed")
					writeWSHandlerEvent(t, conn, wsGroupPricingSecond)
					readWSHandlerEvent(t, conn, "response.created")
					writeWSHandlerEvent(t, conn, `{"type":"response.interrupt","response_id":"resp_usage_e2e_1","mode":"cancel","event_id":"stale-turn"}`)
					rejected := readWSHandlerEvent(t, conn, "error")
					require.Equal(t, "response_not_active", gjson.GetBytes(rejected, "error.code").String())
					require.EqualValues(t, 400, gjson.GetBytes(rejected, "status").Int())
					require.Equal(t, "stale-turn", gjson.GetBytes(rejected, "event_id").String())
					require.Equal(t, "stale-turn", gjson.GetBytes(rejected, "event_id").String())
					resume()
					second := readWSHandlerEvent(t, conn, "response.completed")
					require.Equal(t, "resp_usage_e2e_2", gjson.GetBytes(second, "response.id").String())
					return [][]byte{first, second}
				},
			})
			require.Len(t, got.logs, 2)
			require.Len(t, got.upstreamPayloads, 2)
			probe.assertTurns(t, 2)
		})
	}
}

// Read the technical journal through its actual recovery port. Partial/unknown
// observations have no canonical row to query until binding is justified.
func nativeWSFixtureRecoveryRecords(t *testing.T, journal *wsNativeFixtureJournal) []*service.NativeIntentRecoveryInput {
	t.Helper()
	journal.mu.Lock()
	keys := make([]billing.NativeIntentKey, 0, len(journal.finalizations))
	for key := range journal.finalizations {
		keys = append(keys, key)
	}
	journal.mu.Unlock()
	records := make([]*service.NativeIntentRecoveryInput, 0, len(keys))
	for _, key := range keys {
		record, err := journal.LoadNativeIntentRecovery(t.Context(), key)
		require.NoError(t, err)
		require.NotNil(t, record.Intent)
		require.NotNil(t, record.Finalization)
		records = append(records, record)
	}
	return records
}
