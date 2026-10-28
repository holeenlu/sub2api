import { ref, toRaw, watch, type Ref } from 'vue'


function copy<T>(value: T): T {
  const raw = toRaw(value)
  if (Array.isArray(raw)) return raw.map(item => copy(item)) as T
  if (raw && typeof raw === 'object') {
    return Object.fromEntries(Object.entries(raw).map(([key, item]) => [key, copy(item)])) as T
  }
  return raw
}


/** The panel owns its editable snapshot; parent snapshots are never mutated. */
export function usePanelField<T>(read: () => T, update: (value: T) => void): Ref<T> {
  const local = ref(copy(read())) as Ref<T>
  watch(read, value => {
    if (JSON.stringify(value) !== JSON.stringify(local.value)) local.value = copy(value)
  }, { deep: true, flush: 'sync' })
  watch(local, value => {
    if (JSON.stringify(value) !== JSON.stringify(read())) update(copy(value))
  }, { deep: true, flush: 'sync' })
  return local
}
