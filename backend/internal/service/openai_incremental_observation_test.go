package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const incrementalUser = `{"type":"message","role":"user","content":"private-low-entropy-prompt"}`
const incrementalOutput = `{"type":"message","id":"private-item-id","role":"assistant","status":"completed","content":[{"type":"output_text","text":"private-answer","annotations":[]}]}`
const incrementalNext = `{"type":"message","role":"user","content":"next"}`

func incrementalRequest(items string, extra ...string) []byte {
	suffix := ""
	if len(extra) > 0 {
		suffix = "," + extra[0]
	}
	return []byte(`{"model":"gpt-5.5","input":[` + items + `]` + suffix + `}`)
}
func incrementalCompleted(items string) []byte {
	return []byte(`{"type":"response.completed","response":{"id":"resp_p2","status":"completed","output":[` + items + `]}}`)
}
func incrementalCacheItem(kind, breakpoint string) string {
	field := ""
	if breakpoint != "" {
		field = `,"prompt_cache_breakpoint":` + breakpoint
	}
	part := `{"type":"input_text","text":"stable prefix"` + field + `}`
	if kind == "message" {
		return `{"type":"message","role":"developer","content":[` + part + `]}`
	}
	return `{"type":"` + kind + `","call_id":"call_1","output":[` + part + `]}`
}
func incrementalFixture(t testing.TB) (*OpenAIGatewayService, *gin.Context, *Account, openAIIncrementalObservationOptions) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	c.Request.Header.Set("session_id", "explicit-private-session")
	group := int64(9)
	c.Set("api_key", &APIKey{ID: 7, UserID: 8, GroupID: &group})
	account := &Account{ID: 10, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{"chatgpt_account_id": "private-owner"}}
	scope, _ := resolveOpenAIWSExecutionScope(c, incrementalRequest(incrementalUser), 7)
	require.NotEmpty(t, scope)
	opts := openAIIncrementalObservationOptions{ExecutionScope: scope, WindowID: "window-1", Endpoint: "https://chatgpt.com/backend-api/codex/responses", CredentialGeneration: "private-bearer", SettingsIdentity: "proxy1-http-settings1"}
	return &OpenAIGatewayService{}, c, account, opts
}
func incrementalSeed(t testing.TB, s *OpenAIGatewayService, c *gin.Context, a *Account, o openAIIncrementalObservationOptions, extra ...string) {
	t.Helper()
	txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser, extra...), o)
	txn.ObserveEvent(incrementalCompleted(incrementalOutput))
	r := txn.Finish(context.Background(), true, o.ConnID)
	require.Equal(t, "completed", r.AnchorReason)
}

func TestOpenAIIncrementalPrefixAndStateAreSeparate(t *testing.T) {
	for _, tc := range []struct {
		name, conn, binding string
		ready               bool
		want                string
	}{
		{"http", "", "", false, "unknown"},
		{"held same connection", "conn-1", "conn-1", true, "true"},
		{"lost binding", "conn-1", "", true, "false"},
		{"other connection", "conn-1", "conn-2", true, "false"},
		{"unverified lease", "conn-1", "conn-1", false, "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			o.ConnID = "conn-1"
			incrementalSeed(t, s, c, a, o)
			if tc.binding != "" {
				s.getOpenAIWSStateStore().BindResponseConn("resp_p2", tc.binding, time.Minute)
			}
			o.ConnID, o.ConnectionReady = tc.conn, tc.ready
			body := incrementalRequest(incrementalUser + "," + incrementalOutput + "," + incrementalNext)
			before := string(body)
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			txn.ObserveEvent(incrementalCompleted(""))
			report := txn.Finish(context.Background(), true, tc.conn)
			require.True(t, report.Assessable)
			require.Equal(t, "true", report.PrefixMatch)
			require.Equal(t, tc.want, report.StateReady)
			require.Equal(t, 2, report.CandidateItems)
			require.Equal(t, int64(len(incrementalUser)+len(incrementalOutput)), report.CandidateSemanticBytes)
			require.Equal(t, before, string(body), "observer must not mutate final send bytes")
			require.Equal(t, report, txn.Finish(context.Background(), false, "other"), "finish is idempotent")
			encoded, err := json.Marshal(report)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "private-")
			require.NotContains(t, string(encoded), "resp_p2")
			require.NotContains(t, string(encoded), "conn-1")
		})
	}
}

