package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

func (s *VendorService) vendorAccountRateLimit(ctx context.Context, vID, id int64, operation string) error {
	if s.rdb == nil {
		return vendorAccountUnavailable()
	}
	ok, err := s.rdb.SetNX(ctx, fmt.Sprintf("vendor:account:rate:%d:%d:%s", vID, id, operation), 1, 15*time.Second).Result()
	if err != nil {
		return vendorAccountUnavailable()
	}
	if !ok {
		return infraerrors.New(http.StatusTooManyRequests, "VENDOR_ACCOUNT_RATE_LIMIT", "Please wait before retrying this operation")
	}
	return nil
}
func (s *VendorService) RefreshVendorAccount(ctx context.Context, id int64) (*VendorAccountDTO, error) {
	return s.RefreshVendorAccountAuthorization(ctx, id, "")
}

func (s *VendorService) RefreshVendorAccountAuthorization(ctx context.Context, id int64, mode string) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if mode != "" {
		if !validVendorOpenAIMode(mode) || a.Platform != PlatformOpenAI {
			return nil, vendorAccountInputError("Invalid authorization mode")
		}
		if !vendorAllowsOpenAIMode(v, mode) {
			return nil, ErrVendorDenied
		}
		if mode == VendorOpenAIModeBPS && !a.IsExcelOAuth() {
			return s.refreshVendorExcelGrant(ctx, v, a)
		}
		if mode != vendorAccountPrimaryMode(a) {
			return nil, vendorAccountInputError("Authorize this mode first")
		}
	}
	if a.Platform == PlatformOpenAI && !vendorAllowsOpenAIMode(v, vendorAccountPrimaryMode(a)) {
		return nil, ErrVendorDenied
	}
	if a.GetCredential("refresh_token") == "" {
		return nil, vendorAccountInputError("This account requires reauthorization")
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, id, "refresh"); err != nil {
		return nil, err
	}
	release, err := s.vendorTokenLock(ctx, a)
	if err != nil {
		return nil, err
	}
	defer release()
	var verified *vendorVerifiedCredential
	err = s.vendorAccountNetwork(ctx, v, a.ProxyID, a.ProxyID == nil, a, nil, func(ctx context.Context, proxy string) error {
		var e error
		verified, e = s.vendorRefreshImportCredential(ctx, vendorAccountImportSecretRow{Input: VendorAccountInput{Platform: a.Platform, Method: a.GetExtraString("vendor_auth_method"), AuthorizationMode: vendorAccountPrimaryMode(a)}, Credential: *vendorCredentialSnapshot(a)}, proxy)
		return e
	}, func(tx *sql.Tx) error {
		hash, e := s.vendorIdentityHash(a.Platform, verified.Identity)
		if e != nil {
			return e
		}
		var match bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 AND canonical_account_id=$3 AND vendor_id=$4)", a.Platform, hash, id, v.ID).Scan(&match); e != nil {
			return e
		}
		if !match {
			return ErrVendorIdentityBound
		}
		verified.Credentials["_token_version"] = time.Now().UnixMilli()
		data, e := json.Marshal(verified.Credentials)
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE accounts SET credentials=credentials||$1::jsonb,updated_at=NOW() WHERE id=$2", string(data), id); e != nil {
			return e
		}
		return vendorAccountOutbox(ctx, tx, id, a.GroupIDs)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	key := ClaudeTokenCacheKey(a)
	if a.Platform == PlatformOpenAI {
		key = OpenAITokenCacheKey(a)
	}
	_ = s.rdb.Del(ctx, "oauth:token:"+key).Err()
	s.VendorAudit(ctx, v.ID, "account.refresh", id, "success")
	return s.GetVendorAccount(ctx, id)
}

