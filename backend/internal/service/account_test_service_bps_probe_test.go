package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

var bpsProbeTestNonce = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)

func TestBPSAccountProbeUsageEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, wire, state    string
		wantError, hasTokens bool
	}{
		{"unknown is not zero", `{"type":"response.completed","response":{"status":"completed"}}`, "unknown", false, false},
		{"null is not zero", `{"type":"response.completed","response":{"status":"completed","usage":null}}`, "unknown", false, false},
		{"measured zero", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":0,"output_tokens":0}}}`, "measured", false, true},
		{"partial complete", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":2}}}`, "unknown", false, true},
		{"negative", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":-1,"output_tokens":0}}}`, "", true, false},
		{"fraction", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1.5,"output_tokens":0}}}`, "", true, false},
		{"overflow", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":9223372036854775808}}}`, "", true, false},
		{"unsafe persisted integer", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":9007199254740993}}}`, "", true, false},
		{"cached exceeds input", `{"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":1,"input_tokens_details":{"cached_tokens":2}}}}`, "", true, false},
		{"text is not usage", `{"type":"response.output_text.delta","response":{"usage":{"input_tokens":10,"output_tokens":10}}}`, "", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var observed bpsAccountProbeObservation
			err := observed.observe([]byte(tc.wire))
			if tc.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, tc.state, observed.usage.State)
			require.Equal(t, tc.hasTokens, observed.usage.Tokens != nil)
		})
	}
	var observed bpsAccountProbeObservation
	require.NoError(t, observed.observe([]byte(`{"type":"response.in_progress","response":{"id":"original","usage":{"input_tokens":7,"output_tokens":1}}}`)))
	require.Error(t, observed.observe([]byte(`{"type":"response.completed","response":{"id":"other","status":"completed","usage":{"input_tokens":99,"output_tokens":99}}}`)))
	require.Equal(t, "original", observed.usage.ResponseID)
	require.Equal(t, int64(7), *observed.usage.Tokens.Input)
	require.Equal(t, "unknown", observed.usage.State)
	// A terminal without usage does not promote an earlier watermark to final.
	require.NoError(t, observed.observe([]byte(`{"type":"response.completed","response":{"id":"original","status":"completed"}}`)))
	require.Equal(t, "unknown", observed.usage.State)
	require.Equal(t, int64(7), *observed.usage.Tokens.Input)
}

