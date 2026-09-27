package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type ticketBindingProxyRepo struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (r *ticketBindingProxyRepo) GetByID(_ context.Context, id int64) (*Proxy, error) {
	if r.err != nil {
		return nil, r.err
	}
	if r.proxy == nil || r.proxy.ID != id {
		return nil, ErrProxyNotFound
	}
	return r.proxy, nil
}

type ticketBindingUpstream struct {
	HTTPUpstream
	request  *http.Request
	proxy    string
	response *http.Response
	err      error
}

func (u *ticketBindingUpstream) Do(r *http.Request, proxy string, _ int64, _ int) (*http.Response, error) {
	u.request, u.proxy = r, proxy
	return u.response, u.err
}

func boundTicketFixture(t *testing.T) (*OpenAIGatewayService, *Account, *openAICodexTicket, *ticketBindingProxyRepo) {
	t.Helper()
	account := ticketTestAccount(72)
	proxy := &Proxy{ID: 8, Protocol: "http", Host: "pinned.invalid", Port: 8080, Status: StatusActive}
	ticket := verifiedTicket(account, "gpt-6-astra", "opaque-verified-state", "__oailb=harvest; __cflb=edge")
	ticket.Binding = &codexTicketBinding{ProxyID: proxy.ID, ProxyFingerprint: codexTicketProxyFingerprint(proxy.URL()), ExpiresAt: time.Now().Add(time.Minute)}
	h := http.Header{}
	h.Set("session_id", "fresh-harvest-session")
	h.Set("User-Agent", "probe-user-agent")
	captureCodexTicketIdentity(ticket.Binding, h, []byte(`{"prompt_cache_key":"harvest-cache","client_metadata":{"session_id":"fresh-harvest-session"}}`))
	account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
	repo := &ticketBindingProxyRepo{proxy: proxy}
	s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, FailClosed: true}, nil)
	s.settingService = &SettingService{proxyRepo: repo}
	return s, account, ticket, repo
}

func TestCodexTicketBindingHTTPUsesHarvestIdentityAndEgress(t *testing.T) {
	s, account, ticket, _ := boundTicketFixture(t)
	u := &ticketBindingUpstream{response: &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader("response"))}}
	s.httpUpstream = u
	req, _ := http.NewRequest(http.MethodPost, "https://chatgpt.com/backend-api/codex/responses", strings.NewReader(`{"model":"gpt-6-astra","input":"preserve input","prompt_cache_key":"user-cache","client_metadata":{"session_id":"user-session"},"device_id":"foreign"}`))
	req.Header.Set("session_id", "user-session")
	req.Header.Set("conversation_id", "foreign")
	req.Header.Set("Cookie", "__oailb=foreign")
	req.Header.Set("Authorization", "Bearer business-auth")
	req.Header.Set("OpenAI-Beta", openAIWSBetaV2Value)
	require.NoError(t, s.applyOpenAICodexTicket(context.Background(), account, ticket.Model, req.Header))
	req = req.WithContext(context.WithValue(req.Context(), codexTicketRequestContextKey{}, ticket))
	resp, err := s.doOpenAIUpstream(req, "http://account-proxy.invalid", account)
	require.NoError(t, err)
	require.Equal(t, "http://pinned.invalid:8080", u.proxy)
	require.Equal(t, ticket.Cookie, u.request.Header.Get("Cookie"))
	require.Equal(t, "fresh-harvest-session", u.request.Header.Get("session_id"))
	require.Empty(t, u.request.Header.Get("conversation_id"))
	require.Equal(t, "Bearer business-auth", u.request.Header.Get("Authorization"))
	require.Equal(t, openAIWSBetaV2Value, u.request.Header.Get("OpenAI-Beta"))
	body, err := io.ReadAll(u.request.Body)
	require.NoError(t, err)
	require.Equal(t, "preserve input", gjson.GetBytes(body, "input").String())
	require.Equal(t, "harvest-cache", gjson.GetBytes(body, "prompt_cache_key").String())
	require.Equal(t, "fresh-harvest-session", gjson.GetBytes(body, "client_metadata.session_id").String())
	require.False(t, gjson.GetBytes(body, "device_id").Exists())
	replay, err := u.request.GetBody()
	require.NoError(t, err)
	defer func() { require.NoError(t, replay.Close()) }()
	replayed, err := io.ReadAll(replay)
	require.NoError(t, err)
	require.Equal(t, body, replayed)
	require.Equal(t, int64(len(body)), u.request.ContentLength)
	require.True(t, s.codexTicketAccountBusy(context.Background(), account.ID))
	_, err = io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.False(t, s.codexTicketAccountBusy(context.Background(), account.ID))
	require.NoError(t, resp.Body.Close())
	require.False(t, s.codexTicketAccountBusy(context.Background(), account.ID))
}

