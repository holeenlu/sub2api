package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/oauth"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// NativeSupplyIdentityGuard verifies before a native transaction and reserves
// ownership after its account write, inside that same transaction. Proofs are
// opaque; file/JWT identity fields can never construct one.
type NativeSupplyIdentityGuard interface {
	Verify(context.Context, *Account) (*VerifiedSupplyIdentity, error)
	Claim(context.Context, VendorProxySQL, *Account, *VerifiedSupplyIdentity) error
}
type VerifiedSupplyIdentity struct {
	platform, hash, credentialsDigest, email string
	accountID                                int64
	vendorID                                 *int64
	baseline                                 bool
	deferred                                 bool
	identityAccount, identityOrganization    string
}
type nativeSupplyIdentityGuard struct{ service *VendorService }

func (s *VendorService) NativeSupplyIdentityGuard() NativeSupplyIdentityGuard {
	return &nativeSupplyIdentityGuard{s}
}

func vendorSupplyCredentialDigest(credentials map[string]any) (string, error) {
	raw, err := json.Marshal(credentials)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}
func (g *nativeSupplyIdentityGuard) Verify(ctx context.Context, a *Account) (*VerifiedSupplyIdentity, error) {
	return g.verify(ctx, a, false)
}
func (g *nativeSupplyIdentityGuard) verify(ctx context.Context, a *Account, force bool) (*VerifiedSupplyIdentity, error) {
	return g.verifyWithOpenAIProfile(ctx, a, force, g.service.vendorVerifyOpenAIProfile)
}

