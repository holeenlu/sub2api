package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestVendorSettlementBodyRejectsUnknownTrailingAndOversizedInput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		`{"version":1,"amount":1.23}`,
		`{"version":1,"amount":"1.23","vendor_id":2}`,
		`{"version":1,"amount":"1.23"} {}`,
		`{"reason":"` + strings.Repeat("x", 1<<20) + `"}`,
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/", strings.NewReader(body))
		var in service.VendorSettlementPaymentInput
		require.ErrorIs(t, vendorSettlementJSON(c, &in), service.ErrVendorUsageInput)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/", strings.NewReader(`{"version":1,"amount":"1.23","paid_at":"2026-10-01T00:00:00Z","channel":"offline","reference":"0001234567890123456","idempotency_key":"pay-1"}`))
	var in service.VendorSettlementPaymentInput
	require.NoError(t, vendorSettlementJSON(c, &in))
	require.Equal(t, "0001234567890123456", in.Reference)
	require.Equal(t, "1.23", in.Amount)
}
func TestVendorUsageQueryBoundsParsingAndPrivateResponse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, query := range []string{"authorization_mode=", "authorization_mode=unknown", "authorization_mode=bad", "authorization_mode=bps&authorization_mode=native", "vendor_id=-1", "account_id=1%3BDROP", "page=0", "watermark=9223372036854775808", "snapshot_at=yesterday", "all_history=", "all_history=1", "all_history=yes", "all_history=true&all_history=false", "all_history=true&start_date=2026-10-01", "all_history=true&end_date=2026-10-02", "sort_by=", "order=", "sort_by=cost_usd&sort_by=requests", "order=asc&order=desc"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		_, e := vendorUsageQuery(c)
		require.ErrorIs(t, e, service.ErrVendorUsageInput)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", "/?watermark=12&snapshot_at=2026-10-01T00:00:00Z&sort_by=opus_cost_usd&order=asc&authorization_mode=bps", nil)
	f, e := vendorUsageQuery(c)
	require.NoError(t, e)
	require.Equal(t, int64(12), f.Watermark)
	require.True(t, f.WatermarkSet)
	require.Equal(t, "opus_cost_usd", f.SortBy)
	require.Equal(t, "asc", f.Order)
	require.Equal(t, "bps", f.AuthorizationMode)
	for _, query := range []string{"model_page=", "model_page=0", "model_page=-1", "model_page=1&model_page=2"} {
		c.Request = httptest.NewRequest("GET", "/?"+query, nil)
		_, err := vendorUsageQuery(c)
		require.ErrorIs(t, err, service.ErrVendorUsageInput)
	}
	c.Request = httptest.NewRequest("GET", "/?page=3&model_page=2", nil)
	f, e = vendorUsageQuery(c)
	require.NoError(t, e)
	require.Equal(t, 3, f.Page)
	require.Equal(t, 2, f.ModelPage)
	ctx, cancel := vendorUsageRequestContext(c)
	defer cancel()
	deadline, ok := ctx.Deadline()
	require.True(t, ok)
	require.LessOrEqual(t, time.Until(deadline), 45*time.Second)
	require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
}

func TestVendorUsageAllHistoryQueryRetainsPagingAndSnapshot(t *testing.T) {
	for _, flag := range []string{"true", "false"} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("GET", "/?all_history="+flag+"&page=2&page_size=1&vendor_id=4&group_id=3&platform=openai&watermark=0&snapshot_at=2026-10-01T00:00:00Z", nil)
		f, err := vendorUsageQuery(c)
		require.NoError(t, err)
		require.Equal(t, flag == "true", f.AllHistory)
		require.Equal(t, 2, f.Page)
		require.Equal(t, 1, f.PageSize)
		require.Equal(t, int64(4), f.VendorID)
		require.Equal(t, int64(3), f.GroupID)
		require.Equal(t, "openai", f.Platform)
		require.True(t, f.WatermarkSet)
		require.Zero(t, f.Watermark)
		require.Equal(t, "2026-10-01T00:00:00Z", f.SnapshotAt.Format(time.RFC3339))
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("GET", "/?all_history=false&start_date=2026-10-01&end_date=2026-10-02", nil)
	f, err := vendorUsageQuery(c)
	require.NoError(t, err)
	require.False(t, f.AllHistory)
	require.Equal(t, "2026-10-01", f.StartDate)
	require.Equal(t, "2026-10-02", f.EndDate)
}
