package service

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// These are generateContent capabilities, not the Interactions API or the
// Antigravity model-variant dialect. Unknown IDs retain their existing behavior.
// Source: ai.google.dev/gemini-api/docs/generate-content/thinking (2026-10-13).
func geminiThinkingCapabilities(model string) (levels []string, minBudget, maxBudget int64, disable, known bool) {
	model = strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "models/")
	switch model {
	case "gemini-2.5-pro":
		return nil, 128, 32768, false, true
	case "gemini-2.5-flash":
		return nil, 0, 24576, true, true
	case "gemini-2.5-flash-lite":
		return nil, 512, 24576, true, true
	case "gemini-3-pro-preview":
		return []string{"low", "high"}, 0, 0, false, true
	case "gemini-4-argon":
		return []string{"low", "high"}, 0, 0, false, true
	case "gemini-3.1-pro-preview", "gemini-3.1-pro", "gemini-3.8-flash":
		return []string{"low", "medium", "high"}, 0, 0, false, true
	case "gemini-3-flash-preview", "gemini-3.1-flash-lite-preview", "gemini-3.1-flash-lite", "gemini-3.5-flash-lite", "gemini-3.6-flash", "gemini-3.7-flash":
		return []string{"minimal", "low", "medium", "high"}, 0, 0, false, true
	case "gemini-3.1-flash-lite-image":
		return []string{"minimal", "high"}, 0, 0, false, true
	default:
		return nil, 0, 0, false, false
	}
}

func normalizeGeminiThinking(body []byte, model string) ([]byte, error) {
	levels, minBudget, maxBudget, disable, known := geminiThinkingCapabilities(model)
	if !known {
		return body, nil
	}
	configPath := ""
	for _, generation := range []string{"generationConfig", "generation_config"} {
		for _, thinking := range []string{"thinkingConfig", "thinking_config"} {
			path := generation + "." + thinking
			if gjson.GetBytes(body, path).Exists() {
				if configPath != "" {
					return body, fmt.Errorf("duplicate Gemini thinking configuration")
				}
				configPath = path
			}
		}
	}
	if configPath == "" {
		return body, nil
	}
	findField := func(camel, snake string) (gjson.Result, string, error) {
		a, b := gjson.GetBytes(body, configPath+"."+camel), gjson.GetBytes(body, configPath+"."+snake)
		if a.Exists() && b.Exists() {
			return a, "", fmt.Errorf("duplicate Gemini %s", camel)
		}
		if b.Exists() {
			return b, configPath + "." + snake, nil
		}
		return a, configPath + "." + camel, nil
	}
	level, levelPath, err := findField("thinkingLevel", "thinking_level")
	if err != nil {
		return body, err
	}
	budget, budgetPath, err := findField("thinkingBudget", "thinking_budget")
	if err != nil {
		return body, err
	}
	if level.Exists() && budget.Exists() {
		return body, fmt.Errorf("%s accepts either thinkingLevel or thinkingBudget, not both", model)
	}
	if budget.Exists() {
		n, err := strconv.ParseInt(budget.Raw, 10, 64)
		if err != nil || n < -1 {
			return body, fmt.Errorf("thinkingBudget must be an integer of -1 or greater")
		}
		if n == 0 && !disable {
			return body, fmt.Errorf("%s cannot disable thinking; omit thinkingBudget or select a supported thinkingLevel", model)
		}
		if n > 0 && maxBudget > 0 && (n < minBudget || n > maxBudget) {
			return body, fmt.Errorf("%s thinkingBudget must be between %d and %d", model, minBudget, maxBudget)
		}
		return body, nil // -1 and valid explicit budgets remain budgets, never billing levels.
	}
	effort := strings.ToLower(strings.TrimSpace(level.String()))
	if !slices.Contains([]string{"minimal", "low", "medium", "high", "xhigh", "max"}, effort) {
		return body, nil
	}
	if len(levels) == 0 {
		value := maxBudget
		switch effort {
		case "minimal":
			value = minBudget
			if value < 128 {
				value = 128
			}
		case "low":
			value = geminiThinkingBudgetLowMax
		case "medium":
			value = geminiThinkingBudgetMediumMax
		}
		result, err := sjson.DeleteBytes(body, levelPath)
		if err != nil {
			return body, err
		}
		return sjson.SetBytes(result, budgetPath, value)
	}
	if effort == "max" || effort == "xhigh" {
		effort = "high"
	}
	if !slices.Contains(levels, effort) {
		// Prefer a supported lower level; minimal has no lower alternative.
		effort = levels[0]
	}
	if strings.EqualFold(level.String(), effort) {
		return body, nil
	}
	return sjson.SetBytes(body, levelPath, effort)
}

// applyClaudeThinkingToGemini runs after account model mapping. Explicit manual
// budgets take precedence over effort; Gemini cannot express both independently.
// Visibility remains independent and no thought signature is synthesized here.
func applyClaudeThinkingToGemini(body, claudeBody []byte, model string) ([]byte, error) {
	_, _, _, _, known := geminiThinkingCapabilities(model)
	if !known {
		return body, nil
	}
	config := map[string]any{}
	thinking := gjson.GetBytes(claudeBody, "thinking")
	effort := gjson.GetBytes(claudeBody, "output_config.effort").String()
	mode := thinking.Get("type").String()
	switch {
	case mode == "disabled" || effort == "none":
		config["thinkingBudget"] = 0
	case mode == "enabled" && thinking.Get("budget_tokens").Exists():
		value, err := strconv.ParseInt(thinking.Get("budget_tokens").Raw, 10, 64)
		if err != nil {
			return body, fmt.Errorf("thinking.budget_tokens must be an integer")
		}
		config["thinkingBudget"] = value
	case effort != "":
		config["thinkingLevel"] = effort
	case mode == "enabled" || mode == "adaptive":
		config["thinkingBudget"] = -1
	}
	switch thinking.Get("display").String() {
	case "omitted":
		config["includeThoughts"] = false
	case "summarized":
		config["includeThoughts"] = true
	}
	if len(config) == 0 {
		return body, nil
	}
	// Change controls locally so existing visibility/extension fields survive.
	result := body
	for field, value := range config {
		var err error
		result, err = sjson.SetBytes(result, "generationConfig.thinkingConfig."+field, value)
		if err != nil {
			return body, err
		}
	}
	for _, field := range []string{"thinkingBudget", "thinkingLevel"} {
		if _, changed := config[field]; changed {
			opposite := "thinkingBudget"
			if field == opposite {
				opposite = "thinkingLevel"
			}
			var err error
			result, err = sjson.DeleteBytes(result, "generationConfig.thinkingConfig."+opposite)
			if err != nil {
				return body, err
			}
		}
	}
	return normalizeGeminiThinking(result, model)
}
