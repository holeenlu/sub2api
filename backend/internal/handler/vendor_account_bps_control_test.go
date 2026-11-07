package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestVendorBPSControlHandlerBindsConditionAndReturnsConflict(t *testing.T) {
	c, w := vendorHandlerContext(`{"bps_enabled":true,"bps_expected_disabled_at":"2026-10-27T01:02:03Z"}`)
	var in service.VendorAccountUpdate
	require.True(t, vendorAccountBind(c, &in))
	require.NotNil(t, in.BPSEnabled)
	require.True(t, *in.BPSEnabled)
	require.NotNil(t, in.BPSExpectedDisabledAt)
	require.Equal(t, "2026-10-27T01:02:03Z", *in.BPSExpectedDisabledAt)
	vendorAccountReply(c, nil, service.ErrExcelBPSStateConflict)
	require.Equal(t, 409, w.Code)
	require.Contains(t, w.Body.String(), "OPENAI_EXCEL_BPS_STATE_CONFLICT")
	c, w = vendorHandlerContext(`{"bps_enabled":true,"bps_expected_disabled_at":{"grant_fingerprint":"private"}}`)
	require.False(t, vendorAccountBind(c, &in))
	require.Equal(t, 422, w.Code)
	require.NotContains(t, w.Body.String(), "private")
}
