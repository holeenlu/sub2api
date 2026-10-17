package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
)

// DefaultGrokRealtimeDialTimeout bounds the pre-accept upstream handshake.
// The timeout only covers dialing; an established session is not interrupted.
const DefaultGrokRealtimeDialTimeout = 12 * time.Second

// supportedGrokVoiceHTTPEndpoints are xAI Voice HTTP paths we forward as-is.
var supportedGrokVoiceHTTPEndpoints = map[string]struct{}{
	"tts":           {},
	"stt":           {},
	"custom-voices": {},
}

// ForwardGrokVoice forwards the official xAI Voice HTTP APIs (/tts, /stt, and
// the custom-voices CRUD/audio subresources).
// The response is intentionally passed through because TTS returns audio bytes
// while STT returns JSON and xAI may add format-specific headers.
func (s *OpenAIGatewayService) ForwardGrokVoice(ctx context.Context, c *gin.Context, account *Account, endpoint string, body []byte, contentType string) (*OpenAIForwardResult, error) {
	if s == nil || account == nil {
		return nil, fmt.Errorf("grok voice service/account is required")
	}
	if account.Platform != PlatformGrok {
		return nil, fmt.Errorf("account platform %s is not supported for grok voice", account.Platform)
	}
	endpoint = strings.Trim(strings.TrimSpace(endpoint), "/")
	parts := strings.Split(endpoint, "/")
	baseEndpoint := parts[0]
	if _, ok := supportedGrokVoiceHTTPEndpoints[baseEndpoint]; !ok {
		return nil, fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
	}
	if len(parts) > 1 && baseEndpoint != "custom-voices" {
		return nil, fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
	}
	if baseEndpoint == "custom-voices" {
		if len(parts) > 3 || (len(parts) == 3 && parts[2] != "audio") {
			return nil, fmt.Errorf("unsupported grok voice endpoint: %s", endpoint)
		}
	}
	for _, part := range parts[1:] {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "?#\\") {
			return nil, fmt.Errorf("invalid grok voice endpoint path")
		}
	}
	if baseEndpoint == "custom-voices" && (s.grokVoiceRepository() == nil || c == nil || c.Request == nil) {
		return nil, fmt.Errorf("voice ownership unavailable")
	}
	if baseEndpoint == "custom-voices" && len(parts) == 1 && c.Request.Method == http.MethodGet {
		voices, err := s.ListOwnedGrokVoices(ctx, c)
		if err != nil {
			return nil, err
		}
		c.JSON(http.StatusOK, gin.H{"voices": voices})
		return &OpenAIForwardResult{}, nil
	}
	if c != nil {
		if _, exists := c.Get("api_key"); exists {
			bound, err := s.ResolveGrokVoiceAccount(ctx, c, endpoint, body)
			if err != nil {
				return nil, err
			}
			if bound > 0 && bound != account.ID {
				return nil, ErrMediaNotOwned
			}
		}
	}
	token, _, err := s.getRequestCredential(ctx, c, account)
	if err != nil {
		return nil, err
	}
	targetURL, err := buildGrokVoiceURL(account, s.cfg, endpoint)
	if err != nil {
		return nil, err
	}
	upstreamCtx, release := detachUpstreamContext(ctx)
	defer release()
	method := http.MethodPost
	if c != nil && c.Request != nil && strings.TrimSpace(c.Request.Method) != "" {
		method = c.Request.Method
	}
	req, err := http.NewRequestWithContext(upstreamCtx, method, targetURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json, audio/*")
	if strings.TrimSpace(contentType) == "" {
		contentType = "application/json"
	}
	req.Header.Set("Content-Type", contentType)
	// Match media path: CLI identity headers only on the CLI chat proxy.
	// Official api.x.ai voice rejects or mistreats OAuth when CLI headers are stamped.
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(targetURL) {
		applyGrokCLIHeaders(req.Header)
	}
	account.ApplyHeaderOverrides(req.Header)

	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	started := time.Now()
	resp, err := s.httpUpstream.Do(req, proxyURL, account.ID, account.Concurrency)
	SetOpsLatencyMs(c, OpsUpstreamLatencyMsKey, time.Since(started).Milliseconds())
	if err != nil {
		return nil, s.handleOpenAIUpstreamTransportError(ctx, c, account, err, false)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 400 {
		return s.handleGrokMediaErrorResponse(ctx, resp, c, account, resp.Header.Get("x-request-id"), endpoint)
	}
	data, err := ReadUpstreamResponseBody(resp.Body, s.cfg, c, openAITooLargeError)
	if err != nil {
		return nil, err
	}
	durableCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	err = s.persistGrokVoice(durableCtx, c, account, endpoint, data)
	cancel()
	if err != nil {
		return nil, err
	}
	writeGrokMediaResponse(c, resp, data, s.responseHeaderFilter)
	audioUsage := estimateGrokVoiceAudioUsage(baseEndpoint, body, contentType, data, time.Since(started))
	upstreamID := firstNonEmpty(resp.Header.Get("x-request-id"), resp.Header.Get("xai-request-id"))
	return &OpenAIForwardResult{
		// Forced durable money-event id so usage_billing_dedup cannot collapse under a reused client id.
		RequestID:       StableGrokAudioBillingRequestID(upstreamID),
		UpstreamHeaders: resp.Header,
		Model:           baseEndpoint,
		UpstreamModel:   baseEndpoint,
		Duration:        time.Since(started),
		AudioUsage:      audioUsage,
	}, nil
}

// ProxyGrokRealtime relays JSON Realtime events to xAI's native Voice WS.
// Audio is carried as base64 inside JSON events, so preserving the JSON bytes
// is sufficient and avoids translating protocol event types.
func (s *OpenAIGatewayService) ProxyGrokRealtime(ctx context.Context, c *gin.Context, client *coderws.Conn, account *Account, token, model string) (bool, error) {
	if s == nil || client == nil || account == nil {
		return false, fmt.Errorf("realtime service, client, and account are required")
	}
	if account.Platform != PlatformGrok {
		return false, fmt.Errorf("account platform %s is not supported for grok realtime", account.Platform)
	}
	upstream, err := s.OpenGrokRealtime(ctx, account, token, model)
	if err != nil {
		return false, err
	}
	defer func() { _ = upstream.Close() }()
	return s.ProxyGrokRealtimeConn(ctx, c, client, upstream)
}

// GrokRealtimeVoiceAccountChange requests a pre-audio rebind to a voice owner.
// No part of Payload has been forwarded to the old account.
type GrokRealtimeVoiceAccountChange struct {
	AccountID int64
	Payload   []byte
}

func (e *GrokRealtimeVoiceAccountChange) Error() string {
	return "custom voice requires its owning upstream account"
}
func (s *OpenAIGatewayService) ReplayGrokRealtimeVoiceSetup(ctx context.Context, c *gin.Context, u *GrokRealtimeUpstream, change *GrokRealtimeVoiceAccountChange) error {
	if u == nil || change == nil || u.accountID != change.AccountID {
		return ErrMediaNotOwned
	}
	bound, err := s.ResolveGrokVoiceAccount(ctx, c, "realtime", change.Payload)
	if err != nil || bound != u.accountID {
		return ErrMediaNotOwned
	}
	return u.conn.WriteJSON(ctx, json.RawMessage(change.Payload))
}

type GrokRealtimeUpstream struct {
	conn      openAIWSClientConn
	accountID int64
	closeOnce sync.Once
	closeErr  error
}

// GrokRealtimeDialError preserves an HTTP status returned before WebSocket
// upgrade so handlers can apply the normal Grok account policy.
type GrokRealtimeDialError struct {
	StatusCode int
	Err        error
}

func (e *GrokRealtimeDialError) Error() string { return e.Err.Error() }
func (e *GrokRealtimeDialError) Unwrap() error { return e.Err }

func (u *GrokRealtimeUpstream) Close() error {
	if u == nil || u.conn == nil {
		return nil
	}
	u.closeOnce.Do(func() { u.closeErr = u.conn.Close() })
	return u.closeErr
}

func (s *OpenAIGatewayService) OpenGrokRealtime(ctx context.Context, account *Account, token, model string) (*GrokRealtimeUpstream, error) {
	if s == nil || account == nil || account.Platform != PlatformGrok {
		return nil, fmt.Errorf("grok realtime account is required")
	}
	base, err := buildGrokVoiceURL(account, s.cfg, "realtime")
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(base)
	if err != nil {
		return nil, err
	}
	u.Scheme = "wss"
	q := u.Query()
	q.Set("model", firstNonEmpty(model, "grok-voice-latest"))
	u.RawQuery = q.Encode()
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(u.String()) {
		applyGrokCLIHeaders(headers)
	}
	account.ApplyHeaderOverrides(headers)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	conn, status, _, err := s.getOpenAIWSPassthroughDialer().Dial(ctx, u.String(), headers, proxyURL)
	if err != nil {
		return nil, &GrokRealtimeDialError{StatusCode: status, Err: err}
	}
	return &GrokRealtimeUpstream{conn: conn, accountID: account.ID}, nil
}

// HandleGrokRealtimeUpstreamError applies the shared Grok account policy to a
// failed pre-accept WebSocket handshake.
func (s *OpenAIGatewayService) HandleGrokRealtimeUpstreamError(ctx context.Context, account *Account, statusCode int, body []byte) {
	if statusCode <= 0 {
		statusCode = http.StatusBadGateway
	}
	s.handleGrokAccountUpstreamError(ctx, account, statusCode, nil, body)
}

func (s *OpenAIGatewayService) ProxyGrokRealtimeConn(ctx context.Context, c *gin.Context, client *coderws.Conn, upstream *GrokRealtimeUpstream) (bool, error) {
	if s == nil || client == nil || upstream == nil || upstream.conn == nil {
		return false, fmt.Errorf("realtime connection is required")
	}
	conn := upstream.conn

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	errCh := make(chan error, 2)
	var readers sync.WaitGroup
	readers.Add(2)
	var audioObserved atomic.Bool

	// Upstream → client
	go func() {
		defer readers.Done()
		for {
			msg, readErr := conn.ReadMessage(ctx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if grokRealtimeEventHasAudio(msg) {
				audioObserved.Store(true)
			}
			if writeErr := client.Write(ctx, coderws.MessageText, msg); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}()

	// Client → upstream (JSON events only)
	go func() {
		defer readers.Done()
		for {
			kind, msg, readErr := client.Read(ctx)
			if readErr != nil {
				errCh <- readErr
				return
			}
			if kind != coderws.MessageText && kind != coderws.MessageBinary {
				continue
			}
			if grokRealtimeEventHasAudio(msg) {
				audioObserved.Store(true)
			}
			var raw json.RawMessage
			if unmarshalErr := json.Unmarshal(msg, &raw); unmarshalErr != nil {
				errCh <- fmt.Errorf("invalid realtime event: %w", unmarshalErr)
				return
			}
			if c != nil {
				if _, ok := c.Get("api_key"); ok {
					key, keyErr := mediaPrincipal(c)
					if keyErr != nil {
						errCh <- keyErr
						return
					}
					for _, path := range []string{"model", "session.model", "response.model"} {
						model := strings.TrimSpace(gjson.GetBytes(msg, path).String())
						if model != "" && key.Group != nil && !key.Group.ModelAllowlist.Allows(model) {
							errCh <- fmt.Errorf("model is not available for this group")
							return
						}
					}
					bound, err := s.ResolveGrokVoiceAccount(ctx, c, "realtime", msg)
					if err != nil {
						errCh <- err
						return
					}
					if bound > 0 && bound != upstream.accountID {
						if !audioObserved.Load() {
							errCh <- &GrokRealtimeVoiceAccountChange{AccountID: bound, Payload: append([]byte(nil), msg...)}
						} else {
							errCh <- ErrMediaNotOwned
						}
						return
					}
				}
			}
			if writeErr := conn.WriteJSON(ctx, raw); writeErr != nil {
				errCh <- writeErr
				return
			}
		}
	}()

	err := <-errCh
	cancel()
	_ = upstream.Close()
	// Join both pumps before reading usage and releasing the key reservation.
	readers.Wait()
	return audioObserved.Load(), err
}

// ProbeGrokRealtime performs the upstream WebSocket handshake without sending
// any client-visible events. Handlers use it before accepting the downstream
// upgrade so authentication and endpoint failures remain ordinary HTTP errors.
func (s *OpenAIGatewayService) ProbeGrokRealtime(ctx context.Context, account *Account, token, model string) error {
	if s == nil || account == nil {
		return fmt.Errorf("realtime service and account are required")
	}
	if account.Platform != PlatformGrok {
		return fmt.Errorf("account platform %s is not supported for grok realtime", account.Platform)
	}
	base, err := buildGrokVoiceURL(account, s.cfg, "realtime")
	if err != nil {
		return err
	}
	u, err := url.Parse(base)
	if err != nil {
		return err
	}
	u.Scheme = "wss"
	q := u.Query()
	q.Set("model", firstNonEmpty(model, "grok-voice-latest"))
	u.RawQuery = q.Encode()
	headers := http.Header{"Authorization": []string{"Bearer " + token}}
	if account.IsGrokOAuth() && isGrokCLIProxyTarget(u.String()) {
		applyGrokCLIHeaders(headers)
	}
	account.ApplyHeaderOverrides(headers)
	proxyURL := ""
	if account.ProxyID != nil && account.Proxy != nil {
		proxyURL = account.Proxy.URL()
	}
	dialer := s.getOpenAIWSPassthroughDialer()
	conn, _, _, err := dialer.Dial(ctx, u.String(), headers, proxyURL)
	if err != nil {
		return err
	}
	return conn.Close()
}

func awaitGrokRealtimeAudioObserved(errCh <-chan error, audioObserved *atomic.Bool) (bool, error) {
	err := <-errCh
	if audioObserved == nil {
		return false, err
	}
	return audioObserved.Load(), err
}

func grokRealtimeEventHasAudio(msg []byte) bool {
	if !gjson.ValidBytes(msg) {
		return false
	}
	eventType := strings.ToLower(strings.TrimSpace(gjson.GetBytes(msg, "type").String()))
	if !strings.Contains(eventType, "audio") || strings.Contains(eventType, "transcript") {
		return false
	}
	for _, path := range []string{"audio", "delta", "data"} {
		value := gjson.GetBytes(msg, path)
		if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
			return true
		}
	}
	return false
}

// estimateGrokVoiceAudioUsage derives billing units from the request/response.
// TTS: million characters of input text; STT: hours approximated from request body size
// when duration is unknown; custom-voices: no units (nil).
func estimateGrokVoiceAudioUsage(endpoint string, reqBody []byte, contentType string, respBody []byte, elapsed time.Duration) *AudioUsage {
	switch strings.TrimSpace(endpoint) {
	case "tts":
		// Prefer JSON "input" / "text" fields; fallback to raw body length.
		chars := 0
		if gjson.ValidBytes(reqBody) {
			for _, key := range []string{"input", "text", "prompt"} {
				if s := strings.TrimSpace(gjson.GetBytes(reqBody, key).String()); s != "" {
					chars = len([]rune(s))
					break
				}
			}
		}
		if chars <= 0 {
			chars = len(reqBody)
		}
		if chars <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "tts", DurationOrUnits: float64(chars) / 1_000_000.0}
	case "stt":
		// Prefer response duration when present; do not trust client duration_seconds alone
		// (under-report would underbill). Floor against body-size heuristic and elapsed.
		secs := 0.0
		if gjson.ValidBytes(respBody) {
			for _, path := range []string{"duration", "duration_seconds", "audio_duration", "usage.seconds"} {
				if v := gjson.GetBytes(respBody, path); v.Exists() && v.Type == gjson.Number && v.Float() > 0 {
					secs = v.Float()
					break
				}
			}
		}
		// Multipart / body size heuristic: ~16KB/s for compressed speech (lower bound).
		sizeFloor := 0.0
		if len(reqBody) > 0 {
			sizeFloor = float64(len(reqBody)) / 16000.0
		}
		clientSecs := 0.0
		if gjson.ValidBytes(reqBody) {
			if v := gjson.GetBytes(reqBody, "duration_seconds"); v.Exists() && v.Type == gjson.Number {
				clientSecs = v.Float()
			}
		}
		if secs <= 0 {
			secs = elapsed.Seconds()
		}
		if secs <= 0 {
			secs = clientSecs
		}
		if secs <= 0 {
			secs = sizeFloor
		}
		// Cap untrusted client under-report: if client duration is much smaller than
		// size/elapsed floors, bill the larger of floors (anti underbill).
		if clientSecs > 0 && secs == clientSecs {
			floor := sizeFloor
			if elapsed.Seconds() > floor {
				floor = elapsed.Seconds()
			}
			if floor > 0 && clientSecs < floor*0.5 {
				secs = floor
			}
		}
		if secs <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "stt", DurationOrUnits: secs / 3600.0}
	case "realtime":
		mins := elapsed.Minutes()
		if mins <= 0 {
			return nil
		}
		return &AudioUsage{Mode: "realtime", DurationOrUnits: mins}
	default:
		return nil
	}
}

var ErrMediaNotOwned = errors.New("voice resource not found")

type GatewayMediaVoice struct {
	ID                         string
	UserID, GroupID, AccountID int64
	Metadata                   json.RawMessage
}

type GrokVoiceRepository interface {
	PutVoice(context.Context, *GatewayMediaVoice) error
	GetVoice(context.Context, int64, int64, string) (*GatewayMediaVoice, error)
	ListVoices(context.Context, int64, int64) ([]GatewayMediaVoice, error)
	DeleteVoice(context.Context, int64, int64, string) error
}

func (s *OpenAIGatewayService) grokVoiceRepository() GrokVoiceRepository {
	if s == nil {
		return nil
	}
	repo, _ := s.accountRepo.(GrokVoiceRepository)
	return repo
}
func mediaPrincipal(c *gin.Context) (*APIKey, error) {
	if c == nil {
		return nil, ErrMediaNotOwned
	}
	v, ok := c.Get("api_key")
	if !ok {
		return nil, ErrMediaNotOwned
	}
	k, ok := v.(*APIKey)
	if !ok || k == nil || k.UserID <= 0 {
		return nil, ErrMediaNotOwned
	}
	return k, nil
}

// Only documented built-ins are account-independent. Unknown identifiers must
// have durable provenance; old shared upstream voices are never auto-adopted.
func isBuiltinGrokVoice(id string) bool {
	switch strings.ToLower(id) {
	case "ara", "rex", "sal", "eve", "leo":
		return true
	}
	return false
}

func grokVoiceReferences(body []byte) ([]string, error) {
	if len(body) == 0 {
		return nil, nil
	}
	if err := ValidateGatewaySecurityJSON(body); err != nil {
		return nil, err
	}
	var root any
	if err := json.Unmarshal(body, &root); err != nil {
		return nil, err
	}
	out := []string{}
	var walk func(any) error
	walk = func(x any) error {
		switch v := x.(type) {
		case map[string]any:
			for key, value := range v {
				if strings.EqualFold(key, "voice") || strings.EqualFold(key, "voice_id") {
					if key != "voice" && key != "voice_id" {
						return fmt.Errorf("noncanonical voice field")
					}
					switch ref := value.(type) {
					case string:
						if ref != "" {
							out = append(out, ref)
						}
					case map[string]any:
						for field := range ref {
							if field != "id" && field != "type" {
								return fmt.Errorf("unsupported voice reference field")
							}
						}
						id, ok := ref["id"].(string)
						if !ok || id == "" {
							return fmt.Errorf("invalid voice reference")
						}
						out = append(out, id)
					default:
						return fmt.Errorf("invalid voice reference")
					}
				} else if err := walk(value); err != nil {
					return err
				}
			}
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(root); err != nil {
		return nil, err
	}
	return out, nil
}

// ResolveGrokVoiceAccount checks every reference before selecting any upstream.
// The same user's keys share voices, but the owning group/account never changes.
func (s *OpenAIGatewayService) ResolveGrokVoiceAccount(ctx context.Context, c *gin.Context, endpoint string, body []byte) (int64, error) {
	key, err := mediaPrincipal(c)
	if err != nil {
		return 0, err
	}
	refs := []string{}
	if strings.HasPrefix(endpoint, "custom-voices/") {
		id := strings.Split(strings.TrimPrefix(endpoint, "custom-voices/"), "/")[0]
		if id == "" {
			return 0, ErrMediaNotOwned
		}
		refs = append(refs, id)
	}
	if endpoint == "tts" || endpoint == "realtime" {
		r, err := grokVoiceReferences(body)
		if err != nil {
			return 0, err
		}
		for _, id := range r {
			if !isBuiltinGrokVoice(id) {
				refs = append(refs, id)
			}
		}
	}
	var account int64
	for _, id := range refs {
		if s.grokVoiceRepository() == nil {
			return 0, ErrMediaNotOwned
		}
		v, err := s.grokVoiceRepository().GetVoice(ctx, derefGroupID(key.GroupID), key.UserID, id)
		if err != nil {
			return 0, err
		}
		if account != 0 && account != v.AccountID {
			return 0, ErrMediaNotOwned
		}
		account = v.AccountID
	}
	return account, nil
}

func (s *OpenAIGatewayService) ListOwnedGrokVoices(ctx context.Context, c *gin.Context) ([]json.RawMessage, error) {
	key, err := mediaPrincipal(c)
	if err != nil {
		return nil, err
	}
	if s.grokVoiceRepository() == nil {
		return nil, fmt.Errorf("media ownership repository unavailable")
	}
	voices, err := s.grokVoiceRepository().ListVoices(ctx, derefGroupID(key.GroupID), key.UserID)
	if err != nil {
		return nil, err
	}
	out := make([]json.RawMessage, 0, len(voices))
	for _, v := range voices {
		out = append(out, v.Metadata)
	}
	return out, nil
}

func (s *OpenAIGatewayService) persistGrokVoice(ctx context.Context, c *gin.Context, account *Account, endpoint string, data []byte) error {
	if !strings.HasPrefix(endpoint, "custom-voices") {
		return nil
	}
	key, err := mediaPrincipal(c)
	if err != nil {
		return err
	}
	if s.grokVoiceRepository() == nil {
		return fmt.Errorf("media ownership repository unavailable")
	}
	parts := strings.Split(endpoint, "/")
	if c.Request.Method == http.MethodDelete && len(parts) == 2 {
		return s.grokVoiceRepository().DeleteVoice(ctx, derefGroupID(key.GroupID), key.UserID, parts[1])
	}
	if len(parts) == 3 || c.Request.Method == http.MethodGet {
		return nil
	}
	id := gjson.GetBytes(data, "voice_id").String()
	if id == "" {
		id = gjson.GetBytes(data, "id").String()
	}
	if len(parts) == 2 {
		id = parts[1]
	}
	if id == "" || !gjson.ValidBytes(data) {
		return fmt.Errorf("upstream custom voice response has no identity")
	}
	return s.grokVoiceRepository().PutVoice(ctx, &GatewayMediaVoice{ID: id, AccountID: account.ID, GroupID: derefGroupID(key.GroupID), UserID: key.UserID, Metadata: data})
}
