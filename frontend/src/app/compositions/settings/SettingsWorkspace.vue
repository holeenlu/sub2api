<template>
  <component :is="services.DelegatedSettings" v-if="!services.isSuperAdmin" />
  <div v-else class="mx-auto max-w-6xl space-y-6">
    <p v-if="loading" role="status">{{ t('common.loading') }}</p>
    <div v-if="error" role="alert" class="rounded-lg border border-red-300 p-4 text-red-700">
      {{ errorMessage }}
      <button v-if="!draft" type="button" class="btn btn-secondary" @click="load">{{ t('common.tryAgain') }}</button>
    </div>
    <p v-if="revisionMissing" role="alert">{{ t('admin.settings.workspace.revisionMissing') }} <button type="button" class="btn btn-secondary" @click="compare">{{ t('common.refresh') }}</button></p>
    <p v-if="uncertain" role="alert">{{ t('admin.settings.workspace.outcomeUnknown') }} <button type="button" class="btn btn-secondary" @click="compare">{{ t('common.refresh') }}</button></p>
    <template v-if="draft">
      <nav :aria-label="t('admin.settings.title')" class="flex flex-wrap gap-2">
        <button v-for="tab in tabs" :key="tab" :data-settings-tab="tab" type="button" class="btn" :class="activeTab === tab ? 'btn-primary' : 'btn-secondary'" :aria-current="activeTab === tab ? 'page' : undefined" :disabled="saving || loading" @click="selectSettingsTab(tab)">{{ t('admin.settings.tabs.' + tab) }}</button>
      </nav>
      <SettingsConflictReview v-if="conflicted" :conflict="conflict" :baseline="baseline" :draft="draft" :dirty-fields="dirtyFields" :saving="saving" @compare="compare" @merge="mergeReviewed" />
      <fieldset :key="services.scope.value || ''" :disabled="saving || loading" class="space-y-6">
        <AdminRolePermissions v-if="activeTab === 'roles'" ref="rolePermissionsEditor" />
      <section v-show="activeTab === 'general'" role="tabpanel" :aria-label="t('admin.settings.tabs.general')" class="space-y-6">
        <ConsoleSettingsPanel :model-value="projectConsole(draft)" @patch="patch" @validity="validity.Console = $event" />
        <AppearanceSettingsPanel :model-value="projectAppearance(draft)" @patch="patch" @validity="validity.Appearance = $event" />
        <NavigationSettingsPanel :model-value="projectNavigation(draft)" @patch="patch" @validity="validity.Navigation = $event" />
      </section>
      <section v-show="activeTab === 'agreement'" role="tabpanel" :aria-label="t('admin.settings.tabs.agreement')" class="space-y-6">
        <AgreementsSettingsPanel :model-value="projectAgreements(draft)" @patch="patch" @validity="validity.Agreements = $event" />
      </section>
      <section v-show="activeTab === 'security'" role="tabpanel" :aria-label="t('admin.settings.tabs.security')" class="space-y-6">
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <AdminKeysSettingsPanel />
        <RegistrationSettingsPanel :model-value="projectRegistration(draft)" @patch="patch" @validity="validity.Registration = $event" />
        <ClientIpSettingsPanel :model-value="projectClientIp(draft)" @patch="patch" @validity="validity.ClientIp = $event" />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <PanelRateLimitSettingsPanel />
        <CaptchaSettingsPanel :model-value="projectCaptcha(draft)" @patch="patch" @validity="validity.Captcha = $event" />
        <LinuxDoSettingsPanel :model-value="projectLinuxDo(draft)" @patch="patch" @validity="validity.LinuxDo = $event" />
        <EmailOAuthSettingsPanel :model-value="projectEmailOAuth(draft)" @patch="patch" @validity="validity.EmailOAuth = $event" />
        <WeChatSettingsPanel :model-value="projectWeChat(draft)" @patch="patch" @validity="validity.WeChat = $event" />
        <DingTalkSettingsPanel :model-value="projectDingTalk(draft)" @patch="patch" @validity="validity.DingTalk = $event" />
        <OidcSettingsPanel :model-value="projectOidc(draft)" @patch="patch" @validity="validity.Oidc = $event" />
        <AdminAccessKeysSettings />
      </section>
      <section v-show="activeTab === 'users'" role="tabpanel" :aria-label="t('admin.settings.tabs.users')" class="space-y-6">
        <UserDefaultsSettingsPanel :model-value="projectUserDefaults(draft)" @patch="patch" />
        <DefaultQuotaSettingsPanel :model-value="projectDefaultQuota(draft, listPlatformIds())" @patch="patch" />
        <DefaultSubscriptionsSettingsPanel :model-value="projectDefaultSubscriptions(draft)" @patch="patch" @validity="validity.DefaultSubscriptions = $event" />
        <AuthDefaultsSettingsPanel :model-value="projectAuthDefaults(draft)" @patch="patch" @validity="validity.AuthDefaults = $event" />
      </section>
      <section v-show="activeTab === 'gateway'" role="tabpanel" :aria-label="t('admin.settings.tabs.gateway')" class="space-y-6">
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <OverloadCooldownSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <RateLimitCooldownSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <StreamTimeoutSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <RectifierSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <BetaPolicySettingsPanel />
        <FastPolicySettingsPanel :model-value="projectFastPolicy(draft)" @patch="patch" @validity="validity.FastPolicy = $event" />
        <ClaudeVersionSettingsPanel :model-value="projectClaudeVersion(draft)" @patch="patch" @validity="validity.ClaudeVersion = $event" />
        <CodexClientsSettingsPanel :model-value="projectCodexClients(draft)" @patch="patch" @validity="validity.CodexClients = $event" />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <BillingProbeSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <OllamaUsageSettingsPanel />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <OpenCodeUsageSettingsPanel />
        <SchedulingSettingsPanel :model-value="projectScheduling(draft)" @patch="patch" @validity="validity.Scheduling = $event" />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <WebSearchSettingsPanel />
        <ErrorVisibilitySettingsPanel :model-value="projectErrorVisibility(draft)" @patch="patch" @validity="validity.ErrorVisibility = $event" />
      </section>
      <section v-show="activeTab === 'features'" role="tabpanel" :aria-label="t('admin.settings.tabs.features')" class="space-y-6">
        <div class="rounded-xl border p-4" data-testid="excel-bps-gateway-settings">
          <label class="flex items-center gap-2"><input id="excel-bps-global-enabled" type="checkbox" :checked="draft.excel_bps_enabled" @change="patch({ excel_bps_enabled: ($event.target as HTMLInputElement).checked })" /> {{ t('autoBPSOps.globalEnabled') }}</label>
          <p class="mt-2 text-sm text-gray-500">{{ t('autoBPSOps.globalHint') }}</p>
        </div>
        <BpsImagesSettingsPanel :model-value="projectBpsImages(draft)" @patch="patch" @validity="validity.BpsImages = $event" />
        <ChannelMonitorSettingsPanel :model-value="projectChannelMonitor(draft)" @patch="patch" @validity="validity.ChannelMonitor = $event" />
        <AvailableChannelsSettingsPanel :model-value="projectAvailableChannels(draft)" @patch="patch" @validity="validity.AvailableChannels = $event" />
        <ModelPlazaSettingsPanel :model-value="projectModelPlaza(draft)" @patch="patch" @validity="validity.ModelPlaza = $event" />
        <BillingModeSettingsPanel :model-value="projectBillingMode(draft)" @patch="patch" />
        <PluginFeatureSettingsPanel :model-value="projectPluginFeature(draft)" @patch="patch" @validity="validity.PluginFeature = $event" />
        <RiskControlSettingsPanel :model-value="projectRiskControl(draft)" @patch="patch" @validity="validity.RiskControl = $event" />
      </section>
      <section v-show="activeTab === 'payment'" role="tabpanel" :aria-label="t('admin.settings.tabs.payment')" class="space-y-6">
        <PaymentSettingsPanel :model-value="projectPayment(draft)" @patch="patch" @validity="validity.Payment = $event" />
        <RechargeBonusSettingsPanel :model-value="projectRechargeBonus(draft)" @patch="patch" @validity="validity.RechargeBonus = $event" />
        <ProvidersSettingsPanel :model-value="projectProviders(draft)" @patch="patch" @validity="validity.Providers = $event" />
      </section>
      <section v-show="activeTab === 'email'" role="tabpanel" :aria-label="t('admin.settings.tabs.email')" class="space-y-6">
        <SmtpSettingsPanel :model-value="projectSmtp(draft)" @patch="patch" @validity="validity.Smtp = $event" />
        <ExpiryNotificationSettingsPanel :model-value="projectExpiryNotification(draft)" @patch="patch" @validity="validity.ExpiryNotification = $event" />
        <BalanceNotificationSettingsPanel :model-value="projectBalanceNotification(draft)" @patch="patch" @validity="validity.BalanceNotification = $event" />
        <QuotaNotificationSettingsPanel :model-value="projectQuotaNotification(draft)" @patch="patch" @validity="validity.QuotaNotification = $event" />
        <p class="text-sm text-gray-500">{{ t("admin.settings.workspace.independent") }}</p>
        <EmailTemplateEditor />
      </section>
        <section v-show="activeTab === 'backup'"><p>{{ t('admin.settings.workspace.independent') }}</p><BackupPage /></section>
      </fieldset>
      <div v-if="activeTab !== 'backup' && activeTab !== 'roles'" class="sticky bottom-0 flex items-center gap-4 rounded-lg border bg-white p-4 dark:bg-dark-900">
        <span>{{ t('admin.settings.workspace.dirtyCount', { count: dirtyFields.length }) }}</span>
        <button type="button" class="btn btn-primary" data-testid="save-settings" :disabled="saving || loading || conflicted || uncertain || revisionMissing || !dirtyFields.length || !valid" @click="saveSettings">{{ t(saving ? 'admin.settings.saving' : 'admin.settings.saveSettings') }}</button>
      </div>
    </template>
  </div>
