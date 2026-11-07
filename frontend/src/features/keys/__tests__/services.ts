import { ref } from 'vue'
import { config } from '@vue/test-utils'
import { GroupBadge, GroupOptionItem, groupPresentationKey } from '@/features/access-policy/presentation'
import { peakRatePresentation } from '@/features/pricing'
import { vi } from 'vitest'
import { buildCodexModelCatalogUrl, fetchCodexModelsManifest } from '@/features/catalog/codexManifest'
import type { KeysServices } from '../services'
export function createKeysTestServices(overrides: Partial<KeysServices> = {}): KeysServices {
  config.global.provide[groupPresentationKey as symbol] ??= { peakRate: peakRatePresentation, serverUtcOffset: ref(undefined), isSimpleMode: ref(false), platformLabel: (platform: string) => platform }
  return {
    components: { GroupBadge, GroupOptionItem },
    scopeKey: ref<string | null>('session:1|user:1|portal:user|scope:self'), publicSettings: ref(null),
    getAvailableGroups: vi.fn().mockResolvedValue([]), getUserGroupRates: vi.fn().mockResolvedValue({}),
    getKeyUsage: vi.fn().mockResolvedValue({ stats: {} }), notifySuccess: vi.fn(), notifyError: vi.fn(),
    copyText: vi.fn().mockResolvedValue(true), onKeyCreated: vi.fn(),
    codexModels: { buildCatalogUrl: buildCodexModelCatalogUrl, fetchManifest: fetchCodexModelsManifest },
    ...overrides
  }
}