// The verifier argument keeps local signing-key fixtures instance-scoped; every
// production caller uses the fixed provider profile above.
func (g *nativeSupplyIdentityGuard) verifyWithOpenAIProfile(ctx context.Context, a *Account, force bool, verify func(context.Context, *openai.TokenResponse, string, string, string) (*vendorVerifiedCredential, error)) (*VerifiedSupplyIdentity, error) {
	if a == nil {
		return nil, ErrVendorIdentityUnverified
	}
	if a.ParentAccountID != nil || (a.Platform != PlatformAnthropic && a.Platform != PlatformOpenAI) || (a.Type != AccountTypeOAuth && a.Type != AccountTypeSetupToken) {
		return nil, nil
	}
	if !force && g.service.db != nil {
		enforced, err := nativeSupplyIdentityEnforced(ctx, g.service.db)
		if err != nil {
			return nil, err
		}
		if !enforced {
			digest, err := vendorSupplyCredentialDigest(a.Credentials)
			if err != nil {
				return nil, err
			}
			return &VerifiedSupplyIdentity{platform: a.Platform, credentialsDigest: digest, accountID: a.ID, vendorID: a.VendorID, deferred: true}, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	proxy := ""
	if a.ProxyID != nil {
		p := a.Proxy
		if p == nil {
			if g.service.proxies == nil {
				return nil, vendorAccountUnavailable()
			}
			var err error
			p, err = g.service.proxies.GetByID(ctx, *a.ProxyID)
			if err != nil {
				return nil, ErrVendorIdentityUnverified
			}
		}
		if p == nil || p.ID != *a.ProxyID || !SameProxyOwner(p.VendorID, a.VendorID) {
			return nil, ErrVendorIdentityUnverified
		}
		proxy = p.URL()
	}
	var verified *vendorVerifiedCredential
	var err error
	if a.Platform == PlatformOpenAI {
		mode := vendorAccountPrimaryMode(a)
		response := &openai.TokenResponse{AccessToken: a.GetCredential("access_token"), IDToken: a.GetCredential("id_token")}
		if mode == VendorOpenAIModeBPS {
			if force {
				// Baseline reconciliation proves the current AT, not an ID Token
				// retained from an earlier refresh. Legacy resolution also supplies
				// only a fresh AT. Neither operation imports an ID Token.
				response.IDToken = ""
			} else if response.IDToken != "" && a.ID > 0 && g.service.db != nil {
				var storedID string
				err = g.service.db.QueryRowContext(ctx, "SELECT COALESCE(credentials->>'id_token','') FROM accounts WHERE id=$1 AND platform=$2 AND type=$3 AND vendor_id IS NOT DISTINCT FROM $4 AND parent_account_id IS NULL AND deleted_at IS NULL AND credentials->>'client_id'=$5", a.ID, a.Platform, a.Type, a.VendorID, openai.ExcelClientID).Scan(&storedID)
				if err != nil && !errors.Is(err, sql.ErrNoRows) {
					return nil, err
				}
				// A merged credential write can retain the prior ID Token with
				// an old expiry or at_hash. Only a same-owner stored match is
				// historical; a new or changed token must still pass verification.
				if err == nil && storedID == response.IDToken {
					response.IDToken = ""
				}
			}
		}
		verified, err = verify(ctx, response, "oauth", proxy, mode)
	} else {
		verified, err = g.service.vendorVerifyClaude(ctx, &TokenInfo{AccessToken: a.GetCredential("access_token")}, "oauth", proxy)
	}
	if err != nil {
		return nil, err
	}
	hash, err := g.service.vendorIdentityHash(a.Platform, verified.Identity)
	if err != nil {
		return nil, err
	}
	digest, err := vendorSupplyCredentialDigest(a.Credentials)
	if err != nil {
		return nil, err
	}
	proof := &VerifiedSupplyIdentity{platform: a.Platform, hash: hash, credentialsDigest: digest, email: verified.Email, accountID: a.ID, identityAccount: verified.AccountID, identityOrganization: verified.OrganizationID}
	if a.VendorID != nil {
		id := *a.VendorID
		proof.vendorID = &id
	}
	return proof, nil
}
func (g *nativeSupplyIdentityGuard) Claim(ctx context.Context, tx VendorProxySQL, a *Account, proof *VerifiedSupplyIdentity) error {
	relevant := a != nil && a.ParentAccountID == nil && (a.Platform == PlatformAnthropic || a.Platform == PlatformOpenAI) && (a.Type == AccountTypeOAuth || a.Type == AccountTypeSetupToken)
	if !relevant {
		if proof != nil {
			return ErrVendorIdentityUnverified
		}
		return nil
	}
	if proof == nil || a.ID <= 0 || proof.platform != a.Platform || !SameProxyOwner(proof.vendorID, a.VendorID) || (proof.accountID > 0 && proof.accountID != a.ID) {
		return ErrVendorIdentityUnverified
	}
	digest, err := vendorSupplyCredentialDigest(a.Credentials)
	if err != nil || digest != proof.credentialsDigest {
		return ErrVendorAccountConflict
	}
	// Serialize feature-off native writes with the first supplier reservation.
	// An old native installation with no supply inventory keeps native behavior.
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock_shared(hashtextextended('supply-identity-baseline',0))"); err != nil {
		return err
	}
	if proof.deferred {
		enforced, e := nativeSupplyIdentityEnforced(ctx, tx)
		if e != nil {
			return e
		}
		if enforced {
			return ErrVendorAccountConflict
		}
		return nil
	}
	// Confirm the proof describes the persisted row, not a stale pre-write copy.
	rows, err := tx.QueryContext(ctx, "SELECT platform,vendor_id,credentials FROM accounts WHERE id=$1 FOR UPDATE", a.ID)
	if err != nil {
		return err
	}
	var platform string
	var owner sql.NullInt64
	var raw []byte
	if !rows.Next() {
		rows.Close()
		return ErrVendorAccountNotFound
	}
	err = rows.Scan(&platform, &owner, &raw)
	rows.Close()
	if err != nil {
		return err
	}
	var credentials map[string]any
	if json.Unmarshal(raw, &credentials) != nil {
		return ErrVendorAccountConflict
	}
	persistedDigest, e := vendorSupplyCredentialDigest(credentials)
	if e != nil || persistedDigest != proof.credentialsDigest || platform != a.Platform || owner.Valid != (a.VendorID != nil) || (owner.Valid && owner.Int64 != *a.VendorID) {
		return ErrVendorAccountConflict
	}
	if _, err = tx.ExecContext(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "supply:"+a.Platform+":"+proof.hash); err != nil {
		return err
	}
	if g.service.cfg == nil {
		return vendorAccountUnavailable()
	}
	if err = ValidateSupplyIdentityKey(ctx, tx, g.service.cfg.JWT.Secret); err != nil {
		return err
	}
	// An existing native root cannot silently change identity during a refresh.
	rows, err = tx.QueryContext(ctx, "SELECT identity_hash FROM account_supply_identity_claims WHERE canonical_account_id=$1 AND platform=$2", a.ID, a.Platform)
	if err != nil {
		return err
	}
	for rows.Next() {
		var hash string
		if err = rows.Scan(&hash); err != nil {
			break
		}
		if hash != proof.hash {
			err = ErrVendorIdentityBound
			break
		}
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	rows, err = tx.QueryContext(ctx, "SELECT c.canonical_account_id,c.vendor_id,a.deleted_at FROM account_supply_identity_claims c JOIN accounts a ON a.id=c.canonical_account_id WHERE c.platform=$1 AND c.identity_hash=$2 FOR UPDATE OF c", a.Platform, proof.hash)
	if err != nil {
		return err
	}
	var canonical int64
	var claimOwner sql.NullInt64
	var deleted sql.NullTime
	exists := rows.Next()
	if exists {
		err = rows.Scan(&canonical, &claimOwner, &deleted)
	} else {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return err
	}
	if exists {
		if claimOwner.Valid != (a.VendorID != nil) || (claimOwner.Valid && claimOwner.Int64 != *a.VendorID) {
			return ErrVendorIdentityBound
		}
		if canonical != a.ID && !deleted.Valid && !proof.baseline {
			return ErrVendorIdentityBound
		}
		if canonical != a.ID && deleted.Valid && !proof.baseline {
			if err = vendorPreserveIdentityTombstone(ctx, tx, g.service.cfg.JWT.Secret, canonical, a.VendorID, a.Platform, proof.hash); err != nil {
				return err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE account_supply_identity_claims SET canonical_account_id=$1 WHERE platform=$2 AND identity_hash=$3", a.ID, a.Platform, proof.hash); err != nil {
				return err
			}
		}
	} else {
		if _, err = tx.ExecContext(ctx, "INSERT INTO account_supply_identity_claims(platform,identity_hash,canonical_account_id,vendor_id) VALUES($1,$2,$3,$4)", a.Platform, proof.hash, a.ID, a.VendorID); err != nil {
			return err
		}
	}
	metadata, _ := json.Marshal(map[string]any{"vendor_identity_hash": proof.hash, "vendor_verified_email": proof.email, "vendor_identity_attestation": vendorSupplyIdentityAttestation(g.service.cfg.JWT.Secret, a.ID, a.VendorID, a.Platform, proof.hash)})
	_, err = tx.ExecContext(ctx, "UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||$1::jsonb WHERE id=$2", string(metadata), a.ID)
	return err
}

// Signed per-record attestation lets legacy same-owner duplicates reference one
// canonical claim without trusting editable extra/file identity fields.
func vendorSupplyIdentityAttestation(key string, id int64, owner *int64, platform, hash string) string {
	ownerText := "platform"
	if owner != nil {
		ownerText = strconv.FormatInt(*owner, 10)
	}
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("sub2api:supply-identity-attestation:v1\x00" + strconv.FormatInt(id, 10) + "\x00" + ownerText + "\x00" + platform + "\x00" + hash))
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *VendorService) vendorLegacyIdentityBaseline(ctx context.Context, tx VendorProxySQL, platform string, vendorID int64) error {
	rows, err := tx.QueryContext(ctx, "SELECT a.id,a.vendor_id,COALESCE(a.extra->>'vendor_identity_hash',''),COALESCE(a.extra->>'vendor_identity_attestation',''),EXISTS(SELECT 1 FROM account_supply_identity_claims c WHERE c.platform=a.platform AND c.identity_hash=a.extra->>'vendor_identity_hash' AND c.vendor_id IS NOT DISTINCT FROM a.vendor_id) FROM accounts a WHERE a.platform=$1 AND a.parent_account_id IS NULL AND a.type IN ('oauth','setup-token') AND a.vendor_id IS DISTINCT FROM $2 AND NOT EXISTS(SELECT 1 FROM account_supply_identity_claims c WHERE c.platform=a.platform AND c.canonical_account_id=a.id)", platform, vendorID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var owner *int64
		var hash, attestation string
		var claimed bool
		if err = rows.Scan(&id, &owner, &hash, &attestation, &claimed); err != nil {
			return err
		}
		if s.cfg == nil || !claimed || hash == "" || subtle.ConstantTimeCompare([]byte(attestation), []byte(vendorSupplyIdentityAttestation(s.cfg.JWT.Secret, id, owner, platform, hash))) != 1 {
			return ErrVendorIdentityBound
		}
	}
	return rows.Err()
}

func nativeSupplyIdentityEnforced(ctx context.Context, q VendorProxySQL) (bool, error) {
	rows, err := q.QueryContext(ctx, "SELECT EXISTS(SELECT 1 FROM settings WHERE key='vendor_console_enabled' AND value='true') OR EXISTS(SELECT 1 FROM account_supply_identity_claims) OR EXISTS(SELECT 1 FROM accounts WHERE vendor_id IS NOT NULL)")
	if err != nil {
		return false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return false, vendorAccountUnavailable()
	}
	var enforced bool
	err = rows.Scan(&enforced)
	return enforced, err
}

type SupplyIdentityReconcileRow struct {
	AccountID int64  `json:"account_id"`
	Status    string `json:"status"`
	Message   string `json:"message,omitempty"`
}
type SupplyIdentityReconcileResult struct {
	BaselineReady bool                         `json:"baseline_ready"`
	Rows          []SupplyIdentityReconcileRow `json:"rows"`
	NextAfterID   int64                        `json:"next_after_id"`
	Done          bool                         `json:"done"`
}

// ReconcileSupplyIdentities is a bounded, explicit superadmin operation. It
// authenticates legacy platform roots without rotating tokens, then snapshots
// and claims them atomically. Unverifiable tombstones stay visible as failures;
// their identities are never guessed from stored or unsigned JWT fields.
func (s *VendorService) ReconcileSupplyIdentities(ctx context.Context, afterID int64, limit int) (*SupplyIdentityReconcileResult, error) {
	if err := s.RequireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if afterID < 0 || limit < 1 || limit > 25 {
		return nil, vendorAccountInputError("Use a nonnegative cursor and limit between 1 and 25")
	}
	if s.rdb == nil {
		return nil, vendorAccountUnavailable()
	}
	nonce, err := oauth.GenerateSessionID()
	if err != nil {
		return nil, err
	}
	locked, err := s.rdb.SetNX(ctx, "vendor:supply-identity:reconcile", nonce, 15*time.Minute).Result()
	if err != nil {
		return nil, vendorAccountUnavailable()
	}
	if !locked {
		return nil, ErrVendorAccountConflict
	}
	defer func() {
		cleanup, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.rdb.Eval(cleanup, "if redis.call('GET',KEYS[1])==ARGV[1] then return redis.call('DEL',KEYS[1]) else return 0 end", []string{"vendor:supply-identity:reconcile"}, nonce).Err()
	}()
	ctx, cancel := context.WithTimeout(ctx, 12*time.Minute)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, "SELECT a.id,a.platform,a.type,a.credentials,a.proxy_id FROM accounts a WHERE a.id>$1 AND a.vendor_id IS NULL AND a.parent_account_id IS NULL AND a.platform IN('anthropic','openai') AND a.type IN('oauth','setup-token') AND NOT EXISTS(SELECT 1 FROM account_supply_identity_claims c WHERE c.platform=a.platform AND c.vendor_id IS NULL AND c.canonical_account_id=a.id) ORDER BY a.id LIMIT $2", afterID, limit)
	if err != nil {
		return nil, err
	}
	accounts := []*Account{}
	for rows.Next() {
		a := &Account{}
		var raw []byte
		if err = rows.Scan(&a.ID, &a.Platform, &a.Type, &raw, &a.ProxyID); err != nil {
			break
		}
		if err = json.Unmarshal(raw, &a.Credentials); err != nil {
			break
		}
		accounts = append(accounts, a)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := &SupplyIdentityReconcileResult{Rows: []SupplyIdentityReconcileRow{}, NextAfterID: afterID, Done: len(accounts) < limit}
	guard := &nativeSupplyIdentityGuard{s}
	for _, a := range accounts {
		if err = s.RequireSuperAdmin(ctx); err != nil {
			return nil, err
		}
		result.NextAfterID = a.ID
		row := SupplyIdentityReconcileRow{AccountID: a.ID, Status: "failed", Message: "Upstream identity unavailable; reauthorize this platform account and retry this row"}
		if a.ProxyID != nil {
			pRows, e := s.db.QueryContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id IS NULL AND p.deleted_at IS NULL", *a.ProxyID)
			if e != nil {
				return nil, e
			}
			if pRows.Next() {
				a.Proxy, e = scanVendorProxy(pRows)
			} else {
				e = ErrProxyNotFound
			}
			pRows.Close()
			if e != nil {
				result.Rows = append(result.Rows, row)
				continue
			}
		}
		proof, e := guard.verify(ctx, a, true)
		if e == nil && proof != nil {
			proof.baseline = true
			tx, e := s.db.BeginTx(ctx, nil)
			if e != nil {
				return nil, e
			}
			e = guard.Claim(ctx, tx, a, proof)
			if e == nil {
				e = s.RequireSuperAdmin(ctx)
			}
			if e == nil {
				e = tx.Commit()
			} else {
				_ = tx.Rollback()
			}
			if e == nil {
				row.Status = "claimed"
				row.Message = ""
			} else if errors.Is(e, ErrVendorIdentityBound) {
				row.Message = "Conflicting existing ownership; review without transferring ownership"
			}
		}
		result.Rows = append(result.Rows, row)
	}
	result.BaselineReady, err = s.supplyIdentityBaselineReady(ctx)
	if err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, 0, "supply.identity.reconcile", 0, "completed")
	return result, nil
}

