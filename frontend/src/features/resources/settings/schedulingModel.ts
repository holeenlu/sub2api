import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
import { normalizeAccountSchedulingThresholdsMap } from './schedulingThresholds'
export type SchedulingDraft = Pick<SettingsDraft, 'allow_ungrouped_key_scheduling' | 'account_scheduling_thresholds' | 'openai_advanced_scheduler_subscription_priority_enabled' | 'openai_low_upstream_rate_priority_enabled' | 'openai_oauth_scheduling_rate_multiplier' | 'grok_default_text_model' | 'grok_cross_client_model_map_enabled' | 'grok_default_base_url_mode' | 'openai_ttft_mode' | 'enable_fingerprint_unification' | 'enable_metadata_passthrough' | 'enable_cch_signing' | 'enable_claude_oauth_system_prompt_injection' | 'claude_oauth_system_prompt_blocks' | 'enable_anthropic_cache_ttl_1h_injection' | 'rewrite_message_cache_control' | 'enable_client_dateline_normalization' | 'antigravity_user_agent_version' | 'openai_codex_user_agent' | 'openai_codex_client_version' | 'openai_codex_version_auto_sync_enabled' | 'claude_code_client_version' | 'claude_code_version_auto_sync_enabled' | 'openai_codex_client_version_synced' | 'claude_code_client_version_synced'>
export const schedulingFields = ['allow_ungrouped_key_scheduling', 'account_scheduling_thresholds', 'openai_advanced_scheduler_subscription_priority_enabled', 'openai_low_upstream_rate_priority_enabled', 'openai_oauth_scheduling_rate_multiplier', 'grok_default_text_model', 'grok_cross_client_model_map_enabled', 'grok_default_base_url_mode', 'openai_ttft_mode', 'enable_fingerprint_unification', 'enable_metadata_passthrough', 'enable_cch_signing', 'enable_claude_oauth_system_prompt_injection', 'claude_oauth_system_prompt_blocks', 'enable_anthropic_cache_ttl_1h_injection', 'rewrite_message_cache_control', 'enable_client_dateline_normalization', 'antigravity_user_agent_version', 'openai_codex_user_agent', 'openai_codex_client_version', 'openai_codex_version_auto_sync_enabled', 'claude_code_client_version', 'claude_code_version_auto_sync_enabled', 'openai_codex_client_version_synced', 'claude_code_client_version_synced'] as const
export function projectScheduling(settings: SettingsDraft): SchedulingDraft {
  return {
    allow_ungrouped_key_scheduling: structuredClone(toRaw(settings.allow_ungrouped_key_scheduling ?? (false))),
    account_scheduling_thresholds: structuredClone(toRaw(settings.account_scheduling_thresholds ?? (normalizeAccountSchedulingThresholdsMap()))),
    openai_advanced_scheduler_subscription_priority_enabled: structuredClone(toRaw(settings.openai_advanced_scheduler_subscription_priority_enabled ?? (false))),
    openai_low_upstream_rate_priority_enabled: structuredClone(toRaw(settings.openai_low_upstream_rate_priority_enabled ?? (false))),
    openai_oauth_scheduling_rate_multiplier: structuredClone(toRaw(settings.openai_oauth_scheduling_rate_multiplier === undefined ? null : settings.openai_oauth_scheduling_rate_multiplier)),
    grok_default_text_model: structuredClone(toRaw(settings.grok_default_text_model ?? ("grok-4.5"))),
    grok_cross_client_model_map_enabled: structuredClone(toRaw(settings.grok_cross_client_model_map_enabled ?? (false))),
    grok_default_base_url_mode: structuredClone(toRaw(settings.grok_default_base_url_mode ?? ("cli"))),
    openai_ttft_mode: structuredClone(toRaw(settings.openai_ttft_mode ?? ("semantic"))),
    enable_fingerprint_unification: structuredClone(toRaw(settings.enable_fingerprint_unification ?? (true))),
    enable_metadata_passthrough: structuredClone(toRaw(settings.enable_metadata_passthrough ?? (false))),
    enable_cch_signing: structuredClone(toRaw(settings.enable_cch_signing ?? (false))),
    enable_claude_oauth_system_prompt_injection: structuredClone(toRaw(settings.enable_claude_oauth_system_prompt_injection ?? (true))),
    claude_oauth_system_prompt_blocks: structuredClone(toRaw(settings.claude_oauth_system_prompt_blocks ?? (""))),
    enable_anthropic_cache_ttl_1h_injection: structuredClone(toRaw(settings.enable_anthropic_cache_ttl_1h_injection ?? (false))),
    rewrite_message_cache_control: structuredClone(toRaw(settings.rewrite_message_cache_control ?? (false))),
    enable_client_dateline_normalization: structuredClone(toRaw(settings.enable_client_dateline_normalization ?? (true))),
    antigravity_user_agent_version: structuredClone(toRaw(settings.antigravity_user_agent_version ?? (""))),
    openai_codex_user_agent: structuredClone(toRaw(settings.openai_codex_user_agent ?? (""))),
    openai_codex_client_version: structuredClone(toRaw(settings.openai_codex_client_version ?? (""))),
    openai_codex_version_auto_sync_enabled: structuredClone(toRaw(settings.openai_codex_version_auto_sync_enabled ?? (true))),
    claude_code_client_version: structuredClone(toRaw(settings.claude_code_client_version ?? (""))),
    claude_code_version_auto_sync_enabled: structuredClone(toRaw(settings.claude_code_version_auto_sync_enabled ?? (true))),
    openai_codex_client_version_synced: structuredClone(toRaw(settings.openai_codex_client_version_synced ?? (""))),
    claude_code_client_version_synced: structuredClone(toRaw(settings.claude_code_client_version_synced ?? (""))),
  }
}
