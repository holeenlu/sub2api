/** Current build metadata. Deployment is performed outside the browser. */
import { apiClient } from '@/shared/http'

export interface VersionInfo {
  version: string
  upstream_version?: string
  build_commit?: string
}

export async function getVersion(options?: { signal?: AbortSignal }): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/version', options)
  return data
}

export const systemAPI = { getVersion }
export default systemAPI
