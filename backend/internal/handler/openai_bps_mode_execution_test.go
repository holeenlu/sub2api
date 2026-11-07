//go:build unit

package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	middleware "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type bpsModeHandlerTransport struct {
	service.HTTPUpstream
	wires []string
	sends int
}

func (u *bpsModeHandlerTransport) PrepareNativeRequest(r *http.Request, _ int64) (*http.Request, error) {
	return r, nil
}
func (*bpsModeHandlerTransport) ValidateNativeRedirect(*http.Request, []*http.Request, string) error {
	return errors.New("unexpected redirect")
}
func (u *bpsModeHandlerTransport) RoundTripOnce(_ *http.Request, options native.TransportOptions) (*http.Response, error) {
	if options.Profile != native.ProfileExcelBPS || u.sends >= len(u.wires) {
		return nil, errors.New("unexpected mode or extra generation")
	}
	wire := u.wires[u.sends]
	u.sends++
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
}
func (*bpsModeHandlerTransport) Do(*http.Request, string, int64, int) (*http.Response, error) {
	return nil, errors.New("BPS public request used legacy Do")
}

// This is an explicit journal contract double. Runtime uses the actual handler,
// BPS converter, shared core, sealed transport and original amount builder;
// repository SQL/commit/recovery behavior has separate PostgreSQL regressions.
type bpsModeHandlerJournal struct {
	*wsNativeFixtureJournal
	input         *service.BPSUsageJournalInput
	commitFailure bool
	written       map[string]bool
	preserved     map[string]map[uint64]model.PreparedIdentity
}

func (j *bpsModeHandlerJournal) RecordBPSUsageEvidence(ctx context.Context, in service.BPSUsageJournalInput) (service.BPSUsageJournalResult, error) {
	if j.commitFailure && in.BeforeSendSequence == 0 {
		return service.BPSUsageJournalResult{}, errors.New("fixture transaction rollback")
	}
	j.mu.Lock()
	defer j.mu.Unlock()
	raw, _ := json.Marshal(in.Finalization.Operation)
	digest := sha256.Sum256(raw)
	if in.BeforeSendSequence != 0 {
		if in.Command != nil || in.Finalization.CanonicalRequestID != "" {
			return service.BPSUsageJournalResult{}, service.ErrNativeOperationBillingReceipt
		}
		root := in.Finalization.Operation
		for _, attempt := range root.Attempts {
			if attempt.Sequence != in.BeforeSendSequence {
				continue
			}
			if !attempt.Identity.Valid() || attempt.Identity.OperationID != root.OperationID || attempt.Identity.APIKeyID != root.APIKeyID || attempt.Identity.UserID != root.UserID || attempt.Send != model.SendUnknown || attempt.Closed || attempt.Usage != nil {
				return service.BPSUsageJournalResult{}, service.ErrNativeOperationBillingReceipt
			}
			if j.preserved == nil {
				j.preserved = make(map[string]map[uint64]model.PreparedIdentity)
			}
			if j.preserved[root.OperationID] == nil {
				j.preserved[root.OperationID] = make(map[uint64]model.PreparedIdentity)
			}
			if old, exists := j.preserved[root.OperationID][attempt.Sequence]; exists && old != attempt.Identity {
				return service.BPSUsageJournalResult{}, service.ErrNativeOperationBillingReceipt
			}
			j.preserved[root.OperationID][attempt.Sequence] = attempt.Identity
			return service.BPSUsageJournalResult{Receipts: []model.ModeBillingReceipt{{APIKeyID: root.APIKeyID, Attempts: []uint64{attempt.Sequence},
				Receipt: model.BillingReceipt{Disposition: model.BillingPendingUsage, Durable: true, EvidenceID: hex.EncodeToString(digest[:])}}}}, nil
		}
		return service.BPSUsageJournalResult{}, service.ErrNativeOperationBillingReceipt
	}
	j.input = &in
	var receipts []model.ModeBillingReceipt
	charged := make(map[uint64]bool)
	if in.Command != nil {
		if j.written == nil {
			j.written = make(map[string]bool)
		}
		if j.logSink != nil && !j.written[in.Command.RequestID] {
			rows := in.Command.UsageLogs
			if len(rows) == 0 {
				rows = []*service.UsageLog{in.Command.UsageLog}
			}
			for _, row := range rows {
				if _, err := j.logSink.Create(ctx, row); err != nil {
					return service.BPSUsageJournalResult{}, err
				}
			}
			j.written[in.Command.RequestID] = true
		}
		for _, sequence := range in.ChargedSequences {
			charged[sequence] = true
		}
		disposition := model.BillingSettled
		if in.SimpleUsage {
			disposition = model.BillingUsageRecorded
		}
		receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: in.Command.APIKeyID, RootRequestID: in.Command.RequestID, Attempts: in.ChargedSequences,
			Receipt: model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: hex.EncodeToString(digest[:])}})
	}
	for _, sequence := range in.Finalization.CurrentSequences {
		if !charged[sequence] {
			disposition := model.BillingPendingUsage
			for _, attempt := range in.Finalization.Operation.Attempts {
				if attempt.Sequence == sequence && service.BPSAttemptNotRequired(attempt) {
					disposition = model.BillingNotRequired
				}
			}
			receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: in.Finalization.Operation.APIKeyID, Attempts: []uint64{sequence},
				Receipt: model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: hex.EncodeToString(digest[:])}})
		}
	}
	return service.BPSUsageJournalResult{Receipts: receipts}, nil
}