func TestOpenAIIncrementalFullHistoryIncludesOutputAndSettings(t *testing.T) {
	for _, tc := range []struct{ name, input, want string }{
		{"full", incrementalUser + "," + incrementalOutput + "," + incrementalNext, "prefix_match"},
		{"missing assistant", incrementalUser + "," + incrementalNext, "input_mismatch"},
		{"shortened", incrementalUser, "input_shortened"},
		{"empty delta", incrementalUser + "," + incrementalOutput, "prefix_match"},
		{"changed answer", incrementalUser + "," + strings.Replace(incrementalOutput, "private-answer", "different", 1), "input_mismatch"},
		{"changed output identity", incrementalUser + "," + strings.Replace(incrementalOutput, "private-item-id", "other-item-id", 1), "input_mismatch"},
		{"reordered members", `{"content":"private-low-entropy-prompt", "role":"user", "type":"message"}` + "," + incrementalOutput, "prefix_match"},
		{"missing status", incrementalUser + "," + strings.Replace(incrementalOutput, `"status":"completed",`, "", 1), "input_mismatch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			incrementalSeed(t, s, c, a, o)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(tc.input), o)
			r := txn.Finish(context.Background(), false, "")
			require.Equal(t, tc.want, r.Reason)
			require.Equal(t, tc.want == "prefix_match", r.PrefixMatch == "true")
		})
	}
	for _, tc := range []struct{ name, old, new string }{
		{"instructions", `"instructions":"one"`, `"instructions":"two"`},
		{"metadata kept", `"client_metadata":{"turn":"one"}`, `"client_metadata":{"turn":"two"}`},
		{"cache hint not ignored", `"prompt_cache_key":"one"`, `"prompt_cache_key":"two"`},
		{"cache lifetime", `"prompt_cache_options":{"ttl":"30m"}`, `"prompt_cache_options":{"ttl":"12h"}`},
		{"cache mode", `"prompt_cache_options":{"mode":"implicit"}`, `"prompt_cache_options":{"mode":"explicit"}`},
		{"cache default is not inferred", `"prompt_cache_options":{}`, `"prompt_cache_options":{"ttl":"30m"}`},
		{"cache retention", `"prompt_cache_retention":"in_memory"`, `"prompt_cache_retention":"24h"`},
		{"cache policies stay independent", `"prompt_cache_retention":"24h"`, `"prompt_cache_options":{"ttl":"12h"}`},
		{"cache options added", `"store":false`, `"store":false,"prompt_cache_options":{}`},
		{"cache options removed", `"store":false,"prompt_cache_options":{}`, `"store":false`},
		{"cache retention added", `"store":false`, `"store":false,"prompt_cache_retention":"24h"`},
		{"cache retention removed", `"store":false,"prompt_cache_retention":"24h"`, `"store":false`},
		{"cache comparison kept", `"prompt_cache_options":{"comparison_response_id":"resp_a"}`, `"prompt_cache_options":{"comparison_response_id":"resp_b"}`},
		{"reasoning default", `"reasoning":{}`, `"reasoning":{"effort":"low"}`},
		{"precision", `"max_output_tokens":9007199254740992`, `"max_output_tokens":9007199254740993`},
		{"numeric spelling narrow subset", `"temperature":1`, `"temperature":1.0`},
		{"application metadata kept", `"metadata":{"turn_id":"one"}`, `"metadata":{"turn_id":"two"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			o.ConnID, o.ConnectionReady = "conn-1", true
			incrementalSeed(t, s, c, a, o, tc.old)
			store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
			store.BindResponseConn("resp_p2", o.ConnID, time.Minute)
			for _, slot := range store.incremental {
				slot.anchor.connWriteGeneration = 1
			}
			lease := &openAIWSConnLease{conn: newOpenAIWSConn(o.ConnID, a.ID, &openAIWSCaptureConn{}, nil)}
			lease.conn.writeGeneration.Store(1)
			body := incrementalRequest(incrementalUser+","+incrementalOutput+","+incrementalNext, tc.new)
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			require.Equal(t, body, txn.wsPayload(t.Context(), body, lease))
			r := txn.Finish(context.Background(), false, "")
			require.Equal(t, "settings_changed", r.Reason)
			require.Equal(t, "false", r.PrefixMatch)
			old := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput, tc.old), o)
			require.Equal(t, "no_anchor_or_evicted", old.Finish(t.Context(), false, "").Reason)
		})
	}
}

func TestOpenAIIncrementalIsolation(t *testing.T) {
	for _, name := range []string{"key", "user", "group", "scope", "account", "credential owner", "credential generation", "authorization mode", "endpoint", "settings", "window", "model"} {
		t.Run(name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			incrementalSeed(t, s, c, a, o)
			raw, _ := c.Get("api_key")
			key := raw.(*APIKey)
			body := incrementalRequest(incrementalUser + "," + incrementalOutput)
			want := "boundary_changed"
			switch name {
			case "key":
				key.ID++
				want = "no_anchor_or_evicted"
			case "user":
				key.UserID++
				want = "no_anchor_or_evicted"
			case "group":
				*key.GroupID++
				want = "no_anchor_or_evicted"
			case "scope":
				o.ExecutionScope = "another-explicit-scope"
				want = "no_anchor_or_evicted"
			case "account":
				a.ID++
			case "credential owner":
				c.Set(codexAccountIdentitySourceContextKey, &Account{ID: 11, Platform: PlatformOpenAI, Type: AccountTypeOAuth})
			case "credential generation":
				o.CredentialGeneration = "rotated-bearer"
			case "authorization mode":
				a.Extra = map[string]any{OpenAISIWCEnabledKey: true}
			case "endpoint":
				o.Endpoint = "https://elsewhere.example/v1/responses"
			case "settings":
				o.SettingsIdentity = "different-proxy"
			case "window":
				o.WindowID = "window-2"
			case "model":
				body = []byte(strings.Replace(string(body), "gpt-5.5", "gpt-5.4", 1))
				want = "settings_changed"
			}
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			r := txn.Finish(context.Background(), false, "")
			require.Equal(t, want, r.Reason)
			require.NotEqual(t, "true", r.PrefixMatch)
		})
	}
	t.Run("window A B A cannot revive old anchor", func(t *testing.T) {
		s, c, a, o := incrementalFixture(t)
		incrementalSeed(t, s, c, a, o)
		o.WindowID = "window-2"
		incrementalSeed(t, s, c, a, o)
		o.WindowID = "window-1"
		r := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput), o).Finish(context.Background(), false, "")
		require.Equal(t, "boundary_changed", r.Reason)
	})
	t.Run("missing identity never falls back to cache or content", func(t *testing.T) {
		s, c, a, o := incrementalFixture(t)
		o.ExecutionScope = ""
		r := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser, `"prompt_cache_key":"same"`), o).Finish(context.Background(), true, "")
		require.Equal(t, "missing_scope", r.Reason)
		require.True(t, r.Candidate)
		require.False(t, r.Assessable)
		o.ExecutionScope = "explicit"
		c.Set("api_key", &APIKey{ID: 7})
		r = s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o).Finish(context.Background(), true, "")
		require.Equal(t, "missing_identity", r.Reason)
		require.Empty(t, s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore).incremental)
	})
}

func TestOpenAIIncrementalUnassessableShapes(t *testing.T) {
	for _, tc := range []struct{ name, body, reason string }{
		{"unknown item", string(incrementalRequest(`{"type":"future_item"}`)), "unknown_item"},
		{"compacted window is opaque", string(incrementalRequest(`{"type":"compaction","encrypted_content":"original-window"},` + incrementalUser)), "unknown_item"},
		{"server compaction keeps full input", string(incrementalRequest(incrementalUser, `"context_management":[{"type":"compaction","compact_threshold":200000}]`)), "unknown_field"},
		{"named lane cannot reuse default anchor", string(incrementalRequest(incrementalUser, `"stream_id":"planner"`)), "unknown_field"},
		{"unknown field", string(incrementalRequest(strings.Replace(incrementalUser, "}", `,"future":true}`, 1))), "unknown_field"},
		{"late tool result metadata", string(incrementalRequest(strings.Replace(incrementalUser, "}", `,"tool_result_metadata":{"late":true}}`, 1))), "unknown_field"},
		{"unknown joined field", string(incrementalRequest(incrementalUser, `"model input":true`)), "unknown_field"},
		{"missing type", string(incrementalRequest(`{"role":"user","content":"hi"}`)), "unknown_item"},
		{"null type", string(incrementalRequest(`{"type":null,"role":"user","content":"hi"}`)), "unknown_item"},
		{"null optional", string(incrementalRequest(incrementalUser, `"reasoning":null`)), "ambiguous_null"},
		{"bad content", string(incrementalRequest(`{"type":"message","role":"user","content":3}`)), "unknown_shape"},
		{"duplicate escaped", `{"model":"gpt-5.5","\u006dodel":"other","input":[]}`, "duplicate_json"},
		{"nested duplicate", string(incrementalRequest(`{"type":"message","role":"user","content":"a","content":"b"}`)), "duplicate_json"},
		{"schema duplicate", string(incrementalRequest(incrementalUser, `"tools":[{"type":"function","name":"echo","parameters":{"type":"object","type":"array"}}]`)), "duplicate_json"},
		{"null parent", string(incrementalRequest(incrementalUser, `"previous_response_id":null`)), "ambiguous_null"},
		{"nonarray input", `{"model":"gpt-5.5","input":"hello"}`, "unknown_shape"},
		{"trailing JSON", string(incrementalRequest(incrementalUser)) + "{}", "invalid_json"},
		{"unpaired surrogate", string(incrementalRequest(`{"type":"message","role":"user","content":"\ud800"}`)), "invalid_json"},
		{"unknown nested part", string(incrementalRequest(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","future":true}]}`)), "unknown_field"},
		{"breakpoint is not a message field", string(incrementalRequest(`{"type":"message","role":"user","content":"hi","prompt_cache_breakpoint":{"mode":"explicit"}}`)), "unknown_field"},
		{"breakpoint is not a tool result field", string(incrementalRequest(`{"type":"function_call_output","call_id":"call_1","output":"ok","prompt_cache_breakpoint":{"mode":"explicit"}}`)), "unknown_field"},
		{"breakpoint is not an output text field", string(incrementalRequest(`{"type":"message","role":"assistant","content":[{"type":"output_text","text":"ok","prompt_cache_breakpoint":{"mode":"explicit"}}]}`)), "unknown_field"},
		{"breakpoint is not a summary field", string(incrementalRequest(`{"type":"reasoning","encrypted_content":"opaque","summary":[{"type":"summary_text","text":"ok","prompt_cache_breakpoint":{"mode":"explicit"}}]}`)), "unknown_field"},
		{"breakpoint is not an additional tools field", string(incrementalRequest(`{"type":"additional_tools","tools":[],"prompt_cache_breakpoint":{"mode":"explicit"}}`)), "unknown_field"},
		{"breakpoint is not a tool definition field", string(incrementalRequest(incrementalUser, `"tools":[{"type":"function","name":"echo","parameters":{},"prompt_cache_breakpoint":{"mode":"explicit"}}]`)), "unknown_field"},
		{"breakpoint is not a namespaced tool field", string(incrementalRequest(incrementalUser, `"tools":[{"type":"namespace","name":"tools","tools":[{"type":"custom","name":"echo","prompt_cache_breakpoint":{"mode":"explicit"}}]}]`)), "unknown_field"},
		{"tool result is not output text", string(incrementalRequest(`{"type":"function_call_output","call_id":"call_1","output":[{"type":"output_text","text":"ok"}]}`)), "unknown_item"},
		{"unknown tool result content field", string(incrementalRequest(`{"type":"function_call_output","call_id":"call_1","output":[{"type":"input_text","text":"ok","future":true}]}`)), "unknown_field"},
		{"tool result missing output", string(incrementalRequest(`{"type":"function_call_output","call_id":"call_1"}`)), "unknown_shape"},
		{"tool result object", string(incrementalRequest(`{"type":"custom_tool_call_output","call_id":"call_1","output":{"text":"ok"}}`)), "unknown_shape"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			incrementalSeed(t, s, c, a, o)
			body := []byte(tc.body)
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			lease := &openAIWSConnLease{conn: newOpenAIWSConn("conn-1", a.ID, &openAIWSCaptureConn{}, nil)}
			require.Equal(t, tc.body, string(txn.wsPayload(t.Context(), body, lease)))
			require.Equal(t, tc.body, string(body))
			txn.ObserveEvent(incrementalCompleted(incrementalOutput))
			r := txn.Finish(context.Background(), true, "")
			require.Equal(t, tc.reason, r.Reason)
			require.False(t, r.Assessable)
			require.Equal(t, "unknown", r.PrefixMatch)
			require.Nil(t, s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore).incremental[txn.key].anchor)
		})
	}
	for _, field := range []string{`"previous_response_id":"resp_native"`, `"generate":false`, `"prompt_cache_options":{"prewarm":true}`, `"generate":true,"prompt_cache_options":{"prewarm":true}`} {
		s, c, a, o := incrementalFixture(t)
		incrementalSeed(t, s, c, a, o)
		txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser, field), o)
		require.False(t, txn.Finish(context.Background(), false, "").Candidate)
		require.Nil(t, s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore).incremental[txn.key].anchor)
	}
}

