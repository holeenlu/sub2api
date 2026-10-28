import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import type { OpenCodeGoUsageStatus } from '@/features/resources/api/accountObservationTypes';
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import type { Account } from '@/features/resources/api/accountTypes';
import type { OllamaCloudUsageState, OpenCodeGoUsageState, OpenCodeGoUsageWindow } from '@/features/resources/api/accountObservationTypes';
import { extractApiErrorMessage, extractI18nErrorMessage } from '@/shared/lib/apiError'
import type { Ref, ComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props } from './editEditorTypes'




interface EditTelemetryInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  props: Readonly<Pick<Props, "account">>
  emit: ((event: "close") => void) & ((event: "updated", account: Account) => void) & ((event: "baseline", payload: Record<string, unknown>) => void)
  t: ReturnType<typeof useI18n>["t"]
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError" | "showSuccess">>
}




export function useEditTelemetryDraft(input: EditTelemetryInput): {
  handleOllamaCloudUsageUpdated: (state: OllamaCloudUsageState) => void
  opencodeGoState: Ref<{ account_id: number; eligible: boolean; auto_refresh_enabled: boolean; snapshot?: { status: OpenCodeGoUsageStatus; data?: { rolling?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; weekly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; monthly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined } | undefined; fetched_at?: string | undefined; last_attempt_at?: string | undefined; next_refresh_at?: string | undefined; failure_count?: number | undefined; http_status?: number | undefined; last_error?: string | undefined } | undefined } | null, OpenCodeGoUsageState | { account_id: number; eligible: boolean; auto_refresh_enabled: boolean; snapshot?: { status: OpenCodeGoUsageStatus; data?: { rolling?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; weekly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; monthly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined } | undefined; fetched_at?: string | undefined; last_attempt_at?: string | undefined; next_refresh_at?: string | undefined; failure_count?: number | undefined; http_status?: number | undefined; last_error?: string | undefined } | undefined } | null>
  opencodeGoLoading: Ref<boolean, boolean>
  opencodeGoSaving: Ref<boolean, boolean>
  opencodeGoRefreshing: Ref<boolean, boolean>
  opencodeGoSnapshot: ComputedRef<{ status: OpenCodeGoUsageStatus; data?: { rolling?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; weekly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined; monthly?: { status?: string | undefined; percent: number; resets_at?: string | undefined } | undefined } | undefined; fetched_at?: string | undefined; last_attempt_at?: string | undefined; next_refresh_at?: string | undefined; failure_count?: number | undefined; http_status?: number | undefined; last_error?: string | undefined } | undefined>
  opencodeGoStatusOk: ComputedRef<boolean>
  opencodeGoStatusLabel: ComputedRef<string>
  opencodeGoFormatPercent: (value?: number) => string
  opencodeGoFormatDate: (value?: string) => string
  opencodeGoWindowSummary: (window?: OpenCodeGoUsageWindow) => string
  applyOpenCodeGoState: (next: OpenCodeGoUsageState) => void
  loadOpenCodeGoUsage: () => Promise<void>
  refreshOpenCodeGoUsage: () => Promise<void>
  upstreamBillingAutoProbeEnabled: Ref<boolean, boolean>
  upstreamBillingRateSyncEnabled: Ref<boolean, boolean>
  upstreamRequestIdHeader: Ref<string, string>
  readUpstreamRequestIdHeader: (extra: unknown) => string
  handleUpstreamBillingRateSyncChange: (enabled: boolean) => void
  handleUpstreamBillingAutoProbeChange: (enabled: boolean) => void
} {


  const handleOllamaCloudUsageUpdated = (state: OllamaCloudUsageState) => {
    if (input.props.account) input.emit('updated', { ...input.props.account, ollama_cloud_usage: state })
  }


  // OpenCode Go usage panel state
  const opencodeGoState = ref<OpenCodeGoUsageState | null>(input.props.account?.opencode_go_usage ?? null)

  const opencodeGoLoading = ref(false)

  const opencodeGoSaving = ref(false)

  const opencodeGoRefreshing = ref(false)

  const opencodeGoSnapshot = computed(() => opencodeGoState.value?.snapshot)

  const opencodeGoStatusOk = computed(() => opencodeGoSnapshot.value?.status === 'ok')

  const opencodeGoStatusLabel = computed(() => {
    if (!opencodeGoSnapshot.value) return input.t('admin.accounts.opencodeGo.notRefreshed')
    if (opencodeGoSnapshot.value.status === 'unauthorized') return input.t('admin.accounts.opencodeGo.unauthorized')
    if (opencodeGoSnapshot.value.status === 'failed') return input.t('admin.accounts.opencodeGo.failed')
    return input.t('admin.accounts.opencodeGo.ok')
  })

  const opencodeGoFormatPercent = (value?: number) => typeof value === 'number' && Number.isFinite(value)
    ? `${value.toFixed(value % 1 ? 1 : 0)}%`
    : '-'

  const opencodeGoFormatDate = (value?: string) => {
    if (!value) return '-'
    const date = new Date(value)
    return Number.isNaN(date.getTime()) ? value : date.toLocaleString()
  }

  const opencodeGoWindowSummary = (window?: OpenCodeGoUsageWindow) => {
    if (!window) return '-'
    const reset = window.resets_at ? opencodeGoFormatDate(window.resets_at) : null
    return reset
      ? input.t('admin.accounts.opencodeGo.windowWithReset', { percent: opencodeGoFormatPercent(window.percent), reset })
      : opencodeGoFormatPercent(window.percent)
  }


  const applyOpenCodeGoState = (next: OpenCodeGoUsageState) => {
    opencodeGoState.value = next
    if (input.props.account) input.emit('updated', { ...input.props.account, opencode_go_usage: next })
  }


  const loadOpenCodeGoUsage = async () => {
    const pending = input.resourceTasks.capture()
    try {

      opencodeGoLoading.value = true
      try {
        applyOpenCodeGoState(await pending.wait(async () => accountsAPI.getOpenCodeGoUsage(input.props.account!.id)))
      } catch (error) {
        if (!pending.current()) throw new StaleResourceTask()

        input.appStore.showError(extractApiErrorMessage(error, input.t('admin.accounts.opencodeGo.loadFailed')))
      } finally {
        if (pending.current()) {

          opencodeGoLoading.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const refreshOpenCodeGoUsage = async () => {
    const pending = input.resourceTasks.capture()
    try {

      opencodeGoRefreshing.value = true
      try {
        applyOpenCodeGoState(await pending.wait(async () => accountsAPI.refreshOpenCodeGoUsage(input.props.account!.id)))
        input.appStore.showSuccess(input.t('admin.accounts.opencodeGo.refreshSuccess'))
      } catch (error) {
        if (!pending.current()) throw new StaleResourceTask()

        input.appStore.showError(extractI18nErrorMessage(
          error,
          input.t,
          'admin.accounts.opencodeGo.errors',
          input.t('admin.accounts.opencodeGo.refreshFailed')
        ))
      } finally {
        if (pending.current()) {

          opencodeGoRefreshing.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }

  const upstreamBillingAutoProbeEnabled = ref(false)

  const upstreamBillingRateSyncEnabled = ref(false)
  // For antigravity accounts: enable mixed scheduling
  // 上游ID：直接上游声明请求标识的响应头名，留空不记录。
  const upstreamRequestIdHeader = ref('')

  const readUpstreamRequestIdHeader = (extra: unknown): string => {
    const value = (extra as Record<string, unknown> | undefined)?.upstream_request_id_header
    return typeof value === 'string' ? value : ''
  }


  const handleUpstreamBillingRateSyncChange = (enabled: boolean) => {
    upstreamBillingRateSyncEnabled.value = enabled
    if (enabled) {
      upstreamBillingAutoProbeEnabled.value = true
    }
  }


  const handleUpstreamBillingAutoProbeChange = (enabled: boolean) => {
    upstreamBillingAutoProbeEnabled.value = enabled
    if (!enabled) {
      upstreamBillingRateSyncEnabled.value = false
    }
  }
  return { handleOllamaCloudUsageUpdated, opencodeGoState, opencodeGoLoading, opencodeGoSaving, opencodeGoRefreshing, opencodeGoSnapshot, opencodeGoStatusOk, opencodeGoStatusLabel, opencodeGoFormatPercent, opencodeGoFormatDate, opencodeGoWindowSummary, applyOpenCodeGoState, loadOpenCodeGoUsage, refreshOpenCodeGoUsage, upstreamBillingAutoProbeEnabled, upstreamBillingRateSyncEnabled, upstreamRequestIdHeader, readUpstreamRequestIdHeader, handleUpstreamBillingRateSyncChange, handleUpstreamBillingAutoProbeChange }
}
