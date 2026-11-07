package service

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/gateway/model"
	"github.com/stretchr/testify/require"
)

func TestNativeHTTPReasoningSnapshotCannotBorrowEarlierProof(t *testing.T) {
	for _, family := range []nativeUpstreamFamily{nativeUpstreamResponses, nativeUpstreamChat} {
		for _, complete := range []bool{false, true} {
			name := "omitted"
			if complete {
				name = "same_snapshot"
			}
			t.Run(string(family)+"/"+name, func(t *testing.T) {
				d := nativeOperationDecoder{kind: family, sse: true}
				envelope := func(usage string, final bool) []byte {
					if family == nativeUpstreamChat {
						return []byte("{\"id\":\"r\",\"object\":\"chat.completion.chunk\",\"choices\":[],\"usage\":" + usage + "}")
					}
					kind := "response.in_progress"
					if final {
						kind = "response.completed"
					}
					return []byte("{\"type\":\"" + kind + "\",\"response\":{\"id\":\"r\",\"usage\":" + usage + "}}")
				}
				full := "{\"input_tokens\":5,\"output_tokens\":2,\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}}"
				first := d.observePayload(envelope(full, false))
				require.Equal(t, model.UsagePartial, first.Usage.Confidence)
				first.Usage.Total.Value = 99
				first.Usage.Reasoning.Value = 99
				current := "{\"input_tokens\":5,\"output_tokens\":2}"
				if complete {
					current = full
				}
				final := d.observePayload(envelope(current, true))
				if family == nativeUpstreamChat {
					final = d.observePayload([]byte("[DONE]"))
				}
				require.Equal(t, model.TerminalCompleted, final.Terminal)
				require.True(t, final.Usage.TerminalConfirmed)
				require.EqualValues(t, 2, final.Usage.Output.Value)
				require.EqualValues(t, 10, final.Usage.Total.Value, "a returned fact does not alias decoder state")
				require.EqualValues(t, 3, final.Usage.Reasoning.Value)
				want := model.UsagePartial
				if complete {
					want = model.UsageFinal
				}
				require.Equal(t, want, final.Usage.Confidence)
			})
		}
	}
}

func TestNativeHTTPReasoningOnlyTrailerDoesNotSettleEarlierWatermark(t *testing.T) {
	for _, family := range []nativeUpstreamFamily{nativeUpstreamResponses, nativeUpstreamChat} {
		t.Run(string(family), func(t *testing.T) {
			d := nativeOperationDecoder{kind: family, sse: true}
			first := "{\"type\":\"response.in_progress\",\"response\":{\"usage\":{\"input_tokens\":5,\"output_tokens\":2}}}"
			last := "{\"type\":\"response.completed\",\"response\":{\"usage\":{\"total_tokens\":10,\"output_tokens_details\":{\"reasoning_tokens\":3}}}}"
			if family == nativeUpstreamChat {
				first = "{\"choices\":[],\"usage\":{\"prompt_tokens\":5,\"completion_tokens\":2}}"
				last = "{\"choices\":[],\"usage\":{\"total_tokens\":10,\"completion_tokens_details\":{\"reasoning_tokens\":3}}}"
			}
			require.NotNil(t, d.observePayload([]byte(first)).Usage)
			fact := d.observePayload([]byte(last))
			if family == nativeUpstreamChat {
				fact = d.observePayload([]byte("[DONE]"))
			}
			require.Equal(t, model.TerminalCompleted, fact.Terminal)
			require.Equal(t, model.UsagePartial, fact.Usage.Confidence)
			require.EqualValues(t, 2, fact.Usage.Output.Value)
			require.Nil(t, fact.Usage.Total, "a separate auxiliary-only trailer cannot rewrite the prior snapshot")
			require.Nil(t, fact.Usage.Reasoning)
		})
	}
}
