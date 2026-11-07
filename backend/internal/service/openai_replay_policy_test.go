package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIForwardReplaySafetyFollowsFinalToolPolicy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name             string
		tools            string
		extra            map[string]any
		wantImage        bool
		wantRecordedSafe bool
	}{
		{"strip_client_image", `[{"type":"image_generation"}]`, map[string]any{"codex_image_generation_explicit_tool_policy": "strip"}, false, true},
		{"passthrough_strip_client_image", `[{"type":"image_generation"}]`, map[string]any{"openai_passthrough": true, "codex_image_generation_explicit_tool_policy": "strip"}, false, true},
		{"disable_injection_keeps_client_image", `[{"type":"image_generation"}]`, map[string]any{"codex_image_generation_bridge": false}, true, false},
		{"inject_hosted_image", `[{"type":"function","name":"local_fn","parameters":{"type":"object","properties":{}}}]`, map[string]any{"codex_image_generation_bridge": true}, true, false},
		{"flatten_namespace", `[{"type":"namespace","name":"local","tools":[{"type":"function","name":"run","parameters":{"type":"object","properties":{}}}]}]`, map[string]any{"openai_responses_flatten_namespaces": true}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(nil))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.144.1")
			c.Set("api_key", &APIKey{ID: 99, Group: &Group{ID: 21, AllowImageGeneration: true}})
			upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 500, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"server_error","code":"server_error","message":"mock server failure"}}`))}}
			svc := &OpenAIGatewayService{cfg: &config.Config{}, httpUpstream: upstream}
			account := &Account{ID: 124, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Concurrency: 1, Credentials: map[string]any{"access_token": "test-token", "chatgpt_account_id": "test-account"}, Extra: tc.extra, Status: StatusActive, Schedulable: true, RateMultiplier: f64p(1)}
			body := []byte(`{"model":"gpt-5.5","stream":true,"instructions":"test","input":"test","tools":` + tc.tools + `}`)
			_, err := svc.Forward(context.Background(), c, account, body)
			require.Error(t, err)
			require.NotEmpty(t, upstream.lastBody, "must reach mock upstream")
			hasImage := gjson.GetBytes(upstream.lastBody, `tools.#(type=="image_generation")`).Exists()
			safe, known := c.Get("openai_uncertain_replay_safe")
			require.True(t, known)
			require.Equal(t, tc.wantImage, hasImage)
			require.Equal(t, tc.wantRecordedSafe, safe)
			if tc.name == "flatten_namespace" {
				require.Equal(t, "function", gjson.GetBytes(upstream.lastBody, "tools.0.type").String())
			}
			require.Equal(t, openAIWSUncertainExecutionReplaySafe(upstream.lastBody), safe, "replay policy must describe what the provider actually received")
		})
	}
}
