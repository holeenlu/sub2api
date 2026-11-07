package dto

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestAccountBPSFailureHidesServerCASIdentity(t *testing.T) {
	a := &service.Account{ID: 7, Extra: map[string]any{
		"openai_excel_bps": false,
		service.ExcelBPSFailureKey: map[string]any{
			"grant_fingerprint": "private-grant",
			"policy":            map[string]any{"groups": "private-group-policy"},
		},
	}}
	full := AccountFromServiceShallow(a)
	full.ExcelBPS403Recovery = &service.ExcelBPS403RecoveryStatus{State: "reauthorization_required", Reason: "grant_missing"}
	for _, view := range []any{full, AccountListItemFromAccount(full)} {
		raw, err := json.Marshal(view)
		require.NoError(t, err)
		require.NotContains(t, string(raw), "private-grant")
		require.NotContains(t, string(raw), "private-group-policy")
		require.NotContains(t, string(raw), service.ExcelBPSFailureKey)
		require.Contains(t, string(raw), "reauthorization_required")
	}
	require.Contains(t, a.Extra, service.ExcelBPSFailureKey, "projection must not alter the authoritative snapshot")
}
