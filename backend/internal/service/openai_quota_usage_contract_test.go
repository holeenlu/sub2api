package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

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
