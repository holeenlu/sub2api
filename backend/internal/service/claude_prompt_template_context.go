package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/claude"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

var (
	claudePromptVariablePattern   = regexp.MustCompile(`\{\{?\s*([a-z_]+)\s*\}\}?`)
	claudePromptMemorySlugPattern = regexp.MustCompile(`[^A-Za-z0-9._]+`)
	claudePromptWindowsCWDPattern = regexp.MustCompile(`^[A-Za-z]:[\\/]`)
)

func claudePromptEnvironment(env map[string]json.RawMessage, vars map[string]string) (string, string) {
	read := func(name string) string {
		var value string
		_ = json.Unmarshal(env[name], &value)
		value = strings.TrimSpace(value)
		if len(value) > 256 || strings.ContainsAny(value, "\r\n\x00") {
			return ""
		}
		return value
	}
	lines := []string{"# Environment"}
	cwdLine, platformLine, osLine := "", "", ""
	if cwd := vars["cwd"]; cwd != "" {
		cwdLine = " - Primary working directory: " + cwd + "\n"
		lines = append(lines, strings.TrimSuffix(cwdLine, "\n"))
	}
	if platform := read("platform"); platform != "" {
		platformLine = "\n - Platform: " + platform
		lines = append(lines, strings.TrimPrefix(platformLine, "\n"))
	}
	shell := read("shell")
	if shell == "" {
		shell = "unknown"
	} else {
		lines = append(lines, " - Shell: "+shell)
	}
	if kernel := read("linux_kernel"); kernel != "" {
		osLine = "\n - OS Version: Linux " + kernel
		lines = append(lines, strings.TrimPrefix(osLine, "\n"))
	}
	for _, item := range []struct{ key, label string }{{"locale", "Locale"}, {"timezone", "Timezone"}} {
		value := vars[item.key]
		if value == "" || len(value) > 64 || strings.ContainsAny(value, "\r\n\x00") {
			continue
		}
		line := " - " + item.label + ": " + value
		lines = append(lines, line)
		if strings.HasPrefix(osLine, "\n - OS Version:") {
			osLine += line
		} else {
			osLine += "\n" + line
		}
	}
	model := strings.Split(vars["model"], "[")[0]
	name := model
	for _, family := range []struct{ id, name string }{{"fable-5-1", "Fable 5.1"}, {"fable-5.1", "Fable 5.1"}, {"fable-5", "Fable 5"}, {"opus-5-5", "Opus 5.5"}, {"opus-5.5", "Opus 5.5"}, {"opus-5", "Opus 5"}, {"sonnet-5-5", "Sonnet 5.5"}, {"sonnet-5", "Sonnet 5"}, {"haiku-5-5", "Haiku 5.5"}, {"haiku-5.5", "Haiku 5.5"}, {"haiku-4-5", "Haiku 4.5"}, {"opus", "Opus"}, {"sonnet", "Sonnet"}, {"haiku", "Haiku"}} {
		if strings.Contains(strings.ToLower(model), family.id) {
			name = family.name
			break
		}
	}
	cutoff := "January 2026"
	if strings.Contains(model, "sonnet-5-5") || strings.Contains(model, "haiku-5-5") {
		cutoff = "June 2026"
	}
	official := strings.NewReplacer("{cwd_line}", cwdLine, "{git}", "false", "{platform_line}", platformLine, "{shell}", shell, "{os_line}", osLine, "{model_name}", name, "{model}", model, "{knowledge_cutoff}", cutoff).Replace(claude.PromptTemplateEnvironment())
	continuation := claude.PromptTemplateContinuation()
	if cwd := vars["cwd"]; cwd != "" {
		slug := strings.Trim(claudePromptMemorySlugPattern.ReplaceAllString(strings.ReplaceAll(cwd, "\\", "/"), "-"), "-")
		if slug != "" {
			continuation = strings.ReplaceAll(continuation, "/home/kincli/.claude/projects/-home-kincli/memory/", cwd+"/.claude/projects/-"+slug+"/memory/")
		}
	}
	return strings.Join(lines, "\n"), continuation + "\n\n" + official
}

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
	if device := ReadAccountTelemetry(account).DeviceInstallation; device != nil {
		facts := fullClaudeTelemetryEnv(device.Environment, device.CLIVersion, device.Platform, device.Arch, telemetryCredential(account, "account_uuid") != "")
		for name, value := range facts {
			env[name], _ = json.Marshal(value)
		}
	}
	headers := http.Header{}
	if c != nil && c.Request != nil {
		headers = c.Request.Header
	}
	if s.identityService != nil {
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
	out["caller_agent"], _ = extractSystemTextAndCacheControl(normalizeSystemParam(gjson.GetBytes(body, "system").Value()))
	cwd := strings.TrimSpace(gjson.GetBytes(body, "cwd").String())
	if len(cwd) <= 512 && !strings.ContainsAny(cwd, "\r\n\x00") && (strings.HasPrefix(cwd, "/") || strings.HasPrefix(cwd, "~/") || strings.HasPrefix(cwd, "./") || strings.HasPrefix(cwd, "../") || claudePromptWindowsCWDPattern.MatchString(cwd)) {
		out["cwd"] = cwd
	}
	out["env"], out["env_official"] = claudePromptEnvironment(env, out)
	out["env_timezone_only"] = "# Environment\n - Timezone: " + out["timezone"]
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
