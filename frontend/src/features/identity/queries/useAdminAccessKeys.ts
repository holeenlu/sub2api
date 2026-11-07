import { onScopeDispose, ref, watch } from 'vue'
import { CanceledError } from 'axios'
import * as api from '../api/adminAccessKeys'
import type { AdminKey, Permission } from '../api/adminAccessKeys'
import type { StepUpController } from '../composables/useStepUp'
import { normalizeAdminAccessKey } from '../model/adminAccessKeyDraft'

interface AdminAccessKeyContext {
  scope(): string
  listGroups(): Promise<{ id: number; name: string }[]>
  stepUp: Pick<StepUpController, 'run' | 'onCancel'>
}

export function useAdminAccessKeys(context: AdminAccessKeyContext) {
  const items = ref<AdminKey[]>([])
  const permissions = ref<Permission[]>([])
  const groups = ref<{ id: number; name: string }[]>([])
  const secret = ref('')
  const loading = ref(false)
  const loaded = ref(false)
  const busy = ref(false)
  let epoch = 0
  let sequence = 0
  let disposed = false
  let controller: AbortController | undefined

  function invalidate() {
    epoch++
    sequence++
    controller?.abort()
    context.stepUp.onCancel()
    items.value = []; permissions.value = []; groups.value = []; secret.value = ''
    loading.value = false; loaded.value = false; busy.value = false
  }
  watch(context.scope, invalidate, { flush: 'sync' })
  onScopeDispose(() => { disposed = true; invalidate() })

  async function load(): Promise<void> {
    controller?.abort()
    controller = new AbortController()
    const request = ++sequence
    const scope = context.scope()
    const valid = () => !disposed && request === sequence && scope === context.scope()
    loading.value = true
    try {
      const [result, allGroups] = await Promise.all([api.list({ signal: controller.signal }), context.listGroups()])
      if (!valid()) throw new CanceledError('Admin access key scope changed')
      items.value = (result.items ?? []).map(normalizeAdminAccessKey)
      permissions.value = result.permissions ?? []
      groups.value = allGroups ?? []
      loaded.value = true
    } catch (error) {
      if (!valid()) throw new CanceledError('Admin access key scope changed')
      throw error
    } finally {
      if (valid()) loading.value = false
    }
  }

  async function mutate<T>(action: () => Promise<T>, apply: (result: T) => void): Promise<void> {
    if (busy.value || disposed || !loaded.value) return
    const version = epoch
    const scope = context.scope()
    const valid = () => !disposed && version === epoch && scope === context.scope()
    const assertCurrent = () => { if (!valid()) throw new CanceledError('Admin access key scope changed') }
    busy.value = true
    secret.value = ''
    try {
      const result = await context.stepUp.run(async () => { assertCurrent(); return action() })
      assertCurrent()
      apply(result)
    } catch (error) {
      assertCurrent()
      throw error
    } finally {
      if (valid()) busy.value = false
    }
  }

  function replace(item: AdminKey) {
    items.value = [normalizeAdminAccessKey(item), ...items.value.filter(key => key.id !== item.id)]
  }
  return { items, permissions, groups, secret, loading, loaded, busy, load, mutate, replace }
}
