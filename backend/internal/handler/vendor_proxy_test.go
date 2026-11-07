package handler

import (
	"encoding/json"
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestVendorProxyHandlerRejectsReservedAndUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, raw := range []string{`{"vendor_id":99,"password":"SECRET_PASSWORD"}`, `{"test_url":"http://127.0.0.1/"}`, `{} {}`} {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/vendor/proxies", strings.NewReader(raw))
		var in service.VendorProxyInput
		require.False(t, vendorProxyJSON(c, &in))
		require.Equal(t, 422, w.Code)
		require.NotContains(t, w.Body.String(), "SECRET_PASSWORD")
	}
}

func TestVendorProxyImportReceiptFitsNativeIdempotencyRecord(t *testing.T) {
	in := &service.VendorProxyImportResult{InputDigest: strings.Repeat("a", 64)}
	for i := 0; i < service.VendorProxyMaxRows; i++ {
		row := service.VendorProxyImportRow{Line: i + 1, Status: "invalid", Code: "PROXY_REFERENCES_UNAVAILABLE"}
		if i%3 == 0 {
			row.Status, row.Code, row.ProxyID = "created", "", int64(i+100)
		}
		if i%3 == 1 {
			row.Status, row.Code = "duplicate", "PROXY_DUPLICATE"
		}
		in.Rows = append(in.Rows, row)
	}
	receipt := compactVendorProxyImportResult(in)
	b, err := json.Marshal(receipt)
	require.NoError(t, err)
	require.Less(t, len(b), 64*1024)
	var stored any
	require.NoError(t, json.Unmarshal(b, &stored))
	out, err := expandVendorProxyImportReceipt(stored)
	require.NoError(t, err)
	require.Equal(t, in.Rows, out.Rows)
	require.Equal(t, in.InputDigest, out.InputDigest)
	require.Equal(t, 334, out.ValidCount)
	require.Equal(t, 333, out.DuplicateCount)
	require.Equal(t, 333, out.InvalidCount)
}

func TestVendorProxyHandlerHidesInternalErrors(t *testing.T) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/vendor/proxies", nil)
	vendorProxyRespond(c, nil, errors.New("SQL password=SECRET_PASSWORD http://SECRET_USER:SECRET_PASSWORD@private.invalid"))
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "SECRET")
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestVendorProxyHandlerDecodesNativeFallbackFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	raw := `{"fallback_mode":"proxy","backup_proxy_id":12,"expiry_warn_days":0}`
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/vendor/proxies", strings.NewReader(raw))
	var in service.VendorProxyInput
	require.True(t, vendorProxyJSON(c, &in))
	require.Equal(t, "proxy", in.FallbackMode)
	require.EqualValues(t, 12, *in.BackupProxyID)
	require.Zero(t, *in.ExpiryWarnDays)
	c.Request = httptest.NewRequest("PATCH", "/vendor/proxies/1", strings.NewReader(`{"fallback_mode":"none","backup_proxy_id":null,"expiry_warn_days":0}`))
	var patch service.VendorProxyPatch
	require.True(t, vendorProxyJSON(c, &patch))
	require.Equal(t, "none", *patch.FallbackMode)
	require.JSONEq(t, "null", string(patch.BackupProxyID))
	require.Zero(t, *patch.ExpiryWarnDays)
}
