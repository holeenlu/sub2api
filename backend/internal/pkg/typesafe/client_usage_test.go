package typesafe

import (
	"strings"
	"testing"
)

func TestSystemOneObservedUsageDistinguishesAbsentInvalidAndZero(t *testing.T) {
	for _, tc := range []struct {
		name, body            string
		input, output         bool
		wantInput, wantOutput int64
	}{
		{name: "absent", body: `{"answers":{}}`},
		{name: "invalid", body: `{"usage":{"input_tokens":"many","output_tokens":null}}`},
		{name: "negative", body: `{"usage":{"input_tokens":-1}}`},
		{name: "explicit zero", body: `{"usage":{"input_tokens":0,"output_tokens":"0"}}`, input: true, output: true},
		{name: "partial", body: `{"usage":{"input_tokens":"12"}}`, input: true, wantInput: 12},
		{name: "numeric compatibility", body: `{"usage":{"input_tokens":" 12 ","output_tokens":2.6}}`, input: true, output: true, wantInput: 12, wantOutput: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodeSystemOneResponse(strings.NewReader(tc.body))
			if err != nil {
				t.Fatal(err)
			}
			if string(decoded.Body) != tc.body {
				t.Fatal("response wire changed")
			}
			u := decoded.ObservedUsage
			if (u.InputTokens != nil) != tc.input || (u.OutputTokens != nil) != tc.output {
				t.Fatalf("presence: %+v", u)
			}
			if u.InputTokens != nil && *u.InputTokens != tc.wantInput {
				t.Fatalf("input: %d", *u.InputTokens)
			}
			if u.OutputTokens != nil && *u.OutputTokens != tc.wantOutput {
				t.Fatalf("output: %d", *u.OutputTokens)
			}
			if u.Complete() != (tc.input && tc.output) {
				t.Fatal("partial measurement became complete")
			}
		})
	}
}
