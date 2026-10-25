package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
)

// Image-history compaction runs before generation. Its transport failures,
// 5xx and 401 follow the same pre-generation recovery as a failed generation
// request instead of ending the turn with a 502/503 on this account.
func TestExcelBPSCompactionFailuresFollowPreGenerationRecovery(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		transport  bool
		wantReason GatewayFailureReason
		wantStatus int
	}{
		{name: "transport", transport: true, wantReason: ExcelBPSUpstreamFailureReason, wantStatus: http.StatusBadGateway},
		{name: "503", status: http.StatusServiceUnavailable, wantReason: ExcelBPSUpstreamFailureReason, wantStatus: http.StatusServiceUnavailable},
		{name: "401", status: http.StatusUnauthorized, wantReason: "basispoints_credentials_rejected", wantStatus: http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			t.Cleanup(func() { require.NoError(t, svc.CloseExcelBPSImages()) })
			svc.cache = newImagePolicyMemoryCache()
			repo := &excelBPSImageSettingsRepo{values: map[string]string{SettingKeyExcelBPSEnabled: "true", SettingKeyExcelBPSImageRelayEnabled: "true", SettingKeyExcelBPSImageMode: "native", SettingKeyExcelBPSImageBaseURL: "https://images.example", SettingKeyExcelBPSImageLimitPolicy: "auto_compact"}}
			svc.settingService = NewSettingService(repo, svc.cfg)
			account := excelAccount()
			raw := imagePolicyRequest(t, 19, 2)
			raw, e := sjson.SetBytes(raw, "model", "gpt-6-astra")
			require.NoError(t, e)
			var phase int
			svc.httpUpstream = &nativeAttachmentUpstream{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
				_, e := io.ReadAll(req.Body)
				require.NoError(t, e)
				if req.URL.String() == basispoints.AttachmentsURL {
					return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"openai_file_id":"file-policy"}`))}, nil
				}
				phase++
				if tc.transport {
					return nil, &url.Error{Op: "Post", URL: "https://invalid.test", Err: io.ErrUnexpectedEOF}
				}
				return &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM"}}`))}, nil
			}}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", bytes.NewReader(raw))
			c.Request.Header.Set("User-Agent", "codex_cli_rs/0.116.0")
			c.Request.Header.Set("session_id", "policy-session")
			_, err := svc.Forward(context.Background(), c, account, raw)
			var failover *UpstreamFailoverError
			require.ErrorAs(t, err, &failover, "forward error: %v", err)
			require.Equal(t, tc.wantReason, failover.Reason)
			require.Equal(t, tc.wantStatus, failover.StatusCode)
			require.True(t, failover.RejectedBeforeGeneration, "compaction never started generation")
			require.Equal(t, 1, phase, "generation must not start after a failed compaction")
			require.False(t, c.Writer.Written(), "the handler owns the final answer after recovery")
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
		})
	}
}

func TestServeExcelBPSImageKeepsRelayWhenSettingsUnreadable(t *testing.T) {
	relay, err := basispoints.NewImageRelay("https://images.example", t.TempDir())
	require.NoError(t, err)
	t.Cleanup(func() { _ = relay.Close() })
	var pngBytes bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	require.NoError(t, png.Encode(&pngBytes, img))
	body, err := json.Marshal(map[string]any{"input": []any{map[string]any{"type": "message", "role": "user", "content": []any{map[string]any{"type": "input_image", "image_url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes.Bytes())}}}}})
	require.NoError(t, err)
	rewritten, err := relay.Rewrite(body, "scope")
	require.NoError(t, err)
	imageURL := gjson.GetBytes(rewritten, "input.0.content.0.image_url").String()
	require.True(t, strings.HasPrefix(imageURL, "https://images.example/"), imageURL)
	parsed, err := url.Parse(imageURL)
	require.NoError(t, err)

	svc := excelBPSGatewayTestService(nil)
	svc.excelBPSImages = relay
	svc.settingService = NewSettingService(&excelBPSImageSettingsRepo{err: errors.New("store down")}, svc.cfg)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	svc.ServeExcelBPSImage(c)
	require.Equal(t, http.StatusOK, rec.Code, "an in-flight generation keeps its published images while settings are unreadable")
	require.Equal(t, pngBytes.Bytes(), rec.Body.Bytes())

	// Without a configured relay an unreadable store still answers 404.
	svc.excelBPSImages = nil
	rec = httptest.NewRecorder()
	c, _ = gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodGet, parsed.RequestURI(), nil)
	svc.ServeExcelBPSImage(c)
	require.Equal(t, http.StatusNotFound, rec.Code)
}
