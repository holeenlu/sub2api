package session

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExecutionScopeThreadPriority(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		identity ExecutionIdentity
		body     string
		want     string
	}{
		{
			name:     "thread header wins",
			identity: ExecutionIdentity{ThreadIDHeader: " t-plain ", TurnMetadataHeader: `{"thread_id":"t-header"}`, WindowIDHeader: "t-window:0"},
			body:     `{"client_metadata":{"thread_id":"t-body"}}`,
			want:     "t-plain",
		},
		{
			name:     "header metadata precedes window",
			identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":" t-header "}`, WindowIDHeader: "t-window:0"},
			body:     `{"client_metadata":{"thread_id":"t-body"}}`,
			want:     "t-header",
		},
		{
			name:     "window prefix precedes body",
			identity: ExecutionIdentity{WindowIDHeader: " t-window:0:1 "},
			body:     `{"client_metadata":{"thread_id":"t-body"}}`,
			want:     "t-window",
		},
		{
			name:     "empty window prefix falls back to body",
			identity: ExecutionIdentity{WindowIDHeader: " :0"},
			body:     `{"client_metadata":{"thread_id":" t-body ","x-codex-turn-metadata":"{\"thread_id\":\"t-embedded\"}"}}`,
			want:     "t-body",
		},
		{
			name: "body embedded metadata",
			body: `{"client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"t-embedded\"}"}}`,
			want: "t-embedded",
		},
		{
			name:     "invalid typed metadata discards partial thread",
			identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"discard","request_kind":3}`},
			body:     `{"client_metadata":{"thread_id":"t-body"}}`,
			want:     "t-body",
		},
		{
			name:     "valid empty header does not hide body thread",
			identity: ExecutionIdentity{TurnMetadataHeader: `{}`},
			body:     `{"client_metadata":{"thread_id":"t-body"}}`,
			want:     "t-body",
		},
		{name: "response envelope is not a thread source", body: `{"type":"response.create","response":{"client_metadata":{"thread_id":"nested"}}}`},
		{name: "malformed metadata", identity: ExecutionIdentity{TurnMetadataHeader: "{not json"}},
		{name: "content has no identity", body: `{"input":"hi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, thread := ExecutionScopeSeed(tc.identity, []byte(tc.body), 11)
			require.Equal(t, tc.want, thread)
		})
	}
}

func TestExecutionLane(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		identity ExecutionIdentity
		body     string
		want     string
	}{
		{name: "turn", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"t","request_kind":"turn"}`}},
		{name: "prewarm", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"t","request_kind":"prewarm"}`}},
		{name: "compaction", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"t","request_kind":"compaction"}`}},
		{name: "missing kind", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"t"}`}},
		{name: "normalized memory", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"t","request_kind":" Memory "}`}, want: "kind=memory"},
		{name: "unknown kind precedes subagent", identity: ExecutionIdentity{TurnMetadataHeader: `{"request_kind":"future"}`, SubagentHeader: "guardian"}, want: "kind=future"},
		{name: "header wins over body", identity: ExecutionIdentity{TurnMetadataHeader: `{"request_kind":"turn"}`}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`},
		{name: "body metadata fallback", body: `{"client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`, want: "kind=memory"},
		{name: "malformed header fallback", identity: ExecutionIdentity{TurnMetadataHeader: "{not json"}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`, want: "kind=memory"},
		{name: "empty object masks body kind", identity: ExecutionIdentity{TurnMetadataHeader: `{}`}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`},
		{name: "null masks body kind", identity: ExecutionIdentity{TurnMetadataHeader: `null`}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"request_kind\":\"memory\"}"}}`},
		{name: "thread header alone does not merge guardian", identity: ExecutionIdentity{ThreadIDHeader: "t", SubagentHeader: " Guardian "}, want: "subagent=guardian"},
		{name: "thread metadata suppresses guardian lane", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"child","request_kind":"turn"}`, SubagentHeader: "guardian"}},
		{name: "body subagent", body: `{"client_metadata":{"x-openai-subagent":" Guardian "}}`, want: "subagent=guardian"},
		{name: "header subagent wins", identity: ExecutionIdentity{SubagentHeader: "header"}, body: `{"client_metadata":{"x-openai-subagent":"guardian"}}`, want: "subagent=header"},
		{name: "body thread metadata suppresses guardian", identity: ExecutionIdentity{SubagentHeader: "guardian"}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"child\"}"}}`},
		{name: "empty header masks body metadata thread", identity: ExecutionIdentity{TurnMetadataHeader: `{}`, SubagentHeader: "guardian"}, body: `{"client_metadata":{"x-codex-turn-metadata":"{\"thread_id\":\"child\"}"}}`, want: "subagent=guardian"},
		{name: "none", body: `{"input":"hi"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, ExecutionLane(tc.identity, []byte(tc.body)))
			identity := tc.identity
			identity.ConnectionID, identity.StreamID = "conn", "lane"
			want := "connection=conn|stream=lane"
			if tc.want != "" {
				want += "|" + tc.want
			}
			require.Equal(t, want, ExecutionLane(identity, []byte(tc.body)))
		})
	}
}

