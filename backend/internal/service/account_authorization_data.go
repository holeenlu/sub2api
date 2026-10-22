package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

// AccountSIWCData carries the provider registration and its independent grant.
// PrimaryIdentity is portable; database IDs and encryption keys are never exported.
type AccountSIWCData struct {
	Registration siwcRegistration `json:"registration"`
	Credentials  *siwcGrant       `json:"credentials,omitempty"`
}

type AccountAuthorizationData struct {
	BPSCredentials map[string]any   `json:"bps_credentials,omitempty"`
	SIWC           *AccountSIWCData `json:"siwc_authorization,omitempty"`
}

// AccountAuthorizationCiphertexts is internal persistence data, not a wire format.
type AccountAuthorizationCiphertexts struct {
	Excel, SIWCRegistration, SIWCCredentials string
}

type AccountAuthorizationRepository interface {
	// Refreshes the account's primary credentials/extra and reads both private
	// grants in one database snapshot, including grants of disabled protocols.
	AccountAuthorizationSnapshot(context.Context, *Account) (*AccountAuthorizationCiphertexts, error)
	CreateWithAccountAuthorizations(context.Context, *Account, []AccountGroup, *AccountAuthorizationCiphertexts) error
}

type AccountDataAuthorizationService interface {
	ExportAccountAuthorizations(context.Context, *Account) (*AccountAuthorizationData, error)
	PrepareAccountAuthorizations(*Account, *AccountAuthorizationData) (*AccountAuthorizationCiphertexts, error)
}

func (s *OpenAIOAuthService) ExportAccountAuthorizations(ctx context.Context, account *Account) (*AccountAuthorizationData, error) {
	if account.VendorID != nil || account.IsShadow() {
		return nil, authz.ErrPermissionDenied
	}
	if !account.IsOpenAIOAuth() {
		return nil, nil
	}
	if s == nil {
		return nil, excelGrantStorageUnavailable()
	}
	repo, ok := s.excelAccountRepo.(AccountAuthorizationRepository)
	if !ok {
		return nil, excelGrantStorageUnavailable()
	}
	snapshot, err := repo.AccountAuthorizationSnapshot(ctx, account)
	if err != nil {
		return nil, err
	}
	out := &AccountAuthorizationData{}
	if snapshot.Excel != "" || snapshot.SIWCRegistration != "" {
		if s.excelEncryptor == nil {
			return nil, excelGrantStorageUnavailable()
		}
	}
	if snapshot.Excel != "" {
		raw, err := s.excelEncryptor.Decrypt(snapshot.Excel)
		if err != nil || json.Unmarshal([]byte(raw), &out.BPSCredentials) != nil {
			return nil, errors.New("Excel authorization backup could not be decoded")
		}
	}
	if snapshot.SIWCRegistration != "" {
		out.SIWC = &AccountSIWCData{}
		raw, err := s.excelEncryptor.Decrypt(snapshot.SIWCRegistration)
		if err != nil || json.Unmarshal([]byte(raw), &out.SIWC.Registration) != nil || out.SIWC.Registration.PrimaryIdentity != siwcPrimaryIdentity(account) {
			return nil, siwcConflict()
		}
		if snapshot.SIWCCredentials != "" {
			raw, err = s.excelEncryptor.Decrypt(snapshot.SIWCCredentials)
			if err != nil || json.Unmarshal([]byte(raw), &out.SIWC.Credentials) != nil {
				return nil, errors.New("SIWC authorization backup could not be decoded")
			}
		}
	}
	if err := validateAccountAuthorizationData(account, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Explicit super-admin restore validates portable identity and structure without
// consuming rotating refresh tokens on the source installation. Provider expiry
// and revocation are still enforced by the normal refresh/inference paths.
func validateAccountAuthorizationData(account *Account, data *AccountAuthorizationData) error {
	if data == nil {
		return nil
	}
	if account.VendorID != nil || account.IsShadow() {
		return authz.ErrPermissionDenied
	}
	if data.BPSCredentials != nil {
		if !account.IsExcelBPSEligible() || account.IsExcelOAuth() {
			return errors.New("Independent BPS authorization requires an eligible native OpenAI account")
		}
		info, err := decodeExcelOAuthTokenInfo(data.BPSCredentials, true)
		if err != nil || validateExcelIdentity(account, info) != nil {
			return errors.New("BPS authorization does not match the account user and workspace")
		}
	}
	if data.SIWC != nil {
		r, g := data.SIWC.Registration, data.SIWC.Credentials
		if !account.IsSIWCEligible() || !siwcHasPrimaryIdentity(account) || r.PrimaryIdentity != siwcPrimaryIdentity(account) || r.HostID == "" || len(r.HostID) > 256 || (r.ClientID != "" && !openai.ValidSIWCClientID(r.ClientID)) || (r.Subject != "" && (r.Issuer != openai.SIWCIssuer || r.Generation == "")) {
			return errors.New("SIWC registration does not match this account")
		}
		if g != nil && (r.Subject == "" || r.ClientID == "" || g.ExpiresAt <= 0 || (g.AccessToken == "" && g.IDToken == "") || (openai.HasSIWCPermission(g.Scope) && g.AccessToken == "") || len(g.AccessToken) > openai.SIWCTokenLimit || len(g.RefreshToken) > openai.SIWCTokenLimit || len(g.IDToken) > openai.SIWCTokenLimit) {
			return errors.New("SIWC authorization is incomplete")
		}
	}
	return nil
}

func (s *OpenAIOAuthService) PrepareAccountAuthorizations(account *Account, data *AccountAuthorizationData) (*AccountAuthorizationCiphertexts, error) {
	if err := validateAccountAuthorizationData(account, data); err != nil {
		return nil, err
	}
	if data == nil || (data.BPSCredentials == nil && data.SIWC == nil) {
		return nil, nil
	}
	if s == nil || s.excelEncryptor == nil {
		return nil, excelGrantStorageUnavailable()
	}
	encode := func(value any) (string, error) {
		raw, err := json.Marshal(value)
		if err != nil {
			return "", errors.New("Invalid authorization backup")
		}
		return s.excelEncryptor.Encrypt(string(raw))
	}
	out := &AccountAuthorizationCiphertexts{}
	var err error
	if data.BPSCredentials != nil {
		out.Excel, err = encode(data.BPSCredentials)
		if err != nil {
			return nil, excelGrantStorageUnavailable()
		}
	}
	if data.SIWC != nil {
		r := data.SIWC.Registration
		// A restored local session must not inherit cached conversation ownership.
		if r.Subject != "" {
			r.Generation = uuid.NewString()
		}
		out.SIWCRegistration, err = encode(r)
		if err != nil {
			return nil, excelGrantStorageUnavailable()
		}
		if data.SIWC.Credentials != nil {
			out.SIWCCredentials, err = encode(data.SIWC.Credentials)
			if err != nil {
				return nil, excelGrantStorageUnavailable()
			}
		}
	}
	return out, nil
}

// AccountImportOptions is only populated by the explicit backup import path.
type AccountImportOptions struct {
	Status         string
	Schedulable    *bool
	Groups         []AccountGroup
	Authorizations *AccountAuthorizationCiphertexts
}

func (o *AccountImportOptions) apply(account *Account) error {
	if o.Status != "" {
		switch o.Status {
		case StatusActive, StatusDisabled, StatusError:
			account.Status = o.Status
		default:
			return errors.New("Invalid account backup status")
		}
	}
	if o.Schedulable != nil {
		account.Schedulable = *o.Schedulable
	}
	if strings.TrimSpace(account.Name) == "" {
		return errors.New("Account name is required")
	}
	return nil
}
