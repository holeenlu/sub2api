package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

const vendorAccountImportLimit = 25
const vendorAccountImportTTL = 10 * time.Minute

type VendorAccountBundle struct {
	Type           string                   `json:"type"`
	Version        int                      `json:"version"`
	ExportedAt     string                   `json:"exported_at,omitempty"`
	Accounts       []VendorAccountBundleRow `json:"accounts"`
	Proxies        []any                    `json:"proxies"`
	SkippedShadows int                      `json:"skipped_shadows,omitempty"`
	SkippedDeleted int                      `json:"skipped_deleted,omitempty"`
	SkippedEmpty   int                      `json:"skipped_empty,omitempty"`
}
type VendorAccountBundleRow struct {
	Name           string         `json:"name"`
	Notes          *string        `json:"notes,omitempty"`
	Platform       string         `json:"platform"`
	Type           string         `json:"type"`
	Credentials    map[string]any `json:"credentials"`
	BPSCredentials map[string]any `json:"bps_credentials,omitempty"`
}
type VendorAccountImportInput struct {
	Data      json.RawMessage              `json:"data,omitempty"`
	PreviewID string                       `json:"preview_id,omitempty"`
	GroupIDs  []int64                      `json:"group_ids"`
	ProxyID   *int64                       `json:"proxy_id"`
	Direct    bool                         `json:"direct"`
	Mode      string                       `json:"mode"`
	Mappings  []VendorAccountImportMapping `json:"mappings,omitempty"`
}

// Each mapping explicitly replaces one row's group/egress selection. File IDs
// never become bindings. Missing mappings use the validated batch defaults.
type VendorAccountImportMapping struct {
	Index    int     `json:"index"`
	GroupIDs []int64 `json:"group_ids"`
	ProxyID  *int64  `json:"proxy_id"`
	Direct   bool    `json:"direct"`
	Mode     string  `json:"mode,omitempty"`
}

func vendorImportMappings(input VendorAccountImportInput, count int) (map[int]VendorAccountImportMapping, error) {
	out := make(map[int]VendorAccountImportMapping, len(input.Mappings))
	if len(input.Mappings) > count {
		return nil, vendorAccountInputError("Too many import mappings")
	}
	for _, m := range input.Mappings {
		if _, duplicate := out[m.Index]; duplicate || m.Index < 0 || m.Index >= count {
			return nil, vendorAccountInputError("Invalid or duplicate import row index")
		}
		if m.Mode != "" && m.Mode != "skip" && m.Mode != "update" {
			return nil, vendorAccountInputError("Import mode must be skip or update")
		}
		out[m.Index] = m
	}
	return out, nil
}

type VendorAccountImportRow struct {
	Index     int     `json:"index"`
	Status    string  `json:"status"`
	AccountID int64   `json:"account_id,omitempty"`
	Message   string  `json:"message,omitempty"`
	Platform  string  `json:"platform,omitempty"`
	Name      string  `json:"name,omitempty"`
	ProxyID   *int64  `json:"proxy_id"`
	Direct    bool    `json:"direct"`
	GroupIDs  []int64 `json:"group_ids"`
}
type VendorAccountImportResult struct {
	PreviewID string                   `json:"preview_id"`
	ExpiresAt time.Time                `json:"expires_at"`
	Rows      []VendorAccountImportRow `json:"rows"`
	Notice    string                   `json:"notice"`
}
type vendorAccountImportSecretRow struct {
	Input             VendorAccountInput
	BPSCredential     *vendorVerifiedCredential
	Credential        vendorVerifiedCredential
	Ready             bool
	ProxyVersion      int64
	TargetFingerprint string
}
type vendorAccountImportSession struct {
	Authorization *authz.Lease
	VendorID      int64
	PolicyVersion int64
	Status        string
	Ciphertext    string
	Result        VendorAccountImportResult
}

