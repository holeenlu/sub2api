import type { OperationsServices } from '../../services'

export async function getAccountNameMap(services: Pick<OperationsServices, 'listAccountNames'>, signal?: AbortSignal): Promise<Map<number, string>> {
  const names = new Map<number, string>()
  for (let page = 1;;page++) {
    if (signal?.aborted) throw new DOMException('Cancelled', 'AbortError')
    const result = await services.listAccountNames(page, 1000, { signal })
    if (signal?.aborted) throw new DOMException('Cancelled', 'AbortError')
    for (const item of result.items) names.set(item.id, item.name)
    if (!result.items.length || page * 1000 >= result.total) return names
  }
}