func TestOpenAIIncrementalOnlyCompleteSuccessfulOutputAnchors(t *testing.T) {
	for _, tc := range []struct {
		name            string
		events          []string
		success, cancel bool
		reason          string
	}{
		{"completed", []string{string(incrementalCompleted(incrementalOutput))}, true, false, "completed"},
		{"missing terminal", nil, true, false, "output_incomplete"},
		{"missing output", []string{`{"type":"response.completed","response":{"id":"r","status":"completed"}}`}, true, false, "output_incomplete"},
		{"missing status", []string{`{"type":"response.completed","response":{"id":"r","output":[]}}`}, true, false, "output_incomplete"},
		{"null output", []string{`{"type":"response.completed","response":{"id":"r","status":"completed","output":null}}`}, true, false, "output_incomplete"},
		{"switched response", []string{`{"type":"response.created","response":{"id":"different"}}`, string(incrementalCompleted(incrementalOutput))}, true, false, "response_changed"},
		{"failed", []string{`{"type":"response.failed"}`}, true, false, "unsuccessful_terminal"},
		{"incomplete", []string{`{"type":"response.incomplete"}`}, true, false, "unsuccessful_terminal"},
		{"cancelled", []string{`{"type":"response.cancelled"}`}, true, false, "unsuccessful_terminal"},
		{"failed write", []string{string(incrementalCompleted(incrementalOutput))}, false, false, "failed"},
		{"cancelled context", []string{string(incrementalCompleted(incrementalOutput))}, true, true, "cancelled"},
		{"unknown output", []string{string(incrementalCompleted(`{"type":"unknown"}`))}, true, false, "unknown_item"},
		{"duplicate output id", []string{string(incrementalCompleted(incrementalOutput + "," + incrementalOutput))}, true, false, "output_incomplete"},
		{"omitted observed item", []string{`{"type":"response.output_item.added","output_index":1,"item":{"id":"private-item-id","type":"message"}}`, string(incrementalCompleted(incrementalOutput))}, true, false, "output_incomplete"},
		{"different observed id", []string{`{"type":"response.output_text.delta","output_index":0,"item_id":"other","delta":"x"}`, string(incrementalCompleted(incrementalOutput))}, true, false, "output_incomplete"},
		{"late event", []string{string(incrementalCompleted(incrementalOutput)), `{"type":"response.output_item.done"}`}, true, false, "event_after_completed"},
		{"duplicate terminal JSON", []string{`{"type":"response.completed","response":{"id":"r","status":"completed","output":[],"output":[]}}`}, true, false, "duplicate_json"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			for _, event := range tc.events {
				txn.ObserveEvent([]byte(event))
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			r := txn.Finish(ctx, tc.success, "")
			require.Equal(t, tc.reason, r.AnchorReason)
			store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
			require.Equal(t, tc.reason == "completed", store.incremental[txn.key].anchor != nil)
			require.Nil(t, store.incremental[txn.key].pending)
		})
	}
	t.Run("reasoning and calls kept exactly", func(t *testing.T) {
		s, c, a, o := incrementalFixture(t)
		output := `{"type":"reasoning","id":"rs1","summary":[],"encrypted_content":"private-cipher"},{"type":"function_call","id":"fc1","call_id":"fc_1","name":"echo","arguments":"{}","status":"completed"}`
		txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
		txn.ObserveEvent(incrementalCompleted(output))
		require.Equal(t, "completed", txn.Finish(context.Background(), true, "").AnchorReason)
		next := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+output), o)
		require.Equal(t, "prefix_match", next.Finish(context.Background(), false, "").Reason)
	})
}

