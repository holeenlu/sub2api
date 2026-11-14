import { buildApiUrl } from '@/shared/http'

export function buildApiCallbackUrl(path: string): string {
  return new URL(buildApiUrl(path), window.location.origin).href
}
