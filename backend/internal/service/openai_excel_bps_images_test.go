package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const (
	codexDirectImagesURL      = "https://chatgpt.com/backend-api/codex/images/generations"
	codexDirectImagesEditsURL = "https://chatgpt.com/backend-api/codex/images/edits"
)

func newOpenAIImagesEditsTestContext(t *testing.T, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/edits", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = req
	return c, rec
}

func excelBPSImagesAccount(models ...any) *Account {
	account := excelAccount()
	if len(models) == 0 {
		models = []any{"gpt-image-2"}
	}
	account.Extra["openai_excel_bps_models"] = models
	return account
}

// excelBPSImagesUpstream answers BPS with status and Codex with a normal image.
func excelBPSImagesUpstream(status int, urls *[]string) *codexModelsHTTPUpstreamStub {
	return &codexModelsHTTPUpstreamStub{do: func(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
		*urls = append(*urls, req.URL.String())
		if req.URL.Host == "bps.openai.com" && status != http.StatusOK {
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"detail":"PRIVATE_UPSTREAM"}`))}, nil
		}
		return openAIImagesJSONResponse(), nil
	}}
}

func TestExcelBPSImagesRequiresExplicitImageModel(t *testing.T) {
	legacy := excelAccount()
	delete(legacy.Extra, "openai_excel_bps_models")
	require.False(t, legacy.IsExcelBPSEnabledForModel("gpt-image-2"))
	require.False(t, legacy.IsExcelBPSImagesEnabledForModel("gpt-image-2"), "missing selection keeps images on Codex")
	require.False(t, excelBPSImagesAccount("gpt-6-astra").IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	require.True(t, excelBPSImagesAccount().IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	require.False(t, excelBPSImagesAccount("gpt-image-1").IsExcelBPSImagesEnabledForModel("gpt-image-1"), "the Responses image tool has no BPS route")
	require.False(t, excelBPSImagesAccount("gpt-image-2.5-flare").IsExcelBPSImagesEnabledForModel("gpt-image-2.5-flare"),
		"BPS rejects every direct-image model except gpt-image-2, so unlisted ones skip the wasted round trip")

	mapped := excelBPSImagesAccount()
	mapped.Credentials["model_mapping"] = map[string]any{"gpt-image-1": "gpt-image-2"}
	require.True(t, mapped.IsExcelBPSImagesEnabledForModel("gpt-image-1"), "the list matches the mapped model")

	disabled := excelBPSImagesAccount()
	disabled.Extra["openai_excel_bps"] = false
	require.False(t, disabled.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
	apiKey := excelBPSImagesAccount()
	apiKey.Type = AccountTypeAPIKey
	require.False(t, apiKey.IsExcelBPSImagesEnabledForModel("gpt-image-2"))
}

func TestExcelBPSImagesUnsupportedReason(t *testing.T) {
	one := 1
	base := func() *OpenAIImagesRequest {
		return &OpenAIImagesRequest{Endpoint: openAIImagesGenerationsEndpoint, Model: "gpt-image-2", Prompt: "draw",
			N: 2, Size: "1024x1024", Quality: "high", Background: "opaque", OutputFormat: "PNG", Moderation: "auto", ResponseFormat: "url"}
	}
	require.Empty(t, excelBPSImagesUnsupportedReason(base()))
	for want, mutate := range map[string]func(*OpenAIImagesRequest){
		"stream":             func(r *OpenAIImagesRequest) { r.Stream = true },
		"partial_images":     func(r *OpenAIImagesRequest) { r.PartialImages = &one },
		"output_compression": func(r *OpenAIImagesRequest) { r.OutputCompression = &one },
		"style":              func(r *OpenAIImagesRequest) { r.Style = "vivid" },
		"input_fidelity":     func(r *OpenAIImagesRequest) { r.InputFidelity = "high" },
		"output_format":      func(r *OpenAIImagesRequest) { r.OutputFormat = "jpeg" },
		"moderation":         func(r *OpenAIImagesRequest) { r.Moderation = "low" },
		"background":         func(r *OpenAIImagesRequest) { r.Background = "transparent" },
	} {
		parsed := base()
		mutate(parsed)
		require.Equal(t, want, excelBPSImagesUnsupportedReason(parsed))
	}
	require.Equal(t, "missing request", excelBPSImagesUnsupportedReason(nil))

	// Edits need exactly one local image and no mask; the rest stays on Codex.
	edits := func() *OpenAIImagesRequest {
		parsed := base()
		parsed.Endpoint = openAIImagesEditsEndpoint
		parsed.Uploads = []OpenAIImagesUpload{{FieldName: "image", FileName: "src.png", ContentType: "image/png", Data: []byte("PNG")}}
		return parsed
	}
	require.Empty(t, excelBPSImagesUnsupportedReason(edits()))
	dataURLOnly := edits()
	dataURLOnly.Uploads = nil
	dataURLOnly.InputImageURLs = []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("PNG"))}
	require.Empty(t, excelBPSImagesUnsupportedReason(dataURLOnly))
	for want, mutate := range map[string]func(*OpenAIImagesRequest){
		"mask":        func(r *OpenAIImagesRequest) { r.HasMask = true },
		"image count": func(r *OpenAIImagesRequest) { r.Uploads = append(r.Uploads, r.Uploads[0]) },
		"remote image_url": func(r *OpenAIImagesRequest) {
			r.Uploads = nil
			r.InputImageURLs = []string{"https://example.com/a.png"}
		},
	} {
		parsed := edits()
		mutate(parsed)
		require.Equal(t, want, excelBPSImagesUnsupportedReason(parsed))
	}
	noImages := edits()
	noImages.Uploads = nil
	require.Equal(t, "image count", excelBPSImagesUnsupportedReason(noImages))
}

func TestExcelBPSImagesForwardContract(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"  draw  ","size":"1024x1024","quality":"low","n":2,"background":"opaque","output_format":"png","moderation":"auto","response_format":"url"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := excelBPSImagesTestService(&codexModelsHTTPUpstreamStub{do: func(req *http.Request, proxy string, id int64, concurrency int) (*http.Response, error) {
		resp, err := upstream.Do(req, proxy, id, concurrency)
		cancel() // Generation has started; retain its result and usage after disconnect.
		require.NoError(t, req.Context().Err())
		return resp, err
	}})
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	result, err := svc.ForwardImages(ctx, c, excelBPSImagesAccount(), body, parsed, "")

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.lastReq
	require.Equal(t, basispoints.ImagesGenerationsURL, req.URL.String())
	require.NoError(t, upstream.contextErrAtDo)
	require.ErrorIs(t, req.Context().Err(), context.Canceled, "completed upstream context must be released")
	for key, want := range map[string]string{
		"Authorization": "Bearer test-token", "Chatgpt-Account-Id": "test-account", "X-Openai-Account-Id": "test-account",
		"X-Basispoints-Auth-Mode": "chatgpt", "Accept": "application/json", "Content-Type": "application/json",
		"Origin": "https://bps.openai.com", "User-Agent": "Mozilla/5.0",
		"X-Openai-Internal-Basispoints-Client-Product":        "basispoints-excel-plugin",
		"X-Openai-Internal-Basispoints-Client-Agent-Profile":  "excel",
		"X-Openai-Internal-Basispoints-Client-Platform":       "excel",
		"X-Openai-Internal-Basispoints-Client-Editor":         "excel",
		"X-Openai-Internal-Basispoints-Client-Host":           "office",
		"X-Openai-Internal-Basispoints-Client-Runtime":        "desktop",
		"X-Openai-Internal-Basispoints-Client-Platform-Class": "PC",
		"X-Openai-Internal-Basispoints-Office-Host":           "Excel",
		"X-Openai-Internal-Basispoints-Office-Platform":       "PC",
	} {
		require.Equal(t, want, req.Header.Get(key), key)
	}
	require.Empty(t, req.Header.Get("Originator"), "Codex identity headers must not reach BPS")
	require.Equal(t, HTTPUpstreamProfileExcelBPS, HTTPUpstreamProfileFromContext(req.Context()))
	require.True(t, HTTPUpstreamRedirectsDisabled(req.Context()))

	sent := upstream.lastBody
	require.Equal(t, "gpt-image-2", gjson.GetBytes(sent, "model").String())
	require.Equal(t, "  draw  ", gjson.GetBytes(sent, "prompt").String())
	require.Equal(t, "low", gjson.GetBytes(sent, "quality").String())
	require.Equal(t, "1024x1024", gjson.GetBytes(sent, "size").String())
	require.Equal(t, int64(2), gjson.GetBytes(sent, "n").Int())
	for _, key := range []string{"moderation", "response_format", "stream", "user", "style", "input_fidelity"} {
		require.False(t, gjson.GetBytes(sent, key).Exists(), key)
	}

	require.Equal(t, excelBPSImagesEndpoint, result.UpstreamEndpoint)
	require.Equal(t, UsageAuthorizationModeBPS, result.AuthorizationMode)
	require.Equal(t, excelBPSImagesEndpoint, GetActualOpenAIUpstreamEndpoint(c))
	require.Equal(t, "gpt-image-2", result.UpstreamModel)
	require.Equal(t, 1, result.ImageCount)
	require.Equal(t, 20, result.Usage.ImageOutputTokens)
	require.Equal(t, "data:image/png;base64,aGVsbG8=", gjson.GetBytes(rec.Body.Bytes(), "data.0.url").String())
}

// The multipart body carries the same fields the generations endpoint takes,
// plus exactly one image file part with the upload's own MIME type.
func TestExcelBPSImagesEditsBody(t *testing.T) {
	parsed := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2", Prompt: "add a red hat",
		Multipart: true, N: 2, Size: "1024x1024", Quality: "low", Background: "opaque", OutputFormat: "png", Moderation: "auto",
		Uploads: []OpenAIImagesUpload{{FieldName: "image", FileName: "src.png", ContentType: "image/png", Data: []byte("PNGDATA")}}}

	body, contentType, err := buildExcelBPSImagesEditsBody(parsed, "gpt-image-2")

	require.NoError(t, err)
	mediaType, params, err := mime.ParseMediaType(contentType)
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()
	require.Equal(t, map[string][]string{"model": {"gpt-image-2"}, "prompt": {"add a red hat"}, "size": {"1024x1024"},
		"quality": {"low"}, "background": {"opaque"}, "output_format": {"png"}, "n": {"2"}}, map[string][]string(form.Value),
		"moderation must not be forwarded")
	require.Len(t, form.File["image"], 1)
	file := form.File["image"][0]
	require.Equal(t, "src.png", file.Filename)
	require.Equal(t, "image/png", file.Header.Get("Content-Type"))
	opened, err := file.Open()
	require.NoError(t, err)
	defer func() { _ = opened.Close() }()
	data, err := io.ReadAll(opened)
	require.NoError(t, err)
	require.Equal(t, "PNGDATA", string(data))

	// A JSON edit carries the image as a data URL instead of an upload.
	fromDataURL := &OpenAIImagesRequest{Endpoint: openAIImagesEditsEndpoint, Model: "gpt-image-2", Prompt: "add a red hat",
		InputImageURLs: []string{"data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("RAWPNG"))}}
	body, contentType, err = buildExcelBPSImagesEditsBody(fromDataURL, "gpt-image-2")
	require.NoError(t, err)
	_, params, err = mime.ParseMediaType(contentType)
	require.NoError(t, err)
	form, err = multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()
	require.Equal(t, map[string][]string{"model": {"gpt-image-2"}, "prompt": {"add a red hat"}}, map[string][]string(form.Value))
	require.Len(t, form.File["image"], 1)
	require.Equal(t, "image.png", form.File["image"][0].Filename)
	require.Equal(t, "image/png", form.File["image"][0].Header.Get("Content-Type"))
}

func TestExcelBPSImagesEditsForwardContract(t *testing.T) {
	body := []byte(`{"model":"gpt-image-2","prompt":"add a red hat","images":[{"image_url":"data:image/png;base64,` +
		base64.StdEncoding.EncodeToString([]byte("RAWPNG")) + `"}]}`)
	c, _ := newOpenAIImagesEditsTestContext(t, body)
	upstream := &httpUpstreamRecorder{resp: openAIImagesJSONResponse()}
	svc := excelBPSImagesTestService(upstream)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	result, err := svc.ForwardImages(context.Background(), c, excelBPSImagesAccount(), body, parsed, "")

	require.NoError(t, err)
	require.Len(t, upstream.requests, 1)
	req := upstream.lastReq
	require.Equal(t, basispoints.ImagesEditsURL, req.URL.String())
	require.Equal(t, "excel", req.Header.Get("X-Openai-Internal-Basispoints-Client-Editor"))
	mediaType, params, err := mime.ParseMediaType(req.Header.Get("Content-Type"))
	require.NoError(t, err)
	require.Equal(t, "multipart/form-data", mediaType)
	form, err := multipart.NewReader(bytes.NewReader(upstream.lastBody), params["boundary"]).ReadForm(1 << 20)
	require.NoError(t, err)
	defer func() { _ = form.RemoveAll() }()
	require.Equal(t, "gpt-image-2", form.Value["model"][0])
	require.Len(t, form.File["image"], 1)
	require.Equal(t, excelBPSImagesEditsEndpoint, result.UpstreamEndpoint)
	require.Equal(t, excelBPSImagesEditsEndpoint, GetActualOpenAIUpstreamEndpoint(c))
}

func TestExcelBPSImagesEditsFormatRejectionFallsBackToCodex(t *testing.T) {
	var urls []string
	svc := excelBPSImagesTestService(excelBPSImagesUpstream(http.StatusUnprocessableEntity, &urls))
	body := []byte(`{"model":"gpt-image-2","prompt":"add a red hat","images":[{"image_url":"data:image/png;base64,` +
		base64.StdEncoding.EncodeToString([]byte("RAWPNG")) + `"}]}`)
	c, _ := newOpenAIImagesEditsTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)

	account := excelBPSImagesAccount()
	useExcelBPSMixedGrant(t, svc, account)
	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.NoError(t, err)
	require.Equal(t, []string{basispoints.ImagesEditsURL, codexDirectImagesEditsURL}, urls)
	require.Equal(t, "/backend-api/codex/images/edits", result.UpstreamEndpoint)
	require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
}

func TestExcelBPSImagesFormatRejectionFallsBackToCodex(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusNotFound, http.StatusMethodNotAllowed, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var urls []string
			svc := excelBPSImagesTestService(excelBPSImagesUpstream(status, &urls))
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := excelBPSImagesAccount()
			useExcelBPSMixedGrant(t, svc, account)
			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

			require.NoError(t, err)
			require.Equal(t, []string{basispoints.ImagesGenerationsURL, codexDirectImagesURL}, urls)
			require.Equal(t, "/backend-api/codex/images/generations", result.UpstreamEndpoint)
			require.Equal(t, UsageAuthorizationModeNative, result.AuthorizationMode)
			require.NotEqual(t, excelBPSImagesEndpoint, GetActualOpenAIUpstreamEndpoint(c))
			require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-image-2"))
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
		})
	}
}

