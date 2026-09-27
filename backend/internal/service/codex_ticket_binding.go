package service

// Adapted from ranxi2001's ticket egress/cookie binding. ModelTrace verification
// and generation CAS remain authoritative; ticket length is never a proof.
import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const codexTicketCookieLifetime = 240 * time.Second

// Only identity fields are persisted, never the proxy password, authorization,
// request text or complete request headers. Existing extra redaction covers it.
type codexTicketBinding struct {
	ProxyID          int64                      `json:"proxy_id"`
	ProxyFingerprint string                     `json:"proxy_fingerprint"`
	Headers          map[string]string          `json:"headers"`
	Body             map[string]json.RawMessage `json:"body"`
	ExpiresAt        time.Time                  `json:"expires_at"`
}

var codexTicketIdentityHeaders = []string{
	"session_id", "session-id", "conversation_id", "installation_id", "x-codex-installation-id",
	"thread_id", "thread-id", "turn_id", "turn-id", "window_id", "x-codex-window-id",
	"x-client-request-id", "x-codex-turn-metadata", "x-codex-beta-features",
	openAICodexRoutingHintHeader, "accept-language", responsesLiteHeaderKey,
	"User-Agent", "originator", "version",
}

var codexTicketIdentityBodyKeys = []string{"prompt_cache_key", "client_metadata", "device_id"}

func codexTicketProxyFingerprint(proxy string) string {
	digest := sha256.Sum256([]byte(proxy))
	return hex.EncodeToString(digest[:])
}

func captureCodexTicketIdentity(binding *codexTicketBinding, h http.Header, body []byte) {
	if binding == nil {
		return
	}
	binding.Headers = make(map[string]string)
	for _, name := range codexTicketIdentityHeaders {
		if value := h.Get(name); value != "" {
			binding.Headers[name] = value
		}
	}
	binding.Body = make(map[string]json.RawMessage)
	for _, name := range codexTicketIdentityBodyKeys {
		if value := gjson.GetBytes(body, name); value.Exists() {
			binding.Body[name] = json.RawMessage(value.Raw)
		}
	}
}

func codexTicketCredentialExpiry(h http.Header, now time.Time) time.Time {
	// The four-minute fallback comes from ranxi's credential bundle freshness.
	// An earlier server expiry/deletion wins. Never extend freshness on traffic.
	expires := now.Add(codexTicketCookieLifetime)
	for _, cookie := range (&http.Response{Header: h}).Cookies() {
		if cookie.MaxAge < 0 {
			return now
		}
		candidate := cookie.Expires
		if cookie.MaxAge > 0 {
			candidate = now.Add(time.Duration(cookie.MaxAge) * time.Second)
		}
		if !candidate.IsZero() && candidate.Before(expires) {
			expires = candidate
		}
	}
	return expires
}

func applyCodexTicketIdentityHeaders(h http.Header, ticket *openAICodexTicket) {
	if h == nil || ticket == nil {
		return
	}
	if ticket.State == "" {
		h.Del(openAICodexTurnStateHeader)
	} else {
		h.Set(openAICodexTurnStateHeader, ticket.State)
	}
	if ticket.Binding == nil {
		return
	}
	for _, name := range codexTicketIdentityHeaders {
		h.Del(name)
		if value := ticket.Binding.Headers[name]; value != "" {
			h.Set(name, value)
		}
	}
	applyOpenAICodexTicketCookie(h, ticket)
}

func applyCodexTicketIdentityBody(body []byte, ticket *openAICodexTicket) ([]byte, error) {
	if ticket == nil || ticket.Binding == nil {
		return body, nil
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, denyOpenAITurn("ticket_identity_body_invalid")
	}
	var err error
	for _, name := range codexTicketIdentityBodyKeys {
		if value, ok := ticket.Binding.Body[name]; ok {
			body, err = sjson.SetRawBytes(body, name, value)
		} else {
			body, err = sjson.DeleteBytes(body, name)
		}
		if err != nil {
			return nil, denyOpenAITurn("ticket_identity_body_invalid")
		}
	}
	return body, nil
}

