package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type claudeStateTestCache struct {
	stubGatewayCache
	mu          sync.Mutex
	values      map[string]map[string]string
	unavailable bool
}

func (c *claudeStateTestCache) GetClaudeConversationState(_ context.Context, kind string, keys []string) (map[string]string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return nil, errors.New("cache unavailable")
	}
	result := map[string]string{}
	for _, key := range keys {
		if value, ok := c.values[kind][key]; ok {
			result[key] = value
		}
	}
	return result, nil
}
func (c *claudeStateTestCache) StoreClaudeConversationState(_ context.Context, kind string, values map[string]string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return errors.New("cache unavailable")
	}
	if c.values == nil {
		c.values = map[string]map[string]string{}
	}
	if c.values[kind] == nil {
		c.values[kind] = map[string]string{}
	}
	for key, value := range values {
		if _, exists := c.values[kind][key]; !exists || kind != ClaudeStateSignatures {
			c.values[kind][key] = value
		}
	}
	return nil
}
func claudeStateTestContext(key int64) *gin.Context {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/messages", nil)
	c.Set("api_key", &APIKey{ID: key, UserID: 17})
	return c
}
func claudeStateTestAccount(id int64, identity string) *Account {
	return &Account{ID: id, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"account_uuid": identity, "api_key": "test-key-" + identity}}
}

func TestClaudeConversationSignatureOwnersAndThreadBetas(t *testing.T) {
	cache := &claudeStateTestCache{}
	svc := &GatewayService{cache: cache}
	account := claudeStateTestAccount(1, "owner-a")
	c := claudeStateTestContext(11)
	start := []byte(`{"model":"claude-sonnet-5-5","thread":{"type":"create"},"messages":[{"role":"user","content":"hi"}]}`)
	_, betas := svc.prepareClaudeConversation(t.Context(), c, account, start, "beta-a", nil)
	require.Contains(t, betas, "message-threads-2026-08-12")
	require.Contains(t, betas, "mid-conversation-output-config-2026-07-01")
	observer := upstreamResponseModelObserverFromContext(c)
	observer.ObserveAnthropic([]byte(`{"type":"message_start","message":{"id":"msg-first","model":"claude-sonnet-5-5"}}`))
	observer.ObserveAnthropic([]byte(`{"type":"content_block_start","index":80,"content_block":{"type":"thinking","thinking":"","signature":""}}`))
	observer.ObserveAnthropic([]byte(`{"type":"content_block_delta","index":80,"delta":{"type":"signature_delta","signature":"signed-thinking-"}}`))
	observer.ObserveAnthropic([]byte(`{"type":"content_block_delta","index":80,"delta":{"type":"signature_delta","signature":"from-account-a-0123456789"}}`))
	observer.ObserveAnthropic([]byte(`{"type":"content_block_stop","index":80}`))
	history := []byte(`{"model":"claude-sonnet-5-5","thread":{"type":"continue","previous_message_id":"msg-first"},"messages":[{"role":"user","content":"hi"},{"role":"assistant","content":[{"type":"thinking","thinking":"prior thought","signature":"signed-thinking-from-account-a-0123456789"},{"type":"tool_use","id":"call-1","name":"read","input":{"integer":9007199254740993}}]},{"role":"user","content":[{"type":"tool_result","tool_use_id":"call-1","content":"ok"}]}]}`)
	owned, pinned := svc.prepareClaudeConversation(t.Context(), c, account, history, "beta-b", nil)
	require.Equal(t, "thinking", gjson.GetBytes(owned, "messages.1.content.0.type").String())
	require.True(t, strings.HasPrefix(pinned, "beta-a,"))
	require.Contains(t, pinned, "beta-b")
	require.Contains(t, string(owned), "9007199254740993")
	_, filtered := svc.prepareClaudeConversation(t.Context(), c, account, history, "beta-b", map[string]struct{}{"beta-a": {}})
	require.NotContains(t, filtered, "beta-a", "administrator beta drops override inherited flags")

	other := claudeStateTestAccount(2, "owner-b")
	foreign, unpinned := svc.prepareClaudeConversation(t.Context(), c, other, history, "beta-b", nil)
	require.Equal(t, "text", gjson.GetBytes(foreign, "messages.1.content.0.type").String())
	require.Equal(t, "prior thought", gjson.GetBytes(foreign, "messages.1.content.0.text").String())
	require.Equal(t, "tool_use", gjson.GetBytes(foreign, "messages.1.content.1.type").String())
	require.Equal(t, "9007199254740993", gjson.GetBytes(foreign, "messages.1.content.1.input.integer").Raw)
	require.NotContains(t, unpinned, "beta-a")
	crossKey, noPin := svc.prepareClaudeConversation(t.Context(), claudeStateTestContext(12), account, history, "beta-b", nil)
	require.Equal(t, "text", gjson.GetBytes(crossKey, "messages.1.content.0.type").String())
	require.NotContains(t, noPin, "beta-a")
	changedModel := []byte(strings.ReplaceAll(string(history), "claude-sonnet-5-5", "claude-opus-5-5"))
	_, noPin = svc.prepareClaudeConversation(t.Context(), c, account, changedModel, "beta-b", nil)
	require.NotContains(t, noPin, "beta-a")
	cache.unavailable = true
	intact, _ := svc.prepareClaudeConversation(t.Context(), c, other, history, "beta-b", nil)
	require.JSONEq(t, string(history), string(intact), "cache outage must not invent ownership")
}