func TestCodexTicketBindingRejectsRetiredRoutesBeforeSend(t *testing.T) {
	for _, change := range []string{"disabled", "expired", "changed_endpoint", "deleted", "db_error", "ticket_expired", "new_generation"} {
		t.Run(change, func(t *testing.T) {
			s, account, ticket, repo := boundTicketFixture(t)
			s.httpUpstream = &codexTicketFuncUpstream{do: func(*http.Request) (*http.Response, error) {
				t.Fatal("rejected binding sent a request")
				return nil, nil
			}}
			switch change {
			case "disabled":
				repo.proxy.Status = "disabled"
			case "expired":
				past := time.Now().Add(-time.Second)
				repo.proxy.ExpiresAt = &past
			case "changed_endpoint":
				repo.proxy.Host = "changed.invalid"
			case "deleted":
				repo.proxy = nil
			case "db_error":
				repo.err = errors.New("unavailable")
			case "ticket_expired":
				ticket.Binding.ExpiresAt = time.Now().Add(-time.Second)
			case "new_generation":
				fresh := *ticket
				fresh.GenerationID = "new"
				s.openaiCodexTicketLifecycle = &codexInvalidationCapture{current: &fresh}
			}
			req := ticketObservationRequest(t, ticket)
			_, err := s.doOpenAIUpstream(req, "http://must-not-fallback.invalid", account)
			require.True(t, IsOpenAITurnAdmissionError(err), "%v", err)
			if change == "db_error" {
				require.Contains(t, err.Error(), "latest_state_unavailable", "a database outage must preserve continuation state")
			}
			require.False(t, s.codexTicketAccountBusy(context.Background(), account.ID))
			require.Nil(t, account.TempUnschedulableUntil)
		})
	}
}

func TestCodexTicketBindingCookieFreshness(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	for _, tc := range []struct {
		cookie string
		delta  time.Duration
	}{
		{"__oailb=token", 240 * time.Second},
		{"__oailb=token; Max-Age=90", 90 * time.Second},
		{"__oailb=token; Max-Age=9999", 240 * time.Second},
		{"__oailb=; Max-Age=0", 0},
		{"__oailb=token; Expires=" + now.Add(time.Minute).Format(http.TimeFormat), time.Minute},
	} {
		h := http.Header{}
		h.Set("Set-Cookie", tc.cookie)
		require.Equal(t, now.Add(tc.delta), codexTicketCredentialExpiry(h, now))
	}
	s, account, ticket, _ := boundTicketFixture(t)
	ticket.Binding.ExpiresAt = now.Add(-time.Second)
	require.ErrorIs(t, s.applyOpenAICodexTicket(context.Background(), account, ticket.Model, http.Header{}), ErrOpenAICodexTicketUnavailable)
	require.False(t, OpenAICodexTicketReadyModels(account)[ticket.Model])
	for _, status := range OpenAICodexTicketStatuses(account, s.openAICodexTicketConfig(), now) {
		if status.Model == ticket.Model {
			require.False(t, status.Ready)
			require.NotNil(t, status.ExpiresAt)
		}
	}
	// Observing traffic is not a freshness renewal.
	expires := ticket.Binding.ExpiresAt
	s.observeCodexTicketResponse(ticketObservationRequest(t, ticket), changedTicketResponse(), account)
	require.Equal(t, expires, ticket.Binding.ExpiresAt)
}

