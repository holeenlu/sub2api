package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/tidwall/gjson"
)

const (
	ContentModerationFailOpen      = "fail_open"
	ContentModerationFailClosed    = "fail_closed"
	maxModerationGuardEntries      = 4096
	maxModerationGuardPatterns     = 64
	maxModerationGuardPatternBytes = 512
)

// Guard settings extend the existing content audit policy and its scope. Empty
// rule lists really are empty; no hidden provider exemptions or fallback lists.
type ContentModerationDistillationConfig struct {
	Enabled           bool     `json:"enabled"`
	Phrases           []string `json:"phrases"`
	Patterns          []string `json:"patterns"`
	Fingerprints      []string `json:"fingerprints"`
	MinMaxTokens      int      `json:"min_max_tokens"`
	MinThinkingTokens int      `json:"min_thinking_tokens"`
	StructurePattern  string   `json:"structure_pattern"`
}

type ContentModerationRefusalGuardConfig struct {
	Enabled             bool    `json:"enabled"`
	TTLSeconds          int     `json:"ttl_seconds"`
	MaxEntries          int     `json:"max_entries"`
	SimilarityThreshold float64 `json:"similarity_threshold"`
}

type ContentModerationDeviceGuardConfig struct {
	Enabled    bool `json:"enabled"`
	TTLSeconds int  `json:"ttl_seconds"`
	MaxEntries int  `json:"max_entries"`
}

func (cfg *ContentModerationConfig) normalizeGuards() {
	if cfg.FailurePolicy == "" {
		cfg.FailurePolicy = ContentModerationFailOpen
	}
	if cfg.RefusalGuard.TTLSeconds == 0 {
		cfg.RefusalGuard.TTLSeconds = 3600
	}
	if cfg.RefusalGuard.MaxEntries == 0 {
		cfg.RefusalGuard.MaxEntries = 1024
	}
	if cfg.RefusalGuard.SimilarityThreshold == 0 {
		cfg.RefusalGuard.SimilarityThreshold = .9
	}
	if cfg.DeviceGuard.TTLSeconds == 0 {
		cfg.DeviceGuard.TTLSeconds = 900
	}
	if cfg.DeviceGuard.MaxEntries == 0 {
		cfg.DeviceGuard.MaxEntries = 1024
	}
	if cfg.Distillation.MinMaxTokens == 0 {
		cfg.Distillation.MinMaxTokens = 8192
	}
	if cfg.Distillation.MinThinkingTokens == 0 {
		cfg.Distillation.MinThinkingTokens = 8192
	}
	if cfg.Distillation.Phrases == nil {
		cfg.Distillation.Phrases = []string{}
	}
	if cfg.Distillation.Patterns == nil {
		cfg.Distillation.Patterns = []string{}
	}
	if cfg.Distillation.Fingerprints == nil {
		cfg.Distillation.Fingerprints = []string{}
	}
	if cfg.RegexRules == nil {
		cfg.RegexRules = []string{}
	}
}

type contentModerationGuardRules struct {
	distillation []*regexp.Regexp
	hard         []*regexp.Regexp
	structure    *regexp.Regexp
}

