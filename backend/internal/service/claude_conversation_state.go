package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const (
	ClaudeStateSignatures        = "signatures"
	ClaudeStateThreads           = "threads"
	claudeStateTimeout           = 200 * time.Millisecond
	claudeStateMaxSignatures     = 64
	claudeStateMaxSignatureBytes = 1 << 20
)

func mergePlainClaudeConversationTurns(messages []json.RawMessage) []json.RawMessage {
	plain := func(raw json.RawMessage) bool {
		count := 0
		gjson.ParseBytes(raw).ForEach(func(key, value gjson.Result) bool { count++; return true })
		return count == 2 && gjson.GetBytes(raw, "role").Exists() && gjson.GetBytes(raw, "content").Exists()
	}
	blocks := func(raw json.RawMessage) []json.RawMessage {
		content := gjson.GetBytes(raw, "content")
		if content.Type == gjson.String {
			b, _ := json.Marshal(map[string]string{"type": "text", "text": content.String()})
			return []json.RawMessage{b}
		}
		var out []json.RawMessage
		_ = json.Unmarshal([]byte(content.Raw), &out)
		return out
	}
	out := make([]json.RawMessage, 0, len(messages))
	for _, message := range messages {
		if len(out) > 0 && plain(out[len(out)-1]) && plain(message) && gjson.GetBytes(out[len(out)-1], "role").String() == gjson.GetBytes(message, "role").String() {
			combined := append(blocks(out[len(out)-1]), blocks(message)...)
			data, _ := json.Marshal(combined)
			if merged, err := sjson.SetRawBytes(out[len(out)-1], "content", data); err == nil {
				out[len(out)-1] = merged
				continue
			}
		}
		out = append(out, message)
	}
	return out
}

// The existing gateway cache owns bounded, expiring provider observations.
// Keys are digests scoped to the authenticated caller, never client-chosen Redis keys.
type ClaudeConversationStateCache interface {
	GetClaudeConversationState(context.Context, string, []string) (map[string]string, error)
	StoreClaudeConversationState(context.Context, string, map[string]string) error
}

type claudeThreadState struct {
	Owner, Model string
	Betas        []string
}

type claudeConversationObservation struct {
	cache                         ClaudeConversationStateCache
	ctx                           context.Context
	scope, owner, model, threadID string
	betas                         []string
	thread                        bool
	signatures                    map[int]string
}

