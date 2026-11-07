package service

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

type excelBPSContinuationTestEntry struct {
	raw      []byte
	revision uint64
}
type excelBPSContinuationTestStore struct {
	GatewayCache
	mu               sync.Mutex
	entries          map[string]excelBPSContinuationTestEntry
	routingMetadata  map[int64]map[string]int64
	loads, saves     int
	loadErr, saveErr error
	failSaveAt       int
	loaded           chan struct{}
}

func (s *excelBPSContinuationTestStore) GetSessionAccountID(ctx context.Context, groupID int64, key string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	value, ok := s.routingMetadata[groupID][key]
	if !ok {
		return 0, ErrStickySessionNotFound
	}
	return value, nil
}

func (s *excelBPSContinuationTestStore) SetSessionAccountID(ctx context.Context, groupID int64, key string, value int64, _ time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.routingMetadata == nil {
		s.routingMetadata = make(map[int64]map[string]int64)
	}
	if s.routingMetadata[groupID] == nil {
		s.routingMetadata[groupID] = make(map[string]int64)
	}
	s.routingMetadata[groupID][key] = value
	return nil
}

func (s *excelBPSContinuationTestStore) LoadBPSState(ctx context.Context, scope, kind string) ([]byte, uint64, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.loads++
	if s.loadErr != nil {
		return nil, 0, s.loadErr
	}
	entry := s.entries[scope+"/"+kind]
	if s.loaded != nil {
		select {
		case s.loaded <- struct{}{}:
		default:
		}
	}
	return bytes.Clone(entry.raw), entry.revision, nil
}

func (s *excelBPSContinuationTestStore) SaveBPSState(ctx context.Context, scope, kind string, expected *uint64, raw []byte) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.saves++
	if s.saveErr != nil && (s.failSaveAt == 0 || s.saves == s.failSaveAt) {
		return false, s.saveErr
	}
	if len(raw) > basispoints.MaxStateEntryBytes {
		return false, basispoints.ErrStateCapacity
	}
	key := scope + "/" + kind
	entry := s.entries[key]
	if expected == nil || *expected != entry.revision {
		return false, nil
	}
	s.entries[key] = excelBPSContinuationTestEntry{bytes.Clone(raw), entry.revision + 1}
	return true, nil
}

func excelBPSContinuationTestContext() *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	group := int64(17)
	c.Set("api_key", &APIKey{ID: 23, UserID: 42, GroupID: &group, Key: "PRIVATE_API_KEY"})
	return c
}

func excelBPSContinuationTestService() (*OpenAIGatewayService, *excelBPSContinuationTestStore) {
	store := &excelBPSContinuationTestStore{entries: map[string]excelBPSContinuationTestEntry{}}
	cfg := &config.Config{}
	cfg.Gateway.ExcelBPSStateRedisEnabled = true
	return &OpenAIGatewayService{cache: store, cfg: cfg}, store
}

const excelBPSContinuationTestBody = `{"model":"parent-model","instructions":"PRIVATE_PARENT_INSTRUCTIONS","metadata":{"private":"PRIVATE_METADATA"},"tools":[{"type":"function","name":"lookup","parameters":{"type":"object","properties":{"n":{"const":9007199254740993123456789}}}}],"input":[{"role":"user","content":[{"type":"input_text","text":"start"},{"type":"input_image","image_url":"data:image/png;base64,EXACT+/="}],"n":9007199254740993123456789},{"type":"reasoning","encrypted_content":"PRIVATE_CIPHERTEXT+/=","summary":[]}]}`
const excelBPSContinuationTestResponse = `{"id":"resp_test","status":"completed","error":null,"output":[{"type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"answer"}]}]}`
const excelBPSContinuationTestNext = `{"model":"next-model","previous_response_id":"resp_test","input":[{"role":"user","content":"next"}],"metadata":{"current":true}}`