func compileContentModerationGuards(cfg *ContentModerationConfig) (*contentModerationGuardRules, error) {
	invalid := func(message string) error {
		return infraerrors.BadRequest("INVALID_CONTENT_MODERATION_GUARDS", message)
	}
	if cfg.FailurePolicy != ContentModerationFailOpen && cfg.FailurePolicy != ContentModerationFailClosed {
		return nil, invalid("failure_policy must be fail_open or fail_closed")
	}
	if cfg.RefusalGuard.TTLSeconds < 1 || cfg.RefusalGuard.TTLSeconds > 604800 || cfg.DeviceGuard.TTLSeconds < 1 || cfg.DeviceGuard.TTLSeconds > 86400 {
		return nil, invalid("refusal TTL must be 1..604800 seconds; device TTL must be 1..86400 seconds")
	}
	for _, cap := range []int{cfg.RefusalGuard.MaxEntries, cfg.DeviceGuard.MaxEntries} {
		if cap < 1 || cap > maxModerationGuardEntries {
			return nil, invalid("guard capacity must be 1..4096")
		}
	}
	threshold := cfg.RefusalGuard.SimilarityThreshold
	if math.IsNaN(threshold) || math.IsInf(threshold, 0) || threshold < .8 || threshold > 1 {
		return nil, invalid("refusal similarity must be 0.8..1 (1 means exact only)")
	}
	if cfg.Distillation.MinMaxTokens < 1 || cfg.Distillation.MinMaxTokens > 10000000 || cfg.Distillation.MinThinkingTokens < 1 || cfg.Distillation.MinThinkingTokens > 10000000 {
		return nil, invalid("distillation token thresholds must be 1..10000000")
	}
	compile := func(patterns []string) ([]*regexp.Regexp, error) {
		if len(patterns) > maxModerationGuardPatterns {
			return nil, invalid("at most 64 patterns per rule list")
		}
		out := make([]*regexp.Regexp, 0, len(patterns))
		for _, pattern := range patterns {
			if strings.TrimSpace(pattern) == "" || len(pattern) > maxModerationGuardPatternBytes {
				return nil, invalid("patterns must contain 1..512 bytes")
			}
			re, err := regexp.Compile(pattern)
			if err != nil {
				return nil, invalid("invalid RE2 pattern: " + err.Error())
			}
			out = append(out, re)
		}
		return out, nil
	}
	if len(cfg.Distillation.Phrases) > 256 || len(cfg.Distillation.Fingerprints) > 256 {
		return nil, invalid("at most 256 distillation phrases or fingerprints")
	}
	for _, phrase := range cfg.Distillation.Phrases {
		if strings.TrimSpace(phrase) == "" || len(phrase) > 512 {
			return nil, invalid("distillation phrases must contain 1..512 bytes")
		}
	}
	for _, fingerprint := range cfg.Distillation.Fingerprints {
		if strings.TrimSpace(fingerprint) == "" || len(fingerprint) > 512 {
			return nil, invalid("distillation fingerprints must be SHA-256 hex or 1..512 byte text fragments")
		}
	}
	out := &contentModerationGuardRules{}
	var err error
	if out.hard, err = compile(cfg.RegexRules); err != nil {
		return nil, err
	}
	if out.distillation, err = compile(cfg.Distillation.Patterns); err != nil {
		return nil, err
	}
	if cfg.Distillation.StructurePattern != "" {
		patterns, err := compile([]string{cfg.Distillation.StructurePattern})
		if err != nil {
			return nil, err
		}
		out.structure = patterns[0]
	}
	return out, nil
}

