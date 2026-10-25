/** Current build metadata. Deployment is performed outside the browser. */
import { apiClient } from '../client'

export interface VersionInfo {
  version: string
  upstream_version?: string
  build_commit?: string
}

export async function getVersion(): Promise<VersionInfo> {
  const { data } = await apiClient.get<VersionInfo>('/admin/system/version')
  return data
}

export const systemAPI = { getVersion }
export default systemAPI
