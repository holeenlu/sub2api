import type { Query, ResourceScope } from './types'

export function scopeQuery(scope: ResourceScope, query: Query = {}): Query {
  const { vendor_id: _discard, scope: _scope, ...rest } = query
  if (scope.children) {
    if (scope.admin || !scope.readOnly || scope.vendorId !== undefined)
      throw new Error('Invalid child inventory scope')
    return { ...rest, scope: 'children' }
  }
  if (scope.admin && scope.readOnly && scope.vendorId === 0)
    return { ...rest, vendor_id: 0 }
  if (
    (scope.admin || scope.readOnly) &&
    (!Number.isSafeInteger(scope.vendorId) ||
      !scope.vendorId ||
      scope.vendorId <= 0)
  )
    throw new Error('A supplier scope is required')
  // Scope is written last; a caller cannot accidentally retain the previous supplier.
  return scope.admin || scope.readOnly ? { ...rest, vendor_id: scope.vendorId } : rest
}
export function mutationScope(scope: ResourceScope): Query {
  if (scope.readOnly) throw new Error('Supplier resources are read-only')
  return scopeQuery(scope)
}