func (rules *contentModerationGuardRules) match(cfg *ContentModerationConfig, input ContentModerationCheckInput, text string) (string, string) {
	if rules == nil || text == "" {
		return "", ""
	}
	if cfg.Distillation.Enabled {
		lower := strings.ToLower(text)
		for _, phrase := range cfg.Distillation.Phrases {
			if strings.Contains(lower, strings.ToLower(normalizeContentModerationText(phrase))) {
				return "distillation", phrase
			}
		}
		digest := sha256.Sum256([]byte(text))
		fingerprint := hex.EncodeToString(digest[:])
		for _, expected := range cfg.Distillation.Fingerprints {
			if fingerprint == normalizeContentModerationHash(expected) || (normalizeContentModerationHash(expected) == "" && strings.Contains(lower, strings.ToLower(normalizeContentModerationText(expected)))) {
				return "distillation", fingerprint
			}
		}
		for _, re := range rules.distillation {
			if re.MatchString(text) {
				return "distillation", re.String()
			}
		}
		maxTokens := int64(0)
		for _, path := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens", "generationConfig.maxOutputTokens"} {
			value := gjson.GetBytes(input.Body, path)
			if value.Type == gjson.Number && value.Int() > maxTokens {
				maxTokens = value.Int()
			}
		}
		thinking := gjson.GetBytes(input.Body, "thinking.budget_tokens")
		effort := gjson.GetBytes(input.Body, "reasoning.effort").String()
		if effort == "" {
			effort = gjson.GetBytes(input.Body, "output_config.effort").String()
		}
		if effort == "" {
			effort = gjson.GetBytes(input.Body, "reasoning_effort").String()
		}
		expensive := thinking.Int() >= int64(cfg.Distillation.MinThinkingTokens) || effort == "high" || effort == "xhigh" || effort == "max" || gjson.GetBytes(input.Body, "n").Int() > 1 || gjson.GetBytes(input.Body, "thinking.display").String() == "summarized"
		messages := gjson.GetBytes(input.Body, "messages")
		if !messages.Exists() {
			messages = gjson.GetBytes(input.Body, "input")
		}
		singleTurn := true
		users := 0
		for _, item := range messages.Array() {
			role := item.Get("role").String()
			if role == "assistant" || role == "tool" {
				singleTurn = false
			}
			if role == "user" {
				users++
			}
		}
		if users > 1 {
			singleTurn = false
		}
		noTools := len(gjson.GetBytes(input.Body, "tools").Array()) == 0
		if rules.structure != nil && maxTokens >= int64(cfg.Distillation.MinMaxTokens) && expensive && singleTurn && noTools && rules.structure.MatchString(text) {
			return "distillation", rules.structure.String()
		}
	}
	for _, re := range rules.hard {
		if re.MatchString(text) {
			return "regex", re.String()
		}
	}
	return "", ""
}

type contentModerationRefusalEntry struct {
	scope      [32]byte
	imagesHash string
	sketch     []uint64
	runes      int
	expires    time.Time
}

// Only digests and bounded sketches are retained. Capacity is global per
// service instance, not per attacker-controlled device or scope.
type contentModerationGuardMemory struct {
	refusals map[[32]byte]contentModerationRefusalEntry
	devices  map[[32]byte]time.Time
}

func moderationGuardScope(input ContentModerationCheckInput) [32]byte {
	// Authentication owns UserID; metadata/user_id and device values never supply it.
	return sha256.Sum256([]byte(fmt.Sprintf("%d/%d/%q/%q/%q", input.UserID, contentModerationLogGroupID(input.GroupID), input.Provider, input.Model, input.Protocol)))
}

func moderationGuardKey(scope [32]byte, value string) [32]byte {
	return sha256.Sum256(append(append([]byte(nil), scope[:]...), []byte(value)...))
}

func moderationRefusalContent(input ContentModerationCheckInput) ContentModerationInput {
	if input.Protocol != ContentModerationProtocolTypeSafeSystemOne {
		return extractContentModerationInput(input.Protocol, input.Body, false)
	}
	// System One's question contracts belong to context, not the text being
	// compared. Canonical state preserves numeric values as well as strings.
	state := gjson.GetBytes(input.Body, "state")
	if !state.Exists() {
		return ContentModerationInput{}
	}
	var value any
	decoder := json.NewDecoder(strings.NewReader(state.Raw))
	decoder.UseNumber()
	if decoder.Decode(&value) != nil {
		return ContentModerationInput{}
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return ContentModerationInput{}
	}
	return ContentModerationInput{Text: string(raw)}
}

