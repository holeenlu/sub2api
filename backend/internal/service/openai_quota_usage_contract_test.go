package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// The shared usage transport must not merge the callers' responsibilities:
// quota cards may query reset-credit details; Cloud only checks admission.
func TestOpenAIQuotaAndCloudUsageCallerContracts(t *testing.T) {
	for _, tc := range []struct{ name, body, cloudError string }{
		{"allowed", "{\"account_id\":\"workspace\",\"rate_limit\":{\"allowed\":true,\"limit_reached\":false},\"credits\":{\"balance\":\"12345678901234567890.0123\"}}", ""},
		{"exhausted", "{\"account_id\":\"workspace\",\"rate_limit\":{\"allowed\":false,\"limit_reached\":true}}", "quota is exhausted"},
		{"missing rate envelope", "{\"account_id\":\"workspace\"}", "response is incomplete"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var mu sync.Mutex
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				paths = append(paths, r.URL.Path)
				mu.Unlock()
				require.Equal(t, http.MethodGet, r.Method)
				require.Equal(t, "Bearer contract-token", r.Header.Get("Authorization"))
				require.Equal(t, "workspace", r.Header.Get("ChatGPT-Account-Id"))
				w.Header().Set("Content-Type", "application/json")
				if r.URL.Path == "/backend-api/wham/usage" {
					_, _ = w.Write([]byte(tc.body))
					return
				}
				require.Equal(t, "/backend-api/wham/rate-limit-reset-credits", r.URL.Path)
				_, _ = w.Write([]byte("{}"))
			}))
			defer srv.Close()
			factory := newQuotaRedirectingFactory(srv)
			account := &Account{ID: 100, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive,
				Credentials: map[string]any{"chatgpt_account_id": "workspace"}}
			repo := &stubQuotaAccountRepo{accounts: map[int64]*Account{100: account}}
			tokens := &stubQuotaTokenCache{tokens: map[string]string{OpenAITokenCacheKey(account): "contract-token"}}
			quota := NewOpenAIQuotaService(repo, nil, NewOpenAITokenProvider(repo, tokens, nil), factory, nil)
			usage, err := quota.QueryUsage(t.Context(), 100)
			require.NoError(t, err)
			require.Equal(t, "workspace", usage.AccountID)
			require.Positive(t, usage.FetchedAt)
			if usage.Credits != nil {
				require.Equal(t, "12345678901234567890.0123", *usage.Credits.Balance)
			}
			mu.Lock()
			quotaPaths := append([]string(nil), paths...)
			paths = nil
			mu.Unlock()
			require.Equal(t, []string{"/backend-api/wham/usage", "/backend-api/wham/rate-limit-reset-credits"}, quotaPaths)

			gateway := &OpenAIGatewayService{cloudService: &OpenAICloudService{oauth: &OpenAIOAuthService{privacyClientFactory: factory}}}
			access := &OpenAICloudAccess{Account: account, AccessToken: "contract-token"}
			got, err := gateway.checkCloudUsage(t.Context(), access)
			if tc.cloudError == "" {
				require.NoError(t, err)
				require.Same(t, access, got)
			} else {
				require.ErrorContains(t, err, tc.cloudError)
				require.Nil(t, got)
			}
			mu.Lock()
			cloudPaths := append([]string(nil), paths...)
			mu.Unlock()
			require.Equal(t, []string{"/backend-api/wham/usage"}, cloudPaths)
			require.Zero(t, repo.extraUpdateCalls, "usage reads must not write quota-card or account state")
		})
	}
}

func TestCloudUsageCheckPreservesCancellation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("an already canceled usage request reached the upstream")
	}))
	defer srv.Close()
	gateway := &OpenAIGatewayService{cloudService: &OpenAICloudService{oauth: &OpenAIOAuthService{privacyClientFactory: newQuotaRedirectingFactory(srv)}}}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	got, err := gateway.checkCloudUsage(ctx, &OpenAICloudAccess{Account: &Account{}, AccessToken: "contract-token"})
	require.Nil(t, got)
	require.ErrorIs(t, err, context.Canceled)
}