func (s *VendorService) vendorFetchQuota(ctx context.Context, a *Account, proxy string) (*VendorAccountQuota, map[string]any, error) {
	now := time.Now()
	quota := &VendorAccountQuota{Source: "active", UpdatedAt: &now}
	updates := map[string]any{}
	if a.Platform == PlatformOpenAI {
		if s.openaiOAuth == nil || s.openaiOAuth.privacyClientFactory == nil {
			return nil, nil, vendorAccountUnavailable()
		}
		client, err := s.openaiOAuth.privacyClientFactory(proxy)
		if err != nil {
			return nil, nil, vendorAccountUnavailable()
		}
		var payload OpenAIQuotaUsage
		resp, err := client.R().SetContext(ctx).SetHeaders(buildCodexCommonHeaders(a.GetCredential("access_token"), a.GetCredential("chatgpt_account_id"), false)).SetSuccessResult(&payload).Get(chatGPTUsageURL)
		if err != nil || resp == nil || !resp.IsSuccessState() {
			return nil, nil, ErrVendorIdentityUnverified
		}
		if payload.AccountID != "" && payload.AccountID != a.GetCredential("chatgpt_account_id") {
			return nil, nil, ErrVendorIdentityBound
		}
		updates = buildOpenAIAutoResetUsageUpdates(&payload, now)
		if payload.Email != "" {
			updates["vendor_verified_email"] = payload.Email
		}
		quota.FiveHour = vendorQuotaWindow(buildCodexUsageProgressFromExtra(updates, "5h", now))
		quota.SevenDay = vendorQuotaWindow(buildCodexUsageProgressFromExtra(updates, "7d", now))
	} else {
		if a.Type == AccountTypeSetupToken {
			return nil, nil, vendorAccountInputError("This authorization only exposes passive quota snapshots")
		}
		if s.accountUsage == nil || s.accountUsage.usageFetcher == nil {
			return nil, nil, vendorAccountUnavailable()
		}
		native, err := s.accountUsage.usageFetcher.FetchUsageWithOptions(ctx, &ClaudeUsageFetchOptions{AccessToken: a.GetCredential("access_token"), ProxyURL: proxy, AccountID: idVendorAccount(a)})
		if err != nil {
			return nil, nil, ErrVendorIdentityUnverified
		}
		usage := s.accountUsage.buildUsageInfo(native, &now)
		quota.FiveHour = vendorQuotaWindow(usage.FiveHour)
		quota.SevenDay = vendorQuotaWindow(usage.SevenDay)
		quota.SevenDayFable = vendorQuotaWindow(usage.SevenDayFable)
		if usage.FiveHour != nil {
			updates["session_window_utilization"] = usage.FiveHour.Utilization / 100
		}
		for key, p := range map[string]*UsageProgress{"passive_usage_7d": usage.SevenDay, "passive_usage_7d_oi": usage.SevenDayFable} {
			if p != nil {
				updates[key+"_utilization"] = p.Utilization / 100
				if p.ResetsAt != nil {
					updates[key+"_reset"] = p.ResetsAt.Unix()
				}
			}
		}
		updates["passive_usage_sampled_at"] = now.UTC().Format(time.RFC3339)
	}
	return quota, updates, nil
}
func idVendorAccount(a *Account) int64 { return a.ID }
func (s *VendorService) RefreshVendorAccountUsage(ctx context.Context, id int64) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if a.Platform == PlatformOpenAI {
		if !vendorAllowsOpenAIMode(v, VendorOpenAIModeNative) {
			return nil, ErrVendorDenied
		}
		if a.IsExcelOAuth() || a.GetCredential("access_token") == "" {
			return nil, vendorAccountInputError("BPS quota is updated from request telemetry; no native quota authorization is available")
		}
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, id, "usage"); err != nil {
		return nil, err
	}
	var quota *VendorAccountQuota
	var updates map[string]any
	err = s.vendorAccountNetwork(ctx, v, a.ProxyID, a.ProxyID == nil, a, nil, func(ctx context.Context, proxy string) error {
		var e error
		quota, updates, e = s.vendorFetchQuota(ctx, a, proxy)
		return e
	}, func(tx *sql.Tx) error {
		return vendorStoreQuota(ctx, tx, a, quota, updates)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	s.VendorAudit(ctx, v.ID, "account.usage_refresh", id, "success")
	d, err := s.GetVendorAccount(ctx, id)
	if err == nil {
		d.Quota = *quota
	}
	return d, err
}
func vendorStoreQuota(ctx context.Context, tx *sql.Tx, a *Account, quota *VendorAccountQuota, updates map[string]any) error {
	raw, e := json.Marshal(updates)
	if e != nil {
		return e
	}
	if _, e = tx.ExecContext(ctx, "UPDATE accounts SET extra=extra||$1::jsonb,updated_at=NOW() WHERE id=$2", string(raw), a.ID); e != nil {
		return e
	}
	if a.Platform == PlatformAnthropic && quota.FiveHour != nil && quota.FiveHour.ResetsAt != nil {
		if _, e = tx.ExecContext(ctx, "UPDATE accounts SET session_window_end=$1 WHERE id=$2", quota.FiveHour.ResetsAt, a.ID); e != nil {
			return e
		}
	}
	return vendorAccountOutbox(ctx, tx, a.ID, a.GroupIDs)
}
func (s *VendorService) SyncVendorAccount(ctx context.Context, id int64) (*VendorAccountDTO, error) {
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if a.Platform == PlatformOpenAI && !vendorAllowsOpenAIMode(v, vendorAccountPrimaryMode(a)) {
		return nil, ErrVendorDenied
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, id, "sync"); err != nil {
		return nil, err
	}
	var verified *vendorVerifiedCredential
	var quota *VendorAccountQuota
	var updates map[string]any
	err = s.vendorAccountNetwork(ctx, v, a.ProxyID, a.ProxyID == nil, a, nil, func(ctx context.Context, proxy string) error {
		var e error
		if a.Platform == PlatformOpenAI {
			verified, e = s.vendorVerifyOpenAIProfile(ctx, &openai.TokenResponse{AccessToken: a.GetCredential("access_token")}, a.GetExtraString("vendor_auth_method"), proxy, vendorAccountPrimaryMode(a))
		} else {
			verified, e = s.vendorVerifyClaude(ctx, &TokenInfo{AccessToken: a.GetCredential("access_token")}, a.GetExtraString("vendor_auth_method"), proxy)
		}
		if e != nil {
			return e
		}
		if a.Type != AccountTypeSetupToken && !a.IsExcelOAuth() {
			quota, updates, e = s.vendorFetchQuota(ctx, a, proxy)
		}
		return e
	}, func(tx *sql.Tx) error {
		hash, e := s.vendorIdentityHash(a.Platform, verified.Identity)
		if e != nil {
			return e
		}
		var match bool
		if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM account_supply_identity_claims WHERE platform=$1 AND identity_hash=$2 AND canonical_account_id=$3 AND vendor_id=$4)", a.Platform, hash, id, v.ID).Scan(&match); e != nil {
			return e
		}
		if !match {
			return ErrVendorIdentityBound
		}
		metadata := map[string]any{}
		for _, key := range []string{"plan_type", "subscription_expires_at"} {
			if value, ok := verified.Credentials[key]; ok {
				metadata[key] = value
			}
		}
		raw, e := json.Marshal(metadata)
		if e != nil {
			return e
		}
		extra, e := json.Marshal(map[string]any{"vendor_verified_email": verified.Email})
		if e != nil {
			return e
		}
		if _, e = tx.ExecContext(ctx, "UPDATE accounts SET credentials=credentials||$1::jsonb,extra=extra||$2::jsonb,updated_at=NOW() WHERE id=$3", string(raw), string(extra), id); e != nil {
			return e
		}
		if quota != nil {
			return vendorStoreQuota(ctx, tx, a, quota, updates)
		}
		return vendorAccountOutbox(ctx, tx, id, a.GroupIDs)
	})
	if err != nil {
		return nil, err
	}
	s.InvalidateVendorAccounts(ctx, v.ID)
	s.VendorAudit(ctx, v.ID, "account.sync", id, "success")
	d, err := s.GetVendorAccount(ctx, id)
	if err == nil && quota != nil {
		d.Quota = *quota
	}
	return d, err
}