func parseVendorAccountBundle(data []byte) (*VendorAccountBundle, error) {
	if len(data) == 0 || len(data) > 2<<20 {
		return nil, vendorAccountInputError("Import data must be between 1 byte and 2 MiB")
	}
	var bundle VendorAccountBundle
	if json.Unmarshal(data, &bundle) != nil {
		return nil, vendorAccountInputError("Invalid native account bundle")
	}
	if (bundle.Type != "sub2api-data" && bundle.Type != "sub2api-bundle") || bundle.Version != 1 {
		return nil, vendorAccountInputError("Only native sub2api-data/sub2api-bundle version 1 is supported")
	}
	if len(bundle.Accounts) == 0 || len(bundle.Accounts) > vendorAccountImportLimit {
		return nil, vendorAccountInputError("Select between 1 and 25 accounts per batch")
	}
	for _, row := range bundle.Accounts {
		if len(row.Credentials) > 30 || len(row.BPSCredentials) > 30 {
			return nil, vendorAccountInputError("Too many credential fields")
		}
		for _, credentials := range []map[string]any{row.Credentials, row.BPSCredentials} {
			for _, value := range credentials {
				b, _ := json.Marshal(value)
				if len(b) > 32768 {
					return nil, vendorAccountInputError("Credential field is too long")
				}
			}
		}
	}
	return &bundle, nil
}

