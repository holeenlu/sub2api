//go:build unit

package service

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSNextExecutionOriginalInputProof(t *testing.T) {
	for _, tc := range []struct {
		name, body  string
		independent bool
	}{
		{"text", `{"type":"response.create","input":"new turn"}`, true},
		{"full_history", `{"input":[{"role":"user","content":"one"},{"role":"assistant","content":"answer"},{"role":"user","content":"two"}]}`, true},
		{"paired_tool", `{"input":[{"type":"function_call","call_id":"call_1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"call_1","output":"ok"}]}`, true},
		{"cipher_history", `{"input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"compaction","encrypted_content":"summary"},{"role":"user","content":"continue with supplied history"}]}`, true},
		{"explicit_previous_even_with_history", `{"previous_response_id":"resp_bps_old","input":[{"role":"user","content":"full history"}]}`, false},
		{"explicit_previous_with_cipher_history", `{"previous_response_id":"resp_old","input":[{"type":"reasoning","encrypted_content":"opaque"},{"type":"compaction","encrypted_content":"summary"},{"role":"user","content":"full history"}]}`, false},
		{"duplicate_previous", `{"previous_response_id":"","previous_response_id":"resp_old","input":"new"}`, false},
		{"duplicate_input", `{"input":"new","input":[{"type":"item_reference","id":"old"}]}`, false},
		{"implicit_tool", `{"input":[{"type":"function_call_output","call_id":"call_old","output":"ok"}]}`, false},
		{"partial_tool_coverage", `{"input":[{"type":"function_call","call_id":"call_1","name":"f","arguments":"{}"},{"type":"function_call_output","call_id":"call_2","output":"ok"}]}`, false},
		{"conversation", `{"conversation":"conv_old","input":"new"}`, false},
		{"prompt_reference", `{"prompt":{"id":"pmpt_old"},"input":"new"}`, false},
		{"item_reference", `{"input":[{"type":"item_reference","id":"item_old"}]}`, false},
		{"cipher_task", `{"input":[{"type":"agent_message","author":"a","recipient":"b","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`, false},
		{"file_reference", `{"input":[{"role":"user","content":[{"type":"input_file","file_id":"file_old"}]}]}`, false},
		{"nested_cipher", `{"input":[{"role":"user","content":[{"type":"encrypted_content","encrypted_content":"task"}]}]}`, false},
		{"unknown_item", `{"input":[{"type":"future_reference","id":"old"}]}`, false},
		{"cancel", `{"type":"response.cancel","input":"not a create"}`, false},
		{"empty", `{"input":[]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.independent, openAIWSIndependentInput([]byte(tc.body)))
		})
	}
}

func TestOpenAIWSNextExecutionKeepsReadAllocationAndDistinctError(t *testing.T) {
	body := []byte(`{"type":"response.create","model":"client-model","input":"new"}`)
	downstream := &OpenAIWSDownstream{}
	downstream.turn.Store(2)
	svc := &OpenAIGatewayService{}
	err := svc.nextOpenAIWSExecution(context.Background(), nil, downstream, &Account{Platform: PlatformOpenAI}, body, "fallback")
	var next *OpenAIWSNextExecution
	require.ErrorAs(t, err, &next)
	require.Same(t, &body[0], &next.payload[0], "the handoff retains the bounded read allocation without copying its body")
	require.Equal(t, uint64(2), next.turn)
	require.Equal(t, "client-model", next.originalModel)
	var failure *UpstreamFailoverError
	var closeErr *OpenAIWSClientCloseError
	require.False(t, errors.As(err, &failure))
	require.False(t, errors.As(err, &closeErr))
	_, retry := OpenAIWSCurrentTurnRetryPayload(err)
	require.False(t, retry, "new submission must not become current-turn retry")
}

func TestOpenAIWSNextExecutionCloudLivePairedHistoryStaysOwned(t *testing.T) {
	for _, status := range []string{"awaiting_tools", "running", "completed"} {
		t.Run(status, func(t *testing.T) {
			store := newCloudLiveRoutingStore()
			owner := OpenAICloudOwner{UserID: 1, APIKeyID: 2, GroupID: 3, AccountID: 4}
			record := &OpenAICloudSessionRecord{OpenAICloudBinding: OpenAICloudBinding{OpenAICloudOwner: owner, SessionID: "owned", Status: status}}
			require.NoError(t, store.CreateCloudSession(t.Context(), record))
			store.refs["call_cloud_owned"] = true
			svc := &OpenAIGatewayService{cloudSessions: &OpenAICloudSessions{store: store}}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
			group := int64(3)
			c.Set("api_key", &APIKey{ID: 2, UserID: 1, GroupID: &group})
			body := []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"role":"user","content":"history"},{"type":"function_call","name":"echo","call_id":"call_cloud_owned","arguments":"{}"},{"type":"function_call_output","call_id":"call_cloud_owned","output":"answer"}]}`)
			require.True(t, openAIWSIndependentInput(body), "complete syntax alone must not settle live ownership")
			err := svc.nextOpenAIWSExecution(t.Context(), c, &OpenAIWSDownstream{}, &Account{ID: 4, Platform: PlatformOpenAI}, body, "")
			if status == "completed" {
				var next *OpenAIWSNextExecution
				require.ErrorAs(t, err, &next, "completed paired Cloud calls are historical input")
				require.Same(t, &body[0], &next.payload[0])
			} else {
				require.NoError(t, err, "live Cloud tool replies must stay with their original owner")
			}
		})
	}
}

func TestOpenAIWSNextExecutionNativeOwnerCloseKeepsNewAdmission(t *testing.T) {
	svc := &OpenAIGatewayService{}
	c := newOpenAIWSPreemptCodexContext(11, "same-thread")
	account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth}
	body := []byte(`{"type":"response.create","model":"gpt-5.1","input":"next"}`)
	closer := &openAIWSPreemptCloseRecorder{closed: make(chan struct{})}
	parent := t.Context()
	previous, cleanup, armed := svc.BeginOpenAIWSIngressSessionPreemptionWithClient(parent, c, account, body, closer)
	require.True(t, armed)
	defer cleanup()
	released := 0
	ds := &OpenAIWSDownstream{writeCtx: parent, done: make(chan struct{}), admissions: []*openAIWSTurnAdmission{{release: func() { released++ }}}}
	ds.turn.Store(2)
	next := &OpenAIWSNextExecution{downstream: ds, turn: 2, payload: body, originalModel: "gpt-5.1"}
	// Mirror the handler's completed-owner close before accepting the already
	// queued create. This must not call the physical closer or finish admission.
	OpenAIWSSessionOwner(previous).Close()
	require.False(t, OpenAIWSSessionOwner(previous).Preempted())
	ctx, got, model, err := next.Accept(parent, &config.Config{})
	require.NoError(t, err)
	require.Equal(t, "gpt-5.1", model)
	require.Same(t, &body[0], &got[0])
	require.Zero(t, released)
	require.Len(t, ds.admissions, 1)
	require.NoError(t, ctx.Err())
	_, _, _, err = next.Accept(parent, &config.Config{})
	require.Error(t, err, "the same frame cannot establish a second turn budget")
	current, finish, armed := svc.BeginOpenAIWSIngressSessionPreemptionWithClient(parent, c, account, body, closer)
	require.True(t, armed)
	defer finish()
	require.False(t, OpenAIWSSessionOwner(current).Preempted())
	closer.mu.Lock()
	require.Empty(t, closer.codes, "closing a completed owner must not self-preempt the same physical client")
	closer.mu.Unlock()
	ds.completeMultiplexTurn()
	require.Equal(t, 1, released)
}
