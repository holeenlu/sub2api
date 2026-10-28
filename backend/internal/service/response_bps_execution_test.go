package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/gateway/execution"
	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	native "github.com/Wei-Shaw/sub2api/internal/providers/openai"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type responseBPSAccounts struct {
	*nativeAuthorityAccounts
	validations int
	afterCheck  func(int)
}

// In-memory journal contract for actual service sends. Production never falls
// back to this port: the repository/PG suites exercise the persisted boundary.
type responseBPSJournalFixture struct {
	UsageBillingRepository
	mu        sync.Mutex
	preserved map[string]map[uint64]model.PreparedIdentity
	beforeErr error
}

func (j *responseBPSJournalFixture) RecordBPSUsageEvidence(ctx context.Context, in BPSUsageJournalInput) (BPSUsageJournalResult, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return BPSUsageJournalResult{}, err
	}
	if in.BeforeSendSequence != 0 {
		if j.beforeErr != nil {
			return BPSUsageJournalResult{}, j.beforeErr
		}
		if in.Command != nil || in.Finalization.CanonicalRequestID != "" {
			return BPSUsageJournalResult{}, ErrNativeOperationBillingReceipt
		}
		found := false
		for _, attempt := range in.Finalization.Operation.Attempts {
			if attempt.Sequence != in.BeforeSendSequence {
				continue
			}
			if !attempt.Identity.Valid() || attempt.Identity.APIKeyID != in.Finalization.Operation.APIKeyID || attempt.Identity.OperationID != in.Finalization.Operation.OperationID || attempt.Send != model.SendUnknown || attempt.Closed || attempt.Usage != nil {
				return BPSUsageJournalResult{}, ErrNativeOperationBillingReceipt
			}
			if j.preserved == nil {
				j.preserved = make(map[string]map[uint64]model.PreparedIdentity)
			}
			root := attempt.Identity.OperationID
			if j.preserved[root] == nil {
				j.preserved[root] = make(map[uint64]model.PreparedIdentity)
			}
			if old, exists := j.preserved[root][attempt.Sequence]; exists && old != attempt.Identity {
				return BPSUsageJournalResult{}, ErrNativeOperationBillingReceipt
			}
			j.preserved[root][attempt.Sequence] = attempt.Identity
			found = true
		}
		if !found {
			return BPSUsageJournalResult{}, ErrNativeOperationBillingReceipt
		}
	}
	charged := make(map[uint64]bool)
	var receipts []model.ModeBillingReceipt
	if in.Command != nil {
		if in.Command.RequestID != in.Finalization.CanonicalRequestID || in.Command.APIKeyID != in.Finalization.Operation.APIKeyID {
			return BPSUsageJournalResult{}, ErrNativeOperationBillingReceipt
		}
		for _, sequence := range in.ChargedSequences {
			charged[sequence] = true
		}
		receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: in.Command.APIKeyID, RootRequestID: in.Command.RequestID, Attempts: in.ChargedSequences,
			Receipt: model.BillingReceipt{Disposition: model.BillingSettled, Durable: true, EvidenceID: "fixture-bps-money"}})
	}
	for _, sequence := range in.Finalization.CurrentSequences {
		if charged[sequence] {
			continue
		}
		disposition := model.BillingPendingUsage
		for _, attempt := range in.Finalization.Operation.Attempts {
			if attempt.Sequence == sequence && BPSAttemptNotRequired(attempt) {
				disposition = model.BillingNotRequired
			}
		}
		receipts = append(receipts, model.ModeBillingReceipt{APIKeyID: in.Finalization.Operation.APIKeyID, Attempts: []uint64{sequence},
			Receipt: model.BillingReceipt{Disposition: disposition, Durable: true, EvidenceID: "fixture-preserved-bps-seal"}})
	}
	return BPSUsageJournalResult{Receipts: receipts}, nil
}

func (r *responseBPSAccounts) ValidateVendorAuthorizationMode(ctx context.Context, account *Account, group *int64, mode string) error {
	if mode == VendorOpenAIModeBPS {
		return r.modeErr
	}
	return r.nativeAuthorityAccounts.ValidateVendorAuthorizationMode(ctx, account, group, mode)
}