func TestExcelBPSImagesStayOnCodexWhenBPSCannotServe(t *testing.T) {
	for name, tc := range map[string]struct {
		account *Account
		body    string
	}{
		"image model not selected": {account: excelAccount(), body: `{"model":"gpt-image-2","prompt":"draw"}`},
		"model not listed":         {account: excelBPSImagesAccount("gpt-6-astra"), body: `{"model":"gpt-image-2","prompt":"draw"}`},
		"jpeg output":              {account: excelBPSImagesAccount(), body: `{"model":"gpt-image-2","prompt":"draw","output_format":"jpeg"}`},
		"model BPS rejects":        {account: excelBPSImagesAccount("gpt-image-2.5-flare"), body: `{"model":"gpt-image-2.5-flare","prompt":"draw"}`},
		"transparent bg":           {account: excelBPSImagesAccount(), body: `{"model":"gpt-image-2","prompt":"draw","background":"transparent"}`},
	} {
		t.Run(name, func(t *testing.T) {
			var urls []string
			svc := excelBPSImagesTestService(excelBPSImagesUpstream(http.StatusOK, &urls))
			body := []byte(tc.body)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)

			delete(tc.account.Credentials, "client_id")
			_, err = svc.ForwardImages(context.Background(), c, tc.account, body, parsed, "")

			require.NoError(t, err)
			require.Equal(t, []string{codexDirectImagesURL}, urls)
		})
	}
}

