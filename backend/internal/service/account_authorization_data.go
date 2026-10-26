package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/authz"
)

type AccountAuthorizationData struct {
	BPSCredentials map[string]any `json:"bps_credentials,omitempty"`
}

// AccountAuthorizationCiphertexts is internal persistence data, not a wire format.
type AccountAuthorizationCiphertexts struct {
	Excel string
}

type AccountAuthorizationRepository interface {
	// Refreshes the account's primary credentials/extra and reads private
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
	if snapshot.Excel != "" {
		if s.excelEncryptor == nil {
			return nil, excelGrantStorageUnavailable()
		}
		raw, err := s.excelEncryptor.Decrypt(snapshot.Excel)
		if err != nil || json.Unmarshal([]byte(raw), &out.BPSCredentials) != nil {
			return nil, errors.New("Excel authorization backup could not be decoded")
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

	return nil
}

func (s *OpenAIOAuthService) PrepareAccountAuthorizations(account *Account, data *AccountAuthorizationData) (*AccountAuthorizationCiphertexts, error) {
	if err := validateAccountAuthorizationData(account, data); err != nil {
		return nil, err
	}
	if data == nil || data.BPSCredentials == nil {
		return nil, nil
	}
	if s == nil || s.excelEncryptor == nil {
		return nil, excelGrantStorageUnavailable()
	}
	raw, err := json.Marshal(data.BPSCredentials)
	if err != nil {
		return nil, errors.New("Invalid authorization backup")
	}
	ciphertext, err := s.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return nil, excelGrantStorageUnavailable()
	}
	return &AccountAuthorizationCiphertexts{Excel: ciphertext}, nil
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
