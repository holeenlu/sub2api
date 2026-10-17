package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/httpclient"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type vendorVerifiedCredential struct {
	Credentials    map[string]any
	Identity       string
	AccountID      string
	OrganizationID string
	Email          string
	Method         string
}

// Fingerprints use the existing stable server key, domain separated from JWTs.
// Email and rotating tokens are never an account's supply identity.
func VendorSupplyIdentityHash(key, platform, identity string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("sub2api:supply-identity:v1\x00" + platform + "\x00" + identity))
	return hex.EncodeToString(mac.Sum(nil))
}

// ValidateSupplyIdentityKey prevents a JWT/server-key rotation or inconsistent
// instance configuration from silently creating a different ownership namespace.
// Native platform claims must use this same guard and HMAC algorithm.
func ValidateSupplyIdentityKey(ctx context.Context, tx VendorProxySQL, key string) error {
	if len(key) < 32 {
		return vendorAccountUnavailable()
	}
	fingerprint := vendorIdentityKeyFingerprint(key)
	if _, e := tx.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('vendor_supply_identity_key_v1',$1) ON CONFLICT(key) DO NOTHING", fingerprint); e != nil {
		return e
	}
	var stored string
	rows, e := tx.QueryContext(ctx, "SELECT value FROM settings WHERE key='vendor_supply_identity_key_v1' FOR SHARE")
	if e != nil {
		return e
	}
	defer rows.Close()
	if !rows.Next() {
		if e = rows.Err(); e != nil {
			return e
		}
		return vendorAccountUnavailable()
	}
	if e = rows.Scan(&stored); e != nil {
		return e
	}
	if stored != fingerprint {
		return vendorAccountUnavailable()
	}
	return nil
}
func vendorIdentityKeyFingerprint(key string) string {
	sum := sha256.Sum256([]byte("sub2api:supply-identity-key:v1\x00" + key))
	return hex.EncodeToString(sum[:])
}
func (s *VendorService) vendorIdentityHash(platform, identity string) (string, error) {
	if s.cfg == nil || len(s.cfg.JWT.Secret) < 32 || identity == "" {
		return "", vendorAccountUnavailable()
	}
	return VendorSupplyIdentityHash(s.cfg.JWT.Secret, platform, identity), nil
}

// The native shared transport resolves and validates vendor proxy URLs at every
// socket dial. Preserve the URL hostname for HTTPS proxy certificate validation.
func (s *VendorService) vendorAccountEgress(ctx context.Context, tx *sql.Tx, v *Vendor, id *int64, direct bool) (string, int64, error) {
	if id == nil {
		if !direct || !v.AllowDirect {
			return "", 0, vendorAccountInputError("Direct connection is not permitted or explicitly selected")
		}
		return "", 0, nil
	}
	if *id <= 0 || direct {
		return "", 0, vendorAccountInputError("Invalid egress selection")
	}
	p, err := ValidateVendorProxyBinding(ctx, tx, v.ID, *id)
	if err != nil {
		return "", 0, err
	}
	return p.URL(), p.ConfigVersion, nil
}