func TestExcelBPSImages429CoolsBPSAndFailsOver(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"30"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM"}}`))}}
	svc := excelBPSImagesTestService(upstream)
	body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	c, rec := newOpenAIImagesTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := excelBPSImagesAccount()

	result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

	require.Nil(t, result)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.Len(t, upstream.requests, 1, "a request that reached BPS is never replayed")
	require.True(t, svc.isExcelBPSCoolingDown(account, "gpt-image-2"))
	// The next account may use Codex, so nothing BPS-specific is left behind.
	require.Empty(t, GetActualOpenAIUpstreamEndpoint(c))
	require.False(t, c.Writer.Written())
	require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
}

func TestExcelBPSImagesFinalErrorsAreNotReplayed(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusInternalServerError, http.StatusBadGateway} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			var urls []string
			svc := excelBPSImagesTestService(excelBPSImagesUpstream(status, &urls))
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := excelBPSImagesAccount()

			result, err := svc.ForwardImages(context.Background(), c, account, body, parsed, "")

			require.Nil(t, result)
			var upErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upErr)
			require.Equal(t, excelBPSUpstreamAuthClientStatus(status), upErr.StatusCode)
			require.Equal(t, []string{basispoints.ImagesGenerationsURL}, urls)
			require.Equal(t, excelBPSUpstreamAuthClientStatus(status), rec.Code)
			require.Equal(t, "basispoints_upstream_error", gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
			require.Contains(t, gjson.GetBytes(rec.Body.Bytes(), "error.message").String(), "Excel BPS")
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			require.Equal(t, excelBPSImagesEndpoint, GetActualOpenAIUpstreamEndpoint(c))
			require.False(t, svc.isExcelBPSCoolingDown(account, "gpt-image-2"))
		})
	}
}

