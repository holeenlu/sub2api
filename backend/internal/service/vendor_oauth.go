package service

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/authz"
	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type VendorOAuthStartResult struct {
	AuthURL   string    `json:"auth_url"`
	SessionID string    `json:"session_id"`
	ExpiresAt time.Time `json:"expires_at"`
}
type VendorOAuthCompleteInput struct {
	Code  string `json:"code"`
	State string `json:"state"`
}
type VendorOAuthProxyInput struct {
	ProxyID *int64 `json:"proxy_id"`
	Direct  bool   `json:"direct"`
}
type vendorOAuthSession struct {
	Authorization     *authz.Lease       `json:"authorization"`
	VendorID          int64              `json:"vendor_id"`
	PolicyVersion     int64              `json:"policy_version"`
	Input             VendorAccountInput `json:"input"`
	State             string             `json:"state"`
	CodeVerifier      string             `json:"code_verifier"`
	TargetFingerprint string             `json:"target_fingerprint,omitempty"`
	ProxyVersion      int64              `json:"proxy_version"`
	Status            string             `json:"status"`
	AccountID         int64              `json:"account_id,omitempty"`
	ExpiresAt         time.Time          `json:"expires_at"`
}

func vendorOAuthKey(id string) string { return "oauth:vendor:" + id }
func vendorOAuthOperation(id string) string {
	b := sha256.Sum256([]byte(id))
	return hex.EncodeToString(b[:])
}
func strconvVendorID(id int64) string { return strconv.FormatInt(id, 10) }

