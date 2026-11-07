import { ref } from 'vue'
import { vi } from 'vitest'
import type { JobsServices } from '../services'
export function createJobsTestServices(overrides: Partial<JobsServices> = {}): JobsServices {
  return {
    scopeKey: ref('session-a:user:personal'), publicSettings: ref(null),
    listPersonalKeys: vi.fn().mockResolvedValue({ items: [], pages: 1 }),
    notifySuccess: vi.fn(), notifyError: vi.fn(), copyText: vi.fn().mockResolvedValue(true),
    ...overrides,
  }
}
