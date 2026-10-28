package service

import (
	"bytes"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/cespare/xxhash/v2"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ccVersionInBillingRe matches the semver part of cc_version (X.Y.Z).
var ccVersionInBillingRe = regexp.MustCompile(`cc_version=\d+\.\d+\.\d+`)

var ccVersionWithFingerprintInBillingRe = regexp.MustCompile(`cc_version=\d+\.\d+\.\d+\.[0-9a-fA-F]{3}\b`)

// captureClaudeBillingSource saves original user text before system migration.
// Adapted from sub4api a3eaf63afc672ffedf7844e6cfc5cb6750d52a3e; preserve the
// genuine client suffix when versions match, and retain local versioned CCH.
const claudeBillingSourceTextKey = "claude_billing_source_text"

func captureClaudeBillingSource(c *gin.Context, body []byte) {
	if c != nil {
		if _, exists := c.Get(claudeBillingSourceTextKey); !exists {
			c.Set(claudeBillingSourceTextKey, extractFirstUserText(body))
		}
	}
}

func billingSourceText(body []byte, c *gin.Context) string {
	if c != nil {
		if saved, ok := c.Get(claudeBillingSourceTextKey); ok {
			if text, ok := saved.(string); ok {
				return text
			}
		}
	}
	return extractFirstUserText(body)
}

// Match the outgoing UA: verified mimic profile, applied account fingerprint,
// then client UA when fingerprint unification is disabled or unavailable.
func effectiveBillingUserAgent(mimicUserAgent, clientUserAgent, tokenType string, mimicClaudeCode bool, fingerprint *Fingerprint) string {
	if tokenType != "oauth" {
		return ""
	}
	if mimicClaudeCode {
		return mimicUserAgent
	}
	if fingerprint != nil && ExtractCLIVersion(fingerprint.UserAgent) != "" {
		return fingerprint.UserAgent
	}
	if isAcceptableFingerprintUserAgent(clientUserAgent) {
		return clientUserAgent
	}
	return ""
}

// ensureClaudeCodeBillingBlock fills missing attribution for real CC OAuth
// traffic. Existing system content/cache controls retain their bytes and order.
func ensureClaudeCodeBillingBlock(body []byte, userAgent string, c *gin.Context) []byte {
	version := ExtractCLIVersion(userAgent)
	if version == "" {
		return body
	}
	system := gjson.GetBytes(body, "system")
	if system.IsArray() {
		for _, item := range system.Array() {
			if strings.HasPrefix(item.Get("text").String(), "x-anthropic-billing-header") {
				return body
			}
		}
	} else if system.Exists() && system.Type != gjson.String && system.Type != gjson.Null {
		return body
	}
	text := fmt.Sprintf("x-anthropic-billing-header: cc_version=%s.%s; cc_entrypoint=cli;", version, computeClaudeCodeFingerprintFromText(billingSourceText(body, c), version))
	block, err := marshalAnthropicSystemTextBlockWithCacheControl(text, nil)
	if err != nil {
		return body
	}
	items := [][]byte{block}
	if system.IsArray() {
		for _, item := range system.Array() {
			items = append(items, []byte(item.Raw))
		}
	} else if system.Type == gjson.String {
		original, err := marshalAnthropicSystemTextBlockWithCacheControl(system.String(), nil)
		if err != nil {
			return body
		}
		items = append(items, original)
	}
	if updated, ok := setJSONRawBytes(body, "system", buildJSONArrayRaw(items)); ok {
		return updated
	}
	return body
}

// syncBillingHeaderVersion rewrites cc_version in x-anthropic-billing-header
// system text blocks to match the version extracted from userAgent.
// Recompute a recognized suffix only when changing the version. The CLI derives
// it before merging internal system-reminder messages into the wire request, so
// recomputing an already matching version can corrupt a genuine client suffix.
// Only touches system array blocks whose text starts with "x-anthropic-billing-header".
func syncBillingHeaderVersion(body []byte, userAgent string, c *gin.Context) []byte {
	version := ExtractCLIVersion(userAgent)
	if version == "" {
		return body
	}

	systemResult := gjson.GetBytes(body, "system")
	if !systemResult.Exists() || !systemResult.IsArray() {
		return body
	}

	replacement := "cc_version=" + version
	idx := 0
	systemResult.ForEach(func(_, item gjson.Result) bool {
		text := item.Get("text")
		if text.Exists() && text.Type == gjson.String &&
			strings.HasPrefix(text.String(), "x-anthropic-billing-header") &&
			ccVersionInBillingRe.FindString(text.String()) != replacement {
			fingerprintedReplacement := replacement + "." + computeClaudeCodeFingerprintFromText(billingSourceText(body, c), version)
			newText := ccVersionWithFingerprintInBillingRe.ReplaceAllString(text.String(), fingerprintedReplacement)
			newText = ccVersionInBillingRe.ReplaceAllString(newText, replacement)
			if newText != text.String() {
				if updated, err := sjson.SetBytes(body, fmt.Sprintf("system.%d.text", idx), newText); err == nil {
					body = updated
				}
			}
		}
		idx++
		return true
	})

	return body
}

// Claude Code 2.1.301 native CCH uses XXH64 over the serialized request, with
// the billing slot zeroed and selected byte ranges omitted. This is versioned
// independently of the configurable UA: an unverified future CLI is not signed
// with a guessed algorithm. See docs/CLAUDE_REQUEST_FINGERPRINT.md for captures.
const claudeCCHVersion = claude.CLICurrentVersion
const claudeCCHSeed uint64 = 0x4d659218e32a3268

var cchInBillingRe = regexp.MustCompile(`(?:^|; *)cch=([0-9a-fA-F]{5});`)
var cchFieldInBillingRe = regexp.MustCompile(`(?:^|; *)cch=[^;]*(?:;|$)`)

// Generated requests use the complete verified profile, independently of the
// discovered/configured version used to track genuine client identities.
func claudeCodeMimicUserAgent() string {
	return "claude-cli/" + claudeCCHVersion + " (external, cli)"
}

// finalizeClaudeCCH runs after every body rewrite, before creating the request.
// For the official Messages API, it signs verified billing versions and removes
// unverified client CCH after potential body rewrites. It never injects a system
// prompt or touches a user/tool string.
func finalizeClaudeCCH(body []byte, targetURL string) []byte {
	target, err := url.Parse(targetURL)
	if err != nil || target.Scheme != "https" || !strings.EqualFold(target.Hostname(), "api.anthropic.com") ||
		(target.Port() != "" && target.Port() != "443") ||
		(target.Path != "/v1/messages" && target.Path != "/v1/messages/count_tokens") {
		return body
	}
	system := gjson.GetBytes(body, "system")
	if !system.IsArray() {
		return body
	}
	path, billing := "", ""
	for i, item := range system.Array() {
		text := item.Get("text")
		if item.Get("type").String() == "text" && text.Type == gjson.String &&
			strings.HasPrefix(text.String(), "x-anthropic-billing-header:") {
			if ccVersionInBillingRe.FindString(text.String()) != "cc_version="+claudeCCHVersion {
				// This forwarding path may have rewritten metadata/messages already.
				// Never forward an unverifiable client checksum as if still valid.
				cleaned := cchFieldInBillingRe.ReplaceAllString(text.String(), ";")
				if cleaned != text.String() {
					if updated, err := sjson.SetBytes(body, fmt.Sprintf("system.%d.text", i), cleaned); err == nil {
						body = updated
					}
				}
				continue
			}
			path, billing = fmt.Sprintf("system.%d.text", i), text.String()
			break
		}
	}
	if path == "" {
		return body
	}
	if match := cchInBillingRe.FindStringSubmatchIndex(billing); match != nil {
		billing = billing[:match[2]] + "00000" + billing[match[3]:]
	} else {
		if strings.Contains(billing, "cch=") {
			return body
		}
		entrypoint := strings.Index(billing, "cc_entrypoint=")
		if entrypoint < 0 {
			return body
		}
		end := strings.IndexByte(billing[entrypoint:], ';')
		if end < 0 {
			return body
		}
		end += entrypoint + 1
		billing = billing[:end] + " cch=00000;" + billing[end:]
	}
	placeholder, err := sjson.SetBytes(body, path, billing)
	if err != nil {
		return body
	}
	text := gjson.GetBytes(placeholder, path)
	match := cchInBillingRe.FindStringSubmatchIndex(text.Raw)
	if match == nil {
		return body
	}
	slot := text.Index + match[2]
	hash := xxhash.NewWithSeed(claudeCCHSeed)
	_, _ = hash.Write(claudeCCHHashInput(placeholder))
	copy(placeholder[slot:slot+5], fmt.Sprintf("%05x", hash.Sum64()&0xfffff))
	return placeholder
}

// These exclusions intentionally match the native byte scanner, not a JSON
// reserialization: omit model VALUE bytes, and the entire compact fallbacks,
// fallback_credit_token and integer max_tokens properties with one comma.
// Strings, whitespace, key order and all other bytes retain their exact form.
func claudeCCHHashInput(body []byte) []byte {
	out := make([]byte, 0, len(body))
	from := 0
	start, end, found := nextClaudeCCHExcluded(body, from)
	for from < len(body) {
		model := bytes.Index(body[from:], []byte(`"model":"`))
		if model >= 0 {
			model += from
		}
		if found && (model < 0 || start <= model) {
			out = append(out, body[from:start]...)
			from = end
			start, end, found = nextClaudeCCHExcluded(body, from)
			continue
		}
		if model >= 0 {
			value := model + len(`"model":"`)
			if quote := bytes.IndexByte(body[value:], '"'); quote >= 0 {
				out = append(out, body[from:value]...)
				from = value + quote
				// Malformed input can place an exclusion inside the skipped value.
				if found && start < from {
					start, end, found = nextClaudeCCHExcluded(body, from)
				}
				continue
			}
		}
		out = append(out, body[from:]...)
		break
	}
	return out
}

func nextClaudeCCHExcluded(body []byte, from int) (start, end int, found bool) {
	choose := func(s, e int) {
		if e < len(body) && body[e] == ',' {
			e++
		} else if s > from && body[s-1] == ',' {
			s--
		}
		if !found || s < start {
			start, end, found = s, e, true
		}
	}
	const arrayKey = `"fallbacks":[`
	if k := bytes.Index(body[from:], []byte(arrayKey)); k >= 0 {
		s, depth, quoted := from+k, 1, false
		for i := s + len(arrayKey); i < len(body); i++ {
			c := body[i]
			if quoted {
				if c == '\\' && i+1 < len(body) {
					i++
					continue
				}
				if c == '"' {
					quoted = false
				}
				continue
			}
			switch c {
			case '"':
				quoted = true
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					choose(s, i+1)
				}
			}
			if depth == 0 {
				break
			}
		}
	}
	const creditKey = `"fallback_credit_token":"`
	if k := bytes.Index(body[from:], []byte(creditKey)); k >= 0 {
		s := from + k
		value := s + len(creditKey)
		if e := bytes.IndexByte(body[value:], '"'); e >= 0 {
			choose(s, value+e+1)
		}
	}
	const maxKey = `"max_tokens":`
	for cursor := from; cursor < len(body); {
		k := bytes.Index(body[cursor:], []byte(maxKey))
		if k < 0 {
			break
		}
		s := cursor + k
		value := s + len(maxKey)
		e := value
		for e < len(body) && body[e] >= '0' && body[e] <= '9' {
			e++
		}
		if e != value {
			choose(s, e)
			break
		}
		cursor = value
	}
	return
}
