import { apiClient } from '../client'

export interface OAuthModelMappingRule {
  from: string
  to: string
}

export interface OAuthInitialModelMappings {
  enabled: boolean
  platform: string
  rules: OAuthModelMappingRule[]
}

export async function getOAuthInitialModelMappings(): Promise<OAuthInitialModelMappings> {
  const { data } = await apiClient.get<OAuthInitialModelMappings>('/admin/settings/oauth-initial-model-mappings')
  return data
}

export async function saveOAuthInitialModelMappings(value: OAuthInitialModelMappings): Promise<OAuthInitialModelMappings> {
  const { data } = await apiClient.put<OAuthInitialModelMappings>('/admin/settings/oauth-initial-model-mappings', value)
  return data
}