type VendorAccountProbeResult struct {
	AccountID   int64     `json:"account_id"`
	Status      string    `json:"status"`
	LatencyMS   int64     `json:"latency_ms"`
	CollectedAt time.Time `json:"collected_at"`
	CostNotice  string    `json:"cost_notice"`
}

// A probe is a native controlled model test with fixed/default target. Its
// captured body is discarded; only success, latency and time reach the supplier.
func (s *VendorService) ProbeVendorAccount(ctx context.Context, id int64, test *AccountTestService) (*VendorAccountProbeResult, error) {
	if test == nil {
		return nil, vendorAccountUnavailable()
	}
	v, err := s.CurrentVendorForWrite(ctx)
	if err != nil {
		return nil, err
	}
	a, err := s.vendorEditableAccount(ctx, v, id)
	if err != nil {
		return nil, err
	}
	if a.IsSyntheticUITest() {
		return nil, vendorAccountInputError("Synthetic accounts cannot be probed")
	}
	if err = s.vendorAccountRateLimit(ctx, v.ID, id, "probe"); err != nil {
		return nil, err
	}
	if s.rdb == nil {
		return nil, vendorAccountUnavailable()
	}
	budget, err := s.rdb.Eval(ctx, "local n=redis.call('INCR',KEYS[1]);if n==1 then redis.call('PEXPIRE',KEYS[1],60000) end;return n", []string{fmt.Sprintf("vendor:account:probe-budget:%d", v.ID)}).Int()
	if err != nil {
		return nil, vendorAccountUnavailable()
	}
	if budget > 20 {
		return nil, infraerrors.New(429, "VENDOR_ACCOUNT_RATE_LIMIT", "Supplier probe limit reached; retry after one minute")
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	var result *ScheduledTestResult
	clone := *a
	var probeGroup int64
	for _, group := range v.Groups {
		for _, id := range a.GroupIDs {
			if id == group.ID {
				probeGroup = id
				break
			}
		}
		if probeGroup > 0 {
			break
		}
	}
	if probeGroup == 0 {
		return nil, ErrVendorDenied
	}
	ctx = context.WithValue(ctx, vendorProbeGroupKey{}, probeGroup)
	err = s.vendorAccountNetwork(ctx, v, a.ProxyID, a.ProxyID == nil, a, func(tx *sql.Tx) error {
		if a.ProxyID != nil {
			var e error
			clone.Proxy, e = ValidateVendorProxyBinding(ctx, tx, v.ID, *a.ProxyID)
			return e
		}
		return nil
	}, func(ctx context.Context, _ string) error {
		runner := NewAccountTestService(
			&vendorProbeAccountRepository{AccountRepository: s.accounts, account: &clone},
			test.geminiTokenProvider, test.claudeTokenProvider, test.grokTokenProvider,
			test.antigravityGatewayService, qualifiedAccountUpstream(s.accounts, test.httpUpstream, &clone, &probeGroup), test.cfg, test.tlsFPProfileService,
		)
		runner.settingService, runner.pluginManager = test.settingService, test.pluginManager
		runner.openaiGatewayService = test.openaiGatewayService
		runner.agentIdentityWS, runner.grokWSDialer = test.agentIdentityWS, test.grokWSDialer
		var e error
		result, e = runner.RunTestBackground(ctx, id, "")
		return e
	}, nil)
	if err != nil {
		return nil, ErrVendorIdentityUnverified
	}
	if result == nil {
		return nil, vendorAccountUnavailable()
	}
	s.VendorAudit(ctx, v.ID, "account.probe", id, result.Status)
	return &VendorAccountProbeResult{id, result.Status, result.LatencyMs, result.FinishedAt, "Native test may consume upstream quota; no downstream customer is charged"}, nil
}

type vendorProbeAccountRepository struct {
	AccountRepository
	account *Account
}

func (r *vendorProbeAccountRepository) GetByID(_ context.Context, id int64) (*Account, error) {
	if id != r.account.ID {
		return nil, ErrVendorAccountNotFound
	}
	return r.account, nil
}
func (*vendorProbeAccountRepository) ClearError(context.Context, int64) error       { return nil }
func (*vendorProbeAccountRepository) SetError(context.Context, int64, string) error { return nil }
func (*vendorProbeAccountRepository) SetRateLimited(context.Context, int64, time.Time) error {
	return nil
}
func (*vendorProbeAccountRepository) SetTempUnschedulable(context.Context, int64, time.Time, string) error {
	return nil
}
func (*vendorProbeAccountRepository) UpdateExtra(context.Context, int64, map[string]any) error {
	return nil
}