func (r *responseBPSAccounts) ValidateExcelBPSDispatch(ctx context.Context, account *Account, group *int64, upstreamModel string) error {
	r.validations++
	fresh := r.accounts[account.ID]
	fingerprint, bound := ExcelBPSDispatchFingerprint(ctx, account.ID)
	if fresh == nil || !bound || fingerprint != "primary" || !excelPrimaryRefreshCredentialsEqual(fresh.Credentials, account.Credentials) ||
		group == nil || *group != 9 || !fresh.isExcelBPSUpstreamModelEnabled(upstreamModel) {
		return ErrNativeDispatchScopeChanged
	}
	if r.afterCheck != nil {
		r.afterCheck(r.validations)
	}
	return nil
}

func newResponseBPSFixture(t *testing.T, capacity bool) (*nativeOperationFixture, *responseBPSAccounts) {
	t.Helper()
	f := newNativeOperationFixture(t, capacity, 8)
	f.resource.Type = AccountTypeOAuth
	f.resource.Credentials = map[string]any{"access_token": "bps-token", "client_id": openai.ExcelClientID, "chatgpt_account_id": "bps-workspace", "model_mapping": map[string]any{"client-model": "gpt-6-astra"}}
	f.resource.Extra = map[string]any{"openai_excel_bps": true, "openai_excel_bps_models": []string{"gpt-6-astra"}}
	repo := &responseBPSAccounts{nativeAuthorityAccounts: f.accounts}
	svc := excelBPSGatewayTestService(&httpUpstreamRecorder{})
	svc.cfg, svc.httpUpstream, svc.accountRepo = f.service.cfg, f.transport, repo
	svc.userRepo, svc.userSubRepo = f.service.userRepo, f.service.userSubRepo
	svc.usageBillingRepo = &responseBPSJournalFixture{}
	f.service = svc
	f.op.authority.accounts, f.op.authority.checks = repo, repo
	f.op.authority.leases.(*nativeDispatchCapacity).accounts = repo
	f.ctx = WithHTTPUpstreamRedirectsDisabled(WithHTTPUpstreamProfile(withExcelBPSUpstreamModel(f.ctx, "gpt-6-astra"), HTTPUpstreamProfileExcelBPS))
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": {"bps-money-id"}}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	return f, repo
}

func sendResponseBPSFixture(t *testing.T, f *nativeOperationFixture, role model.AttemptRole, body string) *http.Response {
	t.Helper()
	response, _, err := f.service.doExcelBPSRequest(withResponseBPSRole(f.ctx, role), nil, f.resource, "fixture-scope", []byte(body), "bps-token", "bps-workspace")
	require.NoError(t, err)
	t.Cleanup(func() { _ = response.Body.Close() })
	return response
}