func TestCodexTicketBindingWSRechecksProxyBetweenTurns(t *testing.T) {
	s, account, ticket, repo := boundTicketFixture(t)
	h := http.Header{}
	applyCodexTicketIdentityHeaders(h, ticket)
	binding := bindOpenAIWSTicket(account, ticket.Model, h, ticket)
	require.NoError(t, s.checkOpenAIWSBinding(account, ticket.Model, binding))
	repo.proxy.Status = "disabled"
	err := s.checkOpenAIWSBinding(account, ticket.Model, binding)
	require.True(t, IsOpenAITurnAdmissionError(err))
	require.Contains(t, err.Error(), "ticket_proxy_unavailable")
}

func TestCodexTicketBindingWSRetiresWhenDisabled(t *testing.T) {
	s, account, ticket, _ := boundTicketFixture(t)
	h := http.Header{}
	applyCodexTicketIdentityHeaders(h, ticket)
	binding := bindOpenAIWSTicket(account, ticket.Model, h, ticket)
	s.cfg.Gateway.OpenAICodexTicket.Enabled = false
	require.True(t, IsOpenAITurnAdmissionError(s.checkOpenAIWSBinding(account, ticket.Model, binding)))
	unbound := bindOpenAIWSTicket(account, ticket.Model, http.Header{}, nil)
	require.NoError(t, s.checkOpenAIWSBinding(account, ticket.Model, unbound))
}

func TestCodexTicketBindingWSSessionUpdatePreservesIdentity(t *testing.T) {
	_, _, ticket, _ := boundTicketFixture(t)
	body, err := applyCodexTicketWSIdentityBody([]byte(`{"type":"session.update","client_metadata":{"session_id":"foreign"},"session":{"model":"gpt-6-astra","client_metadata":{"session_id":"foreign"},"prompt_cache_key":"foreign","device_id":"foreign","instructions":"keep instructions"}}`), ticket)
	require.NoError(t, err)
	require.Equal(t, "fresh-harvest-session", gjson.GetBytes(body, "client_metadata.session_id").String())
	require.Equal(t, "fresh-harvest-session", gjson.GetBytes(body, "session.client_metadata.session_id").String())
	require.Equal(t, "harvest-cache", gjson.GetBytes(body, "session.prompt_cache_key").String())
	require.False(t, gjson.GetBytes(body, "session.device_id").Exists())
	require.False(t, gjson.GetBytes(body, "prompt_cache_key").Exists())
	require.Equal(t, "keep instructions", gjson.GetBytes(body, "session.instructions").String())
	require.Equal(t, ticket.Model, gjson.GetBytes(body, "session.model").String())
	plain := []byte(`{"type":"session.update","session":{"model":"gpt-6-astra"}}`)
	unchanged, err := applyCodexTicketWSIdentityBody(plain, ticket)
	require.NoError(t, err)
	require.Equal(t, plain, unchanged)
}

func TestCodexTicketBindingRefreshBeforeExpiryAfterRestart(t *testing.T) {
	for _, refresh := range []int{0, 45, 1800} {
		t.Run(fmt.Sprint(refresh), func(t *testing.T) {
			s := ticketTestService(t, config.OpenAICodexTicketConfig{Enabled: true, HarvestRefreshSeconds: refresh}, nil)
			account := ticketTestAccount(73)
			now := time.Now()
			ticket := verifiedTicket(account, "gpt-6-astra", "state", "__oailb=bound")
			ticket.CapturedAt = now.Add(-220 * time.Second)
			ticket.Binding = &codexTicketBinding{ExpiresAt: now.Add(20 * time.Second)}
			account.Extra[openAICodexTicketExtraKey(ticket.Model)] = ticket
			require.True(t, s.codexTicketAutomaticDue(account, ticket.Model, now), "first scan must refresh before expiry")
			ticket.CapturedAt = now
			ticket.GenerationID = "refreshed"
			ticket.Binding.ExpiresAt = now.Add(240 * time.Second)
			require.False(t, s.codexTicketAutomaticDue(account, ticket.Model, now), "new generation must reset scheduling")
		})
	}
}

