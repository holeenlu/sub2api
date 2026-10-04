package service

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestOpenAIWSBPSHostedFallbackUsesNativeWS(t *testing.T) {
	for _, mode := range []string{OpenAIWSIngressModeCtxPool, OpenAIWSIngressModePassthrough} {
		for _, prewarm := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/prewarm=%v", mode, prewarm), func(t *testing.T) {
				cfg := passthroughLifecycleConfig()
				cfg.Gateway.OpenAIWS.OAuthEnabled = true
				upstream := newStagedPassthroughConn()
				svc := newPassthroughLifecycleService(cfg, upstream)
				pool := newOpenAIWSConnPool(cfg)
				t.Cleanup(pool.Close)
				pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &nativeCodexStagedConn{upstream}})
				svc.openaiWSPool = pool
				account := excelAccount()
				account.Extra["openai_oauth_responses_websockets_v2_mode"] = mode
				account.Extra["openai_oauth_responses_websockets_v2_enabled"] = true
				// Fresh admission must validate the saved BPS account, not a mutated
				// snapshot used only for selecting the native transport.
				svc.accountRepo = &turnAdmissionRepo{account: account}
				first := `{"type":"response.create","model":"gpt-6-astra","input":"search","tools":[{"type":"web_search","external_web_access":true}]}`
				if prewarm {
					first = `{"generate":false,` + first[1:]
				}
				client, serverErr := startBPSWSMemorySession(t, svc, account, first, nil)
				defer client.CloseNow()
				payload := requirePassthroughUpstreamWrite(t, upstream, time.Second)
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(payload, "model").String())
				if prewarm {
					require.Equal(t, gjson.False, gjson.GetBytes(payload, "generate").Type)
				}
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_native_hosted","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`)
				readBPSWSTestTurn(t, client)
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","previous_response_id":"resp_native_hosted","input":"continue"}`)))
				payload = requirePassthroughUpstreamWrite(t, upstream, time.Second)
				require.Equal(t, "gpt-6-astra", gjson.GetBytes(payload, "model").String(), "omitted tools must inherit the native hosted-tool route")
				upstream.Send(`{"type":"response.completed","response":{"id":"resp_native_hosted_2","model":"gpt-6-astra","usage":{"input_tokens":2,"output_tokens":1}}}`)
				readBPSWSTestTurn(t, client)
				require.True(t, account.IsExcelBPSEnabled(), "protocol selection must not mutate saved BPS settings")
				require.Empty(t, svc.httpUpstream.(*httpUpstreamRecorder).requests)
				require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(`{"type":"response.create","model":"gpt-6-astra","input":"plain turn","tools":[]}`)))
				go func() {
					for {
						if _, _, err := client.Read(context.Background()); err != nil {
							return
						}
					}
				}()
				select {
				case err := <-serverErr:
					require.ErrorIs(t, err, ErrOpenAIWSModelSwitchRequiresReconnect, "explicit empty tools switches the next request back to BPS")
				case <-time.After(3 * time.Second):
					t.Fatal("native connection did not stop before switching to BPS")
				}
			})
		}
	}
}

func TestOpenAIWSManualHTTPBridgePreservesBPSAndNativeSwitching(t *testing.T) {
	for _, tc := range []struct {
		name, first, next, wantHost string
	}{
		{"bps to native", `{"type":"response.create","model":"gpt-6-astra","input":"first"}`, `{"type":"response.create","model":"gpt-6-sol","input":"next"}`, "chatgpt.com"},
		{"native to bps", `{"type":"response.create","model":"gpt-6-sol","input":"first"}`, `{"type":"response.create","model":"gpt-6-astra","input":"next"}`, "bps.openai.com"},
		{"bps to hosted capability", `{"type":"response.create","model":"gpt-6-astra","input":"first"}`, `{"type":"response.create","model":"gpt-6-astra","input":"next","tools":[{"type":"web_search","external_web_access":true}]}`, "chatgpt.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := &httpUpstreamRecorder{responses: []*http.Response{
				bpsCompletionResponse(200, bpsCompletedStream("resp_first", "first")),
				bpsCompletionResponse(200, bpsCompletedStream("resp_next", "next")),
			}}
			svc := openAIClientToolsTestService(upstream)
			svc.cfg = passthroughLifecycleConfig()
			account := excelAccount()
			account.Extra["openai_excel_bps_models"] = []string{"gpt-6-astra"}
			account.Extra["openai_oauth_responses_websockets_v2_mode"] = OpenAIWSIngressModeHTTPBridge
			client, serverErr := startBPSWSMemorySession(t, svc, account, tc.first, nil)
			defer client.CloseNow()
			readBPSWSTestTurn(t, client)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			require.NoError(t, client.Write(ctx, coderws.MessageText, []byte(tc.next)))
			readBPSWSTestTurn(t, client)
			require.Len(t, upstream.requests, 2)
			require.Equal(t, tc.wantHost, upstream.lastReq.URL.Host)
			requireBPSWSTestServerExit(t, client, serverErr)
		})
	}
}