// ResolveSupplyIdentity verifies fresh authorization for an identifiable legacy
// platform row, including a tombstone, without restoring or storing credentials.
// The historical stable ID must agree with freshly authenticated upstream data.
func (s *VendorService) ResolveSupplyIdentity(ctx context.Context, id int64, accessToken string, proxyID *int64, direct bool) (*SupplyIdentityReconcileResult, error) {
	if err := s.RequireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if id <= 0 || strings.TrimSpace(accessToken) == "" || len(accessToken) > 32768 || (proxyID != nil && (direct || *proxyID <= 0)) {
		return nil, vendorAccountInputError("Invalid identity resolution input")
	}
	a := &Account{ID: id}
	var raw, extra []byte
	err := s.db.QueryRowContext(ctx, "SELECT platform,type,credentials,extra,proxy_id FROM accounts WHERE id=$1 AND vendor_id IS NULL AND parent_account_id IS NULL", id).Scan(&a.Platform, &a.Type, &raw, &extra, &a.ProxyID)
	if err != nil {
		return nil, ErrVendorAccountNotFound
	}
	if json.Unmarshal(raw, &a.Credentials) != nil || json.Unmarshal(extra, &a.Extra) != nil {
		return nil, ErrVendorAccountConflict
	}
	original := a.Credentials
	originalDigest, err := vendorSupplyCredentialDigest(original)
	if err != nil {
		return nil, err
	}
	legacyAccount, legacyOrg := a.GetCredential("chatgpt_account_id"), ""
	if a.Platform == PlatformAnthropic {
		legacyAccount, legacyOrg = a.GetCredential("account_uuid"), a.GetCredential("org_uuid")
		if legacyAccount == "" {
			legacyAccount = a.GetExtraString("account_uuid")
		}
		if legacyOrg == "" {
			legacyOrg = a.GetExtraString("org_uuid")
		}
	}
	if legacyAccount == "" || (a.Platform == PlatformAnthropic && legacyOrg == "") {
		return nil, vendorAccountInputError("Historical stable identity is missing; automatic reconciliation cannot certify this row")
	}
	if direct {
		a.ProxyID = nil
	} else if proxyID != nil {
		a.ProxyID = proxyID
	}
	if a.ProxyID != nil {
		rows, e := s.db.QueryContext(ctx, "SELECT "+vendorProxyColumns+" FROM proxies p WHERE p.id=$1 AND p.vendor_id IS NULL AND p.deleted_at IS NULL", *a.ProxyID)
		if e != nil {
			return nil, e
		}
		if rows.Next() {
			a.Proxy, e = scanVendorProxy(rows)
		} else {
			e = ErrProxyNotFound
		}
		rows.Close()
		if e != nil {
			return nil, e
		}
	}
	// Retain only the server-stored profile selector, never historical proof.
	a.Credentials = map[string]any{"access_token": strings.TrimSpace(accessToken), "client_id": a.GetCredential("client_id")}
	guard := &nativeSupplyIdentityGuard{s}
	proof, err := guard.verify(ctx, a, true)
	if err != nil {
		return nil, err
	}
	if proof == nil || proof.identityAccount != legacyAccount || proof.identityOrganization != legacyOrg {
		return nil, ErrVendorIdentityUnverified
	}
	proof.credentialsDigest = originalDigest
	proof.baseline = true
	a.Credentials = original
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if err = guard.Claim(ctx, tx, a, proof); err != nil {
		return nil, err
	}
	if err = s.RequireSuperAdmin(ctx); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	s.VendorAudit(ctx, 0, "supply.identity.resolve", id, "claimed")
	ready, err := s.supplyIdentityBaselineReady(ctx)
	if err != nil {
		return nil, err
	}
	return &SupplyIdentityReconcileResult{BaselineReady: ready, Rows: []SupplyIdentityReconcileRow{{AccountID: id, Status: "claimed"}}, NextAfterID: id, Done: true}, nil
}

