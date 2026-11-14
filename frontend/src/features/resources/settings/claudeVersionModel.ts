import { toRaw } from 'vue'
import type { SettingsDraft } from '@/features/system/api'
export type ClaudeVersionDraft = Pick<SettingsDraft, 'min_claude_code_version' | 'max_claude_code_version'>
export const claudeVersionFields = ['min_claude_code_version', 'max_claude_code_version'] as const
export function projectClaudeVersion(settings: SettingsDraft): ClaudeVersionDraft {
  return {
    min_claude_code_version: structuredClone(toRaw(settings.min_claude_code_version ?? (""))),
    max_claude_code_version: structuredClone(toRaw(settings.max_claude_code_version ?? (""))),
  }
}
