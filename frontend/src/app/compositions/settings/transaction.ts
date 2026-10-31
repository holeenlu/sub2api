import { isCancel } from 'axios'
import { toRaw, computed, onScopeDispose, ref, shallowRef, watch, type Ref } from 'vue'
import type { SettingsDraft, SystemSettings, UpdateSettingsRequest } from '@/features/system/settings'
import { writableSettingsFields } from './writableFields'

export type SettingsPatch = Omit<UpdateSettingsRequest, 'revision'>
export interface SettingsTransport {
  get(options?: { signal?: AbortSignal }): Promise<SystemSettings>
  put(request: UpdateSettingsRequest, options?: { signal?: AbortSignal }): Promise<SystemSettings>
  isCancelled?(error: unknown): boolean
}
export interface SettingsConflict {
  server: SystemSettings
  fields: string[]
}
const writeKeys: ReadonlySet<string> = new Set(writableSettingsFields)
const secretKeys = [
  'smtp_password', 'turnstile_secret_key', 'tencent_captcha_app_secret_key',
  'tencent_captcha_cloud_secret_id', 'tencent_captcha_cloud_secret_key',
  'aliyun_captcha_access_key_secret', 'linuxdo_connect_client_secret',
  'dingtalk_connect_client_secret', 'github_oauth_client_secret', 'google_oauth_client_secret',
  'wechat_connect_app_secret', 'wechat_connect_open_app_secret', 'wechat_connect_mp_app_secret',
  'wechat_connect_mobile_app_secret', 'oidc_connect_client_secret'
] as const
export function isSecretSetting(key: string): boolean {
  return secretKeys.some(secret => secret === key)
}
const clone = <T,>(value: T): T => structuredClone(toRaw(value))
const equal = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right)
function asDraft(settings: SystemSettings): SettingsDraft {
  // Every panel projects its own optional defaults; aggregate state remains the actual server snapshot.
  return { ...clone(settings), ...Object.fromEntries(secretKeys.map(key => [key, ''])) } as SettingsDraft
}
function validRevision(value: unknown): value is string {
  return typeof value === 'string' && value.length === 64
}
function fields(value: object): Record<string, unknown> { return value as Record<string, unknown> }
function status(error: unknown): number | undefined {
  if (!error || typeof error !== 'object') return undefined
  const value = fields(error)
  if (typeof value.status === 'number') return value.status
  return typeof value.response === 'object' && value.response !== null ? Number(fields(value.response).status) : undefined
}
export function useSettingsTransaction(transport: SettingsTransport, scope: Readonly<Ref<string | null>>) {
  const baseline = shallowRef<SystemSettings | null>(null)
  const draft = ref<SettingsDraft | null>(null)
  const revision = ref('')
  const dirty = ref<SettingsPatch>({})
  const conflict = shallowRef<SettingsConflict | null>(null)
  const loading = ref(false)
  const saving = ref(false)
  const error = shallowRef<unknown>(null)
  const revisionMissing = ref(false)
  const conflicted = ref(false)
  const uncertain = ref(false)
  const dirtyFields = computed(() => Object.keys(dirty.value))
  let generation = 0
  let comparison = 0
  let controller: AbortController | undefined
  let saveController: AbortController | undefined
  let disposed = false
  const isCancelled = (cause: unknown) => isCancel(cause) || transport.isCancelled?.(cause)
  function reset() {
    ++generation
    ++comparison
    controller?.abort()
    saveController?.abort()
    baseline.value = null
    draft.value = null
    dirty.value = {}
    conflict.value = null
    revision.value = ''
    error.value = null
    loading.value = false
    saving.value = false
    revisionMissing.value = false
    conflicted.value = false
    uncertain.value = false
  }
  function accept(settings: SystemSettings) {
    if (!validRevision(settings.revision)) throw new Error('Settings response is missing a valid revision')
    ++comparison
    baseline.value = clone(settings)
    draft.value = asDraft(settings)
    revision.value = settings.revision
    dirty.value = {}
    conflict.value = null
    conflicted.value = false
    uncertain.value = false
    revisionMissing.value = false
  }
  async function load() {
    if (disposed || saving.value || !scope.value) return false
    const current = ++generation
    const captured = scope.value
    controller?.abort()
    const request = new AbortController()
    controller = request
    loading.value = true
    error.value = null
    try {
      const settings = await transport.get({ signal: request.signal })
      if (current !== generation || captured !== scope.value || request.signal.aborted) return false
      accept(settings)
      return true
    } catch (cause: unknown) {
      if (current === generation && captured === scope.value && !request.signal.aborted && !isCancelled(cause)) error.value = cause
      return false
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function patch(value: Partial<SettingsDraft>) {
    if (disposed || !draft.value || !baseline.value) return
    const base = fields(asDraft(baseline.value))
    for (const [key, next] of Object.entries(value)) {
      if (!writeKeys.has(key) || next === undefined) continue
      fields(draft.value)[key] = clone(next)
      if (equal(base[key], next)) delete fields(dirty.value)[key]
      else fields(dirty.value)[key] = clone(next)
    }
    if (conflict.value) conflict.value = { ...conflict.value, fields: [...new Set([...conflict.value.fields, ...Object.keys(dirty.value)])] }
    error.value = null
  }
  async function compare() {
    if (disposed || !baseline.value || !scope.value) return
    const current = generation
    const captured = scope.value
    controller?.abort()
    const currentComparison = ++comparison
    const request = new AbortController()
    controller = request
    conflicted.value = true
    try {
      const server = await transport.get({ signal: request.signal })
      if (current !== generation || captured !== scope.value || currentComparison !== comparison || request.signal.aborted) return
      if (!validRevision(server.revision)) throw new Error('Settings response is missing a valid revision')
      conflicted.value = true
      const keys = new Set([...Object.keys(server), ...dirtyFields.value])
      conflict.value = {
        server,
        fields: [...keys].filter(key => key !== 'revision' && (!equal(fields(baseline.value || {})[key], fields(server)[key]) || key in dirty.value))
      }
    } catch (cause: unknown) {
      if (current === generation && captured === scope.value && currentComparison === comparison && !request.signal.aborted && !isCancelled(cause)) error.value = cause
    }
  }
  async function save(prepare: (patch: SettingsPatch, draft: SettingsDraft) => SettingsPatch = value => value) {
    if (disposed || saving.value || loading.value || conflicted.value || uncertain.value || revisionMissing.value || !scope.value || !draft.value || !dirtyFields.value.length) return false
    if (!validRevision(revision.value)) { revisionMissing.value = true; return false }
    const current = generation
    const captured = scope.value
    const request = new AbortController()
    saveController = request
    saving.value = true
    error.value = null
    let submitted = false
    try {
      const submittedDraft = clone(draft.value)
      const payload = prepare(clone(dirty.value), clone(submittedDraft))
      if (!Object.keys(payload).length) return false
      submitted = true
      const updated = await transport.put({ ...payload, revision: revision.value }, { signal: request.signal })
      if (current !== generation || captured !== scope.value || request.signal.aborted) return false
      const currentDraft = draft.value
      if (!currentDraft) return false
      // Only acknowledge submitted fields that have not changed since dispatch.
      // A newer edit may even restore the old baseline and no longer be in dirty.
      const retained = Object.fromEntries([...writeKeys].filter(key =>
        (!(key in payload) && key in dirty.value) || !equal(fields(currentDraft)[key], fields(submittedDraft)[key])
      ).map(key => [key, fields(currentDraft)[key]]))
      accept(updated)
      patch(retained)
      return true
    } catch (cause: unknown) {
      if (current !== generation || captured !== scope.value) return false
      // Aborting a wait/transport does not roll back an already sent command.
      if (request.signal.aborted || isCancelled(cause)) return false
      error.value = cause
      if (status(cause) === 409) { conflicted.value = true; await compare() }
      if (status(cause) === 428) revisionMissing.value = true
      if (submitted && (status(cause) === undefined || status(cause) === 0 || (status(cause) || 0) >= 500)) uncertain.value = true
      return false
    } finally {
      if (saveController === request) saveController = undefined
      if (current === generation && captured === scope.value) saving.value = false
    }
  }
  function mergeReviewed(keepLocalFields: readonly string[]) {
    if (!conflict.value || saving.value) return
    const old = clone(dirty.value)
    const server = conflict.value.server
    accept(server)
    patch(Object.fromEntries(keepLocalFields.filter(key => key in old).map(key => [key, fields(old)[key]])))
  }
  watch(scope, () => { reset(); void load() }, { immediate: true, flush: 'sync' })
  onScopeDispose(() => { disposed = true; reset() })
  return { baseline, draft, revision, dirty, dirtyFields, conflict, conflicted, uncertain, revisionMissing, loading, saving, error, load, patch, save, compare, mergeReviewed }
}