</template>
<script setup lang="ts">
import { ConsoleSettingsPanel, projectConsole } from '@/features/system/settings'
import { UserDefaultsSettingsPanel, projectUserDefaults } from '@/features/identity/settings'
import { DefaultQuotaSettingsPanel, projectDefaultQuota } from '@/features/access-policy/settings'
import { computed, onMounted, onScopeDispose, reactive, ref, watch } from 'vue'
import { onBeforeRouteLeave } from 'vue-router'
import { useI18n } from 'vue-i18n'
import { AdminRolePermissions, isStepUpCancelled, isStepUpBlocked, stepUpBlockReason } from '@/features/identity'
import { getSettings, updateSettings } from '@/features/system/api'
import { AdminAccessKeysSettings } from '@/features/identity/settings'
import { EmailTemplateEditor, BackupPage } from '@/features/system/settings'
import { extractApiErrorMessage } from '@/shared/lib/apiError'
import { useSettingsTransaction } from './transaction'
import { useSettingsSave } from './useSettingsSave'
import SettingsConflictReview from './SettingsConflictReview.vue'
import { useSettingsWorkspaceServices } from './ports'
import { prepareSettingsPatch } from './preparePatch'
import { listPlatformIds } from '@/features/catalog'
import { BetaPolicySettingsPanel, FastPolicySettingsPanel, projectFastPolicy, ClientIpSettingsPanel, projectClientIp, PanelRateLimitSettingsPanel, CodexClientsSettingsPanel, projectCodexClients, RiskControlSettingsPanel, projectRiskControl } from '@/features/access-policy/settings'
import { AvailableChannelsSettingsPanel, projectAvailableChannels, ModelPlazaSettingsPanel, projectModelPlaza } from '@/features/catalog/settings'
import { RegistrationSettingsPanel, projectRegistration, CaptchaSettingsPanel, projectCaptcha, LinuxDoSettingsPanel, projectLinuxDo, EmailOAuthSettingsPanel, projectEmailOAuth, WeChatSettingsPanel, projectWeChat, DingTalkSettingsPanel, projectDingTalk, OidcSettingsPanel, projectOidc, AuthDefaultsSettingsPanel, projectAuthDefaults } from '@/features/identity/settings'
import { ErrorVisibilitySettingsPanel, projectErrorVisibility, ChannelMonitorSettingsPanel, projectChannelMonitor } from '@/features/operations/settings'
import { PaymentSettingsPanel, projectPayment, RechargeBonusSettingsPanel, projectRechargeBonus, ProvidersSettingsPanel, projectProviders, BalanceNotificationSettingsPanel, projectBalanceNotification } from '@/features/payments/settings'
import { BillingModeSettingsPanel, projectBillingMode } from '@/features/pricing/settings'
import { OverloadCooldownSettingsPanel, RateLimitCooldownSettingsPanel, StreamTimeoutSettingsPanel, RectifierSettingsPanel, ClaudeVersionSettingsPanel, projectClaudeVersion, BillingProbeSettingsPanel, OllamaUsageSettingsPanel, OpenCodeUsageSettingsPanel, SchedulingSettingsPanel, projectScheduling, WebSearchSettingsPanel, BpsImagesSettingsPanel, projectBpsImages, QuotaNotificationSettingsPanel, projectQuotaNotification } from '@/features/resources/settings'
import { AppearanceSettingsPanel, projectAppearance, NavigationSettingsPanel, projectNavigation, AgreementsSettingsPanel, projectAgreements } from '@/features/site/settings'
import { DefaultSubscriptionsSettingsPanel, projectDefaultSubscriptions, ExpiryNotificationSettingsPanel, projectExpiryNotification } from '@/features/subscriptions/settings'
import { AdminKeysSettingsPanel, PluginFeatureSettingsPanel, projectPluginFeature, SmtpSettingsPanel, projectSmtp } from '@/features/system/settings'
const { t } = useI18n()
const services = useSettingsWorkspaceServices()
// Application HTTP owns the single step-up retry, using this transaction's signal.
const scope = computed(() => services.isSuperAdmin ? services.scope.value : null)
const transaction = useSettingsTransaction({ get: getSettings, isCancelled: isStepUpCancelled, put: updateSettings }, scope)
const save = useSettingsSave(transaction, scope, 'admin.settings.settingsSaved')
const { baseline, draft, dirtyFields, error, revisionMissing, loading, saving, conflict, conflicted, uncertain, patch, load, compare, mergeReviewed } = transaction
const tabs = ['general', 'agreement', 'security', 'users', 'gateway', 'features', 'payment', 'email', 'backup', 'roles'] as const
const activeTab = ref<typeof tabs[number]>('general')
const rolePermissionsEditor = ref<InstanceType<typeof AdminRolePermissions> | null>(null)
let tabEpoch = 0
let disposed = false
watch(scope, () => { ++tabEpoch }, { flush: 'sync' })
onScopeDispose(() => { disposed = true; ++tabEpoch })
async function selectSettingsTab(tab: typeof tabs[number]): Promise<void> {
  if (tab === activeTab.value || saving.value || loading.value || !scope.value) return
  const epoch = ++tabEpoch
  const editor = rolePermissionsEditor.value
  if (activeTab.value === 'roles' && editor && !await editor.confirmLeave()) return
  // A confirmation belongs to the original editor and session, including A → B → A changes.
  if (disposed || epoch !== tabEpoch || !scope.value || editor !== rolePermissionsEditor.value) return
  activeTab.value = tab
}
const validity = reactive<Record<string, boolean>>({})
const valid = computed(() => Object.values(validity).every(Boolean))
const errorMessage = computed(() => {
  const issue = error.value
  if (isStepUpBlocked(issue)) return t(stepUpBlockReason(issue) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN' ? 'stepUp.adminApiKeyForbidden' : 'stepUp.notEnabled')
  if (issue && typeof issue === 'object' && 'reason' in issue && issue.reason === 'STEP_UP_ENABLE_REQUIRES_TOTP') return t('admin.settings.security.stepUpEnableRequiresTotp')
  if (issue && typeof issue === 'object' && 'messageKey' in issue && typeof issue.messageKey === 'string') {
    const params = 'params' in issue && typeof issue.params === 'object' && issue.params ? issue.params : {}
    return t(issue.messageKey, Object.fromEntries(Object.entries(params)))
  }
  return extractApiErrorMessage(issue, t('admin.settings.failedToSave'))
})
async function saveSettings() {
  await save(prepareSettingsPatch)
}
function confirmLeave(): boolean { return !dirtyFields.value.length || window.confirm(t('admin.settings.workspace.leave')) }
onBeforeRouteLeave(confirmLeave)
function beforeUnload(event: BeforeUnloadEvent) { if (dirtyFields.value.length) { event.preventDefault(); event.returnValue = '' } }
onMounted(() => {
  window.addEventListener('beforeunload', beforeUnload)
})
onScopeDispose(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
