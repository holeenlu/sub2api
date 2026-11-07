package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestImageEndpointPartialResultKeepsMixedTokenBoundary(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		ctx := context.Background()
		if dedicated {
			ctx = WithOpenAIImagesEndpoint(ctx)
		}
		result := &ForwardResult{ImageCount: 3, Usage: ClaudeUsage{InputTokens: 7, OutputTokens: 9}, ImageEndpoint: !dedicated}
		got := imageEndpointForwardResult(ctx, result)
		require.Same(t, result, got, "provenance does not replace observed partial usage")
		require.Equal(t, dedicated, got.ImageEndpoint)
		require.Equal(t, 3, got.ImageCount)
		require.Equal(t, 7, got.Usage.InputTokens)
		require.Equal(t, 9, got.Usage.OutputTokens)
		require.Nil(t, imageEndpointForwardResult(ctx, nil))
	}
}

func TestForwardImagesOAuthCountUsesActualOutputAndEndpoint(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("enabled=%t/stream=%t", enabled, stream), func(t *testing.T) {
				body := []byte(fmt.Sprintf(`{"model":"gpt-image-1","prompt":"draw","n":5,"stream":%t}`, stream))
				c, _ := newOpenAIImagesTestContext(t, body)
				c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformOpenAI, AllowImageGeneration: true, ImageCountBilling: enabled}})
				wire := "data: " + `{"type":"response.completed","response":{"id":"resp_images_count","status":"completed","output":[{"id":"img_1","type":"image_generation_call","result":"aGVsbG8="},{"id":"img_2","type":"image_generation_call","result":"aGVsbG8="},{"id":"img_3","type":"image_generation_call","result":"aGVsbG8="}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}}` + "\n\ndata: [DONE]\n\n"
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}, "X-Request-Id": {"req_oauth_count"}},
					Body: io.NopCloser(strings.NewReader(wire)),
				}}
				svc := newOpenAIImagesTestService(upstream)
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: map[string]any{
					"access_token": "token-test", "chatgpt_account_id": "account-test",
				}}
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				result, err := svc.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.ImageEndpoint)
				require.Equal(t, 3, result.ImageCount)
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, "req_oauth_count", result.RequestID)
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}

func TestForwardImagesCountUsesActualOutputAndEndpoint(t *testing.T) {
	for _, mode := range []string{"apikey", "bps"} {
		for _, enabled := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/enabled=%t", mode, enabled), func(t *testing.T) {
				body := []byte(`{"model":"gpt-image-2","prompt":"draw","n":5,"response_format":"b64_json"}`)
				c, _ := newOpenAIImagesTestContext(t, body)
				c.Set("api_key", &APIKey{Group: &Group{Platform: PlatformOpenAI, AllowImageGeneration: true, ImageCountBilling: enabled}})
				upstream := &httpUpstreamRecorder{resp: &http.Response{
					StatusCode: http.StatusOK,
					Header: http.Header{"Content-Type": {"application/json"}, "X-Request-Id": {"req_actual_images"}},
					Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="},{"b64_json":"aGVsbG8="},{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":10,"output_tokens":20,"total_tokens":30}}`)),
				}}
				svc, account := newOpenAIImagesTestService(upstream), newOpenAIImagesAPIKeyAccount()
				if mode == "bps" {
					svc, account = excelBPSImagesTestService(upstream), excelBPSImagesAccount()
				}
				parsed, err := svc.ParseOpenAIImagesRequest(c, body)
				require.NoError(t, err)
				result, err := svc.ForwardImages(c.Request.Context(), c, account, body, parsed, "")
				require.NoError(t, err)
				require.NotNil(t, result)
				require.True(t, result.ImageEndpoint)
				require.Equal(t, 3, result.ImageCount, "actual output wins over requested n=5")
				require.Equal(t, 10, result.Usage.InputTokens)
				require.Equal(t, 20, result.Usage.OutputTokens)
				require.Equal(t, "req_actual_images", result.RequestID)
				require.Len(t, upstream.requests, 1)
			})
		}
	}
}
