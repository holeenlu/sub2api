package service

import (
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

func TestExcelBPSClientToolChoiceRejectsInheritedCatalog(t *testing.T) {
	cache := &basispoints.CatalogCache{}
	_, _, err := basispoints.PrepareWithCatalog([]byte(`{"model":"gpt-6-astra","input":"hello","tools":[{"type":"function","name":"inspect"}]}`), "choice-test", nil, cache)
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-6-astra","input":"continue","tool_choice":{"type":"function","name":"inspect"}}`)
	_, _, err = basispoints.PrepareWithCatalog(body, "choice-test", nil, cache)
	require.ErrorIs(t, err, basispoints.ErrUnsupportedToolChoice)
	require.JSONEq(t, `{"model":"gpt-6-astra","input":"continue","tool_choice":{"type":"function","name":"inspect"}}`, string(body), "rejection must not rewrite a forced choice")
}

func TestExcelBPSClientToolChoicePreservesPolicyRejections(t *testing.T) {
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
				account := excelBPSDualGrantTestAccount(t, svc)
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
