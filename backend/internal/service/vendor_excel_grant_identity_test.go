package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

func TestVendorExcelGrantWriterSeparatesRefreshFromReauthorization(t *testing.T) {
	oauth, _, _ := excelGrantService(t)
	svc := &VendorService{openaiOAuth: oauth}
	grant := excelGrantFixture(t, "test-account", "user", time.Now().Add(time.Hour), "same-token")
	grant[excelGrantIdentityKey] = "untrusted-import-identity"
	write := func(previous string, credentials map[string]any, affected int64) map[string]any {
		t.Helper()
		db, mock := vendorTestDB(t)
		capture := &vendorExcelCiphertextCapture{}
		mock.ExpectBegin()
		if previous == "" {
			mock.ExpectExec("INSERT INTO openai_excel_grants").WithArgs(int64(27), capture).WillReturnResult(sqlmock.NewResult(0, affected))
			mock.ExpectExec("UPDATE accounts SET extra=extra-'openai_excel_bps_authorization_error' WHERE id=\\$1").WithArgs(int64(27)).WillReturnResult(sqlmock.NewResult(0, 1))
		} else {
			mock.ExpectExec("UPDATE openai_excel_grants").WithArgs(int64(27), previous, capture).WillReturnResult(sqlmock.NewResult(0, affected))
			if affected == 1 {
				mock.ExpectExec("UPDATE accounts SET extra=jsonb_set").WithArgs(int64(27), previous, sqlmock.AnyArg()).WillReturnResult(sqlmock.NewResult(0, 1))
			}
		}
		if affected == 1 {
			mock.ExpectCommit()
		} else {
			mock.ExpectRollback()
		}
		tx, err := db.BeginTx(context.Background(), nil)
		require.NoError(t, err)
		if previous == "" {
			err = svc.storeVendorExcelGrant(t.Context(), tx, 27, credentials)
		} else {
			err = svc.storeVendorExcelGrant(t.Context(), tx, 27, credentials, previous)
		}
		if affected == 0 {
			require.ErrorIs(t, err, ErrVendorAccountConflict)
			require.NoError(t, tx.Rollback())
			require.NoError(t, mock.ExpectationsWereMet())
			return nil
		}
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		plain, err := oauth.excelEncryptor.Decrypt(capture.value)
		require.NoError(t, err)
		var saved map[string]any
		require.NoError(t, json.Unmarshal([]byte(plain), &saved))
		require.NoError(t, mock.ExpectationsWereMet())
		return saved
	}
	first := write("", grant, 1)
	second := write("", grant, 1)
	require.NotEqual(t, "untrusted-import-identity", first[excelGrantIdentityKey])
	require.NotEqual(t, first[excelGrantIdentityKey], second[excelGrantIdentityKey], "same-token explicit import is still a new authorization")
	require.Equal(t, "untrusted-import-identity", grant[excelGrantIdentityKey], "do not mutate the caller's verified credential snapshot")
	rotated := write("encrypted-original", first, 1)
	require.Equal(t, first[excelGrantIdentityKey], rotated[excelGrantIdentityKey])
	legacy := shallowCopyMap(grant)
	delete(legacy, excelGrantIdentityKey)
	legacyRotated := write("encrypted-legacy", legacy, 1)
	require.Equal(t, excelStoredGrantIdentity("encrypted-legacy", legacy), legacyRotated[excelGrantIdentityKey])
	write("stale-grant", first, 0)
}
