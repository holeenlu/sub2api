import { getCurrentScope, onScopeDispose, watch } from 'vue'
import { useResourceEditorServices } from '../services'

export class StaleResourceTask extends Error {
  constructor() { super('Resource editor scope changed'); this.name = 'StaleResourceTask' }
}

/** A task may finish remotely after cancellation. Its result must not enter a different editor. */
export function createResourceTaskScope(readScope: () => string) {
  let generation = 0
  let disposed = false
  const invalidate = () => { generation++ }
  const capture = (readTarget?: () => unknown) => {
    const scope = readScope()
    const version = generation
    const target = readTarget ? JSON.stringify(readTarget()) : undefined
    const current = () => !disposed && version === generation && scope === readScope()
      && (!readTarget || target === JSON.stringify(readTarget()))
    return {
      current,
      async wait<T>(action: () => Promise<T>): Promise<T> {
        if (!current()) throw new StaleResourceTask()
        const value = await action()
        if (!current()) throw new StaleResourceTask()
        return value
      }
    }
  }
  function guard<A extends unknown[], R>(action: (...args: A) => R) {
    const pending = capture()
    return (...args: A): R | undefined => pending.current() ? action(...args) : undefined
  }
  return { capture, guard, invalidate, dispose: () => { disposed = true; invalidate() } }
}

export function useResourceTaskScope(target: () => unknown = () => '') {
  const services = useResourceEditorServices()
  // Role/capability revocation may not change the logical login session.
  const readScope = () => JSON.stringify([
    services.scopeKey(), services.access.isSuperAdmin, services.access.canAuthorize, services.access.canManage, target()
  ])
  const scope = createResourceTaskScope(readScope)
  watch(readScope, scope.invalidate, { flush: 'sync' })
  if (getCurrentScope()) onScopeDispose(scope.dispose)
  return scope
}