// Only the current user content is removed from the context hash. In
// particular, previous turns, instructions, tool results and tool schemas
// remain: identical "continue" messages cannot contaminate other conversations.
// UseNumber preserves large JSON numbers in tool arguments without rounding.
func moderationRefusalScope(input ContentModerationCheckInput) ([32]byte, bool) {
	var body map[string]any
	decoder := json.NewDecoder(bytes.NewReader(input.Body))
	decoder.UseNumber()
	if decoder.Decode(&body) != nil || body == nil {
		return [32]byte{}, false
	}
	for _, key := range []string{"model", "stream", "stream_options", "max_tokens", "max_completion_tokens", "max_output_tokens", "metadata", "device_id", "user", "safety_identifier", "prompt_cache_key", "store", "background", "service_tier"} {
		delete(body, key)
	}
	stripMessage := func(messages any, anthropic bool) {
		array, ok := messages.([]any)
		if !ok || len(array) == 0 {
			return
		}
		i := len(array) - 1
		if anthropic {
			for i >= 0 {
				m, _ := array[i].(map[string]any)
				if m["role"] != "system" {
					break
				}
				i--
			}
		}
		if i < 0 {
			return
		}
		message, _ := array[i].(map[string]any)
		if message["role"] == "user" {
			message["content"] = moderationContextContent(message["content"])
		}
	}
	stripResponses := func() {
		var current map[string]any
		switch value := body["input"].(type) {
		case string:
			body["input"] = nil
			return
		case []any:
			if len(value) > 0 {
				current, _ = value[len(value)-1].(map[string]any)
			}
		case map[string]any:
			current = value
		}
		if current == nil {
			return
		}
		role, _ := current["role"].(string)
		if role != "" && role != "user" {
			return
		}
		switch current["type"] {
		case nil, "", "message", "input_text", "input_image":
		default:
			return
		}
		delete(current, "text")
		if content, exists := current["content"]; exists {
			current["content"] = moderationContextContent(content)
		}
	}
	stripGemini := func() {
		if cfg, ok := body["generationConfig"].(map[string]any); ok {
			delete(cfg, "maxOutputTokens")
			if len(cfg) == 0 {
				delete(body, "generationConfig")
			}
		}
		array, ok := body["contents"].([]any)
		if !ok || len(array) == 0 {
			return
		}
		message, _ := array[len(array)-1].(map[string]any)
		if role, _ := message["role"].(string); role != "" && role != "user" {
			return
		}
		parts, _ := message["parts"].([]any)
		for _, part := range parts {
			if p, ok := part.(map[string]any); ok {
				for _, key := range []string{"text", "inline_data", "inlineData", "file_data", "fileData"} {
					delete(p, key)
				}
			}
		}
	}
	switch input.Protocol {
	case ContentModerationProtocolAnthropicMessages:
		stripMessage(body["messages"], true)
	case ContentModerationProtocolOpenAIChat:
		stripMessage(body["messages"], false)
	case ContentModerationProtocolOpenAIResponses:
		stripResponses()
	case ContentModerationProtocolGemini:
		stripGemini()
	case ContentModerationProtocolOpenAIImages:
		delete(body, "prompt")
		delete(body, "images")
	case ContentModerationProtocolTypeSafeSystemOne:
		delete(body, "state")
	default:
		stripResponses()
		stripMessage(body["messages"], false)
		stripGemini()
	}
	h := sha256.New()
	scope := moderationGuardScope(input)
	_, _ = h.Write(scope[:])
	if json.NewEncoder(h).Encode(body) != nil {
		return [32]byte{}, false
	}
	var digest [32]byte
	copy(digest[:], h.Sum(nil))
	return digest, true
}

func moderationContextContent(value any) any {
	switch content := value.(type) {
	case string:
		return nil
	case []any:
		for i, part := range content {
			content[i] = moderationContextContent(part)
		}
	case map[string]any:
		switch content["type"] {
		case nil, "", "text", "input_text", "message":
			delete(content, "text")
			if child, exists := content["content"]; exists {
				content["content"] = moderationContextContent(child)
			}
		case "image", "input_image", "image_url":
			return map[string]any{"type": content["type"]}
			// Tool results and their nested payloads are context, never current text.
		}
	}
	return value
}

func moderationDeviceKey(input ContentModerationCheckInput) ([32]byte, bool) {
	device := strings.TrimSpace(input.DeviceID)
	if input.UserID <= 0 || len(device) < 8 || len(device) > 256 || !utf8.ValidString(device) || strings.ContainsAny(device, "\x00\r\n\t") {
		return [32]byte{}, false
	}
	return moderationGuardKey(moderationGuardScope(input), device), true
}