func TestOpenAIIncrementalConcurrentAndLateCompletion(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	incrementalSeed(t, s, c, a, o)
	body := incrementalRequest(incrementalUser + "," + incrementalOutput)
	first := s.beginOpenAIIncrementalObservation(c, a, body, o)
	second := s.beginOpenAIIncrementalObservation(c, a, body, o)
	first.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "concurrent", first.Finish(context.Background(), true, "").Reason)
	third := s.beginOpenAIIncrementalObservation(c, a, body, o)
	require.Equal(t, "concurrent", third.Finish(context.Background(), false, "").Reason)
	second.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "concurrent", second.Finish(context.Background(), true, "").Reason)
	next := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
	require.Equal(t, "no_anchor_or_evicted", next.report.Reason)
	next.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "completed", next.Finish(context.Background(), true, "").AnchorReason)
	require.Equal(t, "concurrent", first.Finish(context.Background(), true, "").Reason)
	current := s.beginOpenAIIncrementalObservation(c, a, body, o)
	require.Equal(t, "prefix_match", current.Finish(context.Background(), false, "").Reason)
}

func TestOpenAIIncrementalExpiryAndPressureCannotRestoreLateState(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
	store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
	store.incrementalMu.Lock()
	store.incremental[txn.key].expires = time.Now().Add(-time.Second)
	store.incrementalMu.Unlock()
	replacement := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalNext), o)
	require.Equal(t, "expired", replacement.report.Reason)
	txn.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "expired_or_evicted", txn.Finish(context.Background(), true, "").Reason)
	replacement.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "completed", replacement.Finish(context.Background(), true, "").AnchorReason)
	verify := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalNext+","+incrementalOutput), o)
	require.Equal(t, "prefix_match", verify.Finish(context.Background(), false, "").Reason)

	var handles []*openAIIncrementalObservation
	for i := 0; i < 90; i++ {
		o.ExecutionScope = fmt.Sprintf("scope-%d", i)
		handles = append(handles, s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o))
		require.LessOrEqual(t, store.incrementalBytes, openAIIncrementalMaxMemory)
	}
	require.Less(t, len(store.incremental), len(handles), "in-flight reservations are globally bounded per store")
	for _, h := range handles {
		h.ObserveEvent(incrementalCompleted(incrementalOutput))
		h.Finish(context.Background(), true, "")
	}
	require.Equal(t, "expired_or_evicted", handles[0].report.Reason)
	sum := 0
	for _, slot := range store.incremental {
		sum += slot.bytes
		require.Nil(t, slot.pending)
	}
	require.Equal(t, sum, store.incrementalBytes)
	require.LessOrEqual(t, sum, openAIIncrementalMaxMemory)
}

func TestOpenAIIncrementalLimitsAndPrivacy(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	for _, tc := range []struct {
		body   []byte
		reason string
	}{
		{[]byte(strings.Repeat(" ", openAIIncrementalMaxBody+1)), "body_limit"},
		{incrementalRequest(strings.TrimSuffix(strings.Repeat(incrementalUser+",", openAIIncrementalMaxItems+1), ",")), "item_limit"},
	} {
		txn := s.beginOpenAIIncrementalObservation(c, a, tc.body, o)
		require.Equal(t, tc.reason, txn.Finish(context.Background(), false, "").Reason)
	}
	for i := 0; i < openAIIncrementalMaxEntries+5; i++ {
		o.ExecutionScope = fmt.Sprintf("scope-%d", i)
		txn := s.beginOpenAIIncrementalObservation(c, a, []byte(`{"unknown":true}`), o)
		txn.Finish(context.Background(), false, "")
	}
	store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
	require.LessOrEqual(t, len(store.incremental), openAIIncrementalMaxEntries)
	require.LessOrEqual(t, store.incrementalBytes, openAIIncrementalMaxMemory)
	o.ExecutionScope = "privacy-scope"
	incrementalSeed(t, s, c, a, o)
	for _, slot := range store.incremental {
		require.Nil(t, slot.pending)
		if slot.anchor != nil {
			dump := fmt.Sprintf("%+v", *slot.anchor)
			for _, secret := range []string{"private-low-entropy-prompt", "private-answer", "private-bearer", "private-owner", "private-item-id", "privacy-scope"} {
				require.NotContains(t, dump, secret)
			}
			require.Len(t, slot.anchor.items, 2)
		}
	}
	plain := sha256.Sum256([]byte("yes"))
	require.NotEqual(t, plain, openAIIncrementalHash("item", []byte("yes")))
	require.NotEqual(t, openAIIncrementalHash("item", []byte("yes")), openAIIncrementalHash("scope", []byte("yes")))
	require.NotEqual(t, openAIIncrementalHash("item", []byte("a"), []byte("bc")), openAIIncrementalHash("item", []byte("ab"), []byte("c")))
}

func TestOpenAIIncrementalRaceAndIdempotentFinish(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	const count = 24
	begins := make(chan *openAIIncrementalObservation, count)
	finish := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			begins <- txn
			<-finish
			txn.ObserveEvent(incrementalCompleted(incrementalOutput))
			txn.Finish(context.Background(), true, "")
		}()
	}
	var txns []*openAIIncrementalObservation
	for i := 0; i < count; i++ {
		txns = append(txns, <-begins)
	}
	close(finish)
	wg.Wait()
	for _, txn := range txns {
		require.Equal(t, "concurrent", txn.Finish(context.Background(), true, "").Reason)
	}
	store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
	for _, slot := range store.incremental {
		require.Zero(t, slot.active)
		require.Nil(t, slot.anchor)
		require.Nil(t, slot.pending)
	}
	incrementalSeed(t, s, c, a, o)
	txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput), o)
	txn.ObserveEvent(incrementalCompleted(incrementalNext))
	reports := make(chan openAIIncrementalObservationReport, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); reports <- txn.Finish(context.Background(), true, "") }()
	}
	wg.Wait()
	close(reports)
	for r := range reports {
		require.Equal(t, "prefix_match", r.Reason)
		require.Equal(t, "completed", r.AnchorReason)
	}
}

func TestOpenAIIncrementalClassificationPrecedesIdentity(t *testing.T) {
	for _, tc := range []struct{ field, reason string }{
		{`"previous_response_id":"resp_native"`, "native_continuation"},
		{`"generate":false`, "prewarm"},
		{`"prompt_cache_options":{"prewarm":true}`, "prewarm"},
		{`"generate":true,"prompt_cache_options":{"prewarm":true}`, "prewarm"},
	} {
		for _, missing := range []string{"scope", "auth", "account", "boundary"} {
			t.Run(tc.reason+"/"+missing, func(t *testing.T) {
				s, c, a, o := incrementalFixture(t)
				switch missing {
				case "scope":
					o.ExecutionScope = ""
				case "auth":
					c.Set("api_key", nil)
				case "account":
					a = nil
				case "boundary":
					o.CredentialGeneration = ""
				}
				txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser, tc.field), o)
				report := txn.Finish(context.Background(), true, "")
				require.False(t, report.Candidate)
				require.False(t, report.Assessable)
				require.Equal(t, tc.reason, report.Reason)
				require.Equal(t, "unknown", report.StateReady)
			})
		}
	}
}

