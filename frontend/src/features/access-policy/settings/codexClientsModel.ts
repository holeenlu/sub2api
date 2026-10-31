import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type CodexClientsDraft = Pick<SettingsDraft, 'openai_codex_diagnostic_prompt_template_default' | 'openai_codex_diagnostic_prompt_template' | 'min_codex_version' | 'max_codex_version' | 'codex_cli_only_allow_app_server_clients' | 'codex_cli_only_blacklist' | 'codex_cli_only_whitelist' | 'codex_cli_only_engine_fingerprint_signals'>
export const codexClientsFields = ['openai_codex_diagnostic_prompt_template_default', 'openai_codex_diagnostic_prompt_template', 'min_codex_version', 'max_codex_version', 'codex_cli_only_allow_app_server_clients', 'codex_cli_only_blacklist', 'codex_cli_only_whitelist', 'codex_cli_only_engine_fingerprint_signals'] as const
export function projectCodexClients(settings: SettingsDraft): CodexClientsDraft {
  return {
    openai_codex_diagnostic_prompt_template_default: structuredClone(toRaw(settings.openai_codex_diagnostic_prompt_template_default ?? (""))),
    openai_codex_diagnostic_prompt_template: structuredClone(toRaw(settings.openai_codex_diagnostic_prompt_template ?? (""))),
    min_codex_version: structuredClone(toRaw(settings.min_codex_version ?? (""))),
    max_codex_version: structuredClone(toRaw(settings.max_codex_version ?? (""))),
    codex_cli_only_allow_app_server_clients: structuredClone(toRaw(settings.codex_cli_only_allow_app_server_clients ?? (false))),
    codex_cli_only_blacklist: structuredClone(toRaw(settings.codex_cli_only_blacklist ?? (""))),
    codex_cli_only_whitelist: structuredClone(toRaw(settings.codex_cli_only_whitelist ?? (""))),
    codex_cli_only_engine_fingerprint_signals: structuredClone(toRaw(settings.codex_cli_only_engine_fingerprint_signals ?? (""))),
  }
}