func TestBPSAccountProbeObservedCacheCreation(t *testing.T) {
	count := func(n int64) *int64 { return &n }
	for _, tc := range []struct {
		name, raw string
		want      *ExcelBPSProbeTokens
		invalid   bool
	}{
		{name: "absent", raw: `{}`},
		{name: "null", raw: `{"cache_creation_input_tokens":null,"cache_creation":{"ephemeral_5m_input_tokens":null}}`},
		{name: "explicit zero", raw: `{"input_tokens_details":{"cache_write_tokens":0,"cache_write_tokens_details":{"ttl_30m_tokens":0}}}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(0), CacheCreation30m: count(0)}},
		{name: "canonical zero wins", raw: `{"input_tokens_details":{"cache_creation_tokens":0},"cache_write_tokens":9}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(0)}},
		{name: "nested input alias selects maximum without addition", raw: `{"input_tokens_details":{"cache_write_tokens":2,"cache_creation_tokens":4,"cache_creation_input_tokens":3},"cache_creation_input_tokens":9}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(4)}},
		{name: "top level input alias", raw: `{"cache_creation_input_tokens":3}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(3)}},
		{name: "top level write input alias", raw: `{"cache_write_input_tokens":3}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(3)}},
		{name: "five minute and hour", raw: `{"cache_creation_input_tokens":3,"cache_creation":{"ephemeral_5m_input_tokens":1,"ephemeral_1h_input_tokens":2}}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(3), CacheCreation5m: count(1), CacheCreation1h: count(2)}},
		{name: "prompt lifetime buckets", raw: `{"prompt_tokens_details":{"cache_write_tokens":3,"cache_write_tokens_details":{"ttl_30m_tokens":1,"ttl_12h_tokens":2}}}`,
			want: &ExcelBPSProbeTokens{CacheCreation: count(3), CacheCreation30m: count(1), CacheCreation12h: count(2)}},
		{name: "input bucket object is authoritative", raw: `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":1}},"prompt_tokens_details":{"cache_write_tokens_details":{"ttl_12h_tokens":2}}}`,
			want: &ExcelBPSProbeTokens{CacheCreation30m: count(1)}},
		{name: "no aggregate synthesized from buckets", raw: `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_12h_tokens":2}},"cache_creation":{"ephemeral_5m_input_tokens":1}}`,
			want: &ExcelBPSProbeTokens{CacheCreation12h: count(2), CacheCreation5m: count(1)}},
		{name: "negative aggregate", raw: `{"cache_write_tokens":-1}`, invalid: true},
		{name: "fraction bucket", raw: `{"cache_creation":{"ephemeral_1h_input_tokens":1.5}}`, invalid: true},
		{name: "coerced string is not observed", raw: `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_30m_tokens":"3"}}}`, invalid: true},
		{name: "unsafe persisted bucket", raw: `{"input_tokens_details":{"cache_write_tokens_details":{"ttl_12h_tokens":9007199254740993}}}`, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := bpsProbeUsageTokens(json.RawMessage(tc.raw))
			if tc.invalid {
				require.Error(t, err)
				require.Nil(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
			if got != nil {
				wire, err := json.Marshal(got)
				require.NoError(t, err)
				var fields map[string]json.RawMessage
				require.NoError(t, json.Unmarshal(wire, &fields))
				for field, value := range map[string]*int64{
					"cache_creation_input_tokens": got.CacheCreation,
					"cache_creation_5m_tokens":    got.CacheCreation5m, "cache_creation_1h_tokens": got.CacheCreation1h,
					"cache_creation_30m_tokens": got.CacheCreation30m, "cache_creation_12h_tokens": got.CacheCreation12h,
				} {
					if value == nil {
						require.NotContains(t, fields, field)
					} else {
						require.Contains(t, fields, field)
					}
				}
			}
		})
	}
}

type bpsProbeUpstream struct {
	mode              string
	bodies            [][]byte
	nonce             string
	disableAfterFirst *Account
}

func (u *bpsProbeUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	if req.URL.Host != "bps.openai.com" {
		return nil, fmt.Errorf("probe used %s instead of BPS", req.URL.Host)
	}
	body, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	u.bodies = append(u.bodies, body)
	if u.nonce == "" {
		u.nonce = bpsProbeTestNonce.FindString(string(body))
	}
	if u.nonce == "" {
		return nil, fmt.Errorf("probe request is missing a nonce")
	}
	step := len(u.bodies)
	if step == 1 && u.disableAfterFirst != nil {
		u.disableAfterFirst.Extra["openai_excel_bps"] = false
	}
	var output []any
	switch step {
	case 1, 3:
		answer := u.nonce
		if step == 1 && u.mode == "wrong_basic" || step == 3 && u.mode == "wrong_final" {
			answer = "wrong answer"
		}
		output = []any{map[string]any{"type": "message", "role": "assistant", "content": []any{
			map[string]any{"type": "output_text", "text": answer},
		}}}
	case 2:
		if !bytes.Contains(body, []byte(bpsAccountProbeTool)) {
			return nil, fmt.Errorf("probe tool was not declared")
		}
		name, nonce := bpsAccountProbeTool, u.nonce
		if u.mode == "wrong_tool" {
			name = "other_tool"
		}
		if u.mode == "wrong_argument" {
			nonce = "wrong nonce"
		}
		envelope, _ := json.Marshal(map[string]any{"name": name, "arguments": map[string]any{"nonce": nonce}})
		output = []any{map[string]any{"type": "function_call", "id": "fc_probe", "call_id": "call_probe",
			"name": "run_officejs", "status": "completed", "arguments": map[string]any{"code": string(envelope), "summary": "Echo nonce"}}}
	default:
		return nil, fmt.Errorf("unexpected fourth probe request")
	}
	if step == 3 && !bytes.Contains(body, []byte(`"function_call_output"`)) {
		return nil, fmt.Errorf("probe did not return the tool result")
	}
	eventType, status := "response.completed", "completed"
	if step == 1 && u.mode == "failed_terminal" {
		eventType, status = "response.failed", "failed"
	}
	event, _ := json.Marshal(map[string]any{"type": eventType, "response": map[string]any{
		"id": fmt.Sprintf("resp_probe_%d", step), "status": status, "model": "gpt-6-astra", "output": output,
	}})
	wire := "event: " + eventType + "\ndata: " + string(event) + "\n\n"
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(wire))}, nil
}