func TestExcelBPSContinuationAcrossConnectionsAndInstances(t *testing.T) {
	writer, store := excelBPSContinuationTestService()
	c, account := excelBPSContinuationTestContext(), excelAccount()
	c.Request.Header.Set("session_id", "socket-one")
	finish, err := writer.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
	reader := &OpenAIGatewayService{cache: store, cfg: writer.cfg}
	next := excelBPSContinuationTestContext()
	next.Request.Header.Set("session_id", "socket-two")
	account.Credentials["access_token"] = "rotated-access-token"
	restored, err := reader.restoreExcelBPSContinuation(t.Context(), next, account, []byte(excelBPSContinuationTestNext))
	require.NoError(t, err)
	require.False(t, gjson.GetBytes(restored, "previous_response_id").Exists())
	require.Equal(t, "next-model", gjson.GetBytes(restored, "model").String())
	require.False(t, gjson.GetBytes(restored, "instructions").Exists())
	require.True(t, gjson.GetBytes(restored, "metadata.current").Bool())
	require.NotContains(t, string(restored), "PRIVATE_METADATA")
	require.NotContains(t, string(restored), "PRIVATE_PARENT_INSTRUCTIONS")
	require.Len(t, gjson.GetBytes(restored, "input").Array(), 4)
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "input.0.n").Raw)
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "tools.0.parameters.properties.n.const").Raw)
	require.Equal(t, "PRIVATE_CIPHERTEXT+/=", gjson.GetBytes(restored, "input.1.encrypted_content").String())
	require.Equal(t, "data:image/png;base64,EXACT+/=", gjson.GetBytes(restored, "input.0.content.1.image_url").String())
	for key, entry := range store.entries {
		require.NotContains(t, key, "resp_test")
		require.NotContains(t, key, "PRIVATE_API_KEY")
		require.NotContains(t, string(entry.raw), "PRIVATE_API_KEY")
		require.NotContains(t, string(entry.raw), "rotated-access-token")
	}
	// The restored full input itself is a valid source for the next response.
	secondResponse := []byte(strings.ReplaceAll(excelBPSContinuationTestResponse, "resp_test", "resp_second"))
	secondFinish, err := reader.prepareExcelBPSContinuation(t.Context(), next, account, restored, secondResponse)
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, secondFinish, true, excelBPSContinuationCommitted)
	thirdRequest := []byte(strings.ReplaceAll(excelBPSContinuationTestNext, "resp_test", "resp_second"))
	third, err := (&OpenAIGatewayService{cache: store, cfg: writer.cfg}).restoreExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), account, thirdRequest)
	require.NoError(t, err)
	require.Len(t, gjson.GetBytes(third, "input").Array(), 6)
}

func TestExcelBPSContinuationExplicitToolsAndPrefix(t *testing.T) {
	for _, tools := range []string{"[]", "null", `[{"type":"function","name":"replacement","parameters":{}}]`} {
		t.Run(tools, func(t *testing.T) {
			svc, _ := excelBPSContinuationTestService()
			c, account := excelBPSContinuationTestContext(), excelAccount()
			finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
			require.NoError(t, err)
			requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
			next, err := sjson.SetRawBytes([]byte(excelBPSContinuationTestNext), "tools", []byte(tools))
			require.NoError(t, err)
			next, err = sjson.SetBytes(next, "instructions", "current instructions")
			require.NoError(t, err)
			next, err = sjson.SetBytes(next, "tool_choice", "none")
			require.NoError(t, err)
			restored, err := svc.restoreExcelBPSContinuation(t.Context(), c, account, next)
			require.NoError(t, err)
			require.JSONEq(t, tools, gjson.GetBytes(restored, "tools").Raw)
			require.Equal(t, "current instructions", gjson.GetBytes(restored, "instructions").String())
			require.Equal(t, "none", gjson.GetBytes(restored, "tool_choice").String())
			resent, err := sjson.SetBytes(restored, "previous_response_id", "resp_test")
			require.NoError(t, err)
			deduped, err := svc.restoreExcelBPSContinuation(t.Context(), c, account, resent)
			require.NoError(t, err)
			require.JSONEq(t, string(restored), string(deduped))
		})
	}
}

