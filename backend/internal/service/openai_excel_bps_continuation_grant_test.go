package service

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service/basispoints"
	"github.com/stretchr/testify/require"
)

func TestExcelBPSContinuationRetainsSendingGrantAcrossReauthorization(t *testing.T) {
	oauth, _, account := excelGrantService(t)
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "first")
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	svc, _ := excelBPSContinuationTestService()
	svc.openAITokenProvider = &OpenAITokenProvider{openAIOAuthService: oauth}
	c := excelBPSContinuationTestContext()
	dispatch, err := svc.ExcelBPSDispatchContext(t.Context(), account, grant["access_token"].(string))
	require.NoError(t, err)
	oldIdentity, err := svc.ExcelBPSGrantIdentity(dispatch, account)
	require.NoError(t, err)
	// Reauthorization may race a terminal response from an already-sent turn.
	// The late publication must retain that turn's original grant.
	require.NoError(t, oauth.ImportExcelCredentials(t.Context(), account, grant))
	newIdentity, err := svc.ExcelBPSGrantIdentity(t.Context(), account)
	require.NoError(t, err)
	require.NotEqual(t, oldIdentity, newIdentity)
	retained, err := svc.ExcelBPSGrantIdentity(dispatch, account)
	require.NoError(t, err)
	require.Equal(t, oldIdentity, retained)
	finish, err := svc.prepareExcelBPSContinuation(dispatch, c, account, []byte(excelBPSContinuationTestBody), []byte(excelBPSContinuationTestResponse))
	require.NoError(t, err)
	requireExcelBPSContinuationFinished(t, finish, true, excelBPSContinuationCommitted)
	_, err = svc.restoreExcelBPSContinuation(t.Context(), c, account, []byte(excelBPSContinuationTestNext))
	require.ErrorIs(t, err, basispoints.ErrPreviousResponseNotFound)
}