// Preview only authenticates metadata. Refreshing a token here could rotate the
// source account's refresh token even when the user later cancels the import.
func (s *VendorService) vendorVerifyImportRow(ctx context.Context, row VendorAccountBundleRow, proxy string) (*vendorVerifiedCredential, error) {
	get := func(key string) string { v, _ := row.Credentials[key].(string); return strings.TrimSpace(v) }
	if row.Platform == PlatformOpenAI && row.Type == AccountTypeOAuth {
		if get("client_id") != "" && get("client_id") != openai.ClientID && get("client_id") != openai.ExcelClientID {
			return nil, vendorAccountInputError("Unsupported OAuth client/profile")
		}
		if get("refresh_token") == "" {
			return nil, vendorAccountInputError("Refresh authorization required")
		}
		mode := VendorOpenAIModeNative
		if get("client_id") == openai.ExcelClientID {
			mode = VendorOpenAIModeBPS
		}
		return s.vendorVerifyOpenAIProfile(ctx, &openai.TokenResponse{AccessToken: get("access_token"), RefreshToken: get("refresh_token"), IDToken: get("id_token"), ExpiresIn: 0}, "refresh_token", proxy, mode)
	}
	if row.Platform == PlatformAnthropic && (row.Type == AccountTypeOAuth || row.Type == AccountTypeSetupToken) {
		method := "oauth"
		if row.Type == AccountTypeSetupToken {
			method = "setup-token-manual"
		}
		if row.Type == AccountTypeOAuth && get("refresh_token") == "" {
			return nil, vendorAccountInputError("Refresh authorization required")
		}
		return s.vendorVerifyClaude(ctx, &TokenInfo{AccessToken: get("access_token"), RefreshToken: get("refresh_token")}, method, proxy)
	}
	return nil, vendorAccountInputError("Unsupported or unverifiable authorization; obtain a fresh export")
}
func (s *VendorService) PreviewVendorAccountImport(ctx context.Context, input VendorAccountImportInput) (*VendorAccountImportResult, error) {
	bundle, err := parseVendorAccountBundle(input.Data)
	if err != nil {
		return nil, err
	}
	if input.Mode == "" {
		input.Mode = "skip"
	}
	if input.Mode != "skip" && input.Mode != "update" {
		return nil, vendorAccountInputError("Import mode must be skip or update")
	}
	mappings, err := vendorImportMappings(input, len(bundle.Accounts))
	if err != nil {
		return nil, err
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	lease := authz.CaptureLease(ctx)
	if lease == nil || lease.SessionID == "" {
		return nil, authz.ErrAuthorizationExpired
	}
	if err = authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	if s.rdb == nil || s.openaiOAuth == nil || s.openaiOAuth.excelEncryptor == nil {
		return nil, vendorAccountUnavailable()
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, 0, "import_preview"); err != nil {
		return nil, err
	}
	id, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	result := VendorAccountImportResult{PreviewID: id, ExpiresAt: time.Now().Add(vendorAccountImportTTL), Rows: []VendorAccountImportRow{}, Notice: "Owner, resource IDs, system configuration and proxy credentials in files are ignored. New accounts are paused. Each row shows its final explicit egress and group mapping."}
	secrets := make([]vendorAccountImportSecretRow, len(bundle.Accounts))
	seen := map[string]bool{}
	for i, row := range bundle.Accounts {
		item := VendorAccountImportRow{Index: i, Status: "failed", Platform: row.Platform, Name: row.Name, ProxyID: input.ProxyID, Direct: input.Direct}
		in := VendorAccountInput{Name: row.Name, Platform: row.Platform, GroupIDs: input.GroupIDs, ProxyID: input.ProxyID, Direct: input.Direct}
		if row.Platform == PlatformOpenAI && excelMapString(row.Credentials, "client_id") == openai.ExcelClientID {
			in.AuthorizationMode = VendorOpenAIModeBPS
		}
		mode := input.Mode
		if mapping, ok := mappings[i]; ok {
			in.GroupIDs, in.ProxyID, in.Direct = mapping.GroupIDs, mapping.ProxyID, mapping.Direct
			if mapping.Mode != "" {
				mode = mapping.Mode
			}
		}
		item.ProxyID, item.Direct, item.GroupIDs = in.ProxyID, in.Direct, vendorUniqueIDs(in.GroupIDs)
		if row.Notes != nil {
			in.VendorNotes = *row.Notes
		}
		if e := normalizeVendorAccountInput(&in); e != nil {
			item.Message = "Invalid account fields or egress mapping"
			result.Rows = append(result.Rows, item)
			continue
		}
		if e := validateVendorAuthorization(v, in); e != nil {
			item.Message = "Platform or authorization mode is not authorized"
			result.Rows = append(result.Rows, item)
			continue
		}
		if len(row.BPSCredentials) > 0 && (in.Platform != PlatformOpenAI || in.AuthorizationMode != VendorOpenAIModeNative || !vendorAllowsOpenAIMode(v, VendorOpenAIModeBPS) || excelMapString(row.BPSCredentials, "client_id") != openai.ExcelClientID) {
			item.Message = "An independent BPS grant requires native and BPS permissions"
			result.Rows = append(result.Rows, item)
			continue
		}
		var verified, bpsVerified *vendorVerifiedCredential
		var proxyVersion int64
		err = s.vendorAccountNetwork(ctx, v, in.ProxyID, in.Direct, nil, func(tx *sql.Tx) error {
			if e := s.ValidateVendorGroups(ctx, v, in.Platform, in.GroupIDs); e != nil {
				return e
			}
			_, version, e := s.vendorAccountEgress(ctx, tx, v, in.ProxyID, in.Direct)
			proxyVersion = version
			return e
		}, func(ctx context.Context, proxy string) error {
			rowCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
			defer cancel()
			var e error
			verified, e = s.vendorVerifyImportRow(rowCtx, row, proxy)
			if e == nil && len(row.BPSCredentials) > 0 {
				bpsVerified, e = s.vendorVerifyImportRow(rowCtx, VendorAccountBundleRow{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Credentials: row.BPSCredentials}, proxy)
				if e == nil {
					e = vendorOpenAIGrantsMatch(verified, bpsVerified)
				}
			}
			return e
		}, func(tx *sql.Tx) error {
			in.Method = verified.Method
			hash, e := s.vendorIdentityHash(in.Platform, verified.Identity)
			if e != nil {
				return e
			}
			if seen[hash] {
				item.Status = "skipped"
				item.Message = "Duplicate identity in this batch"
				return nil
			}
			seen[hash] = true
			var existing int64
			e = tx.QueryRowContext(ctx, "SELECT c.canonical_account_id FROM account_supply_identity_claims c JOIN accounts a ON a.id=c.canonical_account_id WHERE c.platform=$1 AND c.identity_hash=$2 AND c.vendor_id=$3 AND a.deleted_at IS NULL", in.Platform, hash, v.ID).Scan(&existing)
			if e == nil {
				item.AccountID = existing
				if mode == "skip" {
					item.Status = "skipped"
					item.Message = "Existing account; select update to replace its authorization"
					return nil
				}
				in.TargetAccountID = existing
				in.AuthorizationAction = "reauthorize"
			} else if e != sql.ErrNoRows {
				return e
			}
			if _, _, e = s.reserveVendorIdentity(ctx, tx, v, in, verified); e != nil {
				return e
			}
			item.Status = "create"
			if in.TargetAccountID > 0 {
				item.Status = "update"
			}
			targetFingerprint := ""
			if in.TargetAccountID > 0 {
				target, e := s.vendorEditableAccount(ctx, v, in.TargetAccountID)
				if e != nil {
					return e
				}
				targetFingerprint = vendorAccountEditFingerprint(target)
			}
			secrets[i] = vendorAccountImportSecretRow{Input: in, Credential: *verified, BPSCredential: bpsVerified, Ready: true, ProxyVersion: proxyVersion, TargetFingerprint: targetFingerprint}
			return nil
		})
		if err != nil {
			item.Status = "failed"
			item.Message = "Authorization, ownership, policy or egress verification failed"
		}
		result.Rows = append(result.Rows, item)
	}
	raw, err := json.Marshal(secrets)
	if err != nil {
		return nil, err
	}
	encrypted, err := s.openaiOAuth.excelEncryptor.Encrypt(string(raw))
	if err != nil {
		return nil, vendorAccountUnavailable()
	}
	session := &vendorAccountImportSession{Authorization: lease, VendorID: v.ID, PolicyVersion: v.PolicyVersion, Status: "pending", Ciphertext: encrypted, Result: result}
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		// All proxy locks follow ascending ID order under the shared vendor lock.
		proxyIDs := []int64{}
		versions := map[int64]int64{}
		for _, row := range secrets {
			if !row.Ready {
				continue
			}
			if row.Input.ProxyID == nil {
				if _, _, e := s.vendorAccountEgress(ctx, tx, v, nil, row.Input.Direct); e != nil {
					return e
				}
				continue
			}
			proxyID := *row.Input.ProxyID
			if previous, ok := versions[proxyID]; ok && previous != row.ProxyVersion {
				return ErrVendorAccountConflict
			}
			versions[proxyID] = row.ProxyVersion
			proxyIDs = append(proxyIDs, proxyID)
		}
		for _, proxyID := range vendorUniqueIDs(proxyIDs) {
			_, version, e := s.vendorAccountEgress(ctx, tx, v, &proxyID, false)
			if e != nil {
				return e
			}
			if versions[proxyID] != version {
				return ErrVendorAccountConflict
			}
			if e = s.vendorRegisterOAuthProxy(ctx, v.ID, "import:"+id, &proxyID, result.ExpiresAt); e != nil {
				return e
			}
		}
		return oauth.PutSharedSession(ctx, s.rdb, "oauth:vendor-import:"+id, session, time.Until(result.ExpiresAt))
	})
	if err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, v.ID, "account.import.preview", 0, "success")
	return &result, nil
}
func (s *VendorService) ImportVendorAccounts(ctx context.Context, input VendorAccountImportInput) (*VendorAccountImportResult, error) {
	if input.PreviewID == "" {
		preview, err := s.PreviewVendorAccountImport(ctx, input)
		if err != nil {
			return nil, err
		}
		input.PreviewID = preview.PreviewID
	}
	if len(input.PreviewID) < 16 || len(input.PreviewID) > 128 {
		return nil, ErrVendorAccountConflict
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	key := "oauth:vendor-import:" + input.PreviewID
	session, raw, err := oauth.ReadSharedSession[vendorAccountImportSession](ctx, s.rdb, key)
	if err != nil {
		return nil, ErrVendorAccountConflict
	}
	if session.VendorID != v.ID {
		return nil, ErrVendorAccountNotFound
	}
	if session.PolicyVersion != v.PolicyVersion {
		return nil, ErrVendorAccountConflict
	}
	if err = authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return nil, err
	}
	if session.Status == "completed" {
		return &session.Result, nil
	}
	if session.Status != "pending" {
		// Claiming is one-shot. Recovery only returns rows already committed; it
		// must never re-create a subsequently deleted row or overwrite a later login.
		found := false
		for i := range session.Result.Rows {
			row := &session.Result.Rows[i]
			if row.Status != "create" && row.Status != "update" {
				continue
			}
			var accountID int64
			e := s.db.QueryRowContext(ctx, "SELECT id FROM accounts WHERE vendor_id=$1 AND deleted_at IS NULL AND extra->>'vendor_import_operation'=$2", v.ID, vendorOAuthOperation(input.PreviewID+":"+fmt.Sprint(i))).Scan(&accountID)
			if e == nil {
				found = true
				row.AccountID = accountID
				if row.Status == "create" {
					row.Status = "created"
				} else {
					row.Status = "updated"
				}
			} else {
				row.Status = "unresolved"
				row.Message = "Operation in progress or interrupted; reload before creating a new preview"
			}
		}
		if !found {
			return nil, ErrVendorAccountConflict
		}
		return &session.Result, nil
	}
	if s.openaiOAuth == nil || s.openaiOAuth.excelEncryptor == nil {
		return nil, vendorAccountUnavailable()
	}
	plaintext, err := s.openaiOAuth.excelEncryptor.Decrypt(session.Ciphertext)
	if err != nil {
		return nil, vendorAccountUnavailable()
	}
	var secrets []vendorAccountImportSecretRow
	if json.Unmarshal([]byte(plaintext), &secrets) != nil || len(secrets) != len(session.Result.Rows) {
		return nil, ErrVendorAccountConflict
	}
	session.Status = "claimed"
	claimed, err := oauth.CompareSwapSession(ctx, s.rdb, key, raw, session)
	if err != nil {
		return nil, ErrVendorAccountConflict
	}

	for i, row := range secrets {
		if !row.Ready {
			continue
		}
		rowCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
		var release func()
		if row.Input.TargetAccountID > 0 {
			release, err = s.vendorTokenLock(rowCtx, &Account{ID: row.Input.TargetAccountID, Platform: row.Input.Platform})
			if err != nil {
				cancel()
				session.Result.Rows[i].Status = "failed"
				session.Result.Rows[i].Message = "Account authorization is being refreshed; retry later"
				continue
			}
		}
		var accountID int64
		var verified, bpsVerified *vendorVerifiedCredential
		var target *Account
		if row.Input.TargetAccountID > 0 {
			target, err = s.vendorEditableAccount(rowCtx, v, row.Input.TargetAccountID)
			if err == nil && row.TargetFingerprint != vendorAccountEditFingerprint(target) {
				err = ErrVendorAccountConflict
			}
			if err != nil {
				cancel()
				if release != nil {
					release()
				}
				session.Result.Rows[i].Status = "failed"
				session.Result.Rows[i].Message = "Account changed; create a new preview"
				continue
			}
		}
		err = s.vendorAccountNetwork(rowCtx, v, row.Input.ProxyID, row.Input.Direct, target, func(tx *sql.Tx) error {
			if e := authz.CheckLease(rowCtx, session.Authorization, "accounts.authorize"); e != nil {
				return e
			}
			_, version, e := s.vendorAccountEgress(rowCtx, tx, v, row.Input.ProxyID, row.Input.Direct)
			if e != nil {
				return e
			}
			if version != row.ProxyVersion {
				return ErrVendorAccountConflict
			}
			if e = s.ValidateVendorGroups(rowCtx, v, row.Input.Platform, row.Input.GroupIDs); e != nil {
				return e
			}
			if e = validateVendorAuthorization(v, row.Input); e != nil {
				return e
			}
			if row.BPSCredential != nil && !vendorAllowsOpenAIMode(v, VendorOpenAIModeBPS) {
				return ErrVendorDenied
			}
			_, _, e = s.reserveVendorIdentity(rowCtx, tx, v, row.Input, &row.Credential)
			return e
		}, func(ctx context.Context, proxy string) error {
			var e error
			verified, e = s.vendorRefreshImportCredential(ctx, row, proxy)
			if e == nil && row.BPSCredential != nil {
				bpsInput := row.Input
				bpsInput.AuthorizationMode = VendorOpenAIModeBPS
				bpsVerified, e = s.vendorRefreshImportCredential(ctx, vendorAccountImportSecretRow{Input: bpsInput, Credential: *row.BPSCredential}, proxy)
				if e == nil {
					e = vendorOpenAIGrantsMatch(verified, bpsVerified)
				}
			}
			return e
		}, func(tx *sql.Tx) error {
			if e := authz.CheckLease(rowCtx, session.Authorization, "accounts.authorize"); e != nil {
				return e
			}
			var e error
			accountID, e = s.persistVendorCredential(rowCtx, tx, v, row.Input, verified, true)
			if e != nil {
				return e
			}
			if bpsVerified != nil {
				bpsInput := row.Input
				bpsInput.TargetAccountID, bpsInput.AuthorizationMode, bpsInput.AuthorizationAction = accountID, VendorOpenAIModeBPS, "reauthorize"
				if e = s.persistVendorOpenAIAuthorization(rowCtx, tx, v, accountID, bpsInput, bpsVerified); e != nil {
					return e
				}
			}
			_, e = tx.ExecContext(rowCtx, "UPDATE accounts SET extra=jsonb_set(extra,'{vendor_import_operation}',to_jsonb($1::text)) WHERE id=$2", vendorOAuthOperation(input.PreviewID+":"+fmt.Sprint(i)), accountID)
			return e
		})
		cancel()
		if release != nil {
			release()
		}
		if err != nil {
			session.Result.Rows[i].Status = "failed"
			session.Result.Rows[i].Message = "Account changed, limit reached or verification expired"
		} else {
			session.Result.Rows[i].AccountID = accountID
			cacheKey := ClaudeTokenCacheKey(&Account{ID: accountID})
			if row.Input.Platform == PlatformOpenAI {
				cacheKey = OpenAITokenCacheKey(&Account{ID: accountID})
			}
			_ = s.rdb.Del(ctx, "oauth:token:"+cacheKey).Err()
			session.Result.Rows[i].Status = "created"
			if row.Input.TargetAccountID > 0 {
				session.Result.Rows[i].Status = "updated"
			}
		}
	}
	session.Status = "completed"
	session.Ciphertext = ""
	// Only a successful result replay is stored; authorization material is erased.
	_, _ = oauth.CompareSwapSession(ctx, s.rdb, key, claimed, session)
	s.InvalidateVendorAccounts(ctx, v.ID)
	for _, row := range secrets {
		if row.Input.ProxyID != nil {
			_ = s.ReleaseVendorProxyReference(ctx, v.ID, *row.Input.ProxyID, "oauth", "import:"+input.PreviewID)
		}
	}
	s.VendorAudit(ctx, v.ID, "account.import", 0, "completed")
	return &session.Result, nil
}

