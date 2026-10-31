import type { GroupPlatform } from '@/features/access-policy/api/groupTypes';
export interface FileConfig {
  path: string
  content: string
  hint?: string  // Optional hint message for this file
  highlighted?: string
  downloadName?: string  // Shows a download button saving content under this name
  usesModelCatalog?: boolean  // Model lines come from the key's Codex catalog
}

export interface KeyGuideProps { show: boolean; apiKey: string; baseUrl: string; platform: GroupPlatform | null; claudeCodeOnly?: boolean; allowMessagesDispatch?: boolean }
export type CodexAuthMode = 'legacy' | 'api-key'
export function joinConfigPath(dir: string, file: string, windows: boolean): string {
  if (!windows) return `${dir}/${file}`
  return `${dir}\\${file}`
}

