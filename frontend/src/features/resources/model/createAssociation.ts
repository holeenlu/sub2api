import { readApiError } from '@/shared/lib/apiError'
import type { CreateResourceForm } from './createEditorTypes'
import { useResourceTaskScope, StaleResourceTask } from '../queries/resourceTaskScope'
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import accountsAPI from '@/features/resources/accounts'
import type { AccountPlatform } from '@/features/resources/api/platformTypes';
import type { CheckMixedChannelResponse, CreateAccountRequest } from '@/features/resources/api/accountTypes';
import type { Ref } from 'vue'
import type { ResourceEditorServices } from '../services'




interface CreateAssociationInput {
  resourceTasks: ReturnType<typeof useResourceTaskScope>
  mixedChannelWarningDetails: Ref<{ groupName: string; currentPlatform: string; otherPlatform: string } | null, { groupName: string; currentPlatform: string; otherPlatform: string } | { groupName: string; currentPlatform: string; otherPlatform: string } | null>
  mixedChannelWarningRawMessage: Ref<string, string>
  mixedChannelWarningAction: Ref<(() => Promise<void>) | null, (() => Promise<void>) | null>
  t: ReturnType<typeof useI18n>["t"]
  form: Pick<CreateResourceForm, "platform" | "group_ids">
  appStore: Readonly<Pick<ResourceEditorServices['notices'], "showError">>
  submitting: Ref<boolean, boolean>
}




export function useCreateAssociationDraft(input: CreateAssociationInput): {
  showMixedChannelWarning: Ref<boolean, boolean>
  antigravityMixedChannelConfirmed: Ref<boolean, boolean>
  needsMixedChannelCheck: (platform: AccountPlatform) => boolean
  buildMixedChannelDetails: (resp?: CheckMixedChannelResponse) => { groupName: string; currentPlatform: string; otherPlatform: string } | null
  clearMixedChannelDialog: () => void
  openMixedChannelDialog: (opts: { response?: CheckMixedChannelResponse; message?: string; onConfirm: () => Promise<void> }) => void
  withAntigravityConfirmFlag: (payload: CreateAccountRequest) => CreateAccountRequest
  ensureAntigravityMixedChannelConfirmed: (onConfirm: () => Promise<void>) => Promise<boolean>
  handleMixedChannelConfirm: () => Promise<void>
  handleMixedChannelCancel: () => void
} {


  const showMixedChannelWarning = ref(false)

  const antigravityMixedChannelConfirmed = ref(false)


  const needsMixedChannelCheck = (platform: AccountPlatform) => platform === 'antigravity' || platform === 'anthropic'


  const buildMixedChannelDetails = (resp?: CheckMixedChannelResponse) => {
    const details = resp?.details
    if (!details) {
      return null
    }
    return {
      groupName: details.group_name || 'Unknown',
      currentPlatform: details.current_platform || 'Unknown',
      otherPlatform: details.other_platform || 'Unknown'
    }
  }


  const clearMixedChannelDialog = () => {
    showMixedChannelWarning.value = false
    input.mixedChannelWarningDetails.value = null
    input.mixedChannelWarningRawMessage.value = ''
    input.mixedChannelWarningAction.value = null
  }


  const openMixedChannelDialog = (opts: {
    response?: CheckMixedChannelResponse
    message?: string
    onConfirm: () => Promise<void>
  }) => {
    input.mixedChannelWarningDetails.value = buildMixedChannelDetails(opts.response)
    input.mixedChannelWarningRawMessage.value =
      opts.message || opts.response?.message || input.t('admin.accounts.failedToCreate')
    input.mixedChannelWarningAction.value = opts.onConfirm
    showMixedChannelWarning.value = true
  }


  const withAntigravityConfirmFlag = (payload: CreateAccountRequest): CreateAccountRequest => {
    if (needsMixedChannelCheck(payload.platform) && antigravityMixedChannelConfirmed.value) {
      return {
        ...payload,
        confirm_mixed_channel_risk: true
      }
    }
    const cloned = { ...payload }
    delete cloned.confirm_mixed_channel_risk
    return cloned
  }


  const ensureAntigravityMixedChannelConfirmed = async (onConfirm: () => Promise<void>): Promise<boolean> => {
    const pending = input.resourceTasks.capture()
    const platform = input.form.platform
    const groupIDs = [...input.form.group_ids]
    const sameContext = () => pending.current() && platform === input.form.platform
      && groupIDs.length === input.form.group_ids.length
      && groupIDs.every((id, index) => id === input.form.group_ids[index])
    try {

      if (!needsMixedChannelCheck(input.form.platform)) {
        return true
      }
      if (antigravityMixedChannelConfirmed.value) {
        return true
      }

      try {
        const result = await pending.wait(async () => accountsAPI.checkMixedChannelRisk({
          platform: input.form.platform,
          group_ids: groupIDs
        }))
        if (!sameContext()) return false
        if (!result.has_risk) {
          return true
        }
        openMixedChannelDialog({
          response: result,
          onConfirm: async () => {
            if (!sameContext()) return
            try {

              antigravityMixedChannelConfirmed.value = true
              await pending.wait(async () => onConfirm())

            } catch (error: unknown) {
              if (error instanceof StaleResourceTask) { return }
              throw error
            }
          }
        })
        return false
      } catch (error: unknown) {
        const errorDetails = readApiError(error)
        if (!pending.current()) throw new StaleResourceTask()

        input.appStore.showError(errorDetails.response?.data?.message || errorDetails.response?.data?.detail || input.t('admin.accounts.failedToCreate'))
        return false
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return false }
      throw error
    }
  }


  // Handle mixed channel warning confirmation
  const handleMixedChannelConfirm = async () => {
    const pending = input.resourceTasks.capture()
    try {

      const action = input.mixedChannelWarningAction.value
      if (!action) {
        clearMixedChannelDialog()
        return
      }
      clearMixedChannelDialog()
      input.submitting.value = true
      try {
        await pending.wait(async () => action())
      } finally {
        if (pending.current()) {

          input.submitting.value = false

        }
      }

    } catch (error: unknown) {
      if (error instanceof StaleResourceTask) { return }
      throw error
    }
  }


  const handleMixedChannelCancel = () => {
    clearMixedChannelDialog()
  }
  return { showMixedChannelWarning, antigravityMixedChannelConfirmed, needsMixedChannelCheck, buildMixedChannelDetails, clearMixedChannelDialog, openMixedChannelDialog, withAntigravityConfirmFlag, ensureAntigravityMixedChannelConfirmed, handleMixedChannelConfirm, handleMixedChannelCancel }
}
