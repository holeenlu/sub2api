import type { CatalogPolicyMode } from '@/api/admin/modelCatalog'

export interface AccountCatalogPolicyForm {
  mode: CatalogPolicyMode
  excludedText: string
}