func TestExcelBPSContinuationToolCallAndExactArguments(t *testing.T) {
	svc, _ := excelBPSContinuationTestService()
	c, account := excelBPSContinuationTestContext(), excelAccount()
	response := []byte(`{"id":"resp_test","status":"completed","output":[{"type":"function_call","id":"fc_original","call_id":"call_1","name":"lookup","arguments":"{\"n\":9007199254740993123456789}"}]}`)
	finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), response)
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
	next := []byte(`{"model":"next-model","previous_response_id":"resp_test","input":[{"type":"function_call_output","call_id":"call_1","output":{"n":9007199254740993123456789}}]}`)
	restored, err := svc.restoreExcelBPSContinuation(t.Context(), c, account, next)
	require.NoError(t, err)
	require.Equal(t, `{"n":9007199254740993123456789}`, gjson.GetBytes(restored, "input.2.arguments").String())
	require.Equal(t, "9007199254740993123456789", gjson.GetBytes(restored, "input.3.output.n").Raw)
}

func TestExcelBPSContinuationScopeIsolation(t *testing.T) {
	cases := map[string]func(*gin.Context, *Account){
		"user":          func(c *gin.Context, _ *Account) { getAPIKeyFromContext(c).UserID++ },
		"key":           func(c *gin.Context, _ *Account) { getAPIKeyFromContext(c).ID++ },
		"group":         func(c *gin.Context, _ *Account) { *getAPIKeyFromContext(c).GroupID++ },
		"missing group": func(c *gin.Context, _ *Account) { getAPIKeyFromContext(c).GroupID = nil },
		"account":       func(_ *gin.Context, a *Account) { a.ID++ },
		"credential":    func(_ *gin.Context, a *Account) { a.Credentials["chatgpt_account_id"] = "other-upstream" },
		"upstream user": func(_ *gin.Context, a *Account) { a.Credentials["chatgpt_user_id"] = "other-user" },
		"untrusted":     func(c *gin.Context, _ *Account) { c.Set("api_key", nil) },
		"no user":       func(c *gin.Context, _ *Account) { getAPIKeyFromContext(c).UserID = 0 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			svc, store := excelBPSContinuationTestService()
			finish, err := svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
			require.NoError(t, err)
			requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
			c, account := excelBPSContinuationTestContext(), excelAccount()
			mutate(c, account)
			_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
			require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
			require.Len(t, store.entries, 1)
		})
	}
}

func TestExcelBPSContinuationSkipsIncompleteSources(t *testing.T) {
	for _, tc := range []struct{ name, body, response, path string }{
		{name: "failed", response: `{"id":"resp_test","status":"failed","output":[]}`},
		{name: "cancelled", response: `{"id":"resp_test","status":"cancelled","output":[]}`},
		{name: "incomplete", response: `{"id":"resp_test","status":"incomplete","output":[]}`},
		{name: "queued", response: `{"id":"resp_test","status":"queued","output":[]}`},
		{name: "no output", response: `{"id":"resp_test","status":"completed"}`},
		{name: "null output", response: `{"id":"resp_test","status":"completed","output":null}`},
		{name: "error", response: `{"id":"resp_test","status":"completed","output":[],"error":{"message":"PRIVATE_ERROR"}}`},
		{name: "unfinished output", response: `{"id":"resp_test","status":"completed","output":[{"type":"message","status":"in_progress"}]}`},
		{name: "compact output", response: `{"id":"resp_test","status":"completed","output":[{"type":"compaction","encrypted_content":"opaque"}]}`},
		{name: "previous", body: `{"previous_response_id":"missing","input":"next"}`},
		{name: "prompt", body: `{"prompt":{"id":"hidden"},"input":"next"}`},
		{name: "conversation", body: `{"conversation":"hidden","input":"next"}`},
		{name: "no input", body: `{"model":"test"}`},
		{name: "empty input", body: `{"input":[]}`},
		{name: "orphan output", body: `{"input":[{"type":"function_call_output","call_id":"missing","name":"lookup","output":"orphan"}]}`},
		{name: "reference", body: `{"input":[{"type":"item_reference","id":"missing"}]}`},
		{name: "compact trigger", body: `{"input":[{"type":"compaction_trigger"}]}`},
		{name: "compact path", path: "/v1/responses/compact"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := excelBPSContinuationTestService()
			c := excelBPSContinuationTestContext()
			if tc.path != "" {
				c.Request = httptest.NewRequest(http.MethodPost, tc.path, nil)
			}
			if tc.body == "" {
				tc.body = excelBPSContinuationTestBody
			}
			if tc.response == "" {
				tc.response = excelBPSContinuationTestResponse
			}
			finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, excelAccount(), []byte(tc.body), []byte(tc.response))
			require.NoError(t, err)
			requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationNotApplicable)
			require.Zero(t, store.saves)
			require.Empty(t, store.entries)
		})
	}
	t.Run("named standalone is complete", func(t *testing.T) {
		svc, store := excelBPSContinuationTestService()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), []byte(`{"input":[{"type":"function_call_output","name":"external_task","output":"supplied context"}]}`), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
		require.Len(t, store.entries, 1)
	})
}

