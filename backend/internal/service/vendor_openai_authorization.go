package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/lib/pq"
)

const (
	VendorOpenAIModeNative = "native"
	VendorOpenAIModeBPS    = "bps"
)

func normalizeVendorOpenAIModes(v *Vendor) error {
	if !stringsContain(v.Platforms, PlatformOpenAI) {
		v.OpenAIAuthorizationModes = []string{}
		return nil
	}
	// Omitted legacy policy input retains the native-only admission baseline.
	if v.OpenAIAuthorizationModes == nil {
		v.OpenAIAuthorizationModes = []string{VendorOpenAIModeNative}
	}
	if len(v.OpenAIAuthorizationModes) == 0 || len(v.OpenAIAuthorizationModes) > 2 {
		return ErrVendorInput
	}
	seen := map[string]bool{}
	for _, mode := range v.OpenAIAuthorizationModes {
		if !validVendorOpenAIMode(mode) || seen[mode] {
			return ErrVendorInput
		}
		seen[mode] = true
	}
	return nil
}

func validVendorOpenAIMode(mode string) bool {
	return mode == VendorOpenAIModeNative || mode == VendorOpenAIModeBPS
}

func vendorAllowsOpenAIMode(v *Vendor, mode string) bool {
	return v != nil && stringsContain(v.Platforms, PlatformOpenAI) && stringsContain(v.OpenAIAuthorizationModes, mode)
}

func validateVendorAuthorization(v *Vendor, in VendorAccountInput) error {
	if !stringsContain(v.Platforms, in.Platform) {
		return ErrVendorDenied
	}
	if in.Platform == PlatformOpenAI && !vendorAllowsOpenAIMode(v, in.AuthorizationMode) {
		return ErrVendorDenied
	}
	return nil
}

func normalizeVendorAuthorizationInput(in *VendorAccountInput) error {
	if in.Platform == PlatformOpenAI {
		if in.AuthorizationMode == "" {
			in.AuthorizationMode = VendorOpenAIModeNative
		}
		if !validVendorOpenAIMode(in.AuthorizationMode) {
			return vendorAccountInputError("Unsupported OpenAI authorization mode")
		}
	} else if in.AuthorizationMode != "" {
		return vendorAccountInputError("Authorization mode is only valid for OpenAI")
	}
	if in.AuthorizationAction == "" {
		in.AuthorizationAction = "create"
		if in.TargetAccountID > 0 {
			in.AuthorizationAction = "reauthorize"
		}
	}
	if (in.AuthorizationAction == "create" && in.TargetAccountID != 0) ||
		((in.AuthorizationAction == "attach" || in.AuthorizationAction == "reauthorize") && in.TargetAccountID <= 0) ||
		(in.AuthorizationAction != "create" && in.AuthorizationAction != "attach" && in.AuthorizationAction != "reauthorize") ||
		(in.AuthorizationAction == "attach" && in.Platform != PlatformOpenAI) {
		return vendorAccountInputError("Invalid authorization operation")
	}
	return nil
}

func vendorOpenAIProfile(mode string) (string, string) {
	if mode == VendorOpenAIModeBPS {
		return openai.ExcelClientID, openai.ExcelRedirectURI
	}
	return openai.ClientID, openai.DefaultRedirectURI
}
func vendorOpenAIClient(mode string) string { client, _ := vendorOpenAIProfile(mode); return client }
func vendorAccountPrimaryMode(a *Account) string {
	if a.IsExcelOAuth() {
		return VendorOpenAIModeBPS
	}
	return VendorOpenAIModeNative
}