// session.update may change identity at the root or inside session. Replace
// only fields present in that update; adding response-only fields to a session
// event would change its protocol shape.
func applyCodexTicketWSIdentityBody(body []byte, ticket *openAICodexTicket) ([]byte, error) {
	if ticket == nil || ticket.Binding == nil || gjson.GetBytes(body, "type").String() != "session.update" {
		return applyCodexTicketIdentityBody(body, ticket)
	}
	if !gjson.ValidBytes(body) || !gjson.ParseBytes(body).IsObject() {
		return nil, denyOpenAITurn("ticket_identity_body_invalid")
	}
	for _, prefix := range []string{"", "session."} {
		for _, name := range codexTicketIdentityBodyKeys {
			path := prefix + name
			if !gjson.GetBytes(body, path).Exists() {
				continue
			}
			var err error
			if value, ok := ticket.Binding.Body[name]; ok {
				body, err = sjson.SetRawBytes(body, path, value)
			} else {
				body, err = sjson.DeleteBytes(body, path)
			}
			if err != nil {
				return nil, denyOpenAITurn("ticket_identity_body_invalid")
			}
		}
	}
	return body, nil
}

func (s *OpenAIGatewayService) codexTicketProxy(ctx context.Context, ticket *openAICodexTicket, fallback string) (string, error) {
	if ticket == nil || ticket.Binding == nil {
		return fallback, nil
	}
	if !ticket.usable(time.Now(), 0, false, 0) {
		return "", denyOpenAITicket()
	}
	if s.settingService == nil || s.settingService.proxyRepo == nil || ticket.Binding.ProxyID <= 0 {
		return "", denyOpenAITurn("ticket_proxy_unavailable")
	}
	query, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	proxy, err := s.settingService.proxyRepo.GetByID(query, ticket.Binding.ProxyID)
	if err != nil && !errors.Is(err, ErrProxyNotFound) {
		return "", denyOpenAITurn("latest_state_unavailable")
	}
	if err != nil || proxy == nil || !proxy.IsActive() || proxy.IsExpired(time.Now()) ||
		codexTicketProxyFingerprint(proxy.URL()) != ticket.Binding.ProxyFingerprint {
		return "", denyOpenAITurn("ticket_proxy_unavailable")
	}
	return proxy.URL(), nil
}

func (s *OpenAIGatewayService) prepareCodexTicketHTTPRequest(req *http.Request, account *Account, proxy string) (string, error) {
	ticket, _ := req.Context().Value(codexTicketRequestContextKey{}).(*openAICodexTicket)
	if ticket == nil || ticket.Binding == nil {
		return proxy, nil
	}
	if ticket.AccountID != account.ID {
		return "", denyOpenAITicket()
	}
	if s.openaiCodexTicketLifecycle != nil {
		query, cancel := context.WithTimeout(req.Context(), 3*time.Second)
		raw, err := s.openaiCodexTicketLifecycle.CurrentTicket(query, account.ID, ticket.Model)
		cancel()
		if err != nil {
			return "", denyOpenAITurn("latest_state_unavailable")
		}
		var current openAICodexTicket
		if json.Unmarshal(raw, &current) != nil || current.GenerationID != ticket.GenerationID || !current.usable(time.Now(), 0, false, 0) {
			return "", denyOpenAITicket()
		}
	}
	proxy, err := s.codexTicketProxy(req.Context(), ticket, proxy)
	if err != nil {
		return "", err
	}
	if req.Body != nil {
		body, readErr := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if readErr != nil {
			return "", readErr
		}
		body, err = applyCodexTicketIdentityBody(body, ticket)
		if err != nil {
			return "", err
		}
		req.Body = io.NopCloser(bytes.NewReader(body))
		req.ContentLength = int64(len(body))
		req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(body)), nil }
	}
	applyCodexTicketIdentityHeaders(req.Header, ticket)
	return proxy, nil
}

// Exact credentials, including cookie-only tickets, must match the captured
// generation. Never infer a ticket generation from a returned turn-state.
func codexTicketHeadersMatch(ticket *openAICodexTicket, h http.Header) bool {
	return ticket != nil && strings.TrimSpace(ticket.State) == strings.TrimSpace(h.Get(openAICodexTurnStateHeader)) &&
		(ticket.Cookie == "" || ticket.Cookie == strings.Join(h.Values("Cookie"), "; "))
}