func TestExcelBPSContinuationNoPreviousIsUnchanged(t *testing.T) {
	svc, store := excelBPSContinuationTestService()
	for _, body := range []string{`{"input":"hello"}`, `{"previous_response_id":null}`, `{"previous_response_id":12}`, `{"previous_response_id":{}}`, `{"previous_response_id":" "}`} {
		got, err := svc.restoreExcelBPSContinuation(t.Context(), nil, nil, []byte(body))
		require.NoError(t, err)
		require.Equal(t, body, string(got))
	}
	require.Zero(t, store.loads)
	finish, err := svc.prepareExcelBPSContinuation(t.Context(), nil, excelAccount(), []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationNotApplicable)
	require.Zero(t, store.saves)
}

func TestExcelBPSContinuationRespectsDisabledSharedState(t *testing.T) {
	for _, configured := range []bool{false, true} {
		svc, store := excelBPSContinuationTestService()
		if configured {
			svc.cfg.Gateway.ExcelBPSStateRedisEnabled = false
		} else {
			svc.cfg = nil
		}
		c, account := excelBPSContinuationTestContext(), excelAccount()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationNotApplicable)
		_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
		require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
		require.Zero(t, store.loads)
		require.Zero(t, store.saves)
	}
}

func TestExcelBPSContinuationPendingCommitAndAbort(t *testing.T) {
	t.Run("reader waits only for exact pending response", func(t *testing.T) {
		svc, store := excelBPSContinuationTestService()
		store.loaded = make(chan struct{}, 1)
		c, account := excelBPSContinuationTestContext(), excelAccount()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		for _, entry := range store.entries {
			require.Less(t, len(entry.raw), 1024)
			require.NotContains(t, string(entry.raw), "PRIVATE_CIPHERTEXT")
			require.NotContains(t, string(entry.raw), `"input"`)
			require.NotContains(t, string(entry.raw), `"tools"`)
		}
		done := make(chan error, 1)
		go func() {
			_, readErr := svc.restoreExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), account, []byte(excelBPSContinuationTestNext))
			done <- readErr
		}()
		<-store.loaded
		requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
		require.NoError(t, <-done)
		requireExcelBPSContinuationFinished(t, finish, false, excelBPSContinuationCommitted)
		_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
		require.NoError(t, err)
	})
	t.Run("abort cannot later commit", func(t *testing.T) {
		svc, _ := excelBPSContinuationTestService()
		c, account := excelBPSContinuationTestContext(), excelAccount()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		requireExcelBPSContinuationFinished(t, finish, false, excelBPSContinuationAborted)
		requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationAborted)
		_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
		require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
	})
	t.Run("cancelled request cannot cancel delivery commit", func(t *testing.T) {
		svc, _ := excelBPSContinuationTestService()
		c, account := excelBPSContinuationTestContext(), excelAccount()
		ctx, cancel := context.WithCancel(t.Context())
		finish, err := svc.prepareExcelBPSContinuation(ctx, c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		cancel()
		requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
		_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
		require.NoError(t, err)
	})
	t.Run("pending observes cancellation", func(t *testing.T) {
		svc, store := excelBPSContinuationTestService()
		store.loaded = make(chan struct{}, 1)
		c, account := excelBPSContinuationTestContext(), excelAccount()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		done := make(chan error, 1)
		go func() {
			_, readErr := svc.restoreExcelBPSContinuation(ctx, excelBPSContinuationTestContext(), account, []byte(excelBPSContinuationTestNext))
			done <- readErr
		}()
		<-store.loaded
		cancel()
		require.ErrorIs(t, <-done, context.Canceled)
		requireExcelBPSContinuationFinished(t, finish, false, excelBPSContinuationAborted)
	})
	t.Run("failed commit stays unavailable", func(t *testing.T) {
		svc, store := excelBPSContinuationTestService()
		c, account := excelBPSContinuationTestContext(), excelAccount()
		finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		require.NoError(t, err)
		store.saveErr = errors.New("PRIVATE_REDIS_FAILURE")
		outcome, completionErr := finish(true)
		require.ErrorIs(t, completionErr, basispoints.ErrStateUnavailable)
		require.Equal(t, excelBPSContinuationUnknown, outcome.State)
		require.Equal(t, 2, outcome.Writes)
		started := time.Now()
		_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
		require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
		require.Less(t, time.Since(started), 3*time.Second)
	})
}