func TestResponseBPSActualSendsShareRootBudgetAndExplicitRoles(t *testing.T) {
	f, repo := newResponseBPSFixture(t, true)
	owner := nativeHTTPFromContext(f.ctx)
	roles := []model.AttemptRole{model.AttemptCompact, model.AttemptGenerate, model.AttemptRepair}
	f.transport.inspect = func(request *http.Request, options native.TransportOptions) {
		require.Equal(t, native.ProfileExcelBPS, options.Profile)
		require.Equal(t, f.resource.ID, options.AccountID)
		require.Equal(t, "Bearer bps-token", request.Header.Get("Authorization"))
	}
	for i, role := range roles {
		body := fmt.Sprintf(`{"model":"gpt-6-astra","input":"%d"}`, i)
		response := sendResponseBPSFixture(t, f, role, body)
		if role == model.AttemptCompact {
			require.NoError(t, observeResponseBPSEvent(f.ctx, response, []byte(`{"type":"response.completed","response":{"id":"compact-r","status":"completed","output":[],"usage":{"input_tokens":7,"output_tokens":3}}}`)))
		}
		require.NoError(t, response.Body.Close())
		// A new selection/request context still carries the original root.
		f.ctx = context.WithValue(f.ctx, struct{ cycle int }{i}, true)
		require.Same(t, owner.core, responseBPSFromContext(f.ctx).controller.core)
		require.Same(t, f.op.budget, responseBPSFromContext(f.ctx).controller.budget)
	}
	snapshot := owner.core.Snapshot()
	require.Len(t, snapshot.Attempts, 3)
	for i, attempt := range snapshot.Attempts {
		require.Equal(t, roles[i], attempt.Identity.Role)
		require.Equal(t, VendorOpenAIModeBPS, attempt.Identity.Mode)
		require.Empty(t, attempt.Identity.Binding.BillingRoot)
		require.Equal(t, f.op.request.OperationID, attempt.Identity.Binding.BillingScope)
		require.Equal(t, "bps-money-id", attempt.RequestID)
		require.True(t, attempt.Closed)
	}
	require.Equal(t, 3, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.EqualValues(t, 3, f.transport.sends.Load())
	require.Zero(t, f.transport.legacy.Load(), "qualified Do must not debit another budget")
	require.GreaterOrEqual(t, repo.validations, 6, "authority must run at both Admit and Consume")
	require.Zero(t, snapshot.DelegatedCycles)
	require.False(t, owner.delegated)
}

func TestResponseBPSSameModeRetryAfterNotSentKeepsController(t *testing.T) {
	f, repo := newResponseBPSFixture(t, true)
	repo.afterCheck = func(call int) {
		if call == 1 {
			f.key.Status = "disabled"
		}
	}
	_, _, err := f.service.doExcelBPSRequest(withResponseBPSRole(f.ctx, model.AttemptGenerate), nil, f.resource, "scope", []byte(`{"model":"gpt-6-astra","input":"first"}`), "bps-token", "bps-workspace")
	require.Error(t, err)
	controller := responseBPSFromContext(f.ctx).controller
	f.key.Status, repo.afterCheck = StatusActive, nil
	response := sendResponseBPSFixture(t, f, model.AttemptGenerate, `{"model":"gpt-6-astra","input":"authorized-retry"}`)
	require.NoError(t, response.Body.Close())
	require.Same(t, controller, responseBPSFromContext(f.ctx).controller)
	snapshot := controller.Snapshot()
	require.Len(t, snapshot.Attempts, 2)
	require.Equal(t, model.SendNotSent, snapshot.Attempts[0].Send)
	require.Equal(t, model.SendSent, snapshot.Attempts[1].Send)
	for _, attempt := range snapshot.Attempts {
		require.Equal(t, VendorOpenAIModeBPS, attempt.Identity.Mode)
	}
	require.Equal(t, 2, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.EqualValues(t, 1, f.transport.sends.Load())
}

func TestResponseBPSAuthorityRevocationAtConsumeAndOwnedCapacity(t *testing.T) {
	for _, name := range []string{"key_revoked", "grant_changed", "route_disabled", "missing_capacity", "missing_validator"} {
		t.Run(name, func(t *testing.T) {
			f, repo := newResponseBPSFixture(t, name != "missing_capacity")
			if name == "missing_validator" {
				f.service.accountRepo = f.accounts
			} else {
				repo.afterCheck = func(call int) {
					if call != 1 {
						return
					}
					switch name {
					case "key_revoked":
						f.key.Status = "disabled"
					case "grant_changed":
						f.resource.Credentials["access_token"] = "replacement"
					case "route_disabled":
						f.resource.Extra["openai_excel_bps"] = false
					}
				}
			}
			_, _, err := f.service.doExcelBPSRequest(withResponseBPSRole(f.ctx, model.AttemptGenerate), nil, f.resource, "scope", []byte(`{"model":"gpt-6-astra"}`), "bps-token", "bps-workspace")
			require.Error(t, err)
			require.Zero(t, f.transport.sends.Load())
			require.Zero(t, f.transport.legacy.Load())
			if name == "key_revoked" {
				require.True(t, IsNativeDispatchPrincipalFailure(err))
			}
		})
	}
}

func TestResponseBPSPreparedHeaderAndBodyRemainSealed(t *testing.T) {
	f, repo := newResponseBPSFixture(t, true)
	const body = `{"model":"gpt-6-astra","extension":9007199254740993123}`
	var original *http.Request
	repo.afterCheck = func(call int) {
		if call == 1 {
			original.Header.Set("Authorization", "Bearer tampered")
			original.Header.Set("Chatgpt-Account-Id", "other")
		}
	}
	f.transport.inspect = func(request *http.Request, _ native.TransportOptions) {
		raw, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Equal(t, body, string(raw))
		require.Equal(t, "Bearer bps-token", request.Header.Get("Authorization"))
		require.Equal(t, "bps-workspace", request.Header.Get("Chatgpt-Account-Id"))
	}
	response, _, err := f.service.doExcelBPSRequestTo(withResponseBPSRole(f.ctx, model.AttemptGenerate), nil, f.resource, "scope", basispoints.ResponsesURL, func(ctx context.Context) (*http.Request, error) {
		var err error
		original, err = newExcelBPSRequest(ctx, []byte(body), "bps-token", "bps-workspace")
		return original, err
	})
	require.NoError(t, err)
	require.NoError(t, response.Body.Close())
	snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Equal(t, sha256.Sum256([]byte(body)), snapshot.Attempts[0].Identity.BodyDigest)
}

func TestResponseBPSPublicForwardReportsOriginalConverterFacts(t *testing.T) {
	f, _ := newResponseBPSFixture(t, true)
	wire := "data: " + `{"type":"response.completed","response":{"id":"resp-bps","status":"completed","model":"gpt-6-astra","output":[],"usage":{"input_tokens":7,"output_tokens":2}}}` + "\n\n"
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": {"bps-money-id"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(f.ctx)
	c.Set("api_key", f.key)
	owner := nativeHTTPFromContext(f.ctx)
	c.Writer = &nativeOperationWriter{ResponseWriter: c.Writer, owner: owner}
	result, err := f.service.Forward(f.ctx, c, f.resource, []byte(`{"model":"client-model","input":"hello","stream":true}`))
	require.NoError(t, err)
	require.Equal(t, "bps-money-id", result.RequestID)
	snapshot := owner.core.Snapshot()
	require.Len(t, snapshot.Attempts, 1)
	attempt := snapshot.Attempts[0]
	require.Equal(t, model.AttemptGenerate, attempt.Identity.Role)
	require.Equal(t, model.TerminalCompleted, attempt.Terminal)
	require.Equal(t, model.UsageFinal, attempt.Usage.Confidence)
	require.EqualValues(t, 7, attempt.Usage.Input.Value)
	require.EqualValues(t, 2, attempt.Usage.Output.Value)
	require.True(t, attempt.Closed)
	require.True(t, attempt.Delivery.BusinessCommitted)
	require.False(t, owner.delegated)
	require.Zero(t, f.transport.legacy.Load())
}

type responseBPSBillingFunc func(context.Context, BPSOperationFinalization) ([]model.ModeBillingReceipt, error)

func (f responseBPSBillingFunc) RecordBPSOperationUsage(ctx context.Context, in BPSOperationFinalization) ([]model.ModeBillingReceipt, error) {
	return f(ctx, in)
}
func (responseBPSBillingFunc) RecordNativeOperationUsage(context.Context, NativeOperationFinalization) (NativeIntentFinalizationResult, error) {
	return NativeIntentFinalizationResult{}, errors.New("BPS cannot use the Native amount owner")
}

func TestResponseBPSFinalizationRequiresRealCanonicalReceipt(t *testing.T) {
	for _, name := range []string{"pending_unknown", "wrong_root", "wrong_key", "false_settled", "missing_money_id"} {
		t.Run(name, func(t *testing.T) {
			f, _ := newResponseBPSFixture(t, true)
			response := sendResponseBPSFixture(t, f, model.AttemptGenerate, `{"model":"gpt-6-astra"}`)
			require.NoError(t, response.Body.Close())
			input := &OpenAIRecordUsageInput{Result: &OpenAIForwardResult{RequestID: "bps-money-id"}, APIKey: f.key, User: f.key.User, Account: f.resource}
			if name == "missing_money_id" {
				input.Result.RequestID = "bps-usage-invented"
			}
			calls := 0
			port := responseBPSBillingFunc(func(ctx context.Context, in BPSOperationFinalization) ([]model.ModeBillingReceipt, error) {
				calls++
				require.NoError(t, ctx.Err())
				require.Nil(t, in.Operation.Attempts[0].Usage, "absence must not become zero usage")
				require.Empty(t, in.UsageByAttempt)
				root := "bps-money-id"
				if name == "missing_money_id" {
					root = ""
				}
				require.Equal(t, root, in.CanonicalRequestID)
				receipt := model.ModeBillingReceipt{RootRequestID: root, APIKeyID: f.key.ID, Attempts: in.CurrentSequences, Receipt: model.BillingReceipt{Disposition: model.BillingPendingUsage, Durable: true, EvidenceID: "fixture-journal-ack"}}
				switch name {
				case "wrong_root":
					receipt.RootRequestID = in.Operation.OperationID
				case "wrong_key":
					receipt.APIKeyID++
				case "false_settled":
					receipt.Receipt.Disposition = model.BillingSettled
				}
				return []model.ModeBillingReceipt{receipt}, nil
			})
			canceled, cancel := context.WithCancel(f.ctx)
			cancel()
			for range 2 {
				handled, err := finalizeNativeHTTPUsage(canceled, input, port, time.Second)
				require.True(t, handled)
				if name == "pending_unknown" || name == "missing_money_id" {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, ErrNativeOperationBillingReceipt)
				}
			}
			require.Equal(t, 1, calls)
			require.Equal(t, model.BillingPendingUsage, nativeHTTPFromContext(f.ctx).core.Snapshot().Billing.Disposition)
		})
	}
}

func TestResponseBPSMissingJournalAndLostPublicRootFailClosed(t *testing.T) {
	f, _ := newResponseBPSFixture(t, true)
	response := sendResponseBPSFixture(t, f, model.AttemptCompact, `{"model":"gpt-6-astra"}`)
	require.NoError(t, response.Body.Close())
	f.service.usageBillingRepo = nil
	handled, err := f.service.FinalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{APIKey: f.key, User: f.key.User, Account: f.resource})
	require.True(t, handled)
	require.ErrorIs(t, err, ErrResponseBPSJournalUnavailable)
	snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Equal(t, model.BillingPendingUsage, snapshot.Billing.Disposition)
	require.False(t, snapshot.Billing.Durable)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Set("api_key", f.key)
	_, _, err = f.service.doExcelBPSRequest(t.Context(), c, f.resource, "scope", []byte(`{}`), "bps-token", "bps-workspace")
	require.ErrorIs(t, err, execution.ErrDispatchUnavailable)
	require.Zero(t, f.transport.legacy.Load())
}

