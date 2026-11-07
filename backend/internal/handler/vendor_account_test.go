package handler

import (
	"errors"
	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"net/http/httptest"
	"strings"
	"testing"
)

func vendorHandlerContext(body string) (*gin.Context, *httptest.ResponseRecorder) {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/vendor/accounts", strings.NewReader(body))
	return c, w
}
func TestVendorAccountBindRejectsReservedFieldsAndTrailingJSON(t *testing.T) {
	for _, body := range []string{`{"name":"a","vendor_id":2}`, `{"name":"a"}{}`, `{"name":"a","credentials":{"token":"secret"}}`} {
		c, w := vendorHandlerContext(body)
		var in struct {
			Name string `json:"name"`
		}
		require.False(t, vendorAccountBind(c, &in))
		require.Equal(t, 422, w.Code)
		require.NotContains(t, w.Body.String(), "secret")
	}
}
func TestVendorAccountExportRequiresServerStepUpAndExplicitConfirmation(t *testing.T) {
	h := NewVendorAccountHandler(nil)
	c, w := vendorHandlerContext(`{"ids":[1],"sensitive_confirmed":true,"vendor_accounts_export_step_up":true}`)
	h.Export(c)
	require.Equal(t, 403, w.Code)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
	c, w = vendorHandlerContext(`{"ids":[1]}`)
	c.Set(VendorAccountExportStepUpMarker, true)
	h.Export(c)
	require.Equal(t, 422, w.Code)
}
func TestVendorAccountErrorsDoNotExposeDatabaseOrProviderContent(t *testing.T) {
	c, w := vendorHandlerContext("{}")
	vendorAccountReply(c, nil, errors.New("Bearer secret at 10.0.0.1"))
	require.Equal(t, 503, w.Code)
	require.NotContains(t, w.Body.String(), "secret")
	require.NotContains(t, w.Body.String(), "10.0.0.1")
	c, w = vendorHandlerContext("{}")
	vendorAccountReply(c, nil, authz.ErrAuthorizationExpired)
	require.Equal(t, 401, w.Code)
}
