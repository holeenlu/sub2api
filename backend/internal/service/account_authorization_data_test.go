package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/stretchr/testify/require"
)

type authorizationDataRepo struct {
	*excelGrantTestRepo
}

func (r *authorizationDataRepo) AccountAuthorizationSnapshot(ctx context.Context, a *Account) (*AccountAuthorizationCiphertexts, error) {
	excel, err := r.GetExcelCredentials(ctx, a.ID)
	return &AccountAuthorizationCiphertexts{Excel: excel}, err
}
func (r *authorizationDataRepo) CreateWithAccountAuthorizations(context.Context, *Account, []AccountGroup, *AccountAuthorizationCiphertexts) error {
	panic("unexpected persistence")
}

func TestAccountAuthorizationBackupDifferentIDsAndEncryptionKeys(t *testing.T) {
	s, source, a := excelGrantService(t)
	bps := excelGrantFixture(t, a.GetCredential("chatgpt_account_id"), a.GetCredential("chatgpt_user_id"), time.Now().Add(time.Hour), "backup")
	require.NoError(t, s.ImportExcelCredentials(t.Context(), a, bps))
	s.excelAccountRepo = &authorizationDataRepo{excelGrantTestRepo: source}
	data, err := s.ExportAccountAuthorizations(t.Context(), a)
	require.NoError(t, err)
	require.Equal(t, bps["refresh_token"], data.BPSCredentials["refresh_token"])
	wire, err := json.Marshal(data)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "ciphertext")
	var decoded AccountAuthorizationData
	require.NoError(t, json.Unmarshal(wire, &decoded))
	target, targetRepo, _ := excelGrantService(t)
	target.excelEncryptor = &liveAttestationAES{key: [32]byte{9}}
	copy := *a
	copy.ID = 9001
	prepared, err := target.PrepareAccountAuthorizations(&copy, &decoded)
	require.NoError(t, err)
	_, err = s.excelEncryptor.Decrypt(prepared.Excel)
	require.Error(t, err)
	targetRepo.ciphertext = prepared.Excel
	target.excelAccountRepo = &authorizationDataRepo{excelGrantTestRepo: targetRepo}
	access, err := target.ExcelAccessToken(t.Context(), &copy, nil)
	require.NoError(t, err)
	require.Equal(t, bps["access_token"], access)
	copy.Credentials = map[string]any{"chatgpt_account_id": "wrong-owner"}
	_, err = target.PrepareAccountAuthorizations(&copy, &decoded)
	require.Error(t, err)
}

func TestAccountAuthorizationBackupRejectsInvalidInput(t *testing.T) {
	s, repo, a := excelGrantService(t)
	s.excelAccountRepo = &authorizationDataRepo{excelGrantTestRepo: repo}
	data, err := s.ExportAccountAuthorizations(t.Context(), a)
	require.NoError(t, err)
	require.Nil(t, data.BPSCredentials)
	prepared, err := s.PrepareAccountAuthorizations(a, data)
	require.NoError(t, err)
	require.Nil(t, prepared)
	bps := excelGrantFixture(t, "other-workspace", "other-user", time.Now().Add(time.Hour), "wrong-owner")
	_, err = s.PrepareAccountAuthorizations(a, &AccountAuthorizationData{BPSCredentials: bps})
	require.Error(t, err)
	vendor := int64(9)
	a.VendorID = &vendor
	_, err = s.PrepareAccountAuthorizations(a, data)
	require.ErrorIs(t, err, authz.ErrPermissionDenied)
}
