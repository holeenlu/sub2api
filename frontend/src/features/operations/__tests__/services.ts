import * as errorPresentation from '@/features/usage/errorPresentation'
import { ref } from 'vue'
import { vi } from 'vitest'
import type { OperationsServices } from '../services'
export function createOperationsTestServices(overrides: Partial<OperationsServices> = {}): OperationsServices {
  return {
    errorPresentation,
    scopeKey: ref('session-a:admin:all'),
    permissions: ref({ isSuperAdmin: true, canManageOps: true, canReadPromptContent: true }),
    platformOptions: ref([{ value: 'anthropic', label: 'Anthropic' }, { value: 'openai', label: 'OpenAI' }, { value: 'gemini', label: 'Gemini' }, { value: 'grok', label: 'Grok' }, { value: 'antigravity', label: 'Antigravity' }, { value: 'deepseek', label: 'DeepSeek' }, { value: 'kimi', label: 'Kimi' }, { value: 'minimax', label: 'MiniMax' }]),
    monitoring: ref({ enabled: true, realtimeEnabled: true, queryModeDefault: 'auto' }),
    publicSettings: ref({ channel_monitor_enabled: true, channel_monitor_mode: 'v2' }),
    loadMonitoringSettings: vi.fn().mockResolvedValue(undefined),
    disableRealtimeMonitoring: vi.fn(),
    listGroups: vi.fn().mockResolvedValue([]),
    listAccountNames: vi.fn().mockResolvedValue({ items: [], total: 0 }),
    getTotpStatus: vi.fn().mockResolvedValue({ enabled: true }),
    notifySuccess: vi.fn(), notifyError: vi.fn(), notifyWarning: vi.fn(),
    copyText: vi.fn().mockResolvedValue(true),
    ...overrides,
  }
}
