package service

import "github.com/Wei-Shaw/sub2api/internal/pkg/typesafe"

// modelSeedIDs is the shared curated catalog for group and account editors,
// reviewed against provider documentation on 2026-10-08. It is not an admission
// rule or an exhaustive account catalog. Keep it independent of runtime fallback
// catalogs, which retain legacy aliases and account-test defaults.
// Sources, selection criteria and provider-specific limitations:
// docs/GROUP_MODEL_CANDIDATES.md.
func modelSeedIDs(platform string) []string {
	switch platform {
	case PlatformOpenAI:
		return []string{
			"gpt-6-astra",
			"gpt-6.1-sol",
			"gpt-6-luna",
			"gpt-6-sol",
			"gpt-5.6-sol",
			"gpt-5.6-terra",
			"gpt-5.6-luna",
			"codex-auto-review",
			"gpt-reserve",
			"gpt-image-2.5-sunburst",
			"gpt-image-2.5-flare",
			"gpt-image-2",
		}
	case PlatformGemini:
		return []string{
			"gemini-3.8-flash",
			"gemini-3.5-flash-lite",
			"gemini-3.1-pro-preview",
			"gemini-nano-banana-2.1",
			"gemini-3.1-flash-image",
			"gemini-3.1-flash-lite-image",
			"gemini-3-pro-image",
		}
	case PlatformAntigravity:
		return []string{
			"gemini-3.8-flash-high",
			"gemini-3.8-flash-medium",
			"gemini-3.1-pro-high",
			"claude-sonnet-4-6",
			"claude-opus-4-6-thinking",
			"gemini-3.1-flash-image",
		}
	case PlatformGrok:
		return []string{
			"grok-4.7",
			"grok-imagine-image-2.0",
			"grok-imagine-video-1.5",
		}
	case PlatformKimi:
		return []string{
			"kimi-k3",
			"kimi-k2.7-code",
			"kimi-k2.7-code-highspeed",
			"kimi-k2.6",
			"k3",
			"k3-256k",
			"kimi-for-coding",
			"kimi-for-coding-highspeed",
		}
	case PlatformZhipu:
		return []string{
			"glm-5.3",
			"glm-5.3-flash",
			"glm-5.3-flashx",
			"glm-5.2",
		}
	case PlatformDeepseek:
		return []string{
			"deepseek-flash",
			"deepseek-v4-pro",
		}
	case PlatformMiniMax:
		return []string{
			"MiniMax-M3",
			"MiniMax-M2.7",
			"MiniMax-M2.7-highspeed",
		}
	case PlatformOpenCodeGo:
		return []string{
			"grok-4.7",
			"gpt-6-luna",
			"glm-5.3",
			"glm-5.3-flash",
			"kimi-k3",
			"kimi-k2.7-code",
			"deepseek-v4.1-flash",
			"deepseek-v4-pro",
			"mimo-v2.6-pro",
			"mimo-v2.6-flash",
			"minimax-m3",
			"qwen3.8-max",
			"qwen3.8-flash",
			"longcat-2.0",
			"hy3",
		}
	case PlatformTypeSafe:
		return []string{typesafe.JevLatestModel}
	case PlatformCommandCode, PlatformCline:
		return nil
	case PlatformComposite:
		return compositeDefaultModelsListCandidateIDs()
	case PlatformAnthropic:
		return []string{
			"claude-fable-5-1",
			"claude-opus-5-5",
			"claude-sonnet-5-5",
			"claude-haiku-5-5",
			// Claude Code still uses the small 4.5 model for auxiliary requests.
			"claude-haiku-4-5-20251001",
		}
	default:
		return nil
	}
}