func TestResponseBPSBeforeSendJournalIsRequiredBeforeActualIO(t *testing.T) {
	for _, scenario := range []string{"preserved", "write_failed", "missing_port"} {
		t.Run(scenario, func(t *testing.T) {
			f, _ := newResponseBPSFixture(t, true)
			journal := f.service.usageBillingRepo.(*responseBPSJournalFixture)
			failure := errors.New("pre-send journal transaction failed")
			if scenario == "write_failed" {
				journal.beforeErr = failure
			}
			if scenario == "missing_port" {
				f.service.usageBillingRepo = nil
			}
			f.transport.inspect = func(_ *http.Request, _ native.TransportOptions) {
				journal.mu.Lock()
				defer journal.mu.Unlock()
				attempts := nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts
				require.Len(t, attempts, 1)
				require.Equal(t, attempts[0].Identity, journal.preserved[attempts[0].Identity.OperationID][attempts[0].Sequence])
				require.Equal(t, model.SendUnknown, attempts[0].Send)
				require.Empty(t, attempts[0].RequestID, "pre-send persistence cannot invent a monetary identity")
			}
			response, _, err := f.service.doExcelBPSRequest(withResponseBPSRole(f.ctx, model.AttemptGenerate), nil, f.resource, "scope", []byte(`{"model":"gpt-6-astra"}`), "bps-token", "bps-workspace")
			if scenario == "preserved" {
				require.NoError(t, err)
				require.NoError(t, response.Body.Close())
				require.EqualValues(t, 1, f.transport.sends.Load())
			} else {
				require.Error(t, err)
				if scenario == "write_failed" {
					require.ErrorIs(t, err, failure)
				} else {
					require.ErrorIs(t, err, ErrResponseBPSJournalUnavailable)
				}
				require.Zero(t, f.transport.sends.Load())
				attempts := nativeHTTPFromContext(f.ctx).core.Snapshot().Attempts
				require.Len(t, attempts, 1)
				require.Equal(t, model.SendNotSent, attempts[0].Send)
				require.True(t, attempts[0].Closed)
			}
			require.Zero(t, f.transport.legacy.Load())
		})
	}
}

