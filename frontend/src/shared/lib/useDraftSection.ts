import { reactive, toRaw, watch } from 'vue'

/** Clone domain-owned JSON-shaped form values without retaining Vue proxies. */
export function cloneDraftValue<T>(value: T): T {
  const raw = toRaw(value)
  if (Array.isArray(raw)) return raw.map(item => cloneDraftValue(item)) as T
  if (raw !== null && typeof raw === 'object') {
    return Object.fromEntries(Object.entries(raw).map(([key, item]) => [key, cloneDraftValue(item)])) as T
  }
  return raw
}

/** Own a detached section and publish only the fields edited in that section. */
export function useDraftSection<T extends object>(read: () => Readonly<T>, publish: (patch: Partial<T>) => void): T {
  const local = reactive(cloneDraftValue(read())) as T
  let baseline = cloneDraftValue(read())
  let syncing = false
  watch(read, value => {
    syncing = true
    const next = cloneDraftValue(value)
    for (const key of Object.keys(local) as Array<keyof T>) {
      if (!Object.prototype.hasOwnProperty.call(next, key)) delete local[key]
    }
    Object.assign(local, next)
    baseline = cloneDraftValue(next)
    syncing = false
  }, { deep: true, flush: 'sync' })
  watch(local, value => {
    if (syncing) return
    const patch: Partial<T> = {}
    for (const key of Object.keys(value) as Array<keyof T>) {
      if (JSON.stringify(value[key]) !== JSON.stringify(baseline[key])) patch[key] = cloneDraftValue(value[key])
    }
    baseline = cloneDraftValue(value)
    if (Object.keys(patch).length) publish(patch)
  }, { deep: true, flush: 'sync' })
  return local
}
