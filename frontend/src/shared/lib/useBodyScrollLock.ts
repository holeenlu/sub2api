import { onScopeDispose, watch } from 'vue'

// Only the body scroll resource is shared; each component owns its own token.
const owners = new Set<symbol>()
let restoreBody: (() => void) | undefined

/** Lock while this scope displays an overlay; restore the body after the last owner leaves. */
export function useBodyScrollLock(active: () => boolean): void {
  const owner = Symbol('body-scroll-lock')
  const release = () => {
    if (!owners.delete(owner) || owners.size > 0) return
    restoreBody?.()
    restoreBody = undefined
  }

  const stop = watch(active, isActive => {
    if (!isActive) {
      release()
      return
    }
    if (owners.has(owner)) return
    if (owners.size === 0) {
      const body = document.body
      // Capture only declarations we replace, including longhands and !important.
      const properties = ['overflow', 'overflow-x', 'overflow-y']
      const overflow = properties.map(name => ({
        name, value: body.style.getPropertyValue(name), priority: body.style.getPropertyPriority(name)
      }))
      const hadModalClass = body.classList.contains('modal-open')
      restoreBody = () => {
        for (const name of properties) body.style.removeProperty(name)
        for (const { name, value, priority } of overflow) {
          if (value) body.style.setProperty(name, value, priority)
        }
        if (!hadModalClass) body.classList.remove('modal-open')
      }
      for (const name of properties) body.style.removeProperty(name)
      body.style.setProperty('overflow', 'hidden')
      body.classList.add('modal-open')
    }
    owners.add(owner)
  }, { immediate: true })

  onScopeDispose(() => {
    stop()
    release()
  })
}