func TestExcelBPSImagesTransportErrorIsNotReplayed(t *testing.T) {
	for _, beforeSend := range []bool{false, true} {
		t.Run(fmt.Sprintf("before_send_%t", beforeSend), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{err: errors.New("connection reset by peer")}
			svc := excelBPSImagesTestService(upstream)
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			ctx := c.Request.Context()
			if beforeSend {
				// A canceled child is not evidence of an incoming client disconnect.
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			_, err = svc.ForwardImages(ctx, c, excelBPSImagesAccount(), body, parsed, "")
			var upErr *OpenAIImagesUpstreamError
			require.ErrorAs(t, err, &upErr)
			require.Equal(t, http.StatusBadGateway, upErr.StatusCode)
			require.Equal(t, "basispoints_transport_error", upErr.Code)
			require.Equal(t, "Excel BPS connection failed; request was not replayed", upErr.Message)
			calls := 1
			if beforeSend {
				calls = 0
			}
			require.Len(t, upstream.requests, calls)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.NotContains(t, rec.Body.String(), "after sending")
		})
	}
}

func TestExcelBPSImagesAccountTest(t *testing.T) {
	for name, tc := range map[string]struct {
		account  *Account
		status   int
		urls     []string
		contains []string
	}{
		"bps": {account: excelBPSImagesAccount(), status: http.StatusOK, urls: []string{basispoints.ImagesGenerationsURL},
			contains: []string{"Calling Excel BPS /images/generations; image model: gpt-image-2"}},
		"bps rejects format": {account: excelBPSImagesAccount(), status: http.StatusUnprocessableEntity, urls: []string{basispoints.ImagesGenerationsURL, codexDirectImagesURL},
			contains: []string{"Excel BPS rejected this request format", "Calling Codex /images/generations"}},
		// Before this change an image model on a BPS account went to the text
		// BPS test and was sent to /responses.
		"image model not selected": {account: excelAccount(), status: http.StatusOK, urls: []string{codexDirectImagesURL},
			contains: []string{"Calling Codex /images/generations"}},
	} {
		t.Run(name, func(t *testing.T) {
			var urls []string
			upstream := excelBPSImagesUpstream(tc.status, &urls)
			svc := &AccountTestService{httpUpstream: upstream, openaiGatewayService: excelBPSImagesTestService(upstream)}
			c, rec := newOpenAIImagesTestContext(t, nil)

			require.NoError(t, svc.testOpenAIAccountConnection(c, tc.account, "gpt-image-2", "draw", ""))

			require.Equal(t, tc.urls, urls)
			for _, text := range tc.contains {
				require.Contains(t, rec.Body.String(), text)
			}
			require.Contains(t, rec.Body.String(), `"type":"image"`)
			require.Contains(t, rec.Body.String(), `"success":true`)
			require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
		})
	}
}