// Bottom-k sketches over four-rune shingles cap each entry at 2 KiB. Short
// prompts and prompts over the audit text limit use exact matching only.
func moderationRefusalSketch(text string) ([]uint64, int) {
	n := utf8.RuneCountInString(text)
	if n < 80 || n > maxModerationInputRunes {
		return nil, n
	}
	runes := []rune(strings.ToLower(text))
	unique := make(map[uint64]struct{}, len(runes))
	for i := 0; i+4 <= len(runes); i++ {
		h := sha256.Sum256([]byte(string(runes[i : i+4])))
		unique[binary.BigEndian.Uint64(h[:8])] = struct{}{}
	}
	values := make([]uint64, 0, len(unique))
	for h := range unique {
		values = append(values, h)
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	if len(values) > 256 {
		values = values[:256]
	}
	return values, n
}

func moderationRefusalSimilar(a, b []uint64, an, bn int, threshold float64) bool {
	if len(a) < 32 || len(b) < 32 || float64(min(an, bn))/float64(max(an, bn)) < threshold {
		return false
	}
	i, j, common := 0, 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			common++
			i++
			j++
		case a[i] < b[j]:
			i++
		default:
			j++
		}
	}
	return float64(common)/float64(len(a)+len(b)-common) >= threshold
}

func (s *ContentModerationService) pruneGuardMemoryLocked(now time.Time, cfg *ContentModerationConfig) {
	for key, entry := range s.guardMemory.refusals {
		if !entry.expires.After(now) || (cfg != nil && !cfg.RefusalGuard.Enabled) {
			delete(s.guardMemory.refusals, key)
		}
	}
	for key, expires := range s.guardMemory.devices {
		if !expires.After(now) || (cfg != nil && !cfg.DeviceGuard.Enabled) {
			delete(s.guardMemory.devices, key)
		}
	}
	if cfg == nil {
		return
	}
	for len(s.guardMemory.refusals) > cfg.RefusalGuard.MaxEntries {
		var oldest [32]byte
		var expires time.Time
		for key, entry := range s.guardMemory.refusals {
			if expires.IsZero() || entry.expires.Before(expires) {
				oldest, expires = key, entry.expires
			}
		}
		delete(s.guardMemory.refusals, oldest)
	}
	for len(s.guardMemory.devices) > cfg.DeviceGuard.MaxEntries {
		var oldest [32]byte
		var expires time.Time
		for key, expiry := range s.guardMemory.devices {
			if expires.IsZero() || expiry.Before(expires) {
				oldest, expires = key, expiry
			}
		}
		delete(s.guardMemory.devices, oldest)
	}
}

func (s *ContentModerationService) rememberDeviceLocked(input ContentModerationCheckInput, cfg *ContentModerationConfig, now time.Time) {
	if !cfg.DeviceGuard.Enabled || cfg.Mode != ContentModerationModePreBlock || input.riskControlLogOnly {
		return
	}
	key, ok := moderationDeviceKey(input)
	if !ok {
		return
	}
	if s.guardMemory.devices == nil {
		s.guardMemory.devices = make(map[[32]byte]time.Time)
	}
	// Repeated blocked attempts cannot turn a temporary restriction into a
	// permanent ban. Only a new real violation after expiry starts another TTL.
	if expiry, ok := s.guardMemory.devices[key]; !ok || !expiry.After(now) {
		s.guardMemory.devices[key] = now.Add(time.Duration(cfg.DeviceGuard.TTLSeconds) * time.Second)
		s.guardPending = append(s.guardPending, ContentModerationGuardRecord{Kind: "device", Key: hex.EncodeToString(key[:]), ExpiresMS: s.guardMemory.devices[key].UnixMilli()})
	}
}