func TestBPSPublicHandlerUsesModeCoreAndOriginalJournal(t *testing.T) {
	for _, scenario := range []string{"complete", "partial_failure", "unknown", "compact", "repair", "journal_rollback"} {
		t.Run(scenario, func(t *testing.T) {
			cfg := &config.Config{RunMode: config.RunModeSimple, Gateway: config.GatewayConfig{MaxBodySize: 1 << 20, OpenAITurnMaxAttempts: 4}}
			cfg.Default.RateMultiplier = 1
			account := service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true, GroupIDs: []int64{nativeHandlerFixtureGroupID},
				Credentials: map[string]any{"client_id": openai.ExcelClientID, "access_token": "fixture-bps-token", "chatgpt_account_id": "fixture-workspace"},
				Extra:       map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}}}
			authority := newExcelBPSHandlerAuthority(t, cfg, []service.Account{account}, nativeHandlerFixtureGroupID)
			journal := &bpsModeHandlerJournal{wsNativeFixtureJournal: &wsNativeFixtureJournal{}, commitFailure: scenario == "journal_rollback"}
			transport := &bpsModeHandlerTransport{}
			usage := `,"usage":{"input_tokens":7,"output_tokens":2}`
			output := `[]`
			if scenario == "unknown" {
				usage = ""
			}
			if scenario == "compact" {
				output = `[{"type":"compaction","id":"cmp","encrypted_content":"opaque"}]`
			}
			terminal := fmt.Sprintf("data: {\"type\":\"response.completed\",\"response\":{\"id\":\"response-money\",\"status\":\"completed\",\"model\":\"gpt-6-astra\",\"output\":%s%s}}\n\n", output, usage)
			transport.wires = []string{terminal}
			body := `{"model":"gpt-6-astra","stream":true,"input":"hello"}`
			path := "/v1/responses"
			if scenario == "compact" {
				path += "/compact"
			}
			if scenario == "partial_failure" {
				transport.wires = []string{"data: {\"type\":\"response.in_progress\",\"response\":{\"id\":\"response-money\",\"usage\":{\"input_tokens\":7,\"output_tokens\":2}}}\n\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"partial\"}\n\n"}
			}
			if scenario == "repair" {
				body = `{"model":"gpt-6-astra","stream":true,"input":"calculate","tools":[{"type":"custom","name":"exec"}]}`
				tool := func(id, summary string) string {
					args, _ := json.Marshal(map[string]any{"summary": summary, "code": "text(42);", "extended_summary": "{}", "destructive": false, "references": []any{}})
					event, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "status": "completed", "usage": map[string]int{"input_tokens": 7, "output_tokens": 2}, "output": []any{map[string]any{"id": "fc_" + id, "call_id": "call_" + id, "type": "function_call", "name": "run_officejs", "arguments": string(args)}}}})
					return "data: " + string(event) + "\n\n"
				}
				transport.wires = []string{tool("response-money", "Run"), tool("repair-money", "codex2api.custom/exec")}
			}
			concurrency := service.NewConcurrencyService(nil)
			gateway := service.NewOpenAIGatewayService(authority.accounts, nil, journal, authority.users, nil, nil, nil, cfg, nil, concurrency, service.NewBillingService(cfg, nil), nil, nil, transport,
				&service.DeferredService{}, nil, nil, nil, nil, nil, service.NewSettingService(&contentModerationHandlerSettingRepo{values: map[string]string{service.SettingKeyExcelBPSEnabled: "true"}}, cfg), nil)
			t.Cleanup(gateway.CloseOpenAIWSPool)
			cache := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
			cleanupHandlerBillingCache(t, cache)
			require.NoError(t, cache.Start(t.Context()))
			h := NewOpenAIGatewayHandler(gateway, concurrency, cache, authority.keys, nil, nil, nil, nil, cfg)
			router := gin.New()
			router.Use(middleware.RequestBodyLimit(cfg.Gateway.MaxBodySize), func(c *gin.Context) {
				c.Set(string(middleware.ContextKeyAPIKey), newExcelBPSHandlerAPIKey(99, nativeHandlerFixtureGroupID))
				c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 100})
				c.Next()
			})
			router.POST(path, h.Responses)
			request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
			request.Header.Set("Content-Type", "application/json")
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, request)
			require.Equal(t, len(transport.wires), transport.sends, recorder.Body.String())
			if scenario == "journal_rollback" {
				require.Nil(t, journal.input)
				return
			}
			require.NotNil(t, journal.input, recorder.Body.String())
			final := journal.input.Finalization
			require.Zero(t, final.Operation.DelegatedCycles)
			require.Len(t, final.CurrentSequences, transport.sends)
			for _, attempt := range final.Operation.Attempts {
				require.Equal(t, "bps", attempt.Identity.Mode)
				require.True(t, attempt.Closed)
			}
			if scenario == "unknown" {
				require.Nil(t, final.Operation.Attempts[0].Usage)
				require.Nil(t, journal.input.Command)
			} else {
				require.NotNil(t, journal.input.Command)
				require.Equal(t, "response-money", journal.input.Command.RequestID, "actual response.id fallback is a monetary source; operation id is not")
				if scenario == "partial_failure" {
					require.True(t, journal.input.Command.UsageLog.UsageLowerBound)
				}
			}
			if scenario == "compact" {
				require.Equal(t, model.AttemptCompact, final.Operation.Attempts[0].Identity.Role)
			}
			if scenario == "repair" {
				require.Equal(t, model.AttemptRepair, final.Operation.Attempts[1].Identity.Role)
				require.Equal(t, 14, journal.input.Command.InputTokens)
			}
		})
	}
}