func TestOpenAIIncrementalBufferedResponse(t *testing.T) {
	body := []byte(`{"id":"resp_p2","status":"completed","output":[` + incrementalOutput + `]}`)
	t.Run("same complete snapshot as SSE", func(t *testing.T) {
		var reports []openAIIncrementalObservationReport
		for _, buffered := range []bool{false, true} {
			s, c, a, o := incrementalFixture(t)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			if buffered {
				txn.ObserveResponse(body)
			} else {
				txn.ObserveEvent(incrementalCompleted(incrementalOutput))
			}
			require.Equal(t, "completed", txn.Finish(context.Background(), true, "").AnchorReason)
			next := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput+","+incrementalNext), o)
			reports = append(reports, next.Finish(context.Background(), false, ""))
		}
		require.Equal(t, reports[0], reports[1])
		require.Equal(t, "true", reports[0].PrefixMatch)
		require.Equal(t, "unknown", reports[0].StateReady)
	})
	for _, tc := range []struct{ name, body, reason string }{
		{"partial", `{"id":"resp_p2","status":"in_progress","output":[]}`, "output_incomplete"},
		{"missing output", `{"id":"resp_p2","status":"completed"}`, "output_incomplete"},
		{"duplicate", `{"id":"resp_p2","status":"completed","output":[],"output":[]}`, "duplicate_json"},
		{"null", `{"id":"resp_p2","status":"completed","output":null}`, "output_incomplete"},
		{"failure", `{"id":"resp_p2","status":"completed","output":[],"error":{}}`, "unsuccessful_terminal"},
		{"unknown", `{"id":"resp_p2","status":"completed","output":[],"future":true}`, "unknown_field"},
		{"malformed cache options", `{"id":"resp_p2","status":"completed","output":[],"prompt_cache_options":[]}`, "unknown_shape"},
		{"unknown cache lifetime", `{"id":"resp_p2","status":"completed","output":[],"prompt_cache_options":{"ttl":"forever"}}`, "unknown_shape"},
		{"unknown cache option", `{"id":"resp_p2","status":"completed","output":[],"prompt_cache_options":{"ttl":"30m","future":true}}`, "unknown_field"},
		{"event envelope is not a response", string(incrementalCompleted(incrementalOutput)), "unknown_field"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			txn.ObserveResponse([]byte(tc.body))
			require.Equal(t, tc.reason, txn.Finish(context.Background(), true, "").AnchorReason)
			require.Nil(t, txn.store.incremental[txn.key].anchor)
		})
	}
	for _, success := range []bool{false, true} {
		s, c, a, o := incrementalFixture(t)
		txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
		txn.ObserveResponse(body)
		ctx, cancel := context.WithCancel(context.Background())
		if success {
			cancel()
		}
		r := txn.Finish(ctx, success, "")
		cancel()
		require.NotEqual(t, "completed", r.AnchorReason, "downstream failure/cancellation cannot establish a buffered anchor")
	}
}

func TestOpenAIIncrementalDeltaEnvelopeAndFinalSnapshot(t *testing.T) {
	base := `{"type":"response.output_text.delta","response_id":"resp_p2","output_index":0,"item_id":"private-item-id","delta":"private-delta"}`
	for _, tc := range []struct{ name, event, output, reason string }{
		{"known flat delta", base, incrementalOutput, "completed"},
		{"unknown envelope", strings.TrimSuffix(base, "}") + `,"future":true}`, incrementalOutput, "unknown_field"},
		{"duplicate escaped key", strings.TrimSuffix(base, "}") + `,"\u0064elta":"other"}`, incrementalOutput, "duplicate_json"},
		{"nested delta", strings.Replace(base, `"private-delta"`, `{"a":1,"a":2}`, 1), incrementalOutput, "unknown_shape"},
		{"null delta", strings.Replace(base, `"private-delta"`, `null`, 1), incrementalOutput, "unknown_shape"},
		{"ambiguous identity", strings.Replace(base, "private-item-id", `\ud800`, 1), incrementalOutput, "invalid_json"},
		{"unknown final output", base, `{"type":"future"}`, "unknown_item"},
		{"bad final Unicode", base, strings.Replace(incrementalOutput, "private-answer", `\ud800`, 1), "invalid_json"},
		{"delta text is not accumulated", strings.Replace(base, "private-delta", "different-text", 1), incrementalOutput, "completed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			txn.ObserveEvent([]byte(tc.event))
			txn.ObserveEvent(incrementalCompleted(tc.output))
			r := txn.Finish(context.Background(), true, "")
			require.Equal(t, tc.reason, r.AnchorReason)
		})
	}
}

func BenchmarkOpenAIIncrementalDelta(b *testing.B) {
	for _, size := range []int{32, 4096, 65536} {
		payload, _ := json.Marshal(map[string]any{"type": "response.output_text.delta", "item_id": "private-item-id", "output_index": 0, "delta": strings.Repeat("x", size)})
		b.Run(fmt.Sprintf("Observe/%d", size), func(b *testing.B) {
			s, c, a, o := incrementalFixture(b)
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				txn.ObserveEvent(payload)
			}
			b.StopTimer()
			require.Empty(b, txn.store.incremental[txn.key].pending.outputReason)
			txn.Finish(context.Background(), false, "")
		})
		b.Run(fmt.Sprintf("StrictJSONOnly/%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, why := openAIIncrementalJSON(payload); why != "" {
					b.Fatal(why)
				}
			}
		})
	}
	b.Run("AlreadyUnassessable", func(b *testing.B) {
		s, c, a, o := incrementalFixture(b)
		txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser), o)
		txn.ObserveEvent([]byte(`{"type":"future"}`))
		payload := []byte(strings.Repeat("x", 1<<20))
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			txn.ObserveEvent(payload)
		}
	})
}

func BenchmarkOpenAIIncrementalSnapshots(b *testing.B) {
	for _, size := range []int{8192, 131072, 1048576, openAIIncrementalMaxBody - 512} {
		input := strings.Replace(incrementalUser, "private-low-entropy-prompt", strings.Repeat("x", size), 1)
		body := incrementalRequest(input)
		b.Run(fmt.Sprintf("Begin/%d", size), func(b *testing.B) {
			s, c, a, o := incrementalFixture(b)
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
				if r := txn.Finish(context.Background(), false, ""); r.AnchorReason != "failed" {
					b.Fatal(r)
				}
			}
		})
		output := strings.Replace(incrementalOutput, "private-answer", strings.Repeat("x", size), 1)
		response := []byte(`{"id":"r","status":"completed","output":[` + output + `]}`)
		b.Run(fmt.Sprintf("CompletedRead/%d", size), func(b *testing.B) {
			b.SetBytes(int64(len(response)))
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if e := openAIIncrementalReadEvent(response, true); !e.completed {
					b.Fatal(e.reason)
				}
			}
		})
	}
}

