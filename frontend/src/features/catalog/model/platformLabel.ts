import { getPlatformSpec } from './platformCatalog'

/** 平台展示名：来自平台清单（后端 domain/platforms.go），新登记的平台同样适用。 */
export function platformLabel(p: string): string {
  if (p === 'composite') return 'Composite'
  return getPlatformSpec(p)?.display_name ?? (p || 'API')
}