func TestExecutionScopeSeed(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		identity ExecutionIdentity
		apiKeyID int64
		want     string
	}{
		{name: "thread before session", identity: ExecutionIdentity{ThreadIDHeader: " thread-a ", SessionID: "root-session"}, apiKeyID: 11, want: "openai_ws_exec:11|thread=thread-a"},
		{name: "sibling thread", identity: ExecutionIdentity{ThreadIDHeader: "thread-b", SessionID: "root-session"}, apiKeyID: 11, want: "openai_ws_exec:11|thread=thread-b"},
		{name: "other key", identity: ExecutionIdentity{ThreadIDHeader: "thread-a"}, apiKeyID: 12, want: "openai_ws_exec:12|thread=thread-a"},
		{name: "memory thread", identity: ExecutionIdentity{TurnMetadataHeader: `{"thread_id":"thread-a","request_kind":"memory"}`}, apiKeyID: 11, want: "openai_ws_exec:11|thread=thread-a|kind=memory"},
		{name: "explicit session", identity: ExecutionIdentity{SessionID: " root-session "}, apiKeyID: 11, want: "openai_ws_exec:11|session=root-session"},
		{name: "memory session", identity: ExecutionIdentity{SessionID: "root-session", TurnMetadataHeader: `{"request_kind":"memory"}`}, apiKeyID: 11, want: "openai_ws_exec:11|session=root-session|kind=memory"},
		{name: "named stream fallback", identity: ExecutionIdentity{ConnectionID: "conn-a", StreamID: "one"}, apiKeyID: 11, want: "openai_ws_exec:11|connection=conn-a|connection=conn-a|stream=one"},
		{name: "another stream", identity: ExecutionIdentity{ConnectionID: "conn-a", StreamID: "two"}, apiKeyID: 11, want: "openai_ws_exec:11|connection=conn-a|connection=conn-a|stream=two"},
		{name: "another connection", identity: ExecutionIdentity{ConnectionID: "conn-b", StreamID: "one"}, apiKeyID: 11, want: "openai_ws_exec:11|connection=conn-b|connection=conn-b|stream=one"},
		{name: "thread still includes stream", identity: ExecutionIdentity{ThreadIDHeader: "thread-a", ConnectionID: "conn-a", StreamID: "one"}, apiKeyID: 11, want: "openai_ws_exec:11|thread=thread-a|connection=conn-a|stream=one"},
		{name: "default stream is not identity", identity: ExecutionIdentity{ConnectionID: "conn-a"}, apiKeyID: 11},
		{name: "background kind is not identity", identity: ExecutionIdentity{TurnMetadataHeader: `{"request_kind":"memory"}`}, apiKeyID: 11},
		{name: "zero identity", apiKeyID: 11},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, body := range []string{`{"model":"gpt-5.1","input":"first"}`, `{"input":[{"role":"user","content":"second"}]}`} {
				wire := []byte(body)
				seed, _ := ExecutionScopeSeed(tc.identity, wire, tc.apiKeyID)
				require.Equal(t, tc.want, seed, "request content must not change execution identity")
				require.Equal(t, body, string(wire), "raw wire must remain unchanged")
			}
		})
	}
}