func (s *VendorService) StartVendorOAuth(ctx context.Context, in VendorAccountInput) (*VendorOAuthStartResult, error) {
	if err := normalizeVendorAccountInput(&in); err != nil {
		return nil, err
	}
	if in.Token != "" {
		return nil, vendorAccountInputError("Token material is not accepted by OAuth sessions")
	}
	if (in.Platform == PlatformAnthropic && in.Method != "oauth" && in.Method != "setup-token") || (in.Platform == PlatformOpenAI && in.Method != "oauth") {
		return nil, vendorAccountInputError("Unsupported authorization method")
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	if err = validateVendorAuthorization(v, in); err != nil {
		return nil, err
	}
	if s.rdb == nil {
		return nil, vendorAccountUnavailable()
	}
	targetFingerprint := ""
	if in.TargetAccountID > 0 {
		a, e := s.vendorEditableAccount(ctx, v, in.TargetAccountID)
		if e != nil {
			return nil, e
		}
		targetFingerprint = vendorAccountEditFingerprint(a)
		if a.Platform != in.Platform {
			return nil, ErrVendorAccountConflict
		}
	}
	if err = s.ValidateVendorGroups(ctx, v, in.Platform, in.GroupIDs); err != nil {
		return nil, err
	}
	lease := authz.CaptureLease(ctx)
	if lease == nil || lease.SessionID == "" {
		return nil, authz.ErrAuthorizationExpired
	}
	if err = authz.CheckLease(ctx, lease, "accounts.authorize"); err != nil {
		return nil, err
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, 0, "oauth_start"); err != nil {
		return nil, err
	}
	state, err := oauth.GenerateState()
	if err != nil {
		return nil, err
	}
	verifier, err := oauth.GenerateCodeVerifier()
	if err != nil {
		return nil, err
	}
	id, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	session := &vendorOAuthSession{Authorization: lease, VendorID: v.ID, PolicyVersion: v.PolicyVersion, Input: in, State: state, CodeVerifier: verifier, Status: "pending", TargetFingerprint: targetFingerprint, ExpiresAt: time.Now().Add(oauth.SessionTTL)}
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if session.Input.TargetAccountID > 0 {
			if e := s.vendorLockAccount(ctx, tx, v, session.Input.TargetAccountID); e != nil {
				return e
			}
			if e := validateVendorAuthorizationAttachment(ctx, tx, v, session.Input); e != nil {
				return e
			}
		}
		_, version, e := s.vendorAccountEgress(ctx, tx, v, in.ProxyID, in.Direct)
		if e != nil {
			return e
		}
		session.ProxyVersion = version
		if e = s.vendorRegisterOAuthProxy(ctx, v.ID, id, in.ProxyID, session.ExpiresAt); e != nil {
			return e
		}
		return oauth.PutSharedSession(ctx, s.rdb, vendorOAuthKey(id), session, oauth.SessionTTL)
	})
	if err != nil {
		return nil, err
	}
	authURL := oauth.BuildAuthorizationURL(state, oauth.GenerateCodeChallenge(verifier), oauth.ScopeOAuth)
	if in.Method == "setup-token" {
		authURL = oauth.BuildAuthorizationURL(state, oauth.GenerateCodeChallenge(verifier), oauth.ScopeInference)
	}
	if in.Platform == PlatformOpenAI {
		authURL = openai.BuildAuthorizationURLForPlatform(state, openai.GenerateCodeChallenge(verifier), openai.DefaultRedirectURI, openai.OAuthPlatformOpenAI)
		if in.AuthorizationMode == VendorOpenAIModeBPS {
			authURL = openai.BuildExcelAuthorizationURL(state, openai.GenerateCodeChallenge(verifier))
		}
	}
	s.VendorAudit(ctx, v.ID, "account.oauth.start", in.TargetAccountID, "success")
	return &VendorOAuthStartResult{authURL, id, session.ExpiresAt}, nil
}
func (s *VendorService) readVendorOAuth(ctx context.Context, platform, id string) (*Vendor, *vendorOAuthSession, string, error) {
	if len(id) < 16 || len(id) > 128 || strings.ContainsAny(id, ":/ \\ \t\r\n") {
		return nil, nil, "", ErrVendorAccountNotFound
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, nil, "", err
	}
	session, raw, err := oauth.ReadSharedSession[vendorOAuthSession](ctx, s.rdb, vendorOAuthKey(id))
	if err != nil {
		return nil, nil, "", ErrVendorAccountConflict
	}
	if session.VendorID != v.ID || session.Input.Platform != platform {
		return nil, nil, "", ErrVendorAccountNotFound
	}
	if err := validateVendorAuthorization(v, session.Input); err != nil {
		return nil, nil, "", err
	}
	if session.PolicyVersion != v.PolicyVersion || !session.ExpiresAt.After(time.Now()) {
		return nil, nil, "", ErrVendorAccountConflict
	}
	if err = authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); err != nil {
		return nil, nil, "", err
	}
	if session.Authorization == nil || session.Authorization.SessionID == "" {
		return nil, nil, "", authz.ErrAuthorizationExpired
	}
	return v, session, raw, nil
}
func parseVendorOAuthCode(platform, raw, explicitState, expected string, modes ...string) (string, error) {
	raw = strings.TrimSpace(raw)
	if len(raw) == 0 || len(raw) > 16384 || len(explicitState) > 512 {
		return "", vendorAccountInputError("Invalid authorization result")
	}
	code, state := raw, explicitState
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil || u.User != nil {
			return "", vendorAccountInputError("Invalid authorization callback")
		}
		expectedURI := openai.DefaultRedirectURI
		if platform == PlatformOpenAI && len(modes) > 0 && modes[0] == VendorOpenAIModeBPS {
			expectedURI = openai.ExcelRedirectURI
		}
		if platform == PlatformAnthropic {
			expectedURI = oauth.RedirectURI
		}
		base, _ := url.Parse(expectedURI)
		if u.Scheme != base.Scheme || u.Host != base.Host || u.Path != base.Path || u.Fragment != "" {
			return "", vendorAccountInputError("Unexpected authorization callback")
		}
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["code"]) != 1 || len(q["state"]) != 1 || q.Get("error") != "" {
			return "", vendorAccountInputError("Invalid authorization callback")
		}
		code = q.Get("code")
		if state != "" && state != q.Get("state") {
			return "", vendorAccountInputError("Conflicting authorization state")
		}
		state = q.Get("state")
	} else if platform == PlatformAnthropic && strings.Contains(raw, "#") {
		parts := strings.Split(raw, "#")
		if len(parts) != 2 {
			return "", vendorAccountInputError("Invalid authorization code")
		}
		code = parts[0]
		if state != "" && state != parts[1] {
			return "", vendorAccountInputError("Conflicting authorization state")
		}
		state = parts[1]
	}
	if code == "" || strings.ContainsAny(code, "\r\n \t") || state == "" || subtle.ConstantTimeCompare([]byte(state), []byte(expected)) != 1 {
		return "", vendorAccountInputError("Authorization state does not match this session")
	}
	return code, nil
}
func (s *VendorService) CompleteVendorOAuth(ctx context.Context, platform, id string, input VendorOAuthCompleteInput) (*VendorAccountDTO, error) {
	v, session, raw, err := s.readVendorOAuth(ctx, platform, id)
	if err != nil {
		return nil, err
	}
	code, err := parseVendorOAuthCode(platform, input.Code, input.State, session.State, session.Input.AuthorizationMode)
	if err != nil {
		return nil, err
	}
	if session.Status == "completed" {
		return s.GetVendorAccount(ctx, session.AccountID)
	}
	if session.Status != "pending" {
		// A committed DB operation remains recoverable if publishing to Redis failed.
		var accountID int64
		if s.db != nil && s.db.QueryRowContext(ctx, "SELECT id FROM accounts WHERE vendor_id=$1 AND deleted_at IS NULL AND extra->>'vendor_oauth_operation'=$2", v.ID, vendorOAuthOperation(id)).Scan(&accountID) == nil {
			return s.GetVendorAccount(ctx, accountID)
		}
		return nil, ErrVendorAccountConflict
	}
	var target *Account
	if session.Input.TargetAccountID > 0 {
		target, err = s.vendorEditableAccount(ctx, v, session.Input.TargetAccountID)
		if err != nil {
			return nil, err
		}
		if session.TargetFingerprint != vendorAccountEditFingerprint(target) {
			return nil, ErrVendorAccountConflict
		}
		release, e := s.vendorTokenLock(ctx, target)
		if e != nil {
			return nil, e
		}
		defer release()
	}
	var accountID int64
	var verified *vendorVerifiedCredential
	claimedRaw := ""
	err = s.vendorAccountNetwork(ctx, v, session.Input.ProxyID, session.Input.Direct, target, func(tx *sql.Tx) error {
		if e := validateVendorAuthorizationAttachment(ctx, tx, v, session.Input); e != nil {
			return e
		}
		if e := s.ValidateVendorGroups(ctx, v, platform, session.Input.GroupIDs); e != nil {
			return e
		}
		_, version, e := s.vendorAccountEgress(ctx, tx, v, session.Input.ProxyID, session.Input.Direct)
		if e != nil {
			return e
		}
		if version != session.ProxyVersion {
			return ErrVendorAccountConflict
		}
		if e = authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); e != nil {
			return e
		}
		session.Status = "claimed"
		claimedRaw, e = oauth.CompareSwapSession(ctx, s.rdb, vendorOAuthKey(id), raw, session)
		if e != nil {
			return ErrVendorAccountConflict
		}
		return nil
	}, func(ctx context.Context, proxy string) error {
		var e error
		if platform == PlatformAnthropic {
			if s.oauth == nil {
				return vendorAccountUnavailable()
			}
			t, err := s.oauth.exchangeCodeForToken(ctx, code+"#"+session.State, session.CodeVerifier, session.State, proxy, session.Input.Method == "setup-token")
			if err != nil {
				return ErrVendorIdentityUnverified
			}
			verified, e = s.vendorVerifyClaude(ctx, t, session.Input.Method, proxy)
		} else {
			if s.openaiOAuth == nil {
				return vendorAccountUnavailable()
			}
			clientID, redirect := vendorOpenAIProfile(session.Input.AuthorizationMode)
			t, err := s.openaiOAuth.oauthClient.ExchangeCode(ctx, code, session.CodeVerifier, redirect, proxy, clientID)
			if err != nil {
				return ErrVendorIdentityUnverified
			}
			verified, e = s.vendorVerifyOpenAIProfile(ctx, t, session.Input.Method, proxy, session.Input.AuthorizationMode)
		}
		return e
	}, func(tx *sql.Tx) error {
		if e := authz.CheckLease(ctx, session.Authorization, "accounts.authorize"); e != nil {
			return e
		}
		var e error
		accountID, e = s.persistVendorCredential(ctx, tx, v, session.Input, verified, false)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "UPDATE accounts SET extra=jsonb_set(extra,'{vendor_oauth_operation}',to_jsonb($1::text)) WHERE id=$2", vendorOAuthOperation(id), accountID)
		return e
	})
	if err != nil {
		if session.Status == "claimed" && session.Input.ProxyID != nil {
			cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			_ = s.ReleaseVendorProxyReference(cleanup, v.ID, *session.Input.ProxyID, "oauth", id)
			cancel()
		}
		s.VendorAudit(ctx, v.ID, "account.oauth.complete", session.Input.TargetAccountID, "failed")
		return nil, err
	}
	session.Status = "completed"
	session.AccountID = accountID
	// The committed account is the result even if Redis is temporarily unavailable.
	_, _ = oauth.CompareSwapSession(ctx, s.rdb, vendorOAuthKey(id), claimedRaw, session)
	s.InvalidateVendorAccounts(ctx, v.ID)
	if session.Input.ProxyID != nil {
		_ = s.ReleaseVendorProxyReference(ctx, v.ID, *session.Input.ProxyID, "oauth", id)
	}
	cacheKey := ClaudeTokenCacheKey(&Account{ID: accountID})
	if platform == PlatformOpenAI {
		cacheKey = OpenAITokenCacheKey(&Account{ID: accountID})
	}
	_ = s.rdb.Del(ctx, "oauth:token:"+cacheKey).Err()
	s.VendorAudit(ctx, v.ID, "account.oauth.complete", accountID, "success")
	return s.GetVendorAccount(ctx, accountID)
}
func (s *VendorService) RebindVendorOAuthProxy(ctx context.Context, platform, id string, in VendorOAuthProxyInput) error {
	v, session, raw, err := s.readVendorOAuth(ctx, platform, id)
	if err != nil {
		return err
	}
	if session.Status != "pending" {
		return ErrVendorAccountConflict
	}
	old := session.Input.ProxyID
	err = s.vendorAccountTx(ctx, v, func(tx *sql.Tx) error {
		if session.Input.TargetAccountID > 0 {
			if e := s.vendorLockAccount(ctx, tx, v, session.Input.TargetAccountID); e != nil {
				return e
			}
		}
		_, version, e := s.vendorAccountEgress(ctx, tx, v, in.ProxyID, in.Direct)
		if e != nil {
			return e
		}
		if e = s.vendorRegisterOAuthProxy(ctx, v.ID, id, in.ProxyID, session.ExpiresAt); e != nil {
			return e
		}
		session.Input.ProxyID = in.ProxyID
		session.Input.Direct = in.Direct
		session.ProxyVersion = version
		_, e = oauth.CompareSwapSession(ctx, s.rdb, vendorOAuthKey(id), raw, session)
		if e != nil {
			return ErrVendorAccountConflict
		}
		if old != nil && (in.ProxyID == nil || *old != *in.ProxyID) {
			// Release while the shared advisory lock still serializes rebindings.
			_ = s.ReleaseVendorProxyReference(ctx, v.ID, *old, "oauth", id)
		}
		return nil
	})
	return err
}
func (s *VendorService) vendorRegisterOAuthProxy(ctx context.Context, vendorID int64, id string, proxyID *int64, expires time.Time) error {
	if proxyID == nil {
		return nil
	}
	return s.RegisterVendorProxyReference(ctx, vendorID, *proxyID, "oauth", id, expires)
}