// RememberUpstreamRefusal must only be called after IsUpstreamContentRefusal
// confirms an actual upstream refusal. Pass the original request, authenticated
// user, and public model, before any protocol or prompt transformations. Local
// regex/audit failures must never call this method. Scoped state is shared through
// the existing Redis cache, expires, and is cleared by the existing clear API.
func (s *ContentModerationService) RememberUpstreamRefusal(ctx context.Context, input ContentModerationCheckInput) error {
	if s == nil || input.UserID <= 0 {
		return nil
	}
	snapshot, err := s.loadRuntimeSnapshot(ctx)
	if err != nil {
		return err
	}
	cfg := snapshot.config
	if !snapshot.riskControlEnabled || !cfg.Enabled || cfg.Mode == ContentModerationModeOff || !cfg.includesGroup(input.GroupID) || !cfg.includesModel(input.Model) {
		return nil
	}
	if _, allowed := snapshot.allowlistedUsers[input.UserID]; allowed {
		return nil
	}
	if !cfg.RefusalGuard.Enabled && !cfg.DeviceGuard.Enabled {
		return nil
	}
	content := moderationRefusalContent(input)
	if content.IsEmpty() {
		return nil
	}
	scope, valid := moderationRefusalScope(input)
	if !valid {
		return nil
	}
	inputHash := content.Hash()
	entry := contentModerationRefusalEntry{scope: scope, imagesHash: (ContentModerationInput{Images: content.Images}).Hash()}
	if cfg.RefusalGuard.Enabled && cfg.RefusalGuard.SimilarityThreshold < 1 {
		entry.sketch, entry.runes = moderationRefusalSketch(content.Text)
	}
	now := time.Now()
	entry.expires = now.Add(time.Duration(cfg.RefusalGuard.TTLSeconds) * time.Second)
	s.guardMu.Lock()
	if cfg.RefusalGuard.Enabled {
		if s.guardMemory.refusals == nil {
			s.guardMemory.refusals = make(map[[32]byte]contentModerationRefusalEntry)
		}
		key := moderationGuardKey(scope, inputHash)
		s.guardMemory.refusals[key] = entry
		s.guardPending = append(s.guardPending, ContentModerationGuardRecord{Kind: "refusal", Key: hex.EncodeToString(key[:]), Scope: scope, ImagesHash: entry.imagesHash, Sketch: entry.sketch, Runes: entry.runes, ExpiresMS: entry.expires.UnixMilli()})
	}
	s.rememberDeviceLocked(input, cfg, now)
	s.pruneGuardMemoryLocked(now, cfg)
	s.guardMu.Unlock()
	return s.flushGuardMemory(ctx, cfg)
}

