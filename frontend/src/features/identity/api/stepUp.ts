import { apiClient } from '@/shared/http'

export interface StepUpGrant {
  verified: boolean
  expires_in: number
}

export async function verifyStepUp(code: string): Promise<StepUpGrant> {
  const { data } = await apiClient.post<StepUpGrant>('/user/totp/step-up', { code })
  return data
}
