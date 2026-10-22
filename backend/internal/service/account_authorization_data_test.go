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
	*siwcTestRepo
	excel string
}

func (r *authorizationDataRepo) AccountAuthorizationSnapshot(ctx context.Context, a *Account) (*AccountAuthorizationCiphertexts, error) {
	reg, grant, err := r.GetSIWCRecord(ctx, a)
	return &AccountAuthorizationCiphertexts{Excel: r.excel, SIWCRegistration: reg, SIWCCredentials: grant}, err
}
func (r *authorizationDataRepo) CreateWithAccountAuthorizations(context.Context, *Account, []AccountGroup, *AccountAuthorizationCiphertexts) error {
	panic("unexpected persistence")
}
func (r *authorizationDataRepo) GetExcelCredentials(context.Context, int64) (string, error) {
	return r.excel, nil
}
func (r *authorizationDataRepo) StoreExcelCredentials(context.Context, *Account, string, string) (bool, error) {
	panic("unexpected refresh")
}

func TestAccountAuthorizationBackupDifferentIDsAndEncryptionKeys(t *testing.T) {
	s, source, _, a := siwcFixture(t)
	a.Credentials["email"] = "test@example.com"
	a.Credentials["plan_type"] = "plus"
	authorizeSIWC(t, s, a)
	bps := excelGrantFixture(t, "native-workspace", "native-user", time.Now().Add(time.Hour), "backup")
	raw, err := json.Marshal(bps)
	require.NoError(t, err)
	excel, err := s.excelEncryptor.Encrypt(string(raw))
	require.NoError(t, err)
	s.excelAccountRepo = &authorizationDataRepo{siwcTestRepo: source, excel: excel}
	data, err := s.ExportAccountAuthorizations(t.Context(), a)
	require.NoError(t, err)
	require.NotNil(t, data.SIWC)
	require.Equal(t, bps["refresh_token"], data.BPSCredentials["refresh_token"])
	wire, err := json.Marshal(data)
	require.NoError(t, err)
	require.NotContains(t, string(wire), "ciphertext")
	require.NotContains(t, string(wire), "account_id\":70")
	var decoded AccountAuthorizationData
	require.NoError(t, json.Unmarshal(wire, &decoded))
	target, targetRepo, _, _ := siwcFixture(t)
	target.excelEncryptor = &liveAttestationAES{key: [32]byte{9}}
	copy := *a
	copy.ID = 9001
	prepared, err := target.PrepareAccountAuthorizations(&copy, &decoded)
	require.NoError(t, err)
	_, err = s.excelEncryptor.Decrypt(prepared.SIWCRegistration)
	require.Error(t, err)
	targetRepo.records[copy.ID] = siwcTestRecord{prepared.SIWCRegistration, prepared.SIWCCredentials}
	target.excelAccountRepo = &authorizationDataRepo{siwcTestRepo: targetRepo, excel: prepared.Excel}
	_, _, registration, grant, err := target.readSIWCRecord(t.Context(), &copy)
	require.NoError(t, err)
	require.Equal(t, data.SIWC.Registration.ClientID, registration.ClientID)
	require.Equal(t, data.SIWC.Registration.HostID, registration.HostID)
	require.NotEqual(t, data.SIWC.Registration.Generation, registration.Generation)
	require.Equal(t, data.SIWC.Credentials.RefreshToken, grant.RefreshToken)
	access, err := target.SIWCAccessToken(t.Context(), &copy, nil)
	require.NoError(t, err)
	require.Equal(t, grant.AccessToken, access)
	access, err = target.ExcelAccessToken(t.Context(), &copy, nil)
	require.NoError(t, err)
	require.Equal(t, bps["access_token"], access)
	copy.Credentials = map[string]any{"chatgpt_account_id": "wrong-owner"}
	_, err = target.PrepareAccountAuthorizations(&copy, &decoded)
	require.Error(t, err)
	_, _, _, _, err = target.readSIWCRecord(t.Context(), &copy)
	require.Error(t, err)
}

func TestSIWCBindingIgnoresDatabaseIDAndRotatingTokens(t *testing.T) {
	_, _, _, a := siwcFixture(t)
	before := siwcPrimaryIdentity(a)
	a.ID = 9001
	a.Credentials["access_token"] = "rotated"
	a.Credentials["refresh_token"] = "rotated-refresh"
	require.Equal(t, before, siwcPrimaryIdentity(a))
	a.Credentials["chatgpt_user_id"] = "different-user"
	require.NotEqual(t, before, siwcPrimaryIdentity(a))
}

func TestAccountAuthorizationBackupPreservesDisconnectedRegistrationAndRejectsInvalidInput(t *testing.T) {
	s, repo, _, a := siwcFixture(t)
	authorizeSIWC(t, s, a)
	record := repo.records[a.ID]
	record.grant = ""
	repo.records[a.ID] = record
	s.excelAccountRepo = &authorizationDataRepo{siwcTestRepo: repo}
	data, err := s.ExportAccountAuthorizations(t.Context(), a)
	require.NoError(t, err)
	require.NotNil(t, data.SIWC)
	require.Nil(t, data.SIWC.Credentials)
	_, err = s.PrepareAccountAuthorizations(a, data)
	require.NoError(t, err)
	for _, mutate := range []func(*AccountSIWCData){
		func(d *AccountSIWCData) { d.Registration.PrimaryIdentity = "wrong-parent" },
		func(d *AccountSIWCData) { d.Registration.Issuer = "https://untrusted.invalid" },
		func(d *AccountSIWCData) { d.Registration.ClientID = "invalid client id" },
		func(d *AccountSIWCData) { d.Credentials = &siwcGrant{AccessToken: "token", ExpiresAt: 0} },
	} {
		copy := *data.SIWC
		mutate(&copy)
		_, err = s.PrepareAccountAuthorizations(a, &AccountAuthorizationData{SIWC: &copy})
		require.Error(t, err)
	}
	vendor := int64(9)
	a.VendorID = &vendor
	_, err = s.PrepareAccountAuthorizations(a, data)
	require.ErrorIs(t, err, authz.ErrPermissionDenied)
}