func (s *VendorService) supplyIdentityBaselineReady(ctx context.Context) (bool, error) {
	for _, platform := range []string{PlatformAnthropic, PlatformOpenAI} {
		if err := s.vendorLegacyIdentityBaseline(ctx, s.db, platform, 0); err != nil {
			if errors.Is(err, ErrVendorIdentityBound) {
				return false, nil
			}
			return false, err
		}
	}
	return true, nil
}

// Repointing a same-owner canonical claim must not leave the previous tombstone
// unidentifiable. Preserve only a signed, non-secret identity attestation derived
// from the existing trusted claim; never restore credentials or infer identity.
func vendorPreserveIdentityTombstone(ctx context.Context, tx VendorProxySQL, key string, id int64, owner *int64, platform, hash string) error {
	metadata, _ := json.Marshal(map[string]any{"vendor_identity_hash": hash, "vendor_identity_attestation": vendorSupplyIdentityAttestation(key, id, owner, platform, hash)})
	_, err := tx.ExecContext(ctx, "UPDATE accounts SET extra=COALESCE(extra,'{}'::jsonb)||$1::jsonb WHERE id=$2 AND vendor_id IS NOT DISTINCT FROM $3 AND deleted_at IS NOT NULL", string(metadata), id, owner)
	return err
}