func claudeStateDigest(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
func claudeSignatureKey(signature string) string {
	signature = strings.TrimSpace(signature)
	if len(signature) < 24 {
		return ""
	}
	material := signature
	if len(material) > 200 {
		material = material[:100] + material[len(material)-100:]
	}
	return claudeStateDigest(fmt.Sprintf("%d:%s", len(signature), material))
}
func (o *claudeConversationObservation) signatureKey(block gjson.Result) string {
	raw := ""
	switch block.Get("type").String() {
	case "thinking":
		raw = block.Get("signature").String()
	case "redacted_thinking":
		raw = block.Get("data").String()
	}
	if key := claudeSignatureKey(raw); key != "" {
		return claudeStateDigest(o.scope + ":" + key)
	}
	return ""
}
func claudeConversationScope(c *gin.Context) string {
	if c == nil {
		return ""
	}
	value, ok := c.Get("api_key")
	if !ok {
		return ""
	}
	key, ok := value.(*APIKey)
	if !ok || key == nil || key.ID <= 0 {
		return ""
	}
	return fmt.Sprintf("%d:%d:%d", key.UserID, key.ID, derefGroupID(key.GroupID))
}

func (s *GatewayService) prepareClaudeConversation(ctx context.Context, c *gin.Context, account *Account, body []byte, beta string, dropSet map[string]struct{}) ([]byte, string) {
	model := gjson.GetBytes(body, "model").String()
	if account == nil || account.Platform != PlatformAnthropic || !strings.HasPrefix(strings.ToLower(model), "claude-") {
		return body, beta
	}
	_, overridden := account.HeaderOverrideValue("anthropic-beta")
	if !overridden {
		beta = stripBetaTokensWithSet(claudeThreadRequestBetas(body, model, beta), dropSet)
	}
	if s == nil || s.cache == nil {
		return body, beta
	}
	scope := claudeConversationScope(c)
	if scope == "" {
		return body, beta
	}
	owner := claudeStateDigest(fmt.Sprintf("%d:%s", account.ID, anthropicCredentialIdentity(account)))
	o := &claudeConversationObservation{cache: s.cache, ctx: ctx, scope: scope, owner: owner, model: model, thread: gjson.GetBytes(body, "thread").IsObject(), signatures: make(map[int]string)}
	body = o.filterThinking(body)
	if o.thread {
		current := splitClaudeBetas(beta)
		if !overridden && gjson.GetBytes(body, "thread.type").String() == "continue" {
			previous := gjson.GetBytes(body, "thread.previous_message_id").String()
			if previous != "" && len(previous) <= 512 {
				key := claudeStateDigest(scope + ":" + previous)
				lookup, cancel := context.WithTimeout(ctx, claudeStateTimeout)
				values, err := o.cache.GetClaudeConversationState(lookup, ClaudeStateThreads, []string{key})
				cancel()
				var old claudeThreadState
				if err == nil && json.Unmarshal([]byte(values[key]), &old) == nil && old.Owner == owner && old.Model == model {
					current = splitClaudeBetas(strings.Join(append(old.Betas, current...), ","))
				}
			}
		}
		beta = strings.Join(current, ",")
		if !overridden {
			beta = stripBetaTokensWithSet(beta, dropSet)
		}
		o.betas = splitClaudeBetas(beta)
	}
	observer := upstreamResponseModelObserverFromContext(c)
	if observer == nil {
		observer = beginUpstreamResponseModelObservation(c)
	}
	observer.claudeConversation = o
	return body, beta
}

func splitClaudeBetas(raw string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		if token == "" || len(token) > 200 || strings.ContainsAny(token, "\r\n\x00") || seen[token] {
			continue
		}
		if len(out) >= 64 {
			break
		}
		seen[token] = true
		out = append(out, token)
	}
	return out
}

func (o *claudeConversationObservation) filterThinking(body []byte) []byte {
	keys := []string{}
	seen := map[string]bool{}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() != "assistant" {
			continue
		}
		for _, block := range message.Get("content").Array() {
			if key := o.signatureKey(block); key != "" && !seen[key] {
				seen[key] = true
				keys = append(keys, key)
			}
		}
	}
	if len(keys) == 0 {
		return body
	}
	ctx, cancel := context.WithTimeout(o.ctx, claudeStateTimeout)
	owners := make(map[string]string)
	var err error
	for offset := 0; offset < len(keys); offset += 64 {
		var chunk map[string]string
		chunk, err = o.cache.GetClaudeConversationState(ctx, ClaudeStateSignatures, keys[offset:min(offset+64, len(keys))])
		if err != nil {
			break
		}
		for key, owner := range chunk {
			owners[key] = owner
		}
	}
	cancel()
	if err != nil {
		return body
	} // An outage is not evidence of foreign ownership.
	changed := false
	messages := []json.RawMessage{}
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		raw := []byte(message.Raw)
		if message.Get("role").String() == "assistant" && message.Get("content").IsArray() {
			blocks := []json.RawMessage{}
			modified := false
			for _, block := range message.Get("content").Array() {
				key := o.signatureKey(block)
				if key == "" || owners[key] == o.owner {
					blocks = append(blocks, json.RawMessage(block.Raw))
					continue
				}
				modified = true
				if text := block.Get("thinking").String(); block.Get("type").String() == "thinking" && strings.TrimSpace(text) != "" {
					value, _ := json.Marshal(map[string]string{"type": "text", "text": text})
					blocks = append(blocks, value)
				}
			}
			if modified {
				changed = true
				if len(blocks) == 0 {
					// Preserve per-turn control objects while dropping a wholly redacted turn.
					controls := false
					message.ForEach(func(key, value gjson.Result) bool {
						if key.String() != "role" && key.String() != "content" {
							controls = true
						}
						return true
					})
					if !controls {
						continue
					}
					empty, _ := json.Marshal(map[string]string{"type": "text", "text": "\u200b"})
					blocks = append(blocks, empty)
				}
				encoded, _ := json.Marshal(blocks)
				raw, err = sjson.SetRawBytes(raw, "content", encoded)
				if err != nil {
					return body
				}
			}
		}
		messages = append(messages, raw)
	}
	if !changed {
		return body
	}
	encoded, _ := json.Marshal(mergePlainClaudeConversationTurns(messages))
	result, err := sjson.SetRawBytes(body, "messages", encoded)
	if err != nil {
		return body
	}
	return result
}