func TestExcelBPSContinuationImmutableResponseAndFailures(t *testing.T) {
	svc, store := excelBPSContinuationTestService()
	c, account := excelBPSContinuationTestContext(), excelAccount()
	finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
	scope, _ := excelBPSContinuationScope(c, account)
	key := scope + "/" + excelBPSContinuationKind("resp_test")
	before := store.entries[key]
	same, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, same, false, excelBPSContinuationNotApplicable)
	require.Equal(t, before, store.entries[key])
	_, err = svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(`{"input":"different"}`), []byte(excelBPSContinuationTestResponse))
	require.ErrorIs(t, err, errExcelBPSContinuationConflict)
	require.Equal(t, before, store.entries[key])
	missing, err := svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(strings.ReplaceAll(excelBPSContinuationTestNext, "resp_test", "resp_missing")))
	require.Nil(t, missing)
	require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
	for _, failure := range []error{basispoints.ErrStateCapacity, errors.New("PRIVATE_FAILURE")} {
		fresh, broken := excelBPSContinuationTestService()
		broken.saveErr = failure
		_, err = fresh.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
		if errors.Is(failure, basispoints.ErrStateCapacity) {
			require.ErrorIs(t, err, basispoints.ErrStateCapacity)
		} else {
			require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
		}
		require.NotContains(t, err.Error(), "PRIVATE_FAILURE")
	}
	store.loadErr = errors.New("PRIVATE_READ_FAILURE")
	_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
	require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
	noCache := &OpenAIGatewayService{cfg: svc.cfg}
	_, err = noCache.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
	_, err = noCache.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
	require.ErrorIs(t, err, basispoints.ErrStateUnavailable)
}

