import { onScopeDispose, watch, type Ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useSettingsWorkspaceServices } from './ports'
import type { useSettingsTransaction } from './transaction'

/** A confirmed write and a failed projection refresh have different outcomes. */
export function useSettingsSave(
  transaction: Pick<ReturnType<typeof useSettingsTransaction>, 'save'>,
  scope: Readonly<Ref<string | null>>,
  savedMessageKey: string
): (prepare?: Parameters<typeof transaction.save>[0]) => Promise<void> {
  const services = useSettingsWorkspaceServices()
  const { t } = useI18n()
  let generation = 0
  let refresh = 0
  let disposed = false
  watch(scope, () => { ++generation }, { flush: 'sync' })
  onScopeDispose(() => { disposed = true })

  return async prepare => {
    const current = generation
    const captured = scope.value
    const isCurrent = () => !disposed && current === generation && captured === scope.value
    if (!await transaction.save(prepare) || !isCurrent()) return

    const currentRefresh = ++refresh
    services.notifications.showSuccess(t(savedMessageKey))
    try {
      // Refresh projections only; reloading the editor would erase newer/other-section drafts.
      await services.refreshSettings()
    } catch {
      if (isCurrent() && currentRefresh === refresh) {
        services.notifications.showError(t('admin.settings.workspace.refreshFailed'))
      }
    }
  }
}
