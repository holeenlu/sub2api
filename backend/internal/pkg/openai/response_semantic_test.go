package openai

import "testing"

func TestResponseStartsSemanticOutput(t *testing.T) {
	tests := []struct {
		name, event, payload string
		want                 bool
	}{
		{name: "metadata", event: "response.created", payload: `{"type":"response.created"}`, want: false},
		{name: "reasoning item", event: "response.output_item.added", payload: `{"type":"response.output_item.added","item":{"type":"reasoning"}}`, want: true},
		{name: "summary part", event: "response.reasoning_summary_part.added", payload: `{"type":"response.reasoning_summary_part.added","part":{"type":"summary_text"}}`, want: true},
		{name: "text delta", event: "response.output_text.delta", payload: `{"type":"response.output_text.delta","delta":"hello"}`, want: true},
		{name: "empty text delta", event: "response.output_text.delta", payload: `{"type":"response.output_text.delta","delta":""}`, want: false},
		{name: "terminal", event: "response.completed", payload: `{"type":"response.completed"}`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResponseStartsSemanticOutput([]byte(tt.payload), tt.event); got != tt.want {
				t.Fatalf("ResponseStartsSemanticOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}
