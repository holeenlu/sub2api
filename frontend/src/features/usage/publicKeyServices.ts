import { inject, type InjectionKey, type Ref } from 'vue'
export interface PublicKeyUsageAppearance { siteName: string; siteLogo: string; docUrl: string; subscriptionEnabled: boolean }
export interface PublicKeyUsageServices {
  appearance: Readonly<Ref<PublicKeyUsageAppearance>>
  ensureSettings(): Promise<unknown>
  notifyInfo(message: string): void
  notifySuccess(message: string): void
  notifyError(message: string): void
}
export const publicKeyUsageServicesKey: InjectionKey<PublicKeyUsageServices> = Symbol('PublicKeyUsageServices')
export function usePublicKeyUsageServices(): PublicKeyUsageServices {
  const services = inject(publicKeyUsageServicesKey)
  if (!services) throw new Error('PublicKeyUsageServices must be provided by the application')
  return services
}