func vendorExportCredentials(a *Account) map[string]any {
	out := map[string]any{}
	for _, key := range []string{"access_token", "refresh_token", "token_type", "expires_at", "scope", "client_id", "id_token", "chatgpt_user_id", "email", "plan_type", "account_uuid", "org_uuid", "chatgpt_account_id"} {
		if v, ok := a.Credentials[key]; ok {
			out[key] = v
		}
	}
	return out
}

// ExportVendorAccounts is only exposed by the handler after shared step-up.
// It never accepts a caller-supplied owner or exports a platform proxy secret.
func (s *VendorService) ExportVendorAccounts(ctx context.Context, ids []int64) (*VendorAccountBundle, error) {
	ids = vendorUniqueIDs(ids)
	if len(ids) == 0 || len(ids) > 100 || ids[0] <= 0 {
		return nil, vendorAccountInputError("Select between 1 and 100 accounts")
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err = authz.Revalidate(ctx); err != nil {
		return nil, err
	}
	out := &VendorAccountBundle{Type: "sub2api-data", Version: 1, ExportedAt: time.Now().UTC().Format(time.RFC3339), Accounts: []VendorAccountBundleRow{}, Proxies: []any{}}
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		for _, id := range ids {
			var deleted sql.NullTime
			var archived bool
			var parent sql.NullInt64
			if e := tx.QueryRowContext(ctx, "SELECT deleted_at,parent_account_id,COALESCE(extra->>'vendor_archived_at','')<>'' FROM accounts WHERE id=$1 AND vendor_id=$2 FOR UPDATE", id, v.ID).Scan(&deleted, &parent, &archived); e != nil {
				return ErrVendorAccountNotFound
			}
			if deleted.Valid {
				out.SkippedDeleted++
				continue
			}
			if archived {
				return ErrVendorAccountArchived
			}
			if parent.Valid {
				out.SkippedShadows++
				continue
			}
			a, err := s.accounts.GetByID(ctx, id)
			if err != nil || a == nil || a.VendorID == nil || *a.VendorID != v.ID {
				return ErrVendorAccountNotFound
			}
			if a.ParentAccountID != nil {
				out.SkippedShadows++
				continue
			}
			credentials := vendorExportCredentials(a)
			var bpsCredentials map[string]any
			if a.Platform == PlatformOpenAI {
				if !vendorAllowsOpenAIMode(v, vendorAccountPrimaryMode(a)) {
					credentials = nil
				}
				if !a.IsExcelOAuth() && vendorAllowsOpenAIMode(v, VendorOpenAIModeBPS) {
					var ciphertext string
					err := tx.QueryRowContext(ctx, "SELECT ciphertext FROM openai_excel_grants WHERE account_id=$1", id).Scan(&ciphertext)
					if err != nil && err != sql.ErrNoRows {
						return err
					}
					if ciphertext != "" {
						if s.openaiOAuth == nil || s.openaiOAuth.excelEncryptor == nil {
							return excelGrantStorageUnavailable()
						}
						plain, err := s.openaiOAuth.excelEncryptor.Decrypt(ciphertext)
						if err != nil || json.Unmarshal([]byte(plain), &bpsCredentials) != nil {
							return excelGrantStorageUnavailable()
						}
						bpsCredentials = vendorExportCredentials(&Account{Credentials: bpsCredentials})
					}
				}
			}
			if excelMapString(credentials, "access_token") == "" && excelMapString(bpsCredentials, "access_token") != "" {
				credentials, bpsCredentials = bpsCredentials, nil
			}
			if excelMapString(credentials, "access_token") == "" {
				out.SkippedEmpty++
				continue
			}
			notes := a.VendorNotes
			out.Accounts = append(out.Accounts, VendorAccountBundleRow{Name: a.Name, Notes: &notes, Platform: a.Platform, Type: a.Type, Credentials: credentials, BPSCredentials: bpsCredentials})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = authz.Revalidate(ctx); err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, v.ID, "account.export", 0, fmt.Sprintf("success:%d", len(out.Accounts)))
	return out, nil
}

// Confirmation verifies the refresh grant as well as the access-token identity.
// Preview never rotates tokens; skipped rows never reach this method.
func (s *VendorService) vendorRefreshImportCredential(ctx context.Context, row vendorAccountImportSecretRow, proxy string) (*vendorVerifiedCredential, error) {
	refresh, _ := row.Credential.Credentials["refresh_token"].(string)
	var verified *vendorVerifiedCredential
	var err error
	if row.Input.Platform == PlatformOpenAI {
		if row.Input.AuthorizationMode == "" {
			row.Input.AuthorizationMode = VendorOpenAIModeNative
			if excelMapString(row.Credential.Credentials, "client_id") == openai.ExcelClientID {
				row.Input.AuthorizationMode = VendorOpenAIModeBPS
			}
		}
		if s.openaiOAuth == nil || refresh == "" {
			return nil, ErrVendorIdentityUnverified
		}
		t, e := s.openaiOAuth.oauthClient.RefreshTokenWithClientID(ctx, refresh, proxy, vendorOpenAIClient(row.Input.AuthorizationMode))
		if e != nil || t == nil {
			return nil, ErrVendorIdentityUnverified
		}
		if t.RefreshToken == "" {
			t.RefreshToken = refresh
		}
		verified, err = s.vendorVerifyOpenAIProfile(ctx, t, row.Input.Method, proxy, row.Input.AuthorizationMode)
	} else if refresh != "" {
		if s.oauth == nil {
			return nil, vendorAccountUnavailable()
		}
		t, e := s.oauth.RefreshToken(ctx, refresh, proxy)
		if e != nil || t == nil {
			return nil, ErrVendorIdentityUnverified
		}
		if t.RefreshToken == "" {
			t.RefreshToken = refresh
		}
		verified, err = s.vendorVerifyClaude(ctx, t, row.Input.Method, proxy)
	} else {
		access, _ := row.Credential.Credentials["access_token"].(string)
		verified, err = s.vendorVerifyClaude(ctx, &TokenInfo{AccessToken: access}, row.Input.Method, proxy)
	}
	if err != nil {
		return nil, err
	}
	if verified.Identity != row.Credential.Identity {
		return nil, ErrVendorIdentityBound
	}
	return verified, nil
}

// Both values have independently passed their fixed profile's identity verifier.
func vendorOpenAIGrantsMatch(primary, bps *vendorVerifiedCredential) error {
	if primary == nil || bps == nil || primary.Identity == "" || primary.Identity != bps.Identity {
		return ErrVendorIdentityBound
	}
	for _, key := range []string{"chatgpt_user_id", "email"} {
		a, b := excelMapString(primary.Credentials, key), excelMapString(bps.Credentials, key)
		if a != "" && b != "" && !strings.EqualFold(a, b) {
			return ErrVendorIdentityBound
		}
	}
	return nil
}
