package admin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/handler/dto"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

type recoveryStatusAdmin struct {
	service.AdminService
	enabled bool
}

func (s recoveryStatusAdmin) ExcelBPS403RecoveryStatuses(_ context.Context, accounts []*service.Account) map[int64]*service.ExcelBPS403RecoveryStatus {
	states := map[int64]*service.ExcelBPS403RecoveryStatus{}
	for _, a := range accounts {
		states[a.ID] = service.ExcelBPS403RecoveryStatusFromAccount(a, s.enabled, time.Now())
	}
	return states
}
func TestAccountRecoveryStatusSurvivesLiteProjectionAndGlobalPause(t *testing.T) {
	a := &service.Account{ID: 7, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Extra: map[string]any{
		service.ExcelBPS403DisabledAtKey: "2026-10-26T00:00:00Z", service.ExcelBPS403RecoveryIntervalMinutesKey: 30,
	}}
	h := &AccountHandler{adminService: recoveryStatusAdmin{enabled: false}}
	// Status enrichment works without the unrelated authorization service.
	item := h.buildAccountResponseWithRuntime(t.Context(), a)
	require.NotNil(t, item.ExcelBPS403Recovery)
	require.Equal(t, "global_disabled", item.ExcelBPS403Recovery.Reason)
	lite := dto.AccountListItemFromAccount(item.Account)
	require.Equal(t, item.ExcelBPS403Recovery, lite.ExcelBPS403Recovery)
	raw, err := json.Marshal(lite)
	require.NoError(t, err)
	require.Contains(t, string(raw), `"excel_bps_403_recovery"`)
	require.Contains(t, string(raw), `"interval_minutes":30`)
}