func TestResponseBPSObserverPositiveWatermarkSurvivesZeroTerminalFinalization(t *testing.T) {
	f, _ := newResponseBPSFixture(t, true)
	wire := "data: " + `{"type":"response.in_progress","response":{"id":"r-watermark","usage":{"input_tokens":7,"output_tokens":2}}}` + "\n\n" +
		"data: " + `{"type":"response.completed","response":{"id":"r-watermark","status":"completed","output":[],"usage":{"input_tokens":0,"output_tokens":0}}}` + "\n\n"
	f.transport.reply = func(*http.Request, native.TransportOptions) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"X-Request-Id": {"bps-money-id"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(f.ctx)
	c.Set("api_key", f.key)
	result, err := f.service.Forward(f.ctx, c, f.resource, []byte(`{"model":"client-model","input":"hello","stream":true}`))
	require.NoError(t, err)
	port := responseBPSBillingFunc(func(_ context.Context, in BPSOperationFinalization) ([]model.ModeBillingReceipt, error) {
		require.Len(t, in.CurrentSequences, 1)
		attempt := in.Operation.Attempts[0]
		require.True(t, attempt.Closed)
		require.Equal(t, model.TerminalCompleted, attempt.Terminal)
		require.Equal(t, model.UsagePartial, attempt.Usage.Confidence)
		require.EqualValues(t, 7, attempt.Usage.Input.Value)
		require.EqualValues(t, 2, attempt.Usage.Output.Value)
		require.Nil(t, attempt.Usage.CacheRead)
		require.Nil(t, attempt.Usage.CacheWrite)
		require.Nil(t, attempt.Usage.ImageInput)
		billable, lower := BPSAttemptBillable(attempt)
		require.True(t, billable)
		require.True(t, lower)
		require.Equal(t, 7, in.UsageByAttempt[attempt.Sequence].InputTokens)
		require.Equal(t, 2, in.UsageByAttempt[attempt.Sequence].OutputTokens)
		return []model.ModeBillingReceipt{{APIKeyID: f.key.ID, RootRequestID: "bps-money-id", Attempts: in.CurrentSequences,
			Receipt: model.BillingReceipt{Disposition: model.BillingSettled, Durable: true, EvidenceID: "fixture-lower-bound-receipt"}}}, nil
	})
	handled, err := finalizeNativeHTTPUsage(f.ctx, &OpenAIRecordUsageInput{Result: result, APIKey: f.key, User: f.key.User, Account: f.resource}, port, time.Second)
	require.True(t, handled)
	require.NoError(t, err)
	require.Equal(t, model.BillingSettled, nativeHTTPFromContext(f.ctx).core.Snapshot().Billing.Disposition)
}

func TestResponseBPSParallelAttachmentControlsUseOwnedPermitAndExclusiveBodies(t *testing.T) {
	f, _ := newResponseBPSFixture(t, true)
	raw, _ := nativeGatewayBody(t)
	plan, err := basispoints.PrepareNativeImages(raw)
	require.NoError(t, err)
	var image basispoints.InlineAttachment
	_, err = plan.Upload(t.Context(), &basispoints.AttachmentUploader{}, func(_ context.Context, attachment basispoints.InlineAttachment) (string, error) {
		image = attachment
		return "file-fixture-extraction", nil
	})
	require.NoError(t, err)
	entered := make(chan int, 2)
	release := make(chan struct{})
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	f.transport.reply = func(request *http.Request, options native.TransportOptions) (*http.Response, error) {
		if request.URL.String() == basispoints.ResponsesURL {
			return bpsCompletionResponse(http.StatusOK, ""), nil
		}
		if request.URL.String() != basispoints.AttachmentsURL || options.Profile != native.ProfileExcelBPS || request.GetBody != nil {
			return nil, errors.New("upload did not retain its sealed exclusive multipart body")
		}
		call := int(f.transport.sends.Load())
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			return nil, err
		}
		entered <- call
		select {
		case <-release:
		case <-request.Context().Done():
			return nil, request.Context().Err()
		}
		return bpsCompletionResponse(http.StatusOK, fmt.Sprintf(`{"openai_file_id":"file-control-%d"}`, call)), nil
	}
	finished := make(chan error, 2)
	start := func() {
		go func() {
			_, err := f.service.uploadExcelBPSAttachment(f.ctx, f.resource, f.key.GroupID, "bps-token", "bps-workspace", "", image)
			finished <- err
		}()
	}
	await := func() {
		t.Helper()
		select {
		case <-entered:
		case <-time.After(2 * time.Second):
			t.Fatal("control I/O was serialized behind a prior live upload")
		}
	}
	start()
	await() // first permit consumed; actual response still outstanding
	start()
	await()
	snapshot := nativeHTTPFromContext(f.ctx).core.Snapshot()
	require.Len(t, snapshot.Attempts, 2)
	for _, attempt := range snapshot.Attempts {
		require.Equal(t, model.AttemptControl, attempt.Identity.Role)
		require.Equal(t, model.DispatchControl, attempt.Identity.Purpose)
		require.Equal(t, model.BodyExclusiveStream, attempt.Identity.BodyKind)
		require.Equal(t, [32]byte{}, attempt.Identity.BodyDigest)
		require.NotEqual(t, [16]byte{}, attempt.Identity.BodyHandle)
		require.Equal(t, model.SendUnknown, attempt.Send)
		require.False(t, attempt.Closed)
	}
	require.NotEqual(t, snapshot.Attempts[0].Identity.BodyHandle, snapshot.Attempts[1].Identity.BodyHandle)
	require.Zero(t, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.Zero(t, f.op.budget.attemptBudget.Snapshot().PreparationRetries)
	unblock()
	for range 2 {
		select {
		case err := <-finished:
			require.NoError(t, err)
		case <-time.After(2 * time.Second):
			t.Fatal("upload did not close/join")
		}
	}
	snapshot = nativeHTTPFromContext(f.ctx).core.Snapshot()
	for _, attempt := range snapshot.Attempts {
		require.True(t, BPSAttemptNotRequired(attempt))
		require.Nil(t, attempt.Usage)
	}
	response := sendResponseBPSFixture(t, f, model.AttemptGenerate, `{"model":"gpt-6-astra","input":"after-upload"}`)
	require.NoError(t, response.Body.Close())
	require.Equal(t, 1, f.op.budget.attemptBudget.Snapshot().Attempts)
	require.Zero(t, f.transport.legacy.Load())
}

func TestExcelBPSStreamStartsClientOutputKeepsEmptyItemsStaged(t *testing.T) {
	for _, tc := range []struct {
		kind, item string
		want       bool
	}{
		{"response.output_item.added", `{"type":"reasoning","encrypted_content":"opaque","summary":[]}`, false},
		{"response.output_item.done", `{"type":"reasoning","summary":[{"type":"summary_text","text":""}]}`, false},
		{"response.output_item.added", `{"type":"message","content":[]}`, false},
		{"response.output_item.done", `{"type":"message","content":[{"type":"refusal","refusal":""}]}`, false},
		{"response.output_item.done", `{"type":"message","content":[{"type":"output_text","text":"answer"}]}`, true},
		{"response.output_item.done", `{"type":"message","content":[{"type":"refusal","refusal":"denied"}]}`, true},
		{"response.output_item.done", `{"type":"message"}`, true},
		{"response.output_item.added", `{"type":"message","content":null}`, true},
		{"response.output_item.done", `{"type":"message","content":[{"type":"unknown"}]}`, true},
		{"response.output_item.done", `{"type":"custom_tool_call","input":"validated"}`, true},
	} {
		t.Run(tc.kind+tc.item, func(t *testing.T) {
			raw := []byte(`{"type":"` + tc.kind + `","item":` + tc.item + `}`)
			require.Equal(t, tc.want, excelBPSStreamStartsClientOutput(raw, tc.kind))
		})
	}
}