func TestExcelBPSContinuationCorruptionExpiryAndBounds(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*excelBPSContinuationSnapshot)
		want   error
	}{
		{"scope", func(s *excelBPSContinuationSnapshot) { s.Scope = "other" }, errExcelBPSContinuationInvalid},
		{"response", func(s *excelBPSContinuationSnapshot) { s.ResponseID = "other" }, errExcelBPSContinuationInvalid},
		{"version", func(s *excelBPSContinuationSnapshot) { s.Version++ }, errExcelBPSContinuationInvalid},
		{"digest", func(s *excelBPSContinuationSnapshot) { s.Digest = "other" }, errExcelBPSContinuationInvalid},
		{"state", func(s *excelBPSContinuationSnapshot) { s.State = "unexpected" }, errExcelBPSContinuationInvalid},
		{"expired", func(s *excelBPSContinuationSnapshot) {
			s.CreatedAt = time.Now().Add(-excelBPSContinuationTTL - time.Second).UnixMilli()
		}, basispoints.ErrPreviousResponseNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, store := excelBPSContinuationTestService()
			c, account := excelBPSContinuationTestContext(), excelAccount()
			finish, err := svc.prepareExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
			require.NoError(t, err)
			requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
			scope, _ := excelBPSContinuationScope(c, account)
			key := scope + "/" + excelBPSContinuationKind("resp_test")
			entry := store.entries[key]
			snapshot, err := decodeExcelBPSContinuation(entry.raw, scope, "resp_test", svc.excelBPSContinuationLimit())
			require.NoError(t, err)
			tc.mutate(snapshot)
			entry.raw, err = encodeExcelBPSContinuation(snapshot, svc.excelBPSContinuationLimit())
			require.NoError(t, err)
			store.entries[key] = entry
			_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
			require.ErrorIs(t, err, tc.want)
		})
	}
	for _, raw := range [][]byte{[]byte("broken"), []byte(excelBPSContinuationMagic + "gnot-gzip"), []byte(excelBPSContinuationMagic + "j{}"), []byte(excelBPSContinuationMagic + "x{}")} {
		_, err := decodeExcelBPSContinuation(raw, "scope", "id", 1024)
		require.ErrorIs(t, err, errExcelBPSContinuationInvalid)
	}
	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	_, err := zw.Write(bytes.Repeat([]byte("x"), 2048))
	require.NoError(t, err)
	require.NoError(t, zw.Close())
	_, err = decodeExcelBPSContinuation(append([]byte(excelBPSContinuationMagic+"g"), compressed.Bytes()...), "scope", "id", 1024)
	require.ErrorIs(t, err, basispoints.ErrStateCapacity)
	bounded := &excelBPSContinuationBoundedBuffer{limit: 8}
	_, err = bounded.Write([]byte("oversized"))
	require.ErrorIs(t, err, basispoints.ErrStateCapacity)
	require.Zero(t, bounded.Len())
	svc, _ := excelBPSContinuationTestService()
	svc.cfg = &config.Config{}
	svc.cfg.Gateway.OpenAIWS.ClientReadLimitBytes = 1024
	require.EqualValues(t, 1024, svc.excelBPSContinuationLimit())
	svc.cfg.Gateway.OpenAIWS.ClientReadLimitBytes = 128 << 20
	svc.cfg.Gateway.ExcelBPSStateRedisEnabled = true
	require.EqualValues(t, 64<<20, svc.excelBPSContinuationLimit())
	oversizedOutput := `{"id":"resp_test","status":"completed","output":[` + strings.Repeat(`{"type":"message"},`, excelBPSContinuationMaxOutputItems) + `{"type":"message"}]}`
	_, err = svc.prepareExcelBPSContinuation(t.Context(), excelBPSContinuationTestContext(), excelAccount(), []byte(excelBPSContinuationTestBody), []byte(oversizedOutput))
	require.ErrorIs(t, err, basispoints.ErrStateCapacity)
}

func TestExcelBPSContinuationCompressesWithinExistingEntryBudget(t *testing.T) {
	input, err := json.Marshal(map[string]any{"role": "user", "content": strings.Repeat("x", basispoints.MaxStateEntryBytes)})
	require.NoError(t, err)
	snapshot := &excelBPSContinuationSnapshot{Version: excelBPSContinuationVersion, Scope: "scope", ResponseID: "id", CreatedAt: time.Now().UnixMilli(), State: "committed", Nonce: "nonce", Input: []json.RawMessage{input}}
	snapshot.Digest, err = excelBPSContinuationDigest(snapshot)
	require.NoError(t, err)
	encoded, err := encodeExcelBPSContinuation(snapshot, excelBPSContinuationMaxBytes)
	require.NoError(t, err)
	require.True(t, bytes.HasPrefix(encoded, []byte(excelBPSContinuationMagic+"g")))
	require.LessOrEqual(t, len(encoded), basispoints.MaxStateEntryBytes)
	decoded, err := decodeExcelBPSContinuation(encoded, "scope", "id", excelBPSContinuationMaxBytes)
	require.NoError(t, err)
	require.Equal(t, snapshot.Digest, decoded.Digest)
	require.Equal(t, input, []byte(decoded.Input[0]))
	_, err = decodeExcelBPSContinuation(encoded, "scope", "id", 1024)
	require.ErrorIs(t, err, basispoints.ErrStateCapacity)
}

func requireExcelBPSContinuationFinished(t *testing.T, finish excelBPSContinuationCompletion, delivered bool, want excelBPSContinuationState) excelBPSContinuationOutcome {
	t.Helper()
	outcome, err := finish(delivered)
	require.NoError(t, err)
	require.Equal(t, want, outcome.State)
	return outcome
}
