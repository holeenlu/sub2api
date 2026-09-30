package service

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"
	"unicode"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// SettingKeyOAuthInitialModelMappings stores the optional rules applied while a
// new OpenAI OAuth account is created.  It is deliberately separate from the
// Excel/BPS template: model routing is an account credential setting and must
// not change BPS defaults or existing accounts.
const SettingKeyOAuthInitialModelMappings = "oauth_initial_model_mappings"

// OAuthModelMappingRule is kept compatible with ranxi2001's PR #219 payload.
// Only a trailing wildcard in From is accepted by the account model mapper.
type OAuthModelMappingRule struct {
	From string `json:"from"`
	To   string `json:"to"`
}

// OAuthInitialModelMappings is a small, local adaptation of PR #219.  The
// upstream PR stores these fields inside its larger automatic-account-config
// document, which is not present in this project.  Keeping an explicit switch
// preserves the existing account-creation behavior until an administrator
// opts in.
type OAuthInitialModelMappings struct {
	Enabled  bool                    `json:"enabled"`
	Platform string                  `json:"platform"`
	Rules    []OAuthModelMappingRule `json:"rules"`
}

func defaultOAuthModelMappings(platform string) []OAuthModelMappingRule {
	if platform == PlatformOpenAI {
		return []OAuthModelMappingRule{{From: "gpt-5.4", To: "gpt-5.5"}}
	}
	return []OAuthModelMappingRule{}
}

func DefaultOAuthInitialModelMappings() OAuthInitialModelMappings {
	return OAuthInitialModelMappings{
		Enabled:  false,
		Platform: PlatformOpenAI,
		Rules:    defaultOAuthModelMappings(PlatformOpenAI),
	}
}

func validateOAuthModelMappings(rules []OAuthModelMappingRule) error {
	bad := func(message string) error {
		return infraerrors.BadRequest("OAUTH_MODEL_MAPPINGS_INVALID", message)
	}
	if len(rules) > 100 {
		return bad("at most 100 model mapping rules allowed")
	}
	seen := make(map[string]bool, len(rules))
	for _, rule := range rules {
		from, to := strings.TrimSpace(rule.From), strings.TrimSpace(rule.To)
		if from == "" || to == "" || len(from) > 256 || len(to) > 256 ||
			strings.IndexFunc(from+to, unicode.IsSpace) >= 0 ||
			strings.IndexFunc(from+to, unicode.IsControl) >= 0 {
			return bad("model names must contain 1–256 bytes without whitespace or control characters")
		}
		if strings.Contains(strings.TrimSuffix(from, "*"), "*") || strings.Contains(to, "*") {
			return bad("only a trailing source wildcard is allowed; target must be a model name")
		}
		if seen[from] {
			return bad("model mapping sources must be unique")
		}
		seen[from] = true
	}
	return nil
}

func validateOAuthInitialModelMappings(settings OAuthInitialModelMappings) error {
	if strings.TrimSpace(settings.Platform) != PlatformOpenAI {
		return infraerrors.BadRequest("OAUTH_MODEL_MAPPINGS_INVALID", "only the OpenAI platform is supported")
	}
	return validateOAuthModelMappings(settings.Rules)
}

func normalizeOAuthModelMappings(settings OAuthInitialModelMappings) OAuthInitialModelMappings {
	settings.Platform = PlatformOpenAI
	rules := make([]OAuthModelMappingRule, 0, len(settings.Rules))
	for _, rule := range settings.Rules {
		rules = append(rules, OAuthModelMappingRule{
			From: strings.TrimSpace(rule.From),
			To:   strings.TrimSpace(rule.To),
		})
	}
	settings.Rules = rules
	return settings
}

func (s *SettingService) GetOAuthInitialModelMappings(ctx context.Context) (OAuthInitialModelMappings, error) {
	result := DefaultOAuthInitialModelMappings()
	if s == nil || s.settingRepo == nil {
		return result, nil
	}
	values, err := s.settingRepo.GetMultiple(ctx, []string{SettingKeyOAuthInitialModelMappings})
	if err != nil {
		return result, err
	}
	if raw := strings.TrimSpace(values[SettingKeyOAuthInitialModelMappings]); raw != "" {
		if err := json.Unmarshal([]byte(raw), &result); err != nil {
			return result, fmt.Errorf("invalid stored OAuth model mappings: %w", err)
		}
	}
	result = normalizeOAuthModelMappings(result)
	if err := validateOAuthInitialModelMappings(result); err != nil {
		return result, err
	}
	return result, nil
}

func (s *SettingService) SaveOAuthInitialModelMappings(ctx context.Context, input OAuthInitialModelMappings) (OAuthInitialModelMappings, error) {
	input = normalizeOAuthModelMappings(input)
	if err := validateOAuthInitialModelMappings(input); err != nil {
		return input, err
	}
	if input.Rules == nil {
		input.Rules = []OAuthModelMappingRule{}
	}
	raw, err := json.Marshal(input)
	if err != nil {
		return input, err
	}
	if s == nil || s.settingRepo == nil {
		return input, nil
	}
	return input, s.settingRepo.SetMultiple(ctx, map[string]string{SettingKeyOAuthInitialModelMappings: string(raw)})
}

// applyOAuthModelMappings mutates only a cloned credentials map. Templates
// replace identity passthroughs, while explicit custom targets win. Malformed
// mappings are left untouched, and callers can safely reuse their original
// credentials map.
func applyOAuthModelMappings(input *CreateAccountInput, rules []OAuthModelMappingRule) bool {
	if input == nil || len(rules) == 0 {
		return false
	}
	mapping := make(map[string]any)
	switch existing := input.Credentials["model_mapping"].(type) {
	case nil:
	case map[string]any:
		maps.Copy(mapping, existing)
	case map[string]string:
		for from, to := range existing {
			mapping[from] = to
		}
	default:
		// Do not silently replace a malformed, explicitly supplied mapping.
		return false
	}
	hadExplicitMapping := len(mapping) > 0
	applied := false
	for _, rule := range rules {
		from, to := strings.TrimSpace(rule.From), strings.TrimSpace(rule.To)
		if current, exists := mapping[from]; exists {
			target, ok := current.(string)
			if !ok || target != from || target == to {
				continue
			}
		}
		mapping[from] = to
		applied = true
	}
	if !applied {
		return false
	}
	input.Credentials = maps.Clone(input.Credentials)
	if input.Credentials == nil {
		input.Credentials = make(map[string]any)
	}
	input.Credentials["model_mapping"] = mapping
	// A convenience alias must not narrow an otherwise unrestricted new OAuth
	// account. Preserve explicit import scopes and administrator choices.
	if input.Platform == PlatformOpenAI && input.Type == AccountTypeOAuth && !hadExplicitMapping {
		if _, explicitMode := input.Credentials[OpenAIModelMappingModeKey]; !explicitMode {
			input.Credentials[OpenAIModelMappingModeKey] = "aliases"
		}
	}
	return true
}

func (s *adminServiceImpl) applyOAuthInitialModelMappings(ctx context.Context, input *CreateAccountInput) error {
	if input == nil || input.Type != AccountTypeOAuth || input.Platform != PlatformOpenAI || s.settingService == nil {
		return nil
	}
	settings, err := s.settingService.GetOAuthInitialModelMappings(ctx)
	if err != nil {
		return err
	}
	if settings.Enabled && settings.Platform == input.Platform {
		applyOAuthModelMappings(input, settings.Rules)
	}
	return nil
}