// Caller holds the owner and account locks and has independently verified the
// upstream identity. Keep the two grants independent in one atomic account edit.
func (s *VendorService) persistVendorOpenAIAuthorization(ctx context.Context, tx *sql.Tx, v *Vendor, id int64, in VendorAccountInput, verified *vendorVerifiedCredential) error {
	var raw, extraRaw []byte
	if err := tx.QueryRowContext(ctx, "SELECT credentials,extra FROM accounts WHERE id=$1 AND vendor_id=$2 FOR UPDATE", id, v.ID).Scan(&raw, &extraRaw); err != nil {
		return err
	}
	a := &Account{ID: id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, VendorID: &v.ID}
	if json.Unmarshal(raw, &a.Credentials) != nil || json.Unmarshal(extraRaw, &a.Extra) != nil {
		return ErrVendorAccountConflict
	}
	if a.Extra == nil {
		a.Extra = map[string]any{}
	}
	if a.GetChatGPTAccountID() != verified.AccountID {
		return ErrVendorIdentityBound
	}
	for _, key := range []string{"chatgpt_user_id", "email"} {
		old, fresh := a.GetCredential(key), excelMapString(verified.Credentials, key)
		if key == "email" {
			old = excelFirstNonEmpty(old, a.GetExtraString("vendor_verified_email"))
			fresh = excelFirstNonEmpty(fresh, verified.Email)
		}
		if old != "" && fresh != "" && !strings.EqualFold(old, fresh) {
			return ErrVendorIdentityBound
		}
	}
	primaryMode := vendorAccountPrimaryMode(a)
	var grant string
	if err := tx.QueryRowContext(ctx, "SELECT ciphertext FROM openai_excel_grants WHERE account_id=$1", id).Scan(&grant); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	present := primaryMode == in.AuthorizationMode && a.GetCredential("access_token") != ""
	if in.AuthorizationMode == VendorOpenAIModeBPS && grant != "" {
		present = true
	}
	if in.AuthorizationAction == "attach" && present {
		return ErrVendorAccountConflict
	}
	if in.AuthorizationMode == VendorOpenAIModeBPS && primaryMode == VendorOpenAIModeNative {
		if err := s.storeVendorExcelGrant(ctx, tx, id, verified.Credentials); err != nil {
			return err
		}
	} else {
		if primaryMode == VendorOpenAIModeBPS && in.AuthorizationMode == VendorOpenAIModeNative && a.GetCredential("access_token") != "" {
			// Preserve the old grant even when its mode permission was revoked.
			// This transfer does not re-enable that mode or admit a new BPS grant.
			if err := s.storeVendorExcelGrant(ctx, tx, id, a.Credentials); err != nil {
				return err
			}
		}
		a.Credentials = verified.Credentials
		a.Extra["vendor_auth_method"] = verified.Method
	}
	// Authorizing a credential never resets an explicit pause, cooldown or a
	// disabled mode. Only the newly attached BPS mode defaults to enabled.
	if in.AuthorizationMode == VendorOpenAIModeBPS {
		if _, configured := a.Extra["openai_excel_bps"]; !configured {
			a.Extra["openai_excel_bps"] = true
		}
	}
	a.Extra["vendor_verified_email"] = verified.Email
	a.Extra["vendor_authorization_version"] = strconv.FormatInt(time.Now().UnixNano(), 10)
	credentials, err := json.Marshal(a.Credentials)
	if err != nil {
		return err
	}
	extra, err := json.Marshal(a.Extra)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=$1::jsonb,extra=$2::jsonb,name=$3,vendor_notes=$4,proxy_id=$5,proxy_fallback_origin_id=NULL,updated_at=NOW() WHERE id=$6 AND vendor_id=$7`, string(credentials), string(extra), in.Name, in.VendorNotes, in.ProxyID, id, v.ID)
	return err
}

type VendorAccountAuthorization struct {
	Mode       string `json:"mode"`
	Authorized bool   `json:"authorized"`
	Enabled    bool   `json:"enabled"`
	Status     string `json:"status"`
	ExpiresAt  string `json:"expires_at,omitempty"`
	CanRefresh bool   `json:"can_refresh"`
}

func vendorAuthorizationState(a *Account, v *Vendor, mode string, credentials map[string]any) VendorAccountAuthorization {
	state := VendorAccountAuthorization{Mode: mode, Authorized: excelMapString(credentials, "access_token") != "", Status: "unconfigured"}
	state.Enabled = mode == VendorOpenAIModeNative && a.Extra["vendor_native_disabled"] != true || mode == VendorOpenAIModeBPS && a.Extra["openai_excel_bps"] == true
	state.ExpiresAt = excelMapString(credentials, "expires_at")
	state.CanRefresh = state.Authorized && !a.IsVendorArchived() && vendorAllowsOpenAIMode(v, mode) && excelMapString(credentials, "refresh_token") != ""
	if state.Authorized {
		state.Status = "active"
		if expiry, err := time.Parse(time.RFC3339, state.ExpiresAt); err == nil && !expiry.After(time.Now()) {
			state.Status = "expired"
		}
		if !state.Enabled {
			state.Status = "disabled"
		}
	}
	if !vendorAllowsOpenAIMode(v, mode) {
		state.Status = "policy_revoked"
	}
	return state
}

// Fetch all private grant metadata in one bounded read. This is a projection,
// never an automatic refresh or a secret-bearing response to the browser.
func (s *VendorService) populateVendorAuthorizations(ctx context.Context, accounts []*Account, v *Vendor, dtos []*VendorAccountDTO) error {
	ids := []int64{}
	byID := map[int64]*Account{}
	for _, a := range accounts {
		if a.Platform == PlatformOpenAI && a.VendorID != nil && *a.VendorID == v.ID {
			ids = append(ids, a.ID)
			byID[a.ID] = a
		}
	}
	for _, d := range dtos {
		d.AuthorizationModes = []string{}
		d.Authorizations = []VendorAccountAuthorization{}
	}
	if len(ids) == 0 {
		return nil
	}
	effective := *v
	if v.ParentVendorID != nil {
		parent, err := s.loadVendor(ctx, *v.ParentVendorID)
		if err != nil {
			return err
		}
		applyVendorParentPolicy(&effective, parent)
	}
	grants := map[int64]map[string]any{}
	failed := map[int64]bool{}
	rows, err := s.db.QueryContext(ctx, "SELECT g.account_id,g.ciphertext FROM openai_excel_grants g JOIN accounts a ON a.id=g.account_id WHERE g.account_id=ANY($1) AND a.vendor_id=$2 AND a.deleted_at IS NULL AND g.ciphertext<>''", pq.Array(ids), v.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var encrypted string
		if err = rows.Scan(&id, &encrypted); err != nil {
			return err
		}
		if s.openaiOAuth == nil || s.openaiOAuth.excelEncryptor == nil {
			failed[id] = true
			continue
		}
		plain, e := s.openaiOAuth.excelEncryptor.Decrypt(encrypted)
		var credentials map[string]any
		if e != nil || json.Unmarshal([]byte(plain), &credentials) != nil {
			failed[id] = true
			continue
		}
		grants[id] = credentials
	}
	if err = rows.Err(); err != nil {
		return err
	}
	for _, d := range dtos {
		a := byID[d.ID]
		if a == nil {
			continue
		}
		credentials := map[string]map[string]any{VendorOpenAIModeBPS: grants[a.ID]}
		credentials[vendorAccountPrimaryMode(a)] = a.Credentials
		d.CanRefresh = false
		usable := false
		for _, mode := range []string{VendorOpenAIModeNative, VendorOpenAIModeBPS} {
			state := vendorAuthorizationState(a, &effective, mode, credentials[mode])
			if mode == VendorOpenAIModeBPS && failed[a.ID] {
				state.Status = "error"
				state.Authorized = true
				state.CanRefresh = false
			}
			if state.Authorized {
				d.AuthorizationModes = append(d.AuthorizationModes, mode)
			}
			if state.Authorized && state.Enabled && (state.Status == "active" || state.Status == "expired" && state.CanRefresh) {
				usable = true
			}
			d.Authorizations = append(d.Authorizations, state)
			if mode == vendorAccountPrimaryMode(a) {
				d.CanRefresh = state.CanRefresh
			}
		}
		if usable {
			blockers := d.Blockers[:0]
			for _, blocker := range d.Blockers {
				if blocker != "authorization_required" {
					blockers = append(blockers, blocker)
				}
			}
			d.Blockers = blockers
			d.Schedulable = a.IsSchedulable() && len(blockers) == 0
		} else {
			d.Schedulable = false
			d.Blockers = append(d.Blockers, "authorization_mode_unavailable")
		}
		if raw, present := a.Extra["openai_excel_bps_models"]; present {
			d.BPSModels = []string{}
			data, _ := json.Marshal(raw)
			_ = json.Unmarshal(data, &d.BPSModels)
		}
	}
	return nil
}

func (s *VendorService) updateVendorAuthorizationSettings(ctx context.Context, tx *sql.Tx, v *Vendor, a *Account, in VendorAccountUpdate) error {
	if in.NativeEnabled == nil && in.BPSEnabled == nil && in.BPSModels == nil && in.BPSAllModels == nil {
		return nil
	}
	if a.Platform != PlatformOpenAI {
		return vendorAccountInputError("Authorization modes are only available for OpenAI")
	}
	if in.NativeEnabled != nil && !vendorAllowsOpenAIMode(v, VendorOpenAIModeNative) {
		return ErrVendorDenied
	}
	if (in.BPSEnabled != nil || in.BPSModels != nil || in.BPSAllModels != nil) && !vendorAllowsOpenAIMode(v, VendorOpenAIModeBPS) {
		return ErrVendorDenied
	}
	if in.BPSModels != nil && len(*in.BPSModels) > 100 {
		return vendorAccountInputError("Too many BPS models")
	}
	updates := map[string]any{"vendor_authorization_version": strconv.FormatInt(time.Now().UnixNano(), 10)}
	if in.NativeEnabled != nil {
		updates["vendor_native_disabled"] = !*in.NativeEnabled
	}
	if in.BPSEnabled != nil {
		updates["openai_excel_bps"] = *in.BPSEnabled
	}
	if in.BPSModels != nil {
		models := []string{}
		for _, model := range *in.BPSModels {
			if model == "" || len(model) > 128 || strings.ContainsAny(model, " \t\n\r") {
				return vendorAccountInputError("Invalid BPS model")
			}
			if !stringsContain(models, model) {
				models = append(models, model)
			}
		}
		updates["openai_excel_bps_models"] = models
	}
	if in.BPSAllModels != nil {
		return vendorAccountInputError("bps_all_models is no longer supported; provide an explicit bps_models list")
	}
	return persistVendorAuthorizationSettings(ctx, tx, a, in, updates)
}

func (s *VendorService) RevokeVendorAuthorization(ctx context.Context, id int64, mode string) (*VendorAccountDTO, error) {
	if !validVendorOpenAIMode(mode) {
		return nil, vendorAccountInputError("Invalid authorization mode")
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	if !vendorAllowsOpenAIMode(v, mode) {
		return nil, ErrVendorDenied
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if a.Platform != PlatformOpenAI {
		return nil, ErrVendorAccountConflict
	}
	release, err := s.vendorTokenLock(ctx, a)
	if err != nil {
		return nil, err
	}
	defer release()
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if e := s.vendorLockAccount(ctx, tx, v, id); e != nil {
			return e
		}
		if e := vendorAccountSnapshotMatches(ctx, tx, a); e != nil {
			return e
		}
		if mode == vendorAccountPrimaryMode(a) {
			if _, e := tx.ExecContext(ctx, "UPDATE accounts SET credentials=credentials-ARRAY['access_token','refresh_token','id_token'] WHERE id=$1", id); e != nil {
				return e
			}
		}
		if mode == VendorOpenAIModeBPS {
			if _, e := tx.ExecContext(ctx, "DELETE FROM openai_excel_grants WHERE account_id=$1", id); e != nil {
				return e
			}
		}
		if _, e := tx.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{vendor_authorization_version}',to_jsonb($2::text)),updated_at=NOW() WHERE id=$1", id, strconv.FormatInt(time.Now().UnixNano(), 10)); e != nil {
			return e
		}
		return vendorAccountOutbox(ctx, tx, id, a.GroupIDs)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	if s.rdb != nil {
		_ = s.rdb.Del(ctx, "oauth:token:"+OpenAITokenCacheKey(a)).Err()
	}
	s.VendorAudit(ctx, v.ID, "account.authorization.revoke."+mode, id, "success")
	return s.GetVendorAccount(ctx, id)
}

func (s *VendorService) refreshVendorExcelGrant(ctx context.Context, v *Vendor, a *Account) (*VendorAccountDTO, error) {
	return s.refreshVendorExcelGrantWithVerifier(ctx, v, a, s.vendorVerifyOpenAIProfile)
}

func (s *VendorService) refreshVendorExcelGrantWithVerifier(ctx context.Context, v *Vendor, a *Account, verify func(context.Context, *openai.TokenResponse, string, string, string) (*vendorVerifiedCredential, error)) (*VendorAccountDTO, error) {
	if s.openaiOAuth == nil || s.openaiOAuth.excelEncryptor == nil {
		return nil, excelGrantStorageUnavailable()
	}
	if err := s.vendorAccountRateLimit(ctx, v.ID, a.ID, "refresh.bps"); err != nil {
		return nil, err
	}
	release, err := s.vendorTokenLock(ctx, a)
	if err != nil {
		return nil, err
	}
	defer release()
	var encrypted string
	if err = s.db.QueryRowContext(ctx, "SELECT ciphertext FROM openai_excel_grants WHERE account_id=$1 AND ciphertext<>''", a.ID).Scan(&encrypted); err != nil {
		return nil, excelAuthRequired()
	}
	plain, err := s.openaiOAuth.excelEncryptor.Decrypt(encrypted)
	if err != nil {
		return nil, excelGrantStorageUnavailable()
	}
	var credentials map[string]any
	if json.Unmarshal([]byte(plain), &credentials) != nil {
		return nil, excelGrantStorageUnavailable()
	}
	saved, err := decodeStoredExcelOAuthTokenInfo(a, credentials, true)
	if err != nil {
		return nil, ErrVendorIdentityBound
	}
	if saved.RefreshToken == "" {
		return nil, vendorAccountInputError("This account requires BPS reauthorization; no refresh token is stored")
	}
	if s.openaiOAuth.oauthClient == nil {
		return nil, vendorAccountUnavailable()
	}
	err = s.vendorAccountNetwork(ctx, v, a.ProxyID, a.ProxyID == nil, a, nil, func(ctx context.Context, proxy string) error {
		response, err := s.openaiOAuth.oauthClient.RefreshTokenWithClientID(ctx, saved.RefreshToken, proxy, openai.ExcelClientID)
		if err != nil || response == nil {
			return ErrVendorIdentityUnverified
		}
		// Verify only this provider response. A retained ID Token belongs to
		// the old AT and must never be presented as fresh identity evidence.
		verified, err := verify(ctx, response, "refresh_token", proxy, VendorOpenAIModeBPS)
		if err != nil {
			return err
		}
		if verified == nil || verified.Identity != saved.ChatGPTAccountID || verified.AccountID != saved.ChatGPTAccountID ||
			excelMapString(verified.Credentials, "chatgpt_user_id") != saved.ChatGPTUserID {
			return ErrVendorIdentityBound
		}
		// Omitted RT/ID Token and display metadata stay in the encrypted grant
		// only after verification succeeds; newly returned fields replace them.
		for key, value := range verified.Credentials {
			credentials[key] = value
		}
		delete(credentials, excelAccessTokenRejectedKey)
		if _, err := decodeStoredExcelOAuthTokenInfo(a, credentials, false); err != nil {
			return ErrVendorIdentityBound
		}
		return nil
	}, func(tx *sql.Tx) error {
		var current string
		if e := tx.QueryRowContext(ctx, "SELECT ciphertext FROM openai_excel_grants WHERE account_id=$1", a.ID).Scan(&current); e != nil || current != encrypted {
			return ErrVendorAccountConflict
		}
		if err := s.storeVendorExcelGrant(ctx, tx, a.ID, credentials, encrypted); err != nil {
			return err
		}
		return vendorAccountOutbox(ctx, tx, a.ID, a.GroupIDs)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	s.VendorAudit(ctx, v.ID, "account.refresh.bps", a.ID, "success")
	return s.GetVendorAccount(ctx, a.ID)
}
