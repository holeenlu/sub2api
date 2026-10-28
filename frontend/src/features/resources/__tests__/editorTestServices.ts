import { vi } from 'vitest'
import { defineComponent } from 'vue'
import { config } from '@vue/test-utils'
import { ref } from 'vue'
import { GroupBadge, groupPresentationKey } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import * as catalog from '@/features/catalog'
import { ModelIcon } from '@/features/catalog/modelIcon'
import grok from '@/features/resources/grok'
import gemini from '@/features/resources/gemini'
import antigravity from '@/features/resources/antigravity'
import cnProviders from '@/features/resources/cnProviders'
import type { ResourceEditorServices } from '../services'
function blocked<T extends object>(source: T): T {
  return Object.fromEntries(Object.entries(source).map(([key, value]) => [key, typeof value === 'function' ? vi.fn().mockRejectedValue(new Error('Unexpected resource API call: ' + key)) : value])) as T
}
export function editorTestServices(): ResourceEditorServices {
  if (Reflect.get(config.global.provide, groupPresentationKey) == null) {
    Reflect.set(config.global.provide, groupPresentationKey, {
      peakRate: peakRatePresentation, serverUtcOffset: ref(undefined), isSimpleMode: ref(false),
      platformLabel: (platform: string) => platform
    })
  }
  return { scopeKey: () => 'test-session:admin:0',
    components: { GroupBadge, ModelIcon },
    statisticsCharts: {
      ModelDistributionChart: defineComponent({ name: 'ModelDistributionChart', props: ['modelStats', 'loading'], template: '<div data-test="model-distribution" />' }),
      EndpointDistributionChart: defineComponent({ name: 'EndpointDistributionChart', props: ['endpointStats', 'loading', 'title'], template: '<div data-test="endpoint-distribution" />' })
    },
    notices: { showError: vi.fn(), showWarning: vi.fn(), showInfo: vi.fn(), showSuccess: vi.fn() },
    access: { isSuperAdmin: true, isSimpleMode: true, canAuthorize: true, canManage: true },
    catalog: { ...catalog, builtin: catalog.BUILTIN_PLATFORM_CATALOG },
    quotaNotificationEnabled: false, managedModelCatalog: vi.fn().mockResolvedValue([]), modelPresets: () => [],
    tlsProfiles: vi.fn().mockResolvedValue([]), webSearchConfig: vi.fn().mockResolvedValue({ enabled: false, providers: [] }),
    credentialAuthorization: { grok: blocked(grok), gemini: blocked(gemini), antigravity: blocked(antigravity) },
    excelAuthorization: { generateUrl: vi.fn(), get: vi.fn().mockResolvedValue({ authorized: false }), save: vi.fn(), revoke: vi.fn() },
    claudeCredits: { getClaudeResetCredits: vi.fn(), redeemClaudeResetCredit: vi.fn() },
    openAccountTest: vi.fn().mockRejectedValue(new Error('Unexpected resource test stream')),
    providerObservations: blocked(cnProviders)
  }
}
