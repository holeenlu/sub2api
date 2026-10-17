package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

type ClaudeUsageServiceSuite struct {
	suite.Suite
	srv     *httptest.Server
	fetcher *claudeUsageService
}

func (s *ClaudeUsageServiceSuite) TearDownTest() {
	if s.srv != nil {
		s.srv.Close()
		s.srv = nil
	}
}

// usageRequestCapture holds captured request data for assertions in the main goroutine.
type usageRequestCapture struct {
	authorization string
	anthropicBeta string
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_Success() {
	var captured usageRequestCapture

	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		captured.authorization = r.Header.Get("Authorization")
		captured.anthropicBeta = r.Header.Get("anthropic-beta")

		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{
  "five_hour": {"utilization": 12.5, "resets_at": "2025-01-01T00:00:00Z"},
  "seven_day": {"utilization": 34.0, "resets_at": "2025-01-08T00:00:00Z"},
  "seven_day_sonnet": {"utilization": 56.0, "resets_at": "2025-01-08T00:00:00Z"}
}`)
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	resp, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.NoError(s.T(), err, "FetchUsage")
	require.Equal(s.T(), 12.5, resp.FiveHour.Utilization, "FiveHour utilization mismatch")
	require.Equal(s.T(), 34.0, resp.SevenDay.Utilization, "SevenDay utilization mismatch")
	require.Equal(s.T(), 56.0, resp.SevenDaySonnet.Utilization, "SevenDaySonnet utilization mismatch")

	// Assertions on captured request data
	require.Equal(s.T(), "Bearer at", captured.authorization, "Authorization header mismatch")
	require.Equal(s.T(), "oauth-2025-04-20", captured.anthropicBeta, "anthropic-beta header mismatch")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_NonOK() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "nope")
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "HTTP 401")
	require.NotContains(s.T(), err.Error(), "nope", "raw upstream error bodies must not reach clients")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_BadJSON() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "not-json")
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "decode response failed")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_RejectsSuccessfulErrorEnvelope() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "{\"error\":{\"type\":\"permission_error\",\"message\":\"sensitive details\"}}")
	}))
	s.fetcher = &claudeUsageService{usageURL: s.srv.URL, allowPrivateHosts: true}
	_, err := s.fetcher.FetchUsage(s.T().Context(), "at", "")
	require.Error(s.T(), err)
	require.NotContains(s.T(), err.Error(), "sensitive details")
}

func (s *ClaudeUsageServiceSuite) TestFetchProfile_SameAuthorizationAndOptionalFields() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(s.T(), "/profile", r.URL.Path)
		require.Equal(s.T(), "Bearer at", r.Header.Get("Authorization"))
		require.Equal(s.T(), "oauth-2025-04-20", r.Header.Get("anthropic-beta"))
		_, _ = io.WriteString(w, `{"organization":{"rate_limit_tier":"default_claude_max_20x","has_extra_usage_enabled":false},"account":{"has_claude_max":true}}`)
	}))
	s.fetcher = &claudeUsageService{profileURL: s.srv.URL + "/profile", allowPrivateHosts: true}
	profile, err := s.fetcher.FetchProfileWithOptions(s.T().Context(), &service.ClaudeUsageFetchOptions{AccessToken: "at"})
	require.NoError(s.T(), err)
	require.JSONEq(s.T(), `"default_claude_max_20x"`, string(profile.Organization["rate_limit_tier"]))
	require.JSONEq(s.T(), "false", string(profile.Organization["has_extra_usage_enabled"]))
}

func (s *ClaudeUsageServiceSuite) TestFetchProfile_DoesNotFollowCredentialRedirects() {
	requests := 0
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		http.Redirect(w, r, "/other", http.StatusFound)
	}))
	s.fetcher = &claudeUsageService{profileURL: s.srv.URL, allowPrivateHosts: true}
	_, err := s.fetcher.FetchProfileWithOptions(s.T().Context(), &service.ClaudeUsageFetchOptions{AccessToken: "at"})
	require.Error(s.T(), err)
	var upstream *service.ClaudeUsageHTTPError
	require.ErrorAs(s.T(), err, &upstream)
	require.Equal(s.T(), http.StatusFound, upstream.StatusCode)
	require.Equal(s.T(), 1, requests)
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_ContextCancel() {
	s.srv = newLocalTestServer(s.T(), http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Never respond - simulate slow server
		<-r.Context().Done()
	}))

	s.fetcher = &claudeUsageService{
		usageURL:          s.srv.URL,
		allowPrivateHosts: true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := s.fetcher.FetchUsage(ctx, "at", "")
	require.Error(s.T(), err, "expected error for cancelled context")
}

func (s *ClaudeUsageServiceSuite) TestFetchUsage_InvalidProxyReturnsError() {
	s.fetcher = &claudeUsageService{
		usageURL:          "http://example.com",
		allowPrivateHosts: true,
	}

	_, err := s.fetcher.FetchUsage(context.Background(), "at", "://bad-proxy-url")
	require.Error(s.T(), err)
	require.ErrorContains(s.T(), err, "create http client failed")
}

func TestClaudeUsageServiceSuite(t *testing.T) {
	suite.Run(t, new(ClaudeUsageServiceSuite))
}