func (s *ContentModerationService) checkLocalGuards(ctx context.Context, input ContentModerationCheckInput, snapshot *contentModerationRuntimeSnapshot) *ContentModerationDecision {
	cfg := snapshot.config
	if !cfg.Distillation.Enabled && len(cfg.RegexRules) == 0 && !cfg.RefusalGuard.Enabled && !cfg.DeviceGuard.Enabled {
		return nil
	}
	if cfg.RefusalGuard.Enabled || cfg.DeviceGuard.Enabled {
		if err := s.syncGuardMemory(ctx); err != nil && cfg.FailurePolicy == ContentModerationFailClosed && cfg.Mode == ContentModerationModePreBlock && !input.riskControlLogOnly {
			return &ContentModerationDecision{Blocked: true, Allowed: false, StatusCode: http.StatusServiceUnavailable, Action: ContentModerationActionError, Message: "Content audit temporarily unavailable"}
		}
	}
	defer func() {
		if err := s.flushGuardMemory(ctx, cfg); err != nil {
			slog.Warn("content_moderation.guard_persist_failed")
		}
	}()
	content := extractContentModerationInput(input.Protocol, input.Body, false)
	category, matched := snapshot.guardRules.match(cfg, input, content.Text)
	var scope [32]byte
	var validScope bool
	var sketch []uint64
	var runes int
	var inputHash, imagesHash string
	if category == "" && input.UserID > 0 && cfg.RefusalGuard.Enabled {
		refusalContent := content
		if input.Protocol == ContentModerationProtocolTypeSafeSystemOne {
			refusalContent = moderationRefusalContent(input)
		}
		scope, validScope = moderationRefusalScope(input)
		validScope = validScope && !refusalContent.IsEmpty()
		inputHash = refusalContent.Hash()
		imagesHash = (ContentModerationInput{Images: refusalContent.Images}).Hash()
		if cfg.RefusalGuard.SimilarityThreshold < 1 {
			sketch, runes = moderationRefusalSketch(refusalContent.Text)
		}
	}
	now := time.Now()
	s.guardMu.Lock()
	s.pruneGuardMemoryLocked(now, cfg)
	if category == "" && input.UserID > 0 {
		if key, ok := moderationDeviceKey(input); ok && cfg.DeviceGuard.Enabled && s.guardMemory.devices[key].After(now) {
			category = "device"
		}
		if category == "" && cfg.RefusalGuard.Enabled && validScope {
			if _, ok := s.guardMemory.refusals[moderationGuardKey(scope, inputHash)]; ok {
				category = "refusal_exact"
			}
			if category == "" && cfg.RefusalGuard.SimilarityThreshold < 1 {
				for _, entry := range s.guardMemory.refusals {
					if entry.scope == scope && entry.imagesHash == imagesHash && moderationRefusalSimilar(sketch, entry.sketch, runes, entry.runes, cfg.RefusalGuard.SimilarityThreshold) {
						category = "refusal_similar"
						break
					}
				}
			}
		}
	}
	// Similarity is evidence about text, not a new violation by a device.
	if category == "distillation" || category == "regex" {
		s.rememberDeviceLocked(input, cfg, now)
	}
	s.pruneGuardMemoryLocked(now, cfg)
	s.guardMu.Unlock()
	if category == "" {
		return nil
	}
	blocked := cfg.Mode == ContentModerationModePreBlock && !input.riskControlLogOnly
	action := ContentModerationActionAllow
	if blocked {
		action = ContentModerationActionHashBlock
		if category == "distillation" || category == "regex" {
			action = ContentModerationActionKeywordBlock
		}
	}
	scores := map[string]float64{category: 1}
	logAction := action
	localRule := category == "distillation" || category == "regex"
	if !localRule {
		logAction = ContentModerationActionHashBlock
	}
	log := s.buildLog(input, cfg, logAction, true, category, 1, scores, content.Text, nil, nil, "")
	log.MatchedKeyword = trimRunes(matched, maxContentModerationBlockedKeywordRunes)
	// Local rules reuse keyword violation handling. Cached/device hits never
	// create global hashes or increment native account-ban counters.
	s.enqueueRecord(input, cfg, log, "", false, localRule)
	if cfg.Mode == ContentModerationModePreBlock {
		s.recordPreBlockSyncMetric(0, action)
	}
	decision := &ContentModerationDecision{Allowed: !blocked, Blocked: blocked, Flagged: true, Action: action, HighestCategory: category, HighestScore: 1, CategoryScores: scores}
	if blocked {
		decision.Message, decision.StatusCode = cfg.BlockMessage, cfg.BlockStatus
	}
	return decision
}

func moderationFailureDecision(input ContentModerationCheckInput, cfg *ContentModerationConfig, allowBlock bool) *ContentModerationDecision {
	blocked := allowBlock && cfg.Mode == ContentModerationModePreBlock && cfg.FailurePolicy == ContentModerationFailClosed && !input.riskControlLogOnly
	d := &ContentModerationDecision{Allowed: !blocked, Blocked: blocked, Action: ContentModerationActionError}
	if blocked {
		d.StatusCode, d.Message = http.StatusServiceUnavailable, "Content audit is temporarily unavailable"
	}
	return d
}
