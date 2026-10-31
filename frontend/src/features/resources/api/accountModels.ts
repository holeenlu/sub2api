import { apiClient } from '@/shared/http'

/** The server compares the effective saved reference; [] means inherit. */
export async function saveAccountModels(id: number, expected: readonly string[], models: readonly string[], options: { signal?: AbortSignal } = {}): Promise<{ models: string[] | null }> {
  return (await apiClient.put<{ models: string[] | null }>(`/admin/models/accounts/${id}/allowlist`, { expected: [...expected], models: [...models] }, options)).data
}
