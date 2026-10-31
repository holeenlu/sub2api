import { readonly, ref } from 'vue'
import { peakRatePresentation } from '@/features/pricing'
import { groupPresentationKey } from '../context'
export function groupTestContext() {
  const isSimpleMode = ref(false)
  const serverUtcOffset = ref<string | null | undefined>(undefined)
  return { isSimpleMode, serverUtcOffset, provide: {
    [groupPresentationKey as symbol]: { peakRate: peakRatePresentation, platformLabel: (platform: string) => platform, isSimpleMode: readonly(isSimpleMode), serverUtcOffset: readonly(serverUtcOffset) },
  } }
}
