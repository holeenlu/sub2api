import { inject, type InjectionKey, type Ref } from 'vue'
import type { PeakRatePresentation } from '@/features/pricing'
import type { GroupPlatform } from '../api/groupTypes'

/** Read-only application facts required by the existing Group display behavior. */
export interface GroupPresentation {
  readonly peakRate: PeakRatePresentation
  /** Catalog-owned platform order and labels; app supplies the live readonly projection. */
  readonly concretePlatformOptions: Readonly<Ref<readonly Readonly<{ value: Exclude<GroupPlatform, 'composite'>; label: string }>[]>>
  platformLabel(platform: string): string
  readonly serverUtcOffset: Readonly<Ref<string | null | undefined>>
  readonly isSimpleMode: Readonly<Ref<boolean>>
}
export const groupPresentationKey: InjectionKey<GroupPresentation> = Symbol('access-policy.group-presentation')
export function useGroupPresentation(): GroupPresentation {
  const presentation = inject(groupPresentationKey)
  if (!presentation) throw new Error('GroupPresentation must be provided by app composition')
  return presentation
}
