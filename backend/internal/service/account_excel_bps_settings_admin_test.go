//go:build unit

package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAdminBPSControlPreservesOmittedSwitchIntent(t *testing.T) {
	for _, patch := range []bool{false, true} {
		a := recoveryTestAccount(time.Now().UTC())
		repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{a.ID: a}}
		s := &adminServiceImpl{accountRepo: repo}
		in := &UpdateAccountInput{Name: "metadata edit", ParameterPatch: patch}
		if patch {
			in.Extra = map[string]any{"openai_compact_supported": true}
		}
		_, err := s.UpdateAccount(t.Context(), a.ID, in)
		require.NoError(t, err)
		require.Len(t, repo.updatedAccounts, 1)
		written := repo.updatedAccounts[0].Extra
		require.NotContains(t, written, "openai_excel_bps", "the repository must see that no explicit switch command was provided")
		require.Equal(t, true, written[ExcelBPSAutoRecoverOn403Key])
		require.Equal(t, true, written["openai_excel_bps_auto_disable_on_403"])
		require.Equal(t, []string{"gpt-6-astra"}, written["openai_excel_bps_models"])
	}
}

func TestAdminBPSControlPassesExplicitSwitchAndCondition(t *testing.T) {
	for _, enabled := range []bool{true, false} {
		a := recoveryTestAccount(time.Now().UTC())
		repo := &accountRepoStubForBulkUpdate{getByIDAccounts: map[int64]*Account{a.ID: a}}
		s := &adminServiceImpl{accountRepo: repo}
		at := a.Extra[ExcelBPS403DisabledAtKey]
		_, err := s.UpdateAccount(t.Context(), a.ID, &UpdateAccountInput{Extra: map[string]any{"openai_excel_bps": enabled, ExcelBPSExpectedDisabledAtKey: at}})
		require.NoError(t, err)
		require.Len(t, repo.updatedAccounts, 1)
		require.Equal(t, enabled, repo.updatedAccounts[0].Extra["openai_excel_bps"])
		require.Equal(t, at, repo.updatedAccounts[0].Extra[ExcelBPSExpectedDisabledAtKey], "the locked storage boundary consumes the condition")
	}
}