func TestClaudeConversationUnknownSignaturesAndRawControls(t *testing.T) {
	svc := &GatewayService{cache: &claudeStateTestCache{}}
	account := claudeStateTestAccount(1, "owner")
	c := claudeStateTestContext(11)
	body := []byte(`{"model":"claude-sonnet-5-5","messages":[{"role":"user","content":"first"},{"role":"assistant","content":[{"type":"redacted_thinking","data":"unrecognized-signed-data-0123456789"}]},{"role":"user","content":"second"},{"role":"assistant","output_config":{"effort":"high"},"content":[{"type":"redacted_thinking","data":"unrecognized-signed-data-0123456789"}]}]}`)
	out, _ := svc.prepareClaudeConversation(t.Context(), c, account, body, "", nil)
	require.Len(t, gjson.GetBytes(out, "messages").Array(), 2)
	require.Equal(t, "first", gjson.GetBytes(out, "messages.0.content.0.text").String())
	require.Equal(t, "second", gjson.GetBytes(out, "messages.0.content.1.text").String())
	require.Equal(t, "high", gjson.GetBytes(out, "messages.1.output_config.effort").String())
	require.NotContains(t, string(out), "unrecognized-signed-data")
	// Unsigned blocks belong to the existing thinking rectifier.
	unsigned := []byte(`{"model":"claude-sonnet-5-5","messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"unsigned","signature":"short"}]}]}`)
	out, _ = svc.prepareClaudeConversation(t.Context(), c, account, unsigned, "", nil)
	require.JSONEq(t, string(unsigned), string(out))
	// More than one cache batch must not turn off ownership filtering.
	blocks := make([]map[string]string, 130)
	for n := range blocks {
		blocks[n] = map[string]string{"type": "thinking", "thinking": "reason", "signature": strings.Repeat("x", 30) + string(rune(n+32))}
	}
	raw, err := json.Marshal(map[string]any{"model": "claude-sonnet-5-5", "messages": []any{map[string]any{"role": "assistant", "content": blocks}}})
	require.NoError(t, err)
	out, _ = svc.prepareClaudeConversation(t.Context(), c, account, raw, "", nil)
	for _, block := range gjson.GetBytes(out, "messages.0.content").Array() {
		require.Equal(t, "text", block.Get("type").String())
	}
}

func TestClaudeConversationPassthroughWireUsesFinalBetas(t *testing.T) {
	svc := &GatewayService{cache: &claudeStateTestCache{}, cfg: &config.Config{}}
	account := claudeStateTestAccount(1, "owner")
	c := claudeStateTestContext(11)
	c.Request.Header.Set("anthropic-beta", "first-beta")
	body := []byte(`{"model":"claude-sonnet-5-5","thread":{"type":"create"},"messages":[{"role":"user","content":"hi"}]}`)
	request, _, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(t.Context(), c, account, body, "test")
	require.NoError(t, err)
	require.Contains(t, getTokenListHeader(request.Header, "anthropic-beta"), "message-threads-2026-08-12")
	observer := upstreamResponseModelObserverFromContext(c)
	require.NotNil(t, observer.claudeConversation)
	observer.ObserveAnthropic([]byte(`{"type":"message","id":"msg-plain","model":"claude-sonnet-5-5","content":[{"type":"thinking","thinking":"reason","signature":"non-streamed-signature-0123456789"}]}`))
	require.Equal(t, splitClaudeBetas(getTokenListHeader(request.Header, "anthropic-beta")), observer.claudeConversation.betas)
	continuation := []byte(`{"model":"claude-sonnet-5-5","thread":{"type":"continue","previous_message_id":"msg-plain"},"messages":[{"role":"assistant","content":[{"type":"thinking","thinking":"reason","signature":"non-streamed-signature-0123456789"}]},{"role":"user","content":"continue"}]}`)
	request, wire, err := svc.buildUpstreamRequestAnthropicAPIKeyPassthrough(t.Context(), c, account, continuation, "test")
	require.NoError(t, err)
	require.Equal(t, "thinking", gjson.GetBytes(wire, "messages.0.content.0.type").String())
	require.Contains(t, getTokenListHeader(request.Header, "anthropic-beta"), "first-beta")
}