func TestCodexTicketBindingPoolSeparatesGenerationAndClientScope(t *testing.T) {
	s, account, ticket, _ := boundTicketFixture(t)
	pool := newOpenAIWSConnPool(&config.Config{})
	defer pool.Close()
	dials := 0
	pool.setClientDialerForTest(turnAdmissionDialerFunc(func(_ context.Context, _ string, h http.Header, p string) (openAIWSClientConn, int, http.Header, error) {
		dials++
		require.Equal(t, "http://pinned.invalid:8080", p)
		require.Equal(t, "fresh-harvest-session", h.Get("session_id"))
		return newStagedPassthroughConn(), 101, http.Header{}, nil
	}))
	req := openAIWSAcquireRequest{Account: account, WSURL: "ws://loopback.invalid/responses", Headers: http.Header{}, Ticket: ticket, TicketScope: "api1/session1", PrepareTicket: s.codexTicketProxy}
	req.Headers.Set(openAICodexTurnStateHeader, ticket.State)
	applyCodexTicketIdentityHeaders(req.Headers, ticket)
	first := normalizeOpenAIWSAcquireCompatibility(req, req.Headers)
	conn, err := pool.dialConn(context.Background(), req)
	require.NoError(t, err)
	defer conn.close()
	require.Equal(t, first, conn.handshakeCompatibility)
	require.Equal(t, ticket.GenerationID, conn.turnBinding.ticket.GenerationID)
	req.TicketScope = "api2/session1"
	require.NotEqual(t, first, normalizeOpenAIWSAcquireCompatibility(req, req.Headers))
	req.TicketScope = "api1/session1"
	next := *ticket
	next.GenerationID = "new"
	req.Ticket = &next
	require.NotEqual(t, first, normalizeOpenAIWSAcquireCompatibility(req, req.Headers))
	require.Equal(t, 1, dials)
}

func TestCodexTicketActivityBusinessCancelsParallelHarvestsAndReleases(t *testing.T) {
	s, account, _, _ := boundTicketFixture(t)
	first, releaseFirst, err := s.beginCodexTicketHarvest(context.Background(), account.ID)
	require.NoError(t, err)
	defer releaseFirst()
	second, releaseSecond, err := s.beginCodexTicketHarvest(context.Background(), account.ID)
	require.NoError(t, err)
	defer releaseSecond()
	release := s.beginCodexTicketBusiness(account)
	require.ErrorIs(t, first.Err(), context.Canceled)
	require.ErrorIs(t, second.Err(), context.Canceled)
	_, _, err = s.beginCodexTicketHarvest(context.Background(), account.ID)
	require.ErrorIs(t, err, ErrCodexTicketBusy)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); done := s.beginCodexTicketBusiness(account); done(); done() }()
	}
	wg.Wait()
	release()
	release()
	require.False(t, s.codexTicketAccountBusy(context.Background(), account.ID))
	_, done, err := s.beginCodexTicketHarvest(context.Background(), account.ID)
	require.NoError(t, err)
	done()
}

func TestCodexTicketBindingPersistsNoProxyCredentials(t *testing.T) {
	_, _, ticket, _ := boundTicketFixture(t)
	h := http.Header{"Authorization": []string{"Bearer secret"}, "Cookie": []string{"private=secret"}}
	h.Set("session_id", "fresh")
	captureCodexTicketIdentity(ticket.Binding, h, []byte(`{"input":"private prompt","client_metadata":{"session_id":"fresh"}}`))
	raw, err := json.Marshal(ticket.Binding)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "secret")
	require.NotContains(t, string(raw), "private prompt")
	require.NotContains(t, string(raw), "pinned.invalid")
}
