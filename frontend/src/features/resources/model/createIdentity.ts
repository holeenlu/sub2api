import type { CreateResourceForm } from './createEditorTypes'
import { ref, computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { type CnAccountMode, type CnApiProtocol } from "@/features/resources/model/credentialsBuilder"
import { formatDateTimeLocalInput, parseDateTimeLocalInput } from '@/shared/lib/format'
import type { Ref, ComputedRef, WritableComputedRef } from 'vue'




interface CreateIdentityInput {
  form: Pick<CreateResourceForm, "platform" | "type" | "expires_at">
  accountMode: Ref<string, string>
  apiProtocol: Ref<CnApiProtocol>
  apiKeyBaseUrl: Ref<string>
  mixedScheduling: Ref<boolean, boolean>
  allowOverages: Ref<boolean, boolean>
  t: ReturnType<typeof useI18n>["t"]
}




export function useCreateIdentityDraft(input: CreateIdentityInput): {
  isOpenCodeGoPlatform: ComputedRef<boolean>
  onCnPresetSelect: (preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) => void
  buildAntigravityExtra: () => Record<string, unknown> | undefined
  mixedChannelWarningDetails: Ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null, { groupName: string; currentPlatform: string; otherPlatform: string } | { groupName: string; currentPlatform: string; otherPlatform: string } | null>
  mixedChannelWarningRawMessage: Ref<string, string>
  mixedChannelWarningAction: Ref<(() => Promise<void>) | null, (() => Promise<void>) | null>
  mixedChannelWarningMessageText: ComputedRef<string>
  geminiHelpLinks: { apiKey: string; aiStudioPricing: string; gcpProject: string; geminiWebActivation: string; countryCheck: string; countryChange: string }
  expiresAtInput: WritableComputedRef<string, string>
  formatDateTimeLocal: (timestampSeconds: number | null) => string
  parseDateTimeLocal: (value: string) => number | null
} {

  const isOpenCodeGoPlatform = computed(() => input.form.platform === 'opencode_go')

  // 点击预设端点：同时回填 base url、账号类型与协议。
  function onCnPresetSelect(preset: { mode: CnAccountMode; protocol: CnApiProtocol; url: string }) {
    input.accountMode.value = preset.mode
    input.apiProtocol.value = preset.protocol
    input.apiKeyBaseUrl.value = preset.url
  }


  function buildAntigravityExtra(): Record<string, unknown> | undefined {
    const extra: Record<string, unknown> = {}
    if (input.mixedScheduling.value) extra.mixed_scheduling = true
    if (input.allowOverages.value) extra.allow_overages = true
    return Object.keys(extra).length > 0 ? extra : undefined
  }

  const mixedChannelWarningDetails = ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null>(
    null
  )

  const mixedChannelWarningRawMessage = ref('')

  const mixedChannelWarningAction = ref<(() => Promise<void>) | null>(null)


  const mixedChannelWarningMessageText = computed(() => {
    if (mixedChannelWarningDetails.value) {
      return input.t('admin.accounts.mixedChannelWarning', mixedChannelWarningDetails.value)
    }
    return mixedChannelWarningRawMessage.value
  })


  const geminiHelpLinks = {
    apiKey: 'https://aistudio.google.com/app/apikey',
    aiStudioPricing: 'https://ai.google.dev/pricing',
    gcpProject: 'https://console.cloud.google.com/welcome/new',
    geminiWebActivation: 'https://gemini.google.com/gems/create?hl=en-US&pli=1',
    countryCheck: 'https://policies.google.com/terms',
    countryChange: 'https://policies.google.com/country-association-form'
  }


  const expiresAtInput = computed({
    get: () => formatDateTimeLocal(input.form.expires_at),
    set: (value: string) => {
      input.form.expires_at = parseDateTimeLocal(value)
    }
  })


  const formatDateTimeLocal = formatDateTimeLocalInput

  const parseDateTimeLocal = parseDateTimeLocalInput
  return { isOpenCodeGoPlatform, onCnPresetSelect, buildAntigravityExtra, mixedChannelWarningDetails, mixedChannelWarningRawMessage, mixedChannelWarningAction, mixedChannelWarningMessageText, geminiHelpLinks, expiresAtInput, formatDateTimeLocal, parseDateTimeLocal }
}