func (o *claudeConversationObservation) store(kind string, values map[string]string) {
	if o == nil || len(values) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(o.ctx), claudeStateTimeout)
	defer cancel()
	_ = o.cache.StoreClaudeConversationState(ctx, kind, values)
}
func (o *claudeConversationObservation) rememberThread(id string) {
	if !o.thread || id == "" || len(id) > 512 || id == o.threadID {
		return
	}
	o.threadID = id
	raw, _ := json.Marshal(claudeThreadState{Owner: o.owner, Model: o.model, Betas: o.betas})
	o.store(ClaudeStateThreads, map[string]string{claudeStateDigest(o.scope + ":" + id): string(raw)})
}
func (o *claudeConversationObservation) rememberSignature(signature string) {
	if key := claudeSignatureKey(signature); key != "" {
		o.store(ClaudeStateSignatures, map[string]string{claudeStateDigest(o.scope + ":" + key): o.owner})
	}
}
func (o *claudeConversationObservation) observe(payload []byte) {
	if o == nil || !gjson.ValidBytes(payload) {
		return
	}
	root := gjson.ParseBytes(payload)
	switch root.Get("type").String() {
	case "message_start":
		o.rememberThread(root.Get("message.id").String())
	case "message", "":
		if !root.Get("content").IsArray() {
			return
		}
		o.rememberThread(root.Get("id").String())
		values := map[string]string{}
		for _, block := range root.Get("content").Array() {
			if key := o.signatureKey(block); key != "" {
				values[key] = o.owner
			}
			if len(values) == claudeStateMaxSignatures {
				o.store(ClaudeStateSignatures, values)
				values = map[string]string{}
			}
		}
		o.store(ClaudeStateSignatures, values)
	case "content_block_start":
		index := int(root.Get("index").Int())
		if index < 0 || len(o.signatures) >= claudeStateMaxSignatures {
			return
		}
		block := root.Get("content_block")
		if block.Get("type").String() == "redacted_thinking" {
			o.rememberSignature(block.Get("data").String())
		}
		if block.Get("type").String() == "thinking" {
			signature := block.Get("signature").String()
			if len(signature) <= claudeStateMaxSignatureBytes {
				o.signatures[index] = signature
			}
		}
	case "content_block_delta":
		index := int(root.Get("index").Int())
		if prefix, ok := o.signatures[index]; ok && root.Get("delta.type").String() == "signature_delta" {
			next := root.Get("delta.signature").String()
			if len(prefix)+len(next) > claudeStateMaxSignatureBytes {
				delete(o.signatures, index)
			} else {
				o.signatures[index] = prefix + next
			}
		}
	case "content_block_stop":
		index := int(root.Get("index").Int())
		if signature, ok := o.signatures[index]; ok {
			o.rememberSignature(signature)
			delete(o.signatures, index)
		}
	}
}

// Thread continuation gates are requested only on models already in the native
// capability catalog. Explicit account header overrides remain authoritative.
func claudeThreadRequestBetas(body []byte, model, beta string) string {
	if !gjson.GetBytes(body, "thread").IsObject() {
		return beta
	}
	tokens := append(splitClaudeBetas(beta), "message-threads-2026-08-12")
	if claude.ModelSupportsMidConversationOutputConfig(model) {
		tokens = append(tokens, claude.BetaMidConversationOutputConfig)
	}
	return strings.Join(splitClaudeBetas(strings.Join(tokens, ",")), ",")
}
