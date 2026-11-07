package claude

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// NormalizeThinking adapts only known Messages API thinking parameters. It
// never rebuilds messages or output_config: signed history, per-message effort,
// visibility and future fields must survive. Unknown models/enums pass through.
// Call with the actual upstream model, after routing and policy mappings.
func NormalizeThinking(body []byte, model string) ([]byte, error) {
	cap, known := ThinkingCapabilitiesForModel(model)
	if !known {
		return body, nil
	}
	result := body
	var patchErr error
	set := func(path string, value any) {
		if patchErr == nil {
			result, patchErr = sjson.SetBytes(result, path, value)
		}
	}
	drop := func(path string) {
		if patchErr == nil {
			result, patchErr = sjson.DeleteBytes(result, path)
		}
	}
	effort := gjson.GetBytes(body, "output_config.effort").String()
	if len(cap.EffortLevels) > 0 && (effort == "xhigh" || effort == "max") && !slices.Contains(cap.EffortLevels, effort) {
		effort = cap.EffortLevels[len(cap.EffortLevels)-1]
		set("output_config.effort", effort)
	}
	mode := gjson.GetBytes(body, "thinking.type").String()
	switch mode {
	case "disabled":
		if !cap.Disable {
			if !cap.BetweenTools {
				return body, fmt.Errorf("%s does not support thinking.type=disabled; omit thinking or use thinking.type=adaptive and output_config.effort (low still allows thinking)", model)
			}
			mode = "between_tools"
			set("thinking.type", mode)
			// Omission retains the provider default and historical effort.
			// Explicit cross-protocol none chooses its effort at conversion.
			switch effort {
			case "xhigh", "max":
				effort = "high"
				set("output_config.effort", effort)
			}
			for _, field := range []string{"budget_tokens", "display", "block_binding"} {
				drop("thinking." + field)
			}
		}
	case "enabled":
		if !cap.Manual && cap.Adaptive {
			if budget := gjson.GetBytes(body, "thinking.budget_tokens"); budget.Exists() {
				n, err := strconv.ParseInt(budget.Raw, 10, 64)
				if err != nil || n < 1024 {
					return body, fmt.Errorf("%s requires an integer thinking.budget_tokens of at least 1024 for manual-thinking conversion", model)
				}
				if effort == "" {
					// Effort is a behavioral approximation, never an exact budget.
					effort = "max"
					for _, level := range []string{"low", "medium", "high"} {
						if n <= int64(ThinkingBudgetForEffort(level)) {
							effort = level
							break
						}
					}
					set("output_config.effort", effort)
				}
			}
			set("thinking.type", "adaptive")
			drop("thinking.budget_tokens")
		}
	case "adaptive":
		if cap.Adaptive {
			drop("thinking.budget_tokens")
		} else if cap.Manual {
			set("thinking.type", "enabled")
			if !gjson.GetBytes(body, "thinking.budget_tokens").Exists() {
				budget, err := DerivedThinkingBudget(effort, gjson.GetBytes(body, "max_tokens").Int())
				if err != nil {
					return body, err
				}
				set("thinking.budget_tokens", budget)
			}
		}
	case "between_tools":
		if !cap.BetweenTools {
			return body, fmt.Errorf("%s does not support thinking.type=between_tools", model)
		}
	}
	if mode == "between_tools" || (mode == "disabled" && cap.DisableHighOnly) {
		if effort == "xhigh" || effort == "max" {
			return body, fmt.Errorf("%s thinking.type=%s supports only low, medium or high effort", model, mode)
		}
	}
	if mode == "between_tools" || (mode == "disabled" && cap.DisableFixedEffort) {
		// Historical overrides cannot be silently rewritten: that changes the
		// conversation prefix to which signed thinking is bound.
		currentEffort := effort
		if currentEffort == "" {
			currentEffort = "high"
			if mode == "disabled" {
				currentEffort = "medium"
			}
		}
		for _, message := range gjson.GetBytes(body, "messages").Array() {
			if value := message.Get("output_config.effort"); value.Exists() && value.String() != currentEffort {
				return body, fmt.Errorf("%s thinking.type=%s does not support changing per-message effort", model, mode)
			}
		}
	}
	if patchErr != nil {
		return body, fmt.Errorf("adapt thinking parameters: %w", patchErr)
	}
	return result, nil
}

// ThinkingBudgetForEffort preserves the gateway's existing conversion budgets.
func ThinkingBudgetForEffort(effort string) int {
	switch effort {
	case "low":
		return 1024
	case "medium":
		return 4096
	case "xhigh", "max":
		return 32768
	default:
		return 10240
	}
}

// DerivedThinkingBudget never raises the caller's output limit. Explicit
// native budgets retain the existing provider-validation/repair contract.
func DerivedThinkingBudget(effort string, maxTokens int64) (int, error) {
	budget := ThinkingBudgetForEffort(effort)
	if maxTokens > 0 && int64(budget) >= maxTokens {
		if maxTokens <= 1024 {
			return 0, fmt.Errorf("manual thinking requires max_tokens greater than 1024; increase the output limit or disable thinking")
		}
		budget = int(maxTokens - 1)
	}
	return budget, nil
}
