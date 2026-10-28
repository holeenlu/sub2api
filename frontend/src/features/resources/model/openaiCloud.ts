import type { Account } from '@/features/resources/api/accountTypes';

// Cloud authorization uses the native Codex OAuth client. Backend eligibility is authoritative.
export function isOpenAICloudAccount(account: Account | null | undefined): boolean {
  if (!account || account.platform !== 'openai' || account.type !== 'oauth' ||
    account.vendor_id != null || account.parent_account_id != null) return false
  const credential = (key: string) => String(account.credentials?.[key] ?? '').trim()
  const modes = [credential('auth_mode'), credential('openai_auth_mode')]
    .map(mode => mode.toLowerCase().replace(/[\s_-]+/g, ''))
  if (modes.some(mode => mode === 'personalaccesstoken' || mode === 'pat' || mode === 'agentidentity')) return false
  const clientID = credential('client_id')
  return !clientID || clientID === 'app_EMoamEEZ73f0CkXaXp7hrann'
}

// The admin detail DTO already exposes these non-secret identity fields.
// Keep the account-kind predicate separate so an incomplete existing account
// can still display its Cloud switch and turn the gateway off.
export function canEnableOpenAICloud(account: Account | null | undefined): boolean {
  return isOpenAICloudAccount(account) && ['chatgpt_account_id', 'chatgpt_user_id'].every(key => {
    const value = account?.credentials?.[key]
    return typeof value === 'string' ? value.trim() !== '' : typeof value === 'number' && Number.isFinite(value)
  })
}

export interface CloudTestResult {
  kind: 'cloud_result'
  response_id: string
  status: 'completed' | 'failed'
  model: string
  output: Array<Record<string, unknown>>
  usage: Record<string, unknown> | null
  error?: { code: string; message: string }
  created_at: number
  completed_at: number
  capabilities: { text: boolean; tools: boolean; usage: boolean; settlement: boolean; image_input: boolean; encrypted_context: boolean; web_search: boolean; image_generation: boolean }
}

// Export the terminal result only, never request headers, OAuth callbacks or the SSE envelope.
export function readCloudTestResult(value: unknown): CloudTestResult | null {
  if (!value || typeof value !== 'object') return null
  const result = value as Record<string, unknown>
  if (result.kind !== 'cloud_result' || !['completed', 'failed'].includes(String(result.status)) ||
    typeof result.response_id !== 'string' || typeof result.model !== 'string' ||
    !Array.isArray(result.output) || result.output.some(item => !item || typeof item !== 'object' || Array.isArray(item)) || typeof result.created_at !== 'number' ||
    typeof result.completed_at !== 'number' || !result.capabilities || typeof result.capabilities !== 'object') return null
  const capabilities = result.capabilities as Record<string, unknown>
  return {
    kind: 'cloud_result', response_id: result.response_id, status: result.status as CloudTestResult['status'],
    model: result.model, output: result.output, usage: result.usage as CloudTestResult['usage'] ?? null,
    ...(result.error && typeof result.error === 'object' ? { error: {
      code: String((result.error as Record<string, unknown>).code ?? ''),
      message: String((result.error as Record<string, unknown>).message ?? '')
    } } : {}),
    created_at: result.created_at, completed_at: result.completed_at,
    capabilities: { text: capabilities.text === true, tools: capabilities.tools === true,
      usage: capabilities.usage === true, settlement: capabilities.settlement === true,
      image_input: capabilities.image_input === true, encrypted_context: capabilities.encrypted_context === true,
      web_search: capabilities.web_search === true, image_generation: capabilities.image_generation === true }
  }
}

export function cloudResultAnswer(result: CloudTestResult | null): string {
  if (result?.status !== 'completed') return ''
  const hasFinal = result.output.some(item => item.type === 'message' && item.role === 'assistant' && item.phase === 'final_answer')
  return result.output.flatMap(item => item.type === 'message' && item.role === 'assistant' &&
    (!hasFinal || item.phase === 'final_answer') && Array.isArray(item.content)
    ? item.content.flatMap(part => part && typeof part === 'object' && part.type === 'output_text' && typeof part.text === 'string' ? [part.text] : [])
    : []).join('\n')
}