func (u *bpsProbeUpstream) DoWithTLS(req *http.Request, proxyURL string, accountID int64, accountConcurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxyURL, accountID, accountConcurrency)
}

func bpsProbeTestService(upstream *bpsProbeUpstream) *AccountTestService {
	gateway := &OpenAIGatewayService{settingService: excelBPSSettingsTestService(), httpUpstream: upstream, cfg: &config.Config{Security: config.SecurityConfig{
		URLAllowlist: config.URLAllowlistConfig{Enabled: false},
	}}}
	return &AccountTestService{openaiGatewayService: gateway}
}

func TestExcelBPSToolProbeRoundtrip(t *testing.T) {
	gin.SetMode(gin.TestMode)
	upstream := &bpsProbeUpstream{}
	svc := bpsProbeTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil).WithContext(context.Background())
	require.NoError(t, svc.testExcelBPSToolRoundtrip(c, excelAccount(), "gpt-6-astra"))
	require.Equal(t, "text/event-stream", rec.Header().Get("Content-Type"))
	require.Len(t, upstream.bodies, 3)
	require.Contains(t, rec.Body.String(), `"type":"test_complete"`)
	require.NotContains(t, rec.Body.String(), upstream.nonce)
	require.Contains(t, string(upstream.bodies[1]), bpsAccountProbeTool)
	require.Contains(t, string(upstream.bodies[2]), `"function_call_output"`)
}

func TestExcelBPSToolProbeRejectsIncompleteStages(t *testing.T) {
	for _, tc := range []struct {
		mode      string
		wantCalls int
	}{
		{"wrong_basic", 1},
		{"failed_terminal", 1},
		{"wrong_tool", 3},
		{"wrong_argument", 2},
		{"wrong_final", 3},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			upstream := &bpsProbeUpstream{mode: tc.mode}
			svc := bpsProbeTestService(upstream)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
			require.Error(t, svc.testExcelBPSToolRoundtrip(c, excelAccount(), "gpt-6-astra"))
			require.Len(t, upstream.bodies, tc.wantCalls)
			require.Contains(t, rec.Body.String(), `"type":"error"`)
			require.NotContains(t, rec.Body.String(), `"type":"test_complete"`)
		})
	}
}

func TestExcelBPSToolProbeRejectsModelsOutsideBPS(t *testing.T) {
	account := excelAccount()
	account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
	upstream := &bpsProbeUpstream{}
	svc := bpsProbeTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
	require.Error(t, svc.testExcelBPSToolRoundtrip(c, account, "gpt-5.6-sol"))
	require.Empty(t, upstream.bodies, "probe must not fall back to another upstream")
}

func TestExcelBPSToolProbeModeDoesNotFallBackToNative(t *testing.T) {
	account := excelAccount()
	delete(account.Extra, "openai_excel_bps")
	upstream := &bpsProbeUpstream{}
	svc := bpsProbeTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
	require.Error(t, svc.testOpenAIAccountConnection(c, account, "gpt-6-astra", "", AccountTestModeBPSTools))
	require.Empty(t, upstream.bodies)
}

