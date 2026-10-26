package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestExcelBPSClientToolChoiceNativeFallbackAvoidsNormalization(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"declared top-level function", `{"input":"hello","tools":[{"type":"function","name":"inspect"}],"tool_choice":{"type":"function","name":"inspect"}}`, true},
		{"namespace name would become auto", `{"input":"hello","tools":[{"type":"namespace","name":"client","tools":[{"type":"function","name":"inspect"}]}],"tool_choice":{"type":"function","name":"client.inspect"}}`, false},
		{"namespace custom would become auto", `{"input":"hello","tools":[{"type":"namespace","name":"client","tools":[{"type":"custom","name":"exec"}]}],"tool_choice":{"type":"custom","name":"client.exec"}}`, false},
		{"invalid JSON", `{"tool_choice":`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(tc.body)
			require.Equal(t, tc.want, excelBPSClientToolChoiceNativeFallbackAllowed(body))
			require.Equal(t, tc.body, string(body))
		})
	}
}

func TestExcelBPSClientToolChoiceNativeFallbackRejectsInheritedCatalog(t *testing.T) {
	cache := &basispoints.CatalogCache{}
	_, _, err := basispoints.PrepareWithCatalog([]byte(`{"model":"gpt-6-astra","input":"hello","tools":[{"type":"function","name":"inspect"}]}`), "choice-test", nil, cache)
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-6-astra","input":"continue","tool_choice":{"type":"function","name":"inspect"}}`)
	_, _, err = basispoints.PrepareWithCatalog(body, "choice-test", nil, cache)
	require.ErrorIs(t, err, basispoints.ErrUnsupportedToolChoice)
	require.False(t, excelBPSClientToolChoiceNativeFallbackAllowed(body), "native receives the original body without the BPS-only catalog")
}

func TestExcelBPSClientToolChoiceFallbackPreservesPolicyRejections(t *testing.T) {
	for _, tc := range []struct {
		name, fields string
	}{
		{"mixed hosted required", `"tools":[{"type":"function","name":"inspect"},{"type":"web_search"}],"tool_choice":"required"`},
		{"mixed hosted named client", `"tools":[{"type":"function","name":"inspect"},{"type":"image_generation"}],"tool_choice":{"type":"function","name":"inspect"}`},
		{"mixed hosted allowed clients", `"tools":[{"type":"function","name":"inspect"},{"type":"file_search"}],"tool_choice":{"type":"allowed_tools","mode":"auto","tools":[{"type":"function","name":"inspect"}]}`},
		{"inherited-only declarations", `"tool_choice":{"type":"function","name":"inspect"}`},
		{"dynamic declarations", `"input":[{"type":"additional_tools","tools":[{"type":"function","name":"later"}]}],"tools":[{"type":"function","name":"inspect"}],"tool_choice":"required"`},
		{"malformed choice", `"tools":[{"type":"function","name":"inspect"}],"tool_choice":{"type":"allowed_tools","mode":"required","tools":[]}`},
		{"undeclared choice", `"tools":[{"type":"function","name":"inspect"}],"tool_choice":{"type":"function","name":"missing"}`},
		{"namespace normalization", `"tools":[{"type":"namespace","name":"client","tools":[{"type":"function","name":"inspect"}]}],"tool_choice":{"type":"function","name":"client.inspect"}`},
	} {
		for _, omit := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/omit=%t", tc.name, omit), func(t *testing.T) {
				upstream := &httpUpstreamRecorder{resp: bpsCompletionResponse(http.StatusOK, bpsCompletedStream("unexpected", "unexpected"))}
				svc := excelBPSGatewayTestService(upstream)
				account := excelBPSNativeFallbackTestAccount(t, svc)
				account.Extra[ExcelBPSOmitUnsupportedToolsKey] = omit
				var source map[string]any
				require.NoError(t, json.Unmarshal([]byte("{"+tc.fields+"}"), &source))
				source["model"] = "gpt-6-astra"
				if _, exists := source["input"]; !exists {
					source["input"] = "hello"
				}
				body, err := json.Marshal(source)
				require.NoError(t, err)
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				_, err = svc.Forward(t.Context(), c, account, body)
				require.Error(t, err)
				require.Equal(t, http.StatusBadRequest, rec.Code)
				require.Equal(t, "basispoints_request_invalid", gjson.Get(rec.Body.String(), "error.code").String())
				require.Equal(t, "tool_choice", gjson.Get(rec.Body.String(), "error.param").String())
				require.Equal(t, basispoints.ErrUnsupportedToolChoice.Error(), gjson.Get(rec.Body.String(), "error.message").String())
				require.Empty(t, upstream.requests, "neither BPS nor native may bypass the request policy")
			})
		}
	}
}

func TestExcelBPSToolChoiceNativeFallbackKeepsAuthorizationBoundary(t *testing.T) {
	for _, mode := range []string{"excel_only", "no_native_grant", "vendor_native_denied", "probe", "cancelled", "already_sent"} {
		t.Run(mode, func(t *testing.T) {
			svc := excelBPSGatewayTestService(nil)
			account := excelBPSNativeFallbackTestAccount(t, svc)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ctx)
			failure := newExcelBPSUpstreamFailureError(http.StatusBadRequest)
			failure.Reason, failure.RequestNotSent = "basispoints_tool_choice", true
			switch mode {
			case "excel_only":
				account = excelAccount()
			case "no_native_grant":
				delete(account.Credentials, "access_token")
				delete(account.Credentials, "refresh_token")
			case "vendor_native_denied":
				account.VendorID = new(int64)
				account.VendorAuthorizationModesLoaded = true
				account.VendorNativeAllowed = false
			case "probe":
				c.Set(bpsAccountProbeRequiredContextKey, true)
			case "cancelled":
				cancel()
			case "already_sent":
				failure.RequestNotSent = false
			}
			require.Nil(t, excelBPSNativeFallbackAccount(ctx, c, account, nil, failure))
		})
	}
}
