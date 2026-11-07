import { ref } from 'vue'
import { vi } from 'vitest'
import type { SupplyServices } from '../services'
export function createSupplyTestServices(overrides: Partial<SupplyServices> = {}): SupplyServices {
  return {
    scopeKey: ref('test:session:vendor'), identity: ref({ userId: 1, role: 'super_admin', isSuperAdmin: true }),
    platformName: value => value === 'openai' ? 'OpenAI' : value === 'anthropic' ? 'Anthropic' : value,
    getAssignableGroups: vi.fn().mockResolvedValue([]), 
    ...overrides,
  }
}
