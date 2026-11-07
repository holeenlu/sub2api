import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { keysAPI } from '../api/keys'
import { useKeysServices, type KeysGroup } from '../services'
import { useKeysRequest } from '../queries/useKeysRequest'
import type { ApiKey, UpdateApiKeyRequest } from '../api/types'

type SelectedKey = Pick<ApiKey, 'id' | 'name'>
type LimitField = 'quota' | 'rate_limit_5h' | 'rate_limit_1d' | 'rate_limit_7d'
type IPField = 'ip_whitelist' | 'ip_blacklist'
type EditableField = LimitField | IPField | 'group_id' | 'status' | 'expires_at'

export interface BulkKeyEditProps { show: boolean; selectedKeys: SelectedKey[]; groups: KeysGroup[] }
export function useBulkKeyEdit(props: BulkKeyEditProps, emit: { (event: 'close'): void; (event: 'updated', ids: number[]): void }) {
  const { t } = useI18n()
  const services = useKeysServices()
  const request = useKeysRequest(services)
  const submitting = ref(false)
  const pendingKeys = ref<SelectedKey[]>([])
  const failures = ref<Array<{ id: number; name: string; message: string }>>([])
  const enabled = reactive<Record<EditableField, boolean>>({
    group_id: false,
    status: false,
    quota: false,
    rate_limit_5h: false,
    rate_limit_1d: false,
    rate_limit_7d: false,
    expires_at: false,
    ip_whitelist: false,
    ip_blacklist: false
  })
  const groupId = ref<number | null>(null)
  const status = ref<'active' | 'inactive'>('active')
  const limits = reactive<Record<LimitField, string | number>>({
    quota: '', rate_limit_5h: '', rate_limit_1d: '', rate_limit_7d: ''
  })
  const ipLists = reactive<Record<IPField, string>>({ ip_whitelist: '', ip_blacklist: '' })
  const neverExpires = ref(false)
  const expirationDate = ref('')
  const limitFields: Array<{ key: LimitField; label: string }> = [
    { key: 'quota', label: 'keys.quotaAmount' },
    { key: 'rate_limit_5h', label: 'keys.rateLimit5h' },
    { key: 'rate_limit_1d', label: 'keys.rateLimit1d' },
    { key: 'rate_limit_7d', label: 'keys.rateLimit7d' }
  ]
  const ipFields: Array<{ key: IPField; label: string }> = [
    { key: 'ip_whitelist', label: 'keys.ipWhitelist' },
    { key: 'ip_blacklist', label: 'keys.ipBlacklist' }
  ]
  const groupOptions = computed(() => props.groups.map((group) => ({ value: group.id, label: group.name })))
  const statusOptions = computed(() => [
    { value: 'active', label: t('keys.enable') },
    { value: 'inactive', label: t('keys.disable') }
  ])

  const validationError = computed(() => {
    if (enabled.group_id && !props.groups.some((group) => group.id === groupId.value)) {
      return t('keys.groupRequired')
    }
    for (const { key } of limitFields) {
      if (!enabled[key]) continue
      const value = String(limits[key]).trim()
      if (!value || !Number.isFinite(Number(value)) || Number(value) < 0) {
        return t('keys.bulkEdit.invalidLimit')
      }
    }
    if (enabled.expires_at && !neverExpires.value && !Number.isFinite(Date.parse(expirationDate.value))) {
      return t('keys.bulkEdit.invalidExpiration')
    }
    return ''
  })
  const canSubmit = computed(() =>
    pendingKeys.value.length > 0 && Object.values(enabled).some(Boolean)
    && !validationError.value && !submitting.value
  )

  watch(() => props.show, (show) => {
    request.cancel()
    submitting.value = false
    if (!show) { pendingKeys.value = []; failures.value = []; return }
    pendingKeys.value = props.selectedKeys.map(({ id, name }) => ({ id, name }))
    failures.value = []
    for (const field of Object.keys(enabled) as EditableField[]) enabled[field] = false
    for (const { key } of limitFields) limits[key] = ''
    for (const { key } of ipFields) ipLists[key] = ''
    groupId.value = null
    status.value = 'active'
    neverExpires.value = false
    expirationDate.value = ''
  }, { immediate: true })

  const close = () => {
    if (!submitting.value) emit('close')
  }

  const errorMessage = (error: unknown): string => {
    const message = (error as { message?: unknown } | null)?.message
    return typeof message === 'string' && message ? message : t('keys.failedToSave')
  }

  const submit = async () => {
    if (!canSubmit.value) return
    const updates: UpdateApiKeyRequest = {}
    if (enabled.group_id) updates.group_id = groupId.value
    if (enabled.status) updates.status = status.value
    for (const { key } of limitFields) {
      if (enabled[key]) updates[key] = Number(limits[key])
    }
    for (const { key } of ipFields) {
      if (enabled[key]) updates[key] = ipLists[key].split('\n').map((ip) => ip.trim()).filter(Boolean)
    }
    if (enabled.expires_at) {
      updates.expires_at = neverExpires.value ? '' : new Date(expirationDate.value).toISOString()
    }

    const ticket = request.begin()
    if (!ticket.isCurrent()) return
    const selected = pendingKeys.value.map(key => ({ ...key }))
    submitting.value = true
    try {
      const result = await keysAPI.bulkUpdate(selected.map((key) => key.id), updates, ticket)
      if (!ticket.isCurrent()) return
      failures.value = result.failures.map(({ id, error }) => ({
        id,
        name: pendingKeys.value.find((key) => key.id === id)?.name ?? '',
        message: errorMessage(error)
      }))
      const failedIds = new Set(result.failures.map(({ id }) => id))
      pendingKeys.value = pendingKeys.value.filter((key) => failedIds.has(key.id))
      if (result.succeededIds.length) emit('updated', result.succeededIds)
      if (result.failures.length) {
        services.notifyError(t('keys.bulkEdit.partialFailure', {
          success: result.succeededIds.length, failed: result.failures.length
        }))
      } else {
        services.notifySuccess(t('keys.bulkEdit.success', { count: result.succeededIds.length }))
        emit('close')
      }
    } catch (error) {
      if (!ticket.isCurrent()) return
      services.notifyError(errorMessage(error))
    } finally {
      if (ticket.isCurrent()) submitting.value = false
    }
  }

  watch(services.scopeKey, () => { pendingKeys.value = []; failures.value = []; submitting.value = false; emit('close') }, { flush: 'sync' })
  return { submitting, pendingKeys, failures, enabled, groupId, status, limits, ipLists, neverExpires, expirationDate, limitFields, ipFields, groupOptions, statusOptions, validationError, canSubmit, close, submit }
}