func TestExcelBPSImagesAccountTestReportsRateLimit(t *testing.T) {
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: http.StatusTooManyRequests, Header: http.Header{"Retry-After": {"30"}},
		Body: io.NopCloser(strings.NewReader(`{"error":{"message":"PRIVATE_UPSTREAM"}}`))}}
	svc := &AccountTestService{httpUpstream: upstream, openaiGatewayService: excelBPSImagesTestService(upstream)}
	c, rec := newOpenAIImagesTestContext(t, nil)

	err := svc.testOpenAIAccountConnection(c, excelBPSImagesAccount(), "gpt-image-2", "draw", "")

	// A single-account test shows the rate limit instead of a failover signal.
	require.EqualError(t, err, excelBPSRateLimitedClientMessage)
	require.Len(t, upstream.requests, 1)
	require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
}

func TestExcelBPSImagesFallbackSharesNativeAttemptBudget(t *testing.T) {
	var urls []string
	svc := excelBPSImagesTestService(excelBPSImagesUpstream(http.StatusUnprocessableEntity, &urls))
	svc.cfg.Gateway.OpenAITurnMaxAttempts = 1
	body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
	c, _ := newOpenAIImagesTestContext(t, body)
	parsed, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	account := excelBPSImagesAccount()
	useExcelBPSMixedGrant(t, svc, account)
	ctx := EnsureOpenAITurnBudget(t.Context(), svc.cfg)
	_, err = svc.ForwardImages(ctx, c, account, body, parsed, "")
	require.ErrorIs(t, err, ErrOpenAITurnAttemptBudgetExhausted)
	require.Equal(t, []string{basispoints.ImagesGenerationsURL}, urls, "the native fallback cannot reset the logical attempt budget")
}

