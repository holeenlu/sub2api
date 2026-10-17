package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var claudePromptVariablePattern = regexp.MustCompile(`\{\{?\s*([a-z_]+)\s*\}\}?`)

func (s *GatewayService) claudePromptAccountVariables(ctx context.Context, c *gin.Context, account *Account, body []byte, requests ...*ParsedRequest) map[string]string {
	out := map[string]string{"timezone": "UTC", "locale": "en-US", "model": gjson.GetBytes(body, "model").String()}
	if account == nil {
		return out
	}
	for _, name := range []string{"timezone", "locale"} {
		if value := telemetryCredential(account, name); value != "" {
			out[name] = value
		}
	}
	env := map[string]json.RawMessage{}
	if profile, exists, err := accountTelemetryProfile(account); err == nil && exists {
		if event, err := parseClaudeTelemetryProfile(profile); err == nil {
			if parsed, err := telemetryObject(event.Data["env"]); err == nil && parsed != nil {
				env = parsed
			}
		}
	}
	headers := http.Header{}
	if c != nil && c.Request != nil {
		headers = c.Request.Header
	}
	if fp, err := s.identityService.GetAccountFingerprint(ctx, account, headers); err == nil {
		for name, value := range map[string]string{"platform": strings.ToLower(fp.StainlessOS), "arch": fp.StainlessArch, "runtimes": fp.StainlessRuntime} {
			if _, exists := env[name]; !exists {
				env[name], _ = json.Marshal(value)
			}
		}
		source := body
		parsed := &ParsedRequest{Body: NewRequestBodyRef(body), MetadataUserID: gjson.GetBytes(body, "metadata.user_id").String()}
		if len(requests) > 0 && requests[0] != nil {
			parsed = requests[0]
		}
		passthrough := false
		if s.settingService != nil {
			_, passthrough, _ = s.settingService.GetGatewayForwardingSettings(ctx)
		}
		if !passthrough && parsed.MetadataUserID == "" {
			if uid := s.buildOAuthMetadataUserID(c, parsed, account, fp); uid != "" {
				source, _ = sjson.SetBytes(body, "metadata.user_id", uid)
			}
		}
		if !passthrough {
			if rewritten, err := s.identityService.RewriteUserIDWithMasking(ctx, source, account, telemetryCredential(account, "account_uuid"), fp.ClientID, fp.UserAgent); err == nil {
				source = rewritten
			}
		}
		if id := ParseMetadataUserID(gjson.GetBytes(source, "metadata.user_id").String()); id != nil {
			out["session_id"] = id.SessionID
		}

	}
	cleanTelemetryEnvironment(env)
	for _, name := range []string{"timezone", "locale"} {
		if value := env[name]; len(value) > 0 {
			var text string
			if json.Unmarshal(value, &text) == nil && text != "" {
				out[name] = text
			}
		}
		env[name], _ = json.Marshal(out[name])
	}
	encoded, _ := json.Marshal(env)
	out["env"] = "Environment: " + string(encoded)
	out["env_official"] = out["env"]
	out["env_timezone_only"] = "Timezone: " + out["timezone"]
	out["caller_agent"], _ = extractSystemTextAndCacheControl(normalizeSystemParam(gjson.GetBytes(body, "system").Value()))
	cwd := gjson.GetBytes(body, "cwd").String()
	if len(cwd) <= 512 && !strings.ContainsAny(cwd, "\r\n\x00") {
		out["cwd"] = cwd
	}
	return out
}

// Overlay blocks share the existing template array and renderer. They append
// only to the last user message and never rewrite tool results or output state.
func applyClaudePromptOverlay(body []byte, expansion, config string, variables ...map[string]string) []byte {
	if isAnthropicFableModel(gjson.GetBytes(body, "model").String()) {
		return body
	}
	blocks, err := parseClaudeOAuthSystemPromptBlocksConfig(config)
	if err != nil {
		return body
	}
	lastUser := ""
	for _, message := range gjson.GetBytes(body, "messages").Array() {
		if message.Get("role").String() == "user" {
			value := message.Get("content")
			lastUser = value.String()
		}
	}
	parts := []string{}
	wrapper := ""
	for _, block := range blocks {
		if block.Enabled != nil && !*block.Enabled {
			continue
		}
		switch block.Placement {
		case "overlay_body":
			if block.Match != "" {
				re, err := regexp.Compile(block.Match)
				if err != nil || !re.MatchString(lastUser) {
					continue
				}
			}
			text, err := expandClaudeOAuthSystemPromptTextTemplate(body, block.Text, expansion, variables...)
			if err == nil && strings.TrimSpace(text) != "" {
				parts = append(parts, text)
			}
		case "overlay_wrapper":
			wrapper = block.Text
		}
	}
	if len(parts) == 0 {
		return body
	}
	text := strings.Join(parts, "\n")
	if wrapper != "" {
		values := map[string]string{"overlay_body": text, "rules": text}
		vars := append(append([]map[string]string{}, variables...), values)
		text, err = expandClaudeOAuthSystemPromptTextTemplate(body, wrapper, expansion, vars...)
		if err != nil {
			return body
		}
	}
	messages := gjson.GetBytes(body, "messages").Array()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Get("role").String() != "user" {
			continue
		}
		content := messages[i].Get("content")
		var blocks []json.RawMessage
		if content.Type == gjson.String {
			raw, _ := json.Marshal(map[string]string{"type": "text", "text": content.String()})
			blocks = append(blocks, raw)
		} else if json.Unmarshal([]byte(content.Raw), &blocks) != nil {
			return body
		}
		extra, _ := json.Marshal(map[string]string{"type": "text", "text": text})
		blocks = append(blocks, extra)
		raw, _ := json.Marshal(blocks)
		out, err := sjson.SetRawBytes(body, fmt.Sprintf("messages.%d.content", i), raw)
		if err == nil {
			return out
		}
		return body
	}
	return body
}