func TestQueryOpenAIQuotaUsageReturnsUninterpretedFailureFacts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		status     int
		body       string
		parseError bool
	}{
		{"unauthorized", http.StatusUnauthorized, "{\"error\":\"expired\"}", false},
		{"rate limited", http.StatusTooManyRequests, "{\"rate_limit\":{\"allowed\":true}}", false},
		{"malformed success", http.StatusOK, "{\"rate_limit\":", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				require.Equal(t, "/backend-api/wham/usage", r.URL.Path)
				require.Equal(t, "caller-identity", r.Header.Get("originator"))
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "42")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()
			client, err := newQuotaRedirectingFactory(srv)("")
			require.NoError(t, err)
			usage, response, err := queryOpenAIQuotaUsage(t.Context(), client, map[string]string{"originator": "caller-identity"})
			if tc.parseError {
				require.Error(t, err)
			} else {
				require.NoError(t, err, "HTTP refusal is a fact for the caller, not an automatic refresh or retry")
				require.Nil(t, usage.RateLimit, "a refusal body must not become an allowed quota observation")
			}
			require.NotNil(t, response)
			require.Equal(t, tc.status, response.StatusCode)
			require.Equal(t, "42", response.Header.Get("Retry-After"))
			require.EqualValues(t, 1, calls.Load())
		})
	}
}

func TestCloudUsageRefreshFailureKeepsNativeFailureScope(t *testing.T) {
	for _, kind := range []string{"database", "transport", "shared_client", "cancel", "invalid_grant"} {
		t.Run(kind, func(t *testing.T) {
			account := nativeRefreshTestAccount()
			repo := &refreshAPIAccountRepo{account: account}
			executor := &refreshAPIExecutorStub{needsRefresh: true}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			switch kind {
			case "database":
				repo.getByIDErr = errors.New("private database failure")
			case "transport":
				executor.err = errors.New("private upstream temporary transport failure")
			case "shared_client":
				executor.err = errors.New("invalid_client")
			case "cancel":
				executor.onRefresh = cancel
			case "invalid_grant":
				executor.err = errors.New("invalid_grant")
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusUnauthorized) }))
			defer server.Close()
			provider := NewOpenAITokenProvider(repo, nil, nil)
			provider.refreshAPI, provider.executor = NewOAuthRefreshAPI(repo, nil), executor
			gateway := &OpenAIGatewayService{cloudService: &OpenAICloudService{tokens: provider, oauth: &OpenAIOAuthService{privacyClientFactory: newQuotaRedirectingFactory(server)}}}
			result, err := gateway.checkCloudUsage(ctx, &OpenAICloudAccess{Account: account, AccessToken: "old-at"})
			require.Nil(t, result)
			switch kind {
			case "cancel":
				require.ErrorIs(t, err, context.Canceled)
			case "invalid_grant":
				require.ErrorIs(t, err, ErrOpenAICloudAuthorization)
			default:
				require.ErrorIs(t, err, errCloudStateUnavailable)
				require.NotContains(t, err.Error(), "private")
			}
			require.Zero(t, repo.updateCalls)
		})
	}
}

func TestQuotaAndCloudCredentialRejectionsUpdateNativeAccount(t *testing.T) {
	for _, path := range []string{"quota", "cloud"} {
		for _, kind := range []string{"account_deactivated", "token_revoked", "temporary_401", "content_policy_violation", "rate_limit_exceeded", "echoed_code"} {
			t.Run(path+"/"+kind, func(t *testing.T) {
				account := nativeRefreshTestAccount()
				account.Credentials["chatgpt_account_id"] = "workspace"
				account.Credentials["expires_at"] = time.Now().Add(time.Hour).Format(time.RFC3339)
				repo := &refreshAPIAccountRepo{account: account}
				provider := NewOpenAITokenProvider(repo, nil, nil)
				code, status := kind, 403
				if kind == "temporary_401" {
					code, status = "unauthorized", 401
				}
				if kind == "rate_limit_exceeded" {
					status = 429
				}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(status)
					if kind == "echoed_code" {
						_, _ = w.Write([]byte(`{"error":{"code":"unknown"},"input":{"error":{"code":"account_deactivated"}}}`))
					} else {
						_, _ = fmt.Fprintf(w, `{"error":{"code":%q}}`, code)
					}
				}))
				defer srv.Close()
				factory := newQuotaRedirectingFactory(srv)
				if path == "quota" {
					_, err := NewOpenAIQuotaService(repo, nil, provider, factory, nil).QueryUsage(t.Context(), account.ID)
					require.Error(t, err)
				} else {
					svc := &OpenAIGatewayService{cloudService: &OpenAICloudService{tokens: provider, oauth: &OpenAIOAuthService{privacyClientFactory: factory}}}
					_, err := svc.checkCloudUsage(t.Context(), &OpenAICloudAccess{Account: account, AccessToken: "old-at"})
					require.Error(t, err)
				}
				if kind == "account_deactivated" || kind == "token_revoked" {
					require.Equal(t, StatusError, account.Status)
					require.False(t, account.Schedulable)
				} else {
					require.Equal(t, StatusActive, account.Status)
					if kind == "temporary_401" {
						require.NotNil(t, account.TempUnschedulableUntil)
					} else {
						require.Nil(t, account.TempUnschedulableUntil)
					}
				}
			})
		}
	}
}