// A fixed native profile endpoint authenticates manual setup tokens. It is a
// metadata request, never a billable model call. An inference-only token whose
// provider does not expose identity is rejected instead of trusting input/JWTs.
func vendorClaudeProfile(ctx context.Context, token, proxy string) (account, org, email string, err error) {
	if token == "" {
		return "", "", "", ErrVendorIdentityUnverified
	}
	client, e := httpclient.GetClient(httpclient.Options{ProxyURL: proxy, Timeout: 20 * time.Second})
	if e != nil {
		return "", "", "", ErrVendorIdentityUnverified
	}
	request, e := http.NewRequestWithContext(ctx, http.MethodGet, claudeResetProfileURL, nil)
	if e != nil {
		return "", "", "", e
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("Accept", "application/json")
	request.Header.Set("anthropic-beta", "oauth-2025-04-20")
	request.Header.Set("x-app", "cli")
	// Never send authorization to a redirect target.
	copyClient := *client
	copyClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, e := copyClient.Do(request)
	if e != nil {
		return "", "", "", ErrVendorIdentityUnverified
	}
	defer resp.Body.Close()
	var body struct {
		Account struct {
			UUID         string `json:"uuid"`
			Email        string `json:"email"`
			EmailAddress string `json:"email_address"`
		} `json:"account"`
		Organization struct {
			UUID string `json:"uuid"`
		} `json:"organization"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.Account.UUID == "" || body.Organization.UUID == "" {
		return "", "", "", ErrVendorIdentityUnverified
	}
	email = body.Account.EmailAddress
	if email == "" {
		email = body.Account.Email
	}
	return body.Account.UUID, body.Organization.UUID, email, nil
}
func vendorClaudeCredentials(t *TokenInfo, method string) *vendorVerifiedCredential {
	c := map[string]any{"access_token": t.AccessToken, "token_type": t.TokenType, "scope": t.Scope}
	if t.ExpiresAt > 0 {
		c["expires_at"] = time.Unix(t.ExpiresAt, 0).Format(time.RFC3339)
	}
	if t.RefreshToken != "" {
		c["refresh_token"] = t.RefreshToken
	}
	c["account_uuid"] = t.AccountUUID
	c["org_uuid"] = t.OrgUUID
	return &vendorVerifiedCredential{Credentials: c, Identity: t.AccountUUID + "\x00" + t.OrgUUID, AccountID: t.AccountUUID, OrganizationID: t.OrgUUID, Email: t.EmailAddress, Method: method}
}
func (s *VendorService) vendorVerifyClaude(ctx context.Context, t *TokenInfo, method, proxy string) (*vendorVerifiedCredential, error) {
	if t == nil || t.AccessToken == "" {
		return nil, ErrVendorIdentityUnverified
	}
	// Account/org in the HTTPS token endpoint response are native verified values.
	if t.AccountUUID == "" || t.OrgUUID == "" {
		a, o, e, err := vendorClaudeProfile(ctx, t.AccessToken, proxy)
		if err != nil {
			return nil, err
		}
		t.AccountUUID = a
		t.OrgUUID = o
		t.EmailAddress = e
	}
	return vendorClaudeCredentials(t, method), nil
}
func (s *VendorService) vendorVerifyOpenAI(ctx context.Context, t *openai.TokenResponse, method, proxy string) (*vendorVerifiedCredential, error) {
	if t == nil || t.AccessToken == "" || s.openaiOAuth == nil {
		return nil, ErrVendorIdentityUnverified
	}
	// Never decode the ID/access token to obtain identity or email. This native
	// metadata response must independently authenticate the returned access token.
	info := fetchChatGPTAccountInfo(ctx, s.openaiOAuth.privacyClientFactory, t.AccessToken, proxy, "")
	if info == nil || info.AccountID == "" || info.AccountID == "default" {
		return nil, ErrVendorIdentityUnverified
	}
	ti := &OpenAITokenInfo{AccessToken: t.AccessToken, RefreshToken: t.RefreshToken, ClientID: openai.ClientID, ChatGPTAccountID: info.AccountID, Email: info.Email, PlanType: info.PlanType, SubscriptionExpiresAt: info.SubscriptionExpiresAt}
	if t.ExpiresIn > 0 {
		ti.ExpiresAt = time.Now().Unix() + int64(t.ExpiresIn)
	}
	return &vendorVerifiedCredential{Credentials: s.openaiOAuth.BuildAccountCredentials(ti), Identity: info.AccountID, AccountID: info.AccountID, Email: info.Email, Method: method}, nil
}
func (s *VendorService) vendorVerifyToken(ctx context.Context, in VendorAccountInput, proxy string) (*vendorVerifiedCredential, error) {
	if strings.TrimSpace(in.Token) == "" || len(in.Token) > 32768 {
		return nil, vendorAccountInputError("Authorization material required")
	}
	switch {
	case in.Platform == PlatformOpenAI && in.Method == "refresh_token":
		if s.openaiOAuth == nil {
			return nil, vendorAccountUnavailable()
		}
		t, err := s.openaiOAuth.oauthClient.RefreshTokenWithClientID(ctx, strings.TrimSpace(in.Token), proxy, vendorOpenAIClient(in.AuthorizationMode))
		if err != nil {
			return nil, ErrVendorIdentityUnverified
		}
		if t.RefreshToken == "" {
			t.RefreshToken = strings.TrimSpace(in.Token)
		}
		return s.vendorVerifyOpenAIProfile(ctx, t, in.Method, proxy, in.AuthorizationMode)
	case in.Platform == PlatformAnthropic && in.Method == "setup-token-manual":
		return s.vendorVerifyClaude(ctx, &TokenInfo{AccessToken: strings.TrimSpace(in.Token), TokenType: "Bearer", Scope: oauth.ScopeInference}, in.Method, proxy)
	default:
		return nil, vendorAccountInputError("Unsupported token authorization method")
	}
}

// reserveVendorIdentity is serialized by both the vendor row and a provider
// identity advisory lock. Keep claims for tombstones and never transfer owners.
func (s *VendorService) reserveVendorIdentity(ctx context.Context, tx *sql.Tx, v *Vendor, in VendorAccountInput, verified *vendorVerifiedCredential) (string, int64, error) {
	hash, err := s.vendorIdentityHash(in.Platform, verified.Identity)
	if err != nil {
		return "", 0, err
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended('supply-identity-baseline',0))"); err != nil {
		return "", 0, err
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "supply:"+in.Platform+":"+hash); err != nil {
		return "", 0, err
	}
	if err = ValidateSupplyIdentityKey(ctx, tx, s.cfg.JWT.Secret); err != nil {
		return "", 0, err
	}
	var owner sql.NullInt64
	var canonical int64
	err = tx.QueryRowContext(ctx, "SELECT canonical_account_id,vendor_id FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 FOR UPDATE", in.Platform, hash).Scan(&canonical, &owner)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return "", 0, err
	}
	if err == nil {
		if !owner.Valid || owner.Int64 != v.ID {
			return "", 0, ErrVendorIdentityBound
		}
		var deleted sql.NullTime
		var archived bool
		if e := tx.QueryRowContext(ctx, "SELECT deleted_at,COALESCE(extra->>'vendor_archived_at','')<>'' FROM accounts WHERE id=$1 AND vendor_id=$2 FOR UPDATE", canonical, v.ID).Scan(&deleted, &archived); e != nil {
			return "", 0, ErrVendorIdentityBound
		}
		if archived {
			return "", 0, ErrVendorAccountArchived
		}
		if !deleted.Valid {
			if in.TargetAccountID != canonical {
				return hash, canonical, ErrVendorIdentityBound
			}
			return hash, canonical, nil
		}
		if in.TargetAccountID != 0 {
			return "", 0, ErrVendorAccountNotFound
		}
	}
	// Legacy root records without claims cannot silently be adopted. Even a
	// matching unverified historic identity blocks takeover (conservative).
	query := `SELECT id,vendor_id,deleted_at FROM accounts WHERE platform=$1 AND parent_account_id IS NULL AND type IN('oauth','setup-token') AND ((credentials->>'account_uuid'=$2 AND credentials->>'org_uuid'=$3) OR (extra->>'account_uuid'=$2 AND extra->>'org_uuid'=$3))`
	args := []any{in.Platform, verified.AccountID, verified.OrganizationID}
	if in.Platform == PlatformOpenAI {
		query = `SELECT id,vendor_id,deleted_at FROM accounts WHERE platform=$1 AND parent_account_id IS NULL AND type='oauth' AND credentials->>'chatgpt_account_id'=$2`
		args = args[:2]
	}
	rows, e := tx.QueryContext(ctx, query, args...)
	if e != nil {
		return "", 0, e
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var owned sql.NullInt64
		var deleted sql.NullTime
		if e = rows.Scan(&id, &owned, &deleted); e != nil {
			return "", 0, e
		}
		if !owned.Valid || owned.Int64 != v.ID {
			return "", 0, ErrVendorIdentityBound
		}
		if !deleted.Valid && id != in.TargetAccountID {
			return "", 0, ErrVendorIdentityBound
		}
	}
	if e = rows.Err(); e != nil {
		return "", 0, e
	}
	// Existing platform/other-owner records with no verified claim cannot be
	// proven distinct from this identity. An administrator must reconcile that
	// baseline before accepting a new supplier identity; never guess from JWTs.
	if e = s.vendorLegacyIdentityBaseline(ctx, tx, in.Platform, v.ID); e != nil {
		return "", 0, e
	}
	return hash, in.TargetAccountID, nil
}
func (s *VendorService) persistVendorCredential(ctx context.Context, tx *sql.Tx, v *Vendor, in VendorAccountInput, verified *vendorVerifiedCredential, paused bool) (int64, error) {
	if err := validateVendorAuthorization(v, in); err != nil {
		return 0, err
	}
	if err := s.ValidateVendorGroups(ctx, v, in.Platform, in.GroupIDs); err != nil {
		return 0, err
	}
	if in.TargetAccountID > 0 {
		if err := s.vendorLockAccount(ctx, tx, v, in.TargetAccountID); err != nil {
			return 0, err
		}
		var platform string
		if err := tx.QueryRowContext(ctx, "SELECT platform FROM accounts WHERE id=$1", in.TargetAccountID).Scan(&platform); err != nil {
			return 0, err
		}
		if platform != in.Platform {
			return 0, ErrVendorAccountConflict
		}
	}
	hash, id, err := s.reserveVendorIdentity(ctx, tx, v, in, verified)
	if err != nil {
		return 0, err
	}
	if in.Name == "" {
		if id > 0 {
			// An omitted label must not rename an existing authorized account.
			if err = tx.QueryRowContext(ctx, "SELECT name FROM accounts WHERE id=$1 AND vendor_id=$2", id, v.ID).Scan(&in.Name); err != nil {
				return 0, err
			}
		} else {
			in.Name = strings.TrimSpace(verified.Email)
			if in.Name == "" {
				in.Name = in.Platform + "-" + verified.AccountID
			}
			// Only the display label is shortened; trusted identity/email stay intact.
			in.Name = strings.ToValidUTF8(in.Name[:min(len(in.Name), 100)], "")
		}
	}
	if in.TargetAccountID > 0 {
		var sameIdentity bool
		e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 AND canonical_account_id=$3 AND vendor_id=$4)", in.Platform, hash, in.TargetAccountID, v.ID).Scan(&sameIdentity)
		if e != nil {
			return 0, e
		}
		if !sameIdentity {
			return 0, ErrVendorIdentityBound
		}
	}

	verified.Credentials["_token_version"] = time.Now().UnixMilli()
	credentials, err := json.Marshal(verified.Credentials)
	if err != nil {
		return 0, err
	}
	extra, err := json.Marshal(map[string]any{"vendor_auth_method": verified.Method, "vendor_verified_email": verified.Email, "vendor_identity_hash": hash})
	if err != nil {
		return 0, err
	}
	typ := AccountTypeOAuth
	if in.Method == "setup-token" || in.Method == "setup-token-manual" {
		typ = AccountTypeSetupToken
	}
	if id == 0 {
		if err = ValidateVendorResourceCapacity(ctx, tx, v.ID, "accounts"); err != nil {
			return 0, err
		}
		native, err := buildAccountForCreate(&CreateAccountInput{Name: in.Name, Platform: in.Platform, Type: typ, Credentials: verified.Credentials, ProxyID: in.ProxyID, Concurrency: DefaultAccountConcurrency, Priority: DefaultAccountPriority}, map[string]any{})
		if err != nil {
			return 0, err
		}
		// Reuse native default/fingerprint normalization, then add verified metadata.
		if native.Extra == nil {
			native.Extra = map[string]any{}
		}
		native.Extra["vendor_auth_method"] = verified.Method
		native.Extra["vendor_verified_email"] = verified.Email
		native.Extra["vendor_identity_hash"] = hash
		if in.AuthorizationMode == VendorOpenAIModeBPS {
			native.Extra["openai_excel_bps"] = true
		}
		extra, err = json.Marshal(native.Extra)
		if err != nil {
			return 0, err
		}
		var pausedAt *time.Time
		if paused {
			now := time.Now()
			pausedAt = &now
		}
		creator, e := ResolveAccountCreator(ctx, tx)
		if e != nil {
			return 0, e
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO accounts(name,platform,type,credentials,extra,proxy_id,concurrency,priority,rate_multiplier,status,schedulable,auto_pause_on_expired,vendor_id,vendor_notes,vendor_paused_at,created_by,created_by_name,created_by_role) VALUES($1,$2,$3,$4::jsonb,$5::jsonb,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18) RETURNING id`, native.Name, native.Platform, native.Type, string(credentials), string(extra), native.ProxyID, native.Concurrency, native.Priority, native.BillingRateMultiplier(), native.Status, native.Schedulable, native.AutoPauseOnExpired, v.ID, in.VendorNotes, pausedAt, creator.ID, creator.Name, creator.Role).Scan(&id)
	} else if in.Platform == PlatformOpenAI {
		err = s.persistVendorOpenAIAuthorization(ctx, tx, v, id, in, verified)
	} else {
		// Only credentials and explicitly editable data change. No error/status,
		// cooldown, pause, history, routing or billing fields are reset by login.
		_, err = tx.ExecContext(ctx, `UPDATE accounts SET credentials=$1::jsonb,extra=extra||$2::jsonb,name=$3,vendor_notes=$4,proxy_id=$5,proxy_fallback_origin_id=NULL,type=$8,updated_at=NOW() WHERE id=$6 AND vendor_id=$7 AND deleted_at IS NULL`, string(credentials), string(extra), in.Name, in.VendorNotes, in.ProxyID, id, v.ID, typ)
	}
	if err != nil {
		return 0, err
	}
	if err = s.vendorBindGroups(ctx, tx, v, id, in.Platform, in.GroupIDs); err != nil {
		return 0, err
	}
	var previousCanonical int64
	e := tx.QueryRowContext(ctx, "SELECT canonical_account_id FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 AND vendor_id=$3 AND canonical_account_id<>$4", in.Platform, hash, v.ID, id).Scan(&previousCanonical)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return 0, e
	}
	if e == nil {
		if e = vendorPreserveIdentityTombstone(ctx, tx, s.cfg.JWT.Secret, previousCanonical, &v.ID, in.Platform, hash); e != nil {
			return 0, e
		}
	}
	claim, err := tx.ExecContext(ctx, `INSERT INTO account_supply_identity_claims(platform,identity_hash,canonical_account_id,vendor_id) VALUES($1,$2,$3,$4) ON CONFLICT(platform,identity_hash) DO UPDATE SET canonical_account_id=EXCLUDED.canonical_account_id WHERE account_supply_identity_claims.vendor_id=EXCLUDED.vendor_id`, in.Platform, hash, id, v.ID)
	if err != nil {
		return 0, err
	}
	if affected, e := claim.RowsAffected(); e != nil || affected != 1 {
		return 0, ErrVendorIdentityBound
	}
	if err = vendorAccountOutbox(ctx, tx, id, in.GroupIDs); err != nil {
		return 0, err
	}
	return id, nil
}
func stringsContain(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func infraVendorAccountLimit() error { return vendorAccountInputError("Account limit reached") }

// Reject duplicate attachment before exchanging/rotating a provider token. The
// persistence check remains authoritative after the network round trip.
func validateVendorAuthorizationAttachment(ctx context.Context, tx *sql.Tx, v *Vendor, in VendorAccountInput) error {
	if in.Platform != PlatformOpenAI || in.AuthorizationAction != "attach" {
		return nil
	}
	var present bool
	err := tx.QueryRowContext(ctx, `SELECT
 (COALESCE(credentials->>'access_token','')<>'' AND
  CASE WHEN credentials->>'client_id'=$4 THEN 'bps' ELSE 'native' END=$3)
 OR ($3='bps' AND EXISTS(SELECT 1 FROM openai_excel_grants g WHERE g.account_id=a.id AND g.ciphertext<>''))
 FROM accounts a WHERE id=$1 AND vendor_id=$2 AND deleted_at IS NULL`, in.TargetAccountID, v.ID, in.AuthorizationMode, openai.ExcelClientID).Scan(&present)
	if err != nil {
		return err
	}
	if present {
		return ErrVendorAccountConflict
	}
	return nil
}

func (s *VendorService) CreateVendorAccount(ctx context.Context, in VendorAccountInput) (*VendorAccountDTO, error) {
	if err := normalizeVendorAccountInput(&in); err != nil {
		return nil, err
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateVendorAuthorization(v, in); err != nil {
		return nil, err
	}
	var target *Account
	if in.TargetAccountID > 0 {
		target, err = s.vendorEditableAccount(ctx, v, in.TargetAccountID)
		if err != nil {
			return nil, err
		}
		if target.Platform != in.Platform {
			return nil, ErrVendorAccountConflict
		}
		release, e := s.vendorTokenLock(ctx, target)
		if e != nil {
			return nil, e
		}
		defer release()
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, in.TargetAccountID, "authorize"); err != nil {
		return nil, err
	}
	var id int64
	var verified *vendorVerifiedCredential
	err = s.vendorAccountNetwork(ctx, v, in.ProxyID, in.Direct, target, func(tx *sql.Tx) error {
		if e := validateVendorAuthorizationAttachment(ctx, tx, v, in); e != nil {
			return e
		}
		if e := s.ValidateVendorGroups(ctx, v, in.Platform, in.GroupIDs); e != nil {
			return e
		}
		if in.Method == "refresh_token" {
			var conflict bool
			if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM accounts WHERE credentials->>'refresh_token'=$1 AND parent_account_id IS NULL AND id<>$2)", strings.TrimSpace(in.Token), in.TargetAccountID).Scan(&conflict); e != nil {
				return e
			}
			if conflict {
				return ErrVendorIdentityBound
			}
		}
		return nil
	}, func(ctx context.Context, proxy string) error {
		var e error
		verified, e = s.vendorVerifyToken(ctx, in, proxy)
		return e
	}, func(tx *sql.Tx) error {
		var e error
		id, e = s.persistVendorCredential(ctx, tx, v, in, verified, false)
		return e
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	key := ClaudeTokenCacheKey(&Account{ID: id})
	if in.Platform == PlatformOpenAI {
		key = OpenAITokenCacheKey(&Account{ID: id})
	}
	_ = s.rdb.Del(ctx, "oauth:token:"+key).Err()
	s.VendorAudit(ctx, v.ID, "account.create", id, "success")
	return s.GetVendorAccount(ctx, id)
}
