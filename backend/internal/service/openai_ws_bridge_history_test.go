package service

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestBPSImplicitToolContinuationRequiresMatchingOwnedHistory(t *testing.T) {
	history := []json.RawMessage{json.RawMessage(`{"type":"custom_tool_call","call_id":"owned-call","name":"exec","input":"text(42)"}`)}
	for _, callID := range []string{"owned-call", "foreign-call"} {
		body := []byte(`{"type":"response.create","model":"gpt-5.1","input":[{"type":"custom_tool_call_output","call_id":"` + callID + `","output":"42"}]}`)
		before := string(body)
		bound, err := bindOpenAIWSImplicitToolContinuation(body, history, "resp_owned")
		require.NoError(t, err)
		require.Equal(t, before, string(body))
		if callID == "owned-call" {
			require.Equal(t, "resp_owned", gjson.GetBytes(bound, "previous_response_id").String())
		} else {
			require.Equal(t, body, bound, "unknown tools must reach the existing orphan rejection")
		}
	}
}