func TestOpenAIIncrementalCompactionInvalidatesScope(t *testing.T) {
	for _, inFlight := range []bool{false, true} {
		t.Run(fmt.Sprintf("in_flight_%t", inFlight), func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			incrementalSeed(t, s, c, a, o)
			history := incrementalRequest(incrementalUser + "," + incrementalOutput)
			var old *openAIIncrementalObservation
			if inFlight {
				old = s.beginOpenAIIncrementalObservation(c, a, history, o)
			}
			o.Compaction = true
			// Compaction invalidates authenticated scope even when no candidate
			// endpoint/credential is available (e.g. a rejected compact attempt).
			o.Endpoint, o.CredentialGeneration, o.SettingsIdentity = "", "", ""
			compact := s.beginOpenAIIncrementalObservation(c, a, []byte(`{"model":"gpt-5.5","input":"opaque compact input"}`), o)
			require.False(t, compact.report.Candidate)
			require.Equal(t, "compact", compact.report.Reason)
			require.Nil(t, compact.store.incremental[compact.key].anchor)
			compact.ObserveResponse([]byte(`{"id":"r","status":"completed","output":[]}`))
			report := compact.Finish(context.Background(), true, "")
			require.Equal(t, "compact", report.Reason)
			require.False(t, report.Candidate)
			if old != nil {
				old.ObserveEvent(incrementalCompleted(incrementalOutput))
				require.Equal(t, "concurrent", old.Finish(context.Background(), true, "").Reason)
			}
			_, _, _, o = incrementalFixture(t)
			next := s.beginOpenAIIncrementalObservation(c, a, history, o)
			require.Equal(t, "no_anchor_or_evicted", next.Finish(context.Background(), false, "").Reason)
		})
	}
	t.Run("classification without scope", func(t *testing.T) {
		s, c, a, o := incrementalFixture(t)
		o.Compaction, o.ExecutionScope = true, ""
		txn := s.beginOpenAIIncrementalObservation(c, a, nil, o)
		report := txn.Finish(context.Background(), true, "")
		require.Equal(t, "compact", report.Reason)
		require.False(t, report.Candidate)
	})
}

func TestOpenAIIncrementalBusyDoesNotWaitOrKeepAnchor(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	incrementalSeed(t, s, c, a, o)
	require.True(t, acquireOpenAIIncrementalParsing(openAIIncrementalParseBudget))
	func() {
		defer openAIIncrementalParsing.Add(-openAIIncrementalParseBudget)
		txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput), o)
		require.Equal(t, "observation_busy", txn.Finish(context.Background(), true, "").Reason)
		require.Equal(t, "observation_busy", openAIIncrementalReadEvent(incrementalCompleted(incrementalOutput), false).reason)
	}()
	next := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput), o)
	require.Equal(t, "no_anchor_or_evicted", next.Finish(context.Background(), false, "").Reason)
}

func TestOpenAIIncrementalSIWCCandidateSeparatesPrefixFromConnectionState(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	a.Extra = map[string]any{OpenAISIWCEnabledKey: true}
	o.Endpoint = "https://api.openai.com/v1/responses"
	incrementalSeed(t, s, c, a, o)
	report := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput+","+incrementalNext), o).Finish(context.Background(), false, "")
	require.True(t, report.Candidate)
	require.True(t, report.Assessable)
	require.Equal(t, "prefix_match", report.Reason)
	require.Equal(t, "true", report.PrefixMatch)
	require.Equal(t, "unknown", report.StateReady)
}

func TestOpenAIIncrementalGeneratedTurnMetadata(t *testing.T) {
	s, c, a, o := incrementalFixture(t)
	c.Request.Header.Set("thread-id", "same-thread")
	makeBody := func(input string) []byte {
		body, _, err := normalizeCodexRequestMetadata(incrementalRequest(input), c.Request.Header.Clone(), a, 7, false)
		require.NoError(t, err)
		return body
	}
	first := makeBody(incrementalUser)
	initial := s.beginOpenAIIncrementalObservation(c, a, first, o)
	initial.ObserveEvent(incrementalCompleted(incrementalOutput))
	require.Equal(t, "completed", initial.Finish(t.Context(), true, "").AnchorReason)
	next := makeBody(incrementalUser + "," + incrementalOutput + "," + incrementalNext)
	require.NotEqual(t, gjson.GetBytes(first, "client_metadata.turn_id").String(), gjson.GetBytes(next, "client_metadata.turn_id").String())
	before := string(next)
	txn := s.beginOpenAIIncrementalObservation(c, a, next, o)
	require.Equal(t, "true", txn.Finish(t.Context(), false, "").PrefixMatch)
	require.Equal(t, before, string(next), "comparison must preserve actual per-turn wire metadata")

	for _, field := range []string{"thread_id", "window_id", "context_window_id", "parent_thread_id", "unknown_extension"} {
		t.Run(field, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			metadata := func(value string) string {
				embedded, err := json.Marshal(map[string]any{"turn_id": value, "turn_started_at_unix_ms": 1, field: value})
				require.NoError(t, err)
				cm, err := json.Marshal(map[string]string{openAIWSTurnMetadataHeader: string(embedded)})
				require.NoError(t, err)
				return `"client_metadata":` + string(cm)
			}
			incrementalSeed(t, s, c, a, o, metadata("old"))
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput, metadata("new")), o)
			require.Equal(t, "settings_changed", txn.Finish(t.Context(), false, "").Reason)
		})
	}
	for _, malformed := range []string{`{"turn_id":1}`, `{"turn_started_at_unix_ms":null}`, `{"turn_id":"a","turn_id":"b"}`, `[]`} {
		cm, err := json.Marshal(map[string]string{openAIWSTurnMetadataHeader: malformed})
		require.NoError(t, err)
		_, reason, _ := openAIIncrementalRequest(incrementalRequest(incrementalUser, `"client_metadata":`+string(cm)))
		require.Equal(t, "invalid_turn_metadata", reason)
	}
	s, c, a, o = incrementalFixture(t)
	incrementalSeed(t, s, c, a, o, `"stream_options":{"include_usage":true}`)
	txn = s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(incrementalUser+","+incrementalOutput, `"stream_options":{"include_usage":false}`), o)
	require.Equal(t, "true", txn.Finish(t.Context(), false, "").PrefixMatch)
}

