package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ctxkey"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestCloudFastPolicyHTTPAndWSAgree(t *testing.T) {
	filter := OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierPriority, Action: BetaPolicyActionFilter,
		Scope: BetaPolicyScopeOAuth, ModelWhitelist: []string{"gpt-5.1"}, FallbackAction: BetaPolicyActionBlock}
	forceMissing := OpenAIFastPolicyRule{ServiceTier: OpenAIFastTierMissing, Action: OpenAIFastPolicyActionForcePriority, Scope: BetaPolicyScopeOAuth}
	block := filter
	block.Action, block.ErrorMessage = BetaPolicyActionBlock, "Fast policy denied this model"
	for _, test := range []struct {
		name       string
		tier       string
		forceGroup bool
		rules      []OpenAIFastPolicyRule
		status     int
	}{
		// The missing-tier rule detects accidental policy application twice: a
		// previously filtered WS frame must not regain priority in the HTTP bridge.
		{"filter_after_model_mapping_once", "fast", false, []OpenAIFastPolicyRule{filter, forceMissing}, http.StatusOK},
		{"group_force_then_filter", "", true, []OpenAIFastPolicyRule{filter}, http.StatusOK},
		{"force_missing_is_unsupported", "", false, []OpenAIFastPolicyRule{forceMissing}, http.StatusBadRequest},
		{"group_force_is_unsupported", "", true, nil, http.StatusBadRequest},
		{"pass_does_not_claim_fast_support", "fast", false, nil, http.StatusBadRequest},
		{"block_before_cloud_dispatch", "fast", false, []OpenAIFastPolicyRule{block}, http.StatusForbidden},
	} {
		for _, transport := range []string{"json", "sse", "ws"} {
			t.Run(test.name+"/"+transport, func(t *testing.T) {
				svc, in, store, wire := cloudForwardFixture(t, nil)
				svc.settingService = newOpenAIGatewayServiceWithSettings(t, &OpenAIFastPolicySettings{Rules: test.rules}).settingService
				ctx, cancel := context.WithTimeout(t.Context(), 6*time.Second)
				defer cancel()
				ctx = context.WithValue(ctx, ctxkey.Group, &Group{ID: *in.APIKey.GroupID, Platform: PlatformOpenAI,
					Status: StatusActive, Hydrated: true, ForceOpenAIFast: test.forceGroup})
				if test.status == http.StatusOK {
					done := make(chan struct{})
					defer func() { cancel(); <-done }()
					go func() { defer close(done); cloudWireStart(t, ctx, wire); cloudWireComplete(wire) }()
				}
				body := map[string]any{"model": "client-model", "input": "hello", "stream": transport != "json"}
				if test.tier != "" {
					body["service_tier"] = test.tier
				}
				var raw string
				if transport == "ws" {
					body["type"] = "response.create"
					body["model"] = "public-model"
					request, err := json.Marshal(body)
					require.NoError(t, err)
					raw = cloudFastPolicyWSResponse(t, svc, in, ctx, request)
				} else {
					request, err := json.Marshal(body)
					require.NoError(t, err)
					requestCtx, c, rec := cloudContractContext(t, svc, in, ctx, request)
					result, err := svc.Forward(requestCtx, c, in.Account, request)
					require.Equal(t, test.status, rec.Code, rec.Body.String())
					if test.status == http.StatusOK {
						require.NoError(t, err)
						require.Equal(t, "gpt-5.1", result.UpstreamModel)
					} else {
						require.Error(t, err)
						require.Nil(t, result)
					}
					raw = rec.Body.String()
				}
				if test.status == http.StatusOK {
					require.Contains(t, raw, `"completed"`)
					require.Equal(t, 1, store.settlementCount())
				} else {
					if test.status == http.StatusBadRequest {
						require.Contains(t, raw, "cloud_unsupported_parameter")
						require.Contains(t, raw, "service_tier")
						if transport == "ws" {
							require.Equal(t, int64(http.StatusBadRequest), gjson.Get(raw, "status").Int())
						}
					} else {
						require.Contains(t, raw, block.ErrorMessage)
					}
					require.Empty(t, wire.writes)
					require.Nil(t, store.snapshot())
					require.Zero(t, store.settlementCount())
					require.Empty(t, svc.cloudBillingWorker.holds)
				}
			})
		}
	}
}

// Exercise the real response.create policy and bridge, not a direct call to
// the Cloud adapter that would bypass WS normalization and model mapping.
func cloudFastPolicyWSResponse(t *testing.T, svc *OpenAIGatewayService, in *OpenAIRecordUsageInput, ctx context.Context, request []byte) string {
	t.Helper()
	ended := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := coderws.Accept(w, r, nil)
		if err != nil {
			ended <- err
			return
		}
		defer conn.CloseNow()
		_, first, err := conn.Read(ctx)
		if err != nil {
			ended <- err
			return
		}
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = r.WithContext(ctx)
		c.Set("api_key", in.APIKey)
		hooks := &OpenAIWSIngressHooks{
			MapRequestModel: func(_ int, original string) (string, error) {
				require.Equal(t, "public-model", original)
				return "client-model", nil
			},
			PrepareCloudTurn: func(c context.Context, _ int, _ []byte, _ string) (context.Context, error) {
				return svc.CaptureCloudBillingSnapshot(c, in)
			},
		}
		ended <- svc.ProxyResponsesWebSocketFromClient(ctx, c, conn, in.Account, "unused", first, hooks)
	}))
	defer server.Close()
	conn, _, err := coderws.Dial(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	require.NoError(t, err)
	defer conn.CloseNow()
	require.NoError(t, conn.Write(ctx, coderws.MessageText, request))
	var terminal []byte
	for {
		_, terminal, err = conn.Read(ctx)
		require.NoError(t, err)
		typ := gjson.GetBytes(terminal, "type").String()
		if typ == "error" || typ == "response.completed" || typ == "response.failed" {
			break
		}
	}
	_ = conn.Close(coderws.StatusNormalClosure, "done")
	select {
	case err = <-ended:
		if gjson.GetBytes(terminal, "type").String() == "response.completed" {
			require.NoError(t, err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	return string(terminal)
}