func TestExcelBPSImagesPolicyAndModelFailuresPreserveBoundaries(t *testing.T) {
	for _, tc := range []struct {
		code         string
		status       int
		cyber, model bool
	}{
		{"cyber_policy", 400, true, false}, {"cyber_policy", 403, true, false},
		{"cyber_policy", 429, true, false}, {"content_policy_violation", 400, false, false},
		{"session_blocked_by_cyber_policy", 400, false, false},
		{"basispoints_model_access_changed", 403, false, true}, {"model_not_allowed", 403, false, true},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.code, tc.status), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(tc.status, `{"error":{"code":"`+tc.code+`","message":"PRIVATE_UPSTREAM"}}`)}
			svc := excelBPSImagesTestService(upstream)
			svc.accountRepo = &excelBPSAutoDisableRepo{disable: func(context.Context, *Account) (bool, error) {
				t.Fatal("request/model rejection must not disable the whole BPS account")
				return false, nil
			}}
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, rec := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			account := excelBPSImagesAccount()
			account.Extra["openai_excel_bps_auto_disable_on_403"] = true
			_, err = svc.ForwardImages(t.Context(), c, account, body, parsed, "")
			require.Error(t, err)
			require.Len(t, upstream.requests, 1, "policy rejection must not trigger native fallback")
			require.Equal(t, basispoints.ImagesGenerationsURL, upstream.requests[0].URL.String())
			require.Equal(t, tc.cyber, GetOpsCyberPolicy(c) != nil)
			if tc.model {
				var failure *UpstreamFailoverError
				require.ErrorAs(t, err, &failure)
				require.False(t, failure.ShouldReportAccountScheduleFailure())
				require.True(t, svc.isExcelBPSUpstreamModelCoolingDown(account.ID, "gpt-image-2"))
				require.False(t, svc.isExcelBPSUpstreamModelCoolingDown(account.ID, "gpt-6-astra"))
				require.False(t, c.Writer.Written())
			} else {
				require.Equal(t, tc.status, rec.Code)
				require.Equal(t, tc.code, gjson.GetBytes(rec.Body.Bytes(), "error.code").String())
				require.NotContains(t, rec.Body.String(), "PRIVATE_UPSTREAM")
			}
		})
	}
}

func TestExcelBPSImagesCooldownStopsDispatch(t *testing.T) {
	for _, modelOnly := range []bool{false, true} {
		t.Run(fmt.Sprint(modelOnly), func(t *testing.T) {
			upstream := &httpUpstreamRecorder{}
			svc := excelBPSImagesTestService(upstream)
			account := excelBPSImagesAccount()
			if modelOnly {
				svc.coolDownExcelBPSModel(account, "gpt-image-2")
			} else {
				svc.coolDownExcelBPS(t.Context(), account, "30")
			}
			body := []byte(`{"model":"gpt-image-2","prompt":"draw"}`)
			c, _ := newOpenAIImagesTestContext(t, body)
			parsed, err := svc.ParseOpenAIImagesRequest(c, body)
			require.NoError(t, err)
			_, err = svc.ForwardImages(t.Context(), c, account, body, parsed, "")
			var failure *UpstreamFailoverError
			require.ErrorAs(t, err, &failure)
			require.Empty(t, upstream.requests)
			require.False(t, c.Writer.Written())
			if !modelOnly {
				require.True(t, failure.RequestNotSent)
				require.Equal(t, 429, failure.StatusCode)
			}
		})
	}
}