func TestOpenAIIncrementalWSPayloadRevalidatesState(t *testing.T) {
	for _, mode := range []string{"same", "binding_changed", "released", "closed", "unusable", "old_connection", "cancelled", "concurrent", "expired", "evicted", "changed_body", "finished", "busy", "intervening_write"} {
		t.Run(mode, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			o.ConnID, o.ConnectionReady = "conn-1", true
			incrementalSeed(t, s, c, a, o)
			store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
			store.BindResponseConn("resp_p2", "conn-1", time.Minute)
			require.Len(t, store.incremental, 1)
			for _, slot := range store.incremental {
				slot.anchor.connWriteGeneration = 1
			}
			body := incrementalRequest(incrementalUser + "," + incrementalOutput + "," + incrementalNext)
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			lease := &openAIWSConnLease{conn: newOpenAIWSConn("conn-1", a.ID, &openAIWSCaptureConn{}, nil)}
			lease.conn.writeGeneration.Store(1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch mode {
			case "binding_changed":
				store.BindResponseConn("resp_p2", "conn-2", time.Minute)
			case "released":
				lease.released.Store(true)
			case "closed":
				close(lease.conn.closedCh)
			case "unusable":
				lease.conn.unusable.Store(true)
			case "old_connection":
				lease.conn.createdAtNano.Store(time.Now().Add(-openAIWSConnMaxAge - time.Second).UnixNano())
			case "cancelled":
				cancel()
			case "concurrent":
				other := s.beginOpenAIIncrementalObservation(c, a, body, o)
				defer other.Finish(t.Context(), false, "")
			case "expired":
				store.incremental[txn.key].expires = time.Now().Add(-time.Second)
			case "evicted":
				store.deleteIncrementalSlot(txn.key)
			case "changed_body":
				body = append(body, ' ')
			case "finished":
				txn.Finish(t.Context(), false, "")
			case "busy":
				require.True(t, acquireOpenAIIncrementalParsing(openAIIncrementalParseBudget))
				defer openAIIncrementalParsing.Add(-openAIIncrementalParseBudget)
			case "intervening_write":
				require.NoError(t, lease.WriteJSONContext(ctx, map[string]any{"type": "response.create", "generate": false}))
			}
			wire := txn.wsPayload(ctx, body, lease)
			if mode == "same" {
				require.Equal(t, "resp_p2", gjson.GetBytes(wire, "previous_response_id").String())
				require.JSONEq(t, "["+incrementalNext+"]", gjson.GetBytes(wire, "input").Raw)
				require.Less(t, len(wire), len(body))
				require.NoError(t, lease.WriteJSONContext(ctx, json.RawMessage(wire)))
				txn.markWSRequestWritten(lease)
				// The next anchor includes full input, never just the sent suffix.
				txn.ObserveEvent(incrementalCompleted(incrementalNext))
				require.Equal(t, "completed", txn.Finish(t.Context(), true, o.ConnID).AnchorReason)
				require.Len(t, store.incremental[txn.key].anchor.items, 4)
				require.Equal(t, uint64(2), store.incremental[txn.key].anchor.connWriteGeneration)
			} else {
				require.Equal(t, body, wire)
				txn.Finish(ctx, false, "")
			}
		})
	}
}