func TestExcelBPSToolProbeStopsWhenBPSIsDisabledBetweenSteps(t *testing.T) {
	account := excelAccount()
	upstream := &bpsProbeUpstream{disableAfterFirst: account}
	svc := bpsProbeTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
	require.Error(t, svc.testExcelBPSToolRoundtrip(c, account, "gpt-6-astra"))
	require.Len(t, upstream.bodies, 1, "the next step must not use native Codex")
	require.NotContains(t, rec.Body.String(), `"type":"test_complete"`)
}

func TestExcelBPSToolProbeRejectsNativeFallbackBeforeDispatch(t *testing.T) {
	upstream := &bpsProbeUpstream{}
	svc := bpsProbeTestService(upstream)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/300/test", nil)
	c.Set(bpsAccountProbeRequiredContextKey, true)
	body := []byte(`{"model":"gpt-6-astra","input":"test","tools":[{"type":"image_generation"}]}`)
	_, err := svc.openaiGatewayService.Forward(c, c, excelAccount(), body)
	require.ErrorContains(t, err, "probe path is unavailable")
	require.Empty(t, upstream.bodies)
}

func TestBPSProbeExactCallRequiresPlaintextDeclaredTool(t *testing.T) {
	const nonce = "12345678-1234-1234-1234-123456789abc"
	for _, tc := range []struct {
		name   string
		call   map[string]any
		wantOK bool
	}{
		{"valid", map[string]any{"type": "function_call", "name": bpsAccountProbeTool, "call_id": "call_probe", "arguments": `{"nonce":"` + nonce + `"}`, "encrypted_function_args": []string{}}, true},
		{"missing plaintext marker", map[string]any{"type": "function_call", "name": bpsAccountProbeTool, "call_id": "call_probe", "arguments": `{"nonce":"` + nonce + `"}`}, false},
		{"encrypted arguments", map[string]any{"type": "function_call", "name": bpsAccountProbeTool, "call_id": "call_probe", "arguments": `{"nonce":"` + nonce + `"}`, "encrypted_function_args": []string{"nonce"}}, false},
		{"wrong nonce", map[string]any{"type": "function_call", "name": bpsAccountProbeTool, "call_id": "call_probe", "arguments": `{"nonce":"wrong"}`, "encrypted_function_args": []string{}}, false},
		{"extra arguments", map[string]any{"type": "function_call", "name": bpsAccountProbeTool, "call_id": "call_probe", "arguments": `{"nonce":"` + nonce + `","extra":true}`, "encrypted_function_args": []string{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.call)
			require.NoError(t, err)
			id, ok := bpsProbeExactCall(bpsAccountProbeResponse{Output: []json.RawMessage{raw}}, nonce)
			require.Equal(t, tc.wantOK, ok)
			if tc.wantOK {
				require.Equal(t, "call_probe", id)
			}
		})
	}
}

func TestExcelBPSProbeAdmissionBoundsConcurrentAccounts(t *testing.T) {
	svc := &AccountTestService{}
	first, ok := svc.beginExcelBPSProbe(1)
	require.True(t, ok)
	defer first()
	_, ok = svc.beginExcelBPSProbe(1)
	require.False(t, ok, "one account must not have overlapping probes")
	second, ok := svc.beginExcelBPSProbe(2)
	require.True(t, ok)
	defer second()
	third, ok := svc.beginExcelBPSProbe(3)
	require.True(t, ok)
	defer third()
	_, ok = svc.beginExcelBPSProbe(4)
	require.False(t, ok, "only three probes may run on one instance")
	first()
	first()
	fourth, ok := svc.beginExcelBPSProbe(4)
	require.True(t, ok)
	fourth()
	_, ok = svc.beginExcelBPSProbe(0)
	require.False(t, ok)
}
