import type { EditResourceForm } from './editEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import type { PlanTypeOption } from './credentialsBuilder'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import type { Group } from '@/features/access-policy/api/groupTypes';
import type { OpenCodeGoUsageState } from '@/features/resources/api/accountObservationTypes';
import { buildPlanTypeOptions, type CnAccountMode, type CnApiProtocol } from "@/features/resources/model/credentialsBuilder"
import { formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/shared/lib/format'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import type { Ref, ComputedRef, WritableComputedRef } from 'vue'
import type { ResourceEditorServices } from '../services'
import type { Props } from './editEditorTypes'




interface EditIdentityInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  props: Readonly<Pick<Props, "groups" | "account">>
  opencodeGoSaving: Ref<boolean, boolean>
  applyOpenCodeGoState: (next: OpenCodeGoUsageState) => void
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
  t: ReturnType<typeof useI18n>["t"]
  editAccountMode: Ref<string, string>
  editApiProtocol: Ref<CnApiProtocol, CnApiProtocol>
  editBaseUrl: Ref<string, string>
  form: Pick<EditResourceForm, "status" | "expires_at">
}




export function useEditIdentityDraft(input: EditIdentityInput): {
  selectableGroups: ComputedRef<Group[]>
  isSparkShadow: ComputedRef<boolean>
  setOpenCodeGoAutoRefresh: (enabled: boolean) => Promise<void>
  onCnPresetSelect: (preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) => void
  GROK_CLIENT_TOOL_CACHE_EXTRA_KEY: "grok_client_tool_cache_enabled"
  isGrokOAuthAccount: ComputedRef<boolean>
  isSyncingAntigravityUpstream: Ref<boolean, boolean>
  ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY: "account_scheduling_threshold"
  ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY: "anthropic_fable_scheduling_threshold"
  mixedChannelWarningDetails: Ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null, { groupName: string; currentPlatform: string; otherPlatform: string } | { groupName: string; currentPlatform: string; otherPlatform: string } | null>
  mixedChannelWarningRawMessage: Ref<string, string>
  mixedChannelWarningAction: Ref<(() => Promise<void>) | null, (() => Promise<void>) | null>
  editPlanType: Ref<string, string>
  planTypeOptions: ComputedRef<PlanTypeOption[]>
  mixedChannelWarningMessageText: ComputedRef<string>
  statusOptions: ComputedRef<{ value: string; label: string }[]>
  expiresAtInput: WritableComputedRef<string, string>
  toPositiveNumber: (value: unknown) => number | null
  formatDateTimeLocal: (timestampSeconds: number | null) => string
  parseDateTimeLocal: (value: string) => number | null
} {


  const selectableGroups = computed(() => {
    const groups = new Map<number, Group>(input.props.groups.map(group => [group.id, group]))
    const assignedIds = new Set(input.props.account?.group_ids ?? [])
    for (const group of input.props.account?.groups ?? []) {
      if (assignedIds.has(group.id) && !groups.has(group.id)) {
        groups.set(group.id, group)
      }
    }
    return Array.from(groups.values())
  })


  // Spark 影子账号(parent_account_id 非空):代理恒继承母账号,不可独立编辑(外审 B/P1),
  // 故隐藏代理选择器。
  const isSparkShadow = computed(() => input.props.account?.parent_account_id != null)


  const setOpenCodeGoAutoRefresh = async (enabled: boolean) => {
    const pending = input.resourceTasks.capture()
    try {

      input.opencodeGoSaving.value = true
      try {
        input.applyOpenCodeGoState(await pending.wait(async () => accountsAPI.setOpenCodeGoUsageAutoRefresh(input.props.account!.id, enabled)))
      } catch (error) {
        if (!pending.current()) throw new StaleResourceTask()

        input.appStore.showError(extractApiErrorMessage(error, input.t('admin.accounts.opencodeGo.autoRefreshFailed')))
      } finally {
        if (pending.current()) {

          input.opencodeGoSaving.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }

  // 点击预设端点：回填 base url 与对应模式/协议。
  function onCnPresetSelect(preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) {
    input.editAccountMode.value = preset.mode
    input.editApiProtocol.value = preset.protocol
    input.editBaseUrl.value = preset.url
  }

  const GROK_CLIENT_TOOL_CACHE_EXTRA_KEY = 'grok_client_tool_cache_enabled'

  const isGrokOAuthAccount = computed(
    () => input.props.account?.platform === 'grok' && input.props.account?.type === 'oauth'
  )

  const isSyncingAntigravityUpstream = ref(false)

  const ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY = 'account_scheduling_threshold'

  const ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY = 'anthropic_fable_scheduling_threshold'

  const mixedChannelWarningDetails = ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null>(
    null
  )

  const mixedChannelWarningRawMessage = ref('')

  const mixedChannelWarningAction = ref<(() => Promise<void>) | null>(null)

  // OpenAI 订阅档位（Plus / Pro 20x / Pro 5x / Business Standard / Business Premium / Free）手动覆盖值,
  // 存于 credentials.plan_type;'' 表示清空/自动识别
  const editPlanType = ref<string>('')

  // OpenAI 订阅档位手动覆盖选项(清空 + Plus/Pro/Free;别名/自定义值友好显示且保留 canonical)
  const planTypeOptions = computed(() =>
    buildPlanTypeOptions(editPlanType.value, input.t('admin.accounts.openai.planTypeClear'))
  )


  const mixedChannelWarningMessageText = computed(() => {
    if (mixedChannelWarningDetails.value) {
      return input.t('admin.accounts.mixedChannelWarning', mixedChannelWarningDetails.value)
    }
    return mixedChannelWarningRawMessage.value
  })


  const statusOptions = computed(() => {
    const options = [
      { value: 'active', label: input.t('common.active') },
      { value: 'inactive', label: input.t('common.inactive') }
    ]
    if (input.form.status === 'disabled') options.push({ value: 'disabled', label: input.t('common.inactive') })
    if (input.form.status === 'error') {
      options.push({ value: 'error', label: input.t('admin.accounts.status.error') })
    }
    return options
  })


  const expiresAtInput = computed({
    get: () => formatDateTimeLocal(input.form.expires_at),
    set: (value: string) => {
      input.form.expires_at = parseDateTimeLocal(value)
    }
  })


  function toPositiveNumber(value: unknown) {
    const num = Number(value)
    if (!Number.isFinite(num) || num <= 0) {
      return null
    }
    return Math.trunc(num)
  }


  const formatDateTimeLocal = formatDateTimeLocalInput

  const parseDateTimeLocal = parseDateTimeLocalInput



  return { selectableGroups, isSparkShadow, setOpenCodeGoAutoRefresh, onCnPresetSelect, GROK_CLIENT_TOOL_CACHE_EXTRA_KEY, isGrokOAuthAccount, isSyncingAntigravityUpstream, ACCOUNT_SCHEDULING_THRESHOLD_CREDENTIAL_KEY, ANTHROPIC_FABLE_SCHEDULING_THRESHOLD_CREDENTIAL_KEY, mixedChannelWarningDetails, mixedChannelWarningRawMessage, mixedChannelWarningAction, editPlanType, planTypeOptions, mixedChannelWarningMessageText, statusOptions, expiresAtInput, toPositiveNumber, formatDateTimeLocal, parseDateTimeLocal }
}