func TestOpenAIIncrementalPromptCacheWSPayload(t *testing.T) {
	cases := []struct{ name, input, nextInput, extra, want string }{
		{"empty options", incrementalUser, "", `"prompt_cache_options":{}`, "prefix_match"},
		{"30m implicit", incrementalUser, "", `"prompt_cache_options":{"mode":"implicit","ttl":"30m","prewarm":false}`, "prefix_match"},
		{"12h explicit", incrementalUser, "", `"prompt_cache_options":{"mode":"explicit","ttl":"12h"}`, "prefix_match"},
		{"legacy memory", incrementalUser, "", `"prompt_cache_retention":"in_memory"`, "prefix_match"},
		{"legacy 24h", incrementalUser, "", `"prompt_cache_retention":"24h"`, "prefix_match"},
		{"independent policies", incrementalUser, "", `"prompt_cache_retention":"24h","prompt_cache_options":{"ttl":"30m"}`, "prefix_match"},
		{"comparison response", incrementalUser, "", `"prompt_cache_options":{"comparison_response_id":"resp_comparison"}`, "prefix_match"},
		{"moved breakpoint",
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"a","prompt_cache_breakpoint":{"mode":"explicit"}},{"type":"input_text","text":"b"}]}`,
			`{"type":"message","role":"user","content":[{"type":"input_text","text":"a"},{"type":"input_text","text":"b","prompt_cache_breakpoint":{"mode":"explicit"}}]}`,
			`"prompt_cache_options":{"mode":"explicit"}`, "input_mismatch"},
	}
	for _, kind := range []string{"message", "function_call_output", "custom_tool_call_output"} {
		for _, pair := range []struct{ name, old, next, want string }{
			{"inherited ttl", `{"mode":"explicit"}`, `{"mode":"explicit"}`, "prefix_match"},
			{"30m ttl", `{"mode":"explicit","ttl":"30m"}`, `{"ttl":"30m", "mode":"explicit"}`, "prefix_match"},
			{"12h ttl", `{"mode":"explicit","ttl":"12h"}`, `{"mode":"explicit","ttl":"12h"}`, "prefix_match"},
			{"changed ttl", `{"mode":"explicit","ttl":"30m"}`, `{"mode":"explicit","ttl":"12h"}`, "input_mismatch"},
			{"default is not inferred", `{"mode":"explicit"}`, `{"mode":"explicit","ttl":"30m"}`, "input_mismatch"},
			{"added breakpoint", "", `{"mode":"explicit"}`, "input_mismatch"},
			{"removed breakpoint", `{"mode":"explicit"}`, "", "input_mismatch"},
		} {
			cases = append(cases, struct{ name, input, nextInput, extra, want string }{
				kind + "/" + pair.name, incrementalCacheItem(kind, pair.old), incrementalCacheItem(kind, pair.next),
				`"prompt_cache_options":{"mode":"explicit","ttl":"30m"}`, pair.want,
			})
		}
	}
	for _, tc := range cases {
		for _, buffered := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/buffered=%t", tc.name, buffered), func(t *testing.T) {
				s, c, a, o := incrementalFixture(t)
				o.ConnID, o.ConnectionReady = "conn-1", true
				seedBody := incrementalRequest(tc.input, tc.extra)
				seed := s.beginOpenAIIncrementalObservation(c, a, seedBody, o)
				// Both completion transports can echo the cache policy.
				response := []byte(`{"id":"resp_p2","status":"completed","output":[` + incrementalOutput + `],` + tc.extra + `}`)
				if buffered {
					seed.ObserveResponse(response)
				} else {
					seed.ObserveEvent([]byte(`{"type":"response.completed","response":` + string(response) + `}`))
				}
				require.Equal(t, "completed", seed.Finish(t.Context(), true, o.ConnID).AnchorReason)
				store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
				store.BindResponseConn("resp_p2", o.ConnID, time.Minute)
				store.incremental[seed.key].anchor.connWriteGeneration = 1
				lease := &openAIWSConnLease{conn: newOpenAIWSConn(o.ConnID, a.ID, &openAIWSCaptureConn{}, nil)}
				lease.conn.writeGeneration.Store(1)
				nextInput := tc.nextInput
				if nextInput == "" {
					nextInput = tc.input
				}
				// A new breakpoint in the suffix must survive delta generation.
				suffix := incrementalCacheItem("message", `{"mode":"explicit","ttl":"12h"}`) + "," + incrementalNext
				extra := strings.ReplaceAll(tc.extra, `"mode":"explicit","ttl":"30m"`, `"ttl":"30m", "mode":"explicit"`)
				body := incrementalRequest(nextInput+","+incrementalOutput+","+suffix, extra)
				before := string(body)
				txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
				wire := txn.wsPayload(t.Context(), body, lease)
				if tc.want == "prefix_match" {
					require.Equal(t, "resp_p2", gjson.GetBytes(wire, "previous_response_id").String())
					require.JSONEq(t, "["+suffix+"]", gjson.GetBytes(wire, "input").Raw)
					require.Less(t, len(wire), len(body))
				} else {
					require.Equal(t, body, wire)
				}
				for _, field := range []string{"prompt_cache_options", "prompt_cache_retention"} {
					require.Equal(t, gjson.GetBytes(body, field).Raw, gjson.GetBytes(wire, field).Raw)
				}
				require.Equal(t, before, string(body), "full request must remain available for recovery")
				require.Equal(t, tc.want, txn.Finish(t.Context(), false, "").Reason)
				// Switching back cannot resurrect the consumed policy/prefix anchor.
				old := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(tc.input+","+incrementalOutput, tc.extra), o)
				require.Equal(t, "no_anchor_or_evicted", old.Finish(t.Context(), false, "").Reason)
			})
		}
	}
}

func TestOpenAIIncrementalInvalidPromptCache(t *testing.T) {
	cases := []struct{ name, input, fields, reason string }{}
	for _, tc := range []struct{ value, reason string }{
		{`null`, "ambiguous_null"}, {`[]`, "unknown_shape"}, {`true`, "unknown_shape"}, {`"30m"`, "unknown_shape"},
		{`{"ttl":"24h"}`, "unknown_shape"}, {`{"ttl":"forever"}`, "unknown_shape"}, {`{"ttl":1800}`, "unknown_shape"},
		{`{"ttl":null}`, "ambiguous_null"}, {`{"ttl":["30m"]}`, "unknown_shape"},
		{`{"mode":"automatic"}`, "unknown_shape"}, {`{"mode":true}`, "unknown_shape"}, {`{"mode":null}`, "ambiguous_null"},
		{`{"prewarm":"false"}`, "unknown_shape"}, {`{"prewarm":null}`, "ambiguous_null"},
		{`{"comparison_response_id":12}`, "unknown_shape"}, {`{"comparison_response_id":null}`, "ambiguous_null"},
		{`{"ttl":"30m","future":true}`, "unknown_field"}, {`{"mode ttl":"explicit"}`, "unknown_field"},
		{`{"ttl":"30m","\u0074tl":"12h"}`, "duplicate_json"},
	} {
		cases = append(cases, struct{ name, input, fields, reason string }{"options/" + tc.value, incrementalNext, `"prompt_cache_options":` + tc.value, tc.reason})
	}
	for _, value := range []string{`null`, `true`, `24`, `[]`, `{}`, `""`, `"30m"`, `"12h"`, `" 24h"`} {
		reason := "unknown_shape"
		if value == "null" {
			reason = "ambiguous_null"
		}
		cases = append(cases, struct{ name, input, fields, reason string }{"retention/" + value, incrementalNext, `"prompt_cache_retention":` + value, reason})
	}
	for _, kind := range []string{"message", "function_call_output", "custom_tool_call_output"} {
		for _, tc := range []struct{ value, reason string }{
			{`null`, "ambiguous_null"}, {`true`, "unknown_shape"}, {`[]`, "unknown_shape"}, {`"explicit"`, "unknown_shape"},
			{`{}`, "unknown_shape"}, {`{"ttl":"30m"}`, "unknown_shape"}, {`{"mode":"implicit"}`, "unknown_shape"}, {`{"mode":1}`, "unknown_shape"},
			{`{"mode":null}`, "ambiguous_null"}, {`{"mode":"explicit","ttl":null}`, "ambiguous_null"},
			{`{"mode":"explicit","ttl":"24h"}`, "unknown_shape"}, {`{"mode":"explicit","ttl":1800}`, "unknown_shape"},
			{`{"mode":"explicit","ttl":{"value":"30m"}}`, "unknown_shape"},
			{`{"mode":"explicit","prewarm":false}`, "unknown_field"}, {`{"mode":"explicit","future":true}`, "unknown_field"},
			{`{"mode":"explicit","mode":"implicit"}`, "duplicate_json"},
		} {
			cases = append(cases, struct{ name, input, fields, reason string }{kind + "/" + tc.value, incrementalCacheItem(kind, tc.value), `"prompt_cache_options":{"mode":"explicit"}`, tc.reason})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			o.ConnID, o.ConnectionReady = "conn-1", true
			incrementalSeed(t, s, c, a, o)
			store := s.getOpenAIWSStateStore().(*defaultOpenAIWSStateStore)
			store.BindResponseConn("resp_p2", o.ConnID, time.Minute)
			for _, slot := range store.incremental {
				slot.anchor.connWriteGeneration = 1
			}
			lease := &openAIWSConnLease{conn: newOpenAIWSConn(o.ConnID, a.ID, &openAIWSCaptureConn{}, nil)}
			lease.conn.writeGeneration.Store(1)
			body := incrementalRequest(incrementalUser+","+incrementalOutput+","+tc.input, tc.fields)
			before := string(body)
			txn := s.beginOpenAIIncrementalObservation(c, a, body, o)
			require.Equal(t, before, string(txn.wsPayload(t.Context(), body, lease)))
			require.Equal(t, before, string(body))
			txn.ObserveEvent(incrementalCompleted(incrementalOutput))
			report := txn.Finish(t.Context(), true, o.ConnID)
			require.Equal(t, tc.reason, report.Reason)
			require.False(t, report.Assessable)
			require.Nil(t, store.incremental[txn.key].anchor)
		})
	}
}

func TestOpenAIIncrementalCustomAndNamespaceTools(t *testing.T) {
	for _, tool := range []string{
		`{"type":"custom","name":"apply_patch"}`,
		`{"type":"custom","name":"apply_patch","format":{"type":"text"}}`,
		`{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"lark","definition":"start: /.+/"}}`,
		`{"type":"namespace","name":"functions","tools":[{"type":"custom","name":"apply_patch","format":{"type":"grammar","syntax":"regex","definition":".*"}},{"type":"function","name":"lookup","parameters":{"maximum":9007199254740993}}]}`,
	} {
		t.Run(tool, func(t *testing.T) {
			s, c, a, o := incrementalFixture(t)
			fields := `"tools":[` + tool + `],"tool_choice":{"type":"custom","name":"apply_patch"}`
			incrementalSeed(t, s, c, a, o, fields)
			input := incrementalUser + "," + incrementalOutput
			txn := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(input, fields), o)
			require.Equal(t, "true", txn.Finish(t.Context(), false, "").PrefixMatch)
			// Grammar, schemas, names and namespaces stay in settings equality.
			for _, pair := range [][2]string{{"apply_patch", "changed_patch"}, {"start: /.+/", "start: /x/"}, {`".*"`, `".+"`}, {"9007199254740993", "9007199254740992"}, {"functions", "other_namespace"}} {
				if !strings.Contains(fields, pair[0]) {
					continue
				}
				incrementalSeed(t, s, c, a, o, fields)
				next := s.beginOpenAIIncrementalObservation(c, a, incrementalRequest(input, strings.ReplaceAll(fields, pair[0], pair[1])), o)
				require.Equal(t, "settings_changed", next.Finish(t.Context(), false, "").Reason)
			}
		})
	}
	for _, tool := range []string{
		`{"type":"custom","name":"patch","format":null}`,
		`{"type":"custom","name":"patch","format":{"type":"grammar","syntax":"future","definition":".*"}}`,
		`{"type":"custom","name":"patch","format":{"type":"text","future":true}}`,
		`{"type":"namespace","name":"tools","tools":[{"type":"future","name":"patch"}]}`,
		`{"type":"namespace","name":"tools","tools":{}}`,
	} {
		pending, reason, _ := openAIIncrementalRequest(incrementalRequest(incrementalUser, `"tools":[`+tool+`]`))
		require.Nil(t, pending, tool)
		require.NotEmpty(t, reason)
	}
}
