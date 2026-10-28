import { inject, type InjectionKey, type Ref } from 'vue'
// Readonly DTO owner: keys. Runtime access is supplied by app composition.
import type { ApiKey } from '@/features/keys'
export type JobsPersonalKey = Readonly<Pick<ApiKey, 'id' | 'key' | 'name' | 'status'>> & {
  readonly group?: Readonly<Pick<NonNullable<ApiKey['group']>, 'name' | 'platform' | 'allow_batch_image_generation'>> | null
}
export interface JobsServices {
  /** Logical session epoch + portal + business scope. No token; null is signed out. */
  scopeKey: Readonly<Ref<string | null>>
  publicSettings: Readonly<Ref<{ api_base_url?: string } | null>>
  listPersonalKeys(page: number, pageSize: number, filters: {
    status: string; sort_by: string; sort_order: string
  }, options: { signal: AbortSignal }): Promise<{ items: JobsPersonalKey[]; pages: number }>
  notifySuccess(message: string): void
  notifyError(message: string): void
  copyText(text: string, successMessage?: string): Promise<boolean>
}
export const jobsServicesKey: InjectionKey<JobsServices> = Symbol('JobsServices')
export function useJobsServices(): JobsServices {
  const services = inject(jobsServicesKey)
  if (!services) throw new Error('JobsServices must be provided by app composition')
  return services
}
