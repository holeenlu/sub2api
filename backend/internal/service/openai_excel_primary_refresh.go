package service

import (
	"context"
	"encoding/json"
	"reflect"
	"slices"
	"strings"
	"sync"
	"time"
)

func (s *OpenAIGatewayService) excelPrimaryMissingRefresh(ctx context.Context, account *Account) error {
	expiry := account.GetOpenAITokenExpiresAt()
	if account.GetCredential("refresh_token") != "" || (account.GetCredential("access_token") != "" && (expiry == nil || expiry.After(time.Now()))) {
		return nil
	}
	if s.accountRepo == nil {
		return excelBPSDependencyFailure()
	}
	fresh, err := s.accountRepo.GetByID(ctx, account.ID)
	if err != nil || fresh == nil || !reflect.DeepEqual(fresh.Credentials, account.Credentials) {
		return excelBPSDependencyFailure()
	}
	return &excelAuthorizationFailure{
		fact:  ExcelBPSFailure{Reason: "refresh_grant_missing", ReauthRequired: true, GrantFingerprint: "primary"},
		cause: excelGrantReauthorizationRequired(),
	}
}

// The existing OAuth owner supplies a receipt only for a successful conditional
// refresh. A cache hit or arbitrary DB reread is not evidence of the same grant.
type AccountExcelPrimaryRefreshRepository interface {
	UpdateExcelPrimaryCredentialsIfUnchanged(context.Context, *Account, map[string]any) (bool, error)
}

type excelPrimaryRefreshKey struct{}
type excelPrimaryRefreshCapture struct {
	mu       sync.Mutex
	expected *Account
	current  *Account
}

func excelPrimaryRefreshCredentialsEqual(a, b map[string]any) bool {
	left, leftErr := json.Marshal(a)
	right, rightErr := json.Marshal(b)
	return leftErr == nil && rightErr == nil && string(left) == string(right)
}

// RecordExcelPrimaryRefresh must be called by the existing refresh owner only
// after its credential CAS committed and after it reread the resulting account.
func RecordExcelPrimaryRefresh(ctx context.Context, before, after *Account) {
	capture, _ := ctx.Value(excelPrimaryRefreshKey{}).(*excelPrimaryRefreshCapture)
	if capture == nil || before == nil || after == nil {
		return
	}
	capture.mu.Lock()
	defer capture.mu.Unlock()
	if !reflect.DeepEqual(capture.expected.Credentials, before.Credentials) ||
		!excelPrimaryRefreshPolicyEqual(capture.expected, before) || !excelPrimaryRefreshPolicyEqual(before, after) {
		return
	}
	capture.current = snapshotOAuthRefreshAccount(after)
}

func excelPrimaryRefreshPolicyEqual(a, b *Account) bool {
	if a == nil || b == nil || !a.IsExcelOAuth() || !b.IsExcelOAuth() {
		return false
	}
	policy := func(account *Account) []byte {
		extra := map[string]any{}
		for key, value := range account.Extra {
			if strings.HasPrefix(key, "openai_excel_bps") || strings.HasPrefix(key, "openai_cloud") || strings.HasPrefix(key, "vendor_") {
				extra[key] = value
			}
		}
		// Normal rotation advances this digest atomically; the actual shutdown
		// identity, claim and every other option must stay unchanged.
		raw, _ := json.Marshal(extra)
		var detached map[string]any
		_ = json.Unmarshal(raw, &detached)
		if record, ok := detached[ExcelBPSFailureKey].(map[string]any); ok {
			if savedPolicy, ok := record["policy"].(map[string]any); ok {
				delete(savedPolicy, "primary")
			}
		}
		groups := append([]int64{}, account.GroupIDs...)
		slices.Sort(groups)
		raw, _ = json.Marshal([]any{
			account.ID, account.Platform, account.Type, account.Status, account.Schedulable,
			account.VendorID, account.ParentAccountID, account.ProxyID, groups,
			account.ExpiresAt, account.AutoPauseOnExpired,
			account.GetChatGPTAccountID(), account.GetChatGPTUserID(), account.GetCredential("client_id"), detached,
		})
		return raw
	}
	return string(policy(a)) == string(policy(b))
}

func (s *OpenAIGatewayService) getExcelBPSAccessTokenSnapshot(ctx context.Context, account *Account) (string, *Account, error) {
	if !account.IsExcelOAuth() {
		token, err := s.getExcelBPSAccessToken(ctx, account)
		return token, account, err
	}
	capture := &excelPrimaryRefreshCapture{expected: snapshotOAuthRefreshAccount(account)}
	ctx = context.WithValue(ctx, excelPrimaryRefreshKey{}, capture)
	token, err := s.getExcelBPSAccessToken(ctx, account)
	if err != nil {
		return "", account, err
	}
	capture.mu.Lock()
	fresh := capture.current
	capture.mu.Unlock()
	if fresh != nil && fresh.GetCredential("access_token") == token {
		return token, fresh, nil
	}
	if token != account.GetCredential("access_token") {
		return "", account, errOAuthRefreshAccountStateChanged
	}
	return token, account, nil
}
