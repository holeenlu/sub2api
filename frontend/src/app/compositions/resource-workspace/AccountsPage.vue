<template>
    <TablePageLayout>
      <template #filters>
        <p v-if="loadError" role="alert" class="mb-3 text-sm text-red-600 dark:text-red-400">
          {{ extractApiErrorMessage(loadError, t('common.error')) }}
        </p>
        <div class="flex flex-wrap-reverse items-start justify-between gap-3">
          <AccountTableFilters
            v-model:searchQuery="params.search"
            :filters="accountTableFilters"
            :groups="groups"
            @update:filters="updateAccountTableFilters"
            @change="debouncedReload"
            @update:searchQuery="debouncedReload"
          />
          <AccountTableActions :can-create="rolePermissions.can('accounts.authorize')"
            :loading="loading"
            @refresh="handleManualRefresh"
            @create="showCreate = true"
          >
            <template #after>
              <!-- Auto Refresh Dropdown -->
              <div class="relative" ref="autoRefreshDropdownRef">
                <button
                  @click="
                    showAutoRefreshDropdown = !showAutoRefreshDropdown;
                    showAccountToolsDropdown = false
                  "
                  class="btn btn-secondary px-2 md:px-3"
                  :title="t('admin.accounts.autoRefresh')"
                >
                  <Icon name="refresh" size="sm" :class="[autoRefreshEnabled ? 'animate-spin' : '']" />
                  <span class="hidden md:inline">
                    {{
                      autoRefreshEnabled
                        ? t('admin.accounts.autoRefreshCountdown', { seconds: autoRefreshCountdown })
                        : t('admin.accounts.autoRefresh')
                    }}
                  </span>
                </button>
                <div
                  v-if="showAutoRefreshDropdown"
                  class="absolute right-0 z-50 mt-2 w-56 origin-top-right rounded-lg border border-gray-200 bg-white shadow-lg dark:border-dark-700 dark:bg-dark-800"
                >
                  <div class="p-2">
                    <button
                      @click="setAutoRefreshEnabled(!autoRefreshEnabled)"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ t('admin.accounts.enableAutoRefresh') }}</span>
                      <Icon v-if="autoRefreshEnabled" name="check" size="sm" class="text-primary-500" />
                    </button>
                    <div class="my-1 border-t border-gray-100 dark:border-dark-700"></div>
                    <button
                      v-for="sec in autoRefreshIntervals"
                      :key="sec"
                      @click="setAutoRefreshInterval(sec)"
                      class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                    >
                      <span>{{ autoRefreshIntervalLabel(sec) }}</span>
                      <Icon v-if="autoRefreshIntervalSeconds === sec" name="check" size="sm" class="text-primary-500" />
                    </button>
                  </div>
                </div>
              </div>

              <!-- More Tools Dropdown -->
              <div class="relative" ref="accountToolsDropdownRef">
                <button
                  ref="accountToolsTriggerRef"
                  @click="toggleAccountToolsDropdown"
                  class="btn btn-secondary px-2 md:px-3"
                  :title="t('admin.accounts.moreActions')"
                  :aria-expanded="showAccountToolsDropdown"
                >
                  <Icon name="more" size="sm" class="md:mr-1.5" />
                  <span class="hidden md:inline">{{ t('admin.accounts.moreActions') }}</span>
                  <Icon name="chevronDown" size="xs" class="ml-1 hidden md:inline" />
                </button>
                <Teleport to="body">
                  <div
                    v-if="showAccountToolsDropdown"
                    class="fixed z-[9999] origin-top-right overflow-hidden rounded-lg border border-gray-200 bg-white shadow-xl dark:border-dark-700 dark:bg-dark-800"
                    :style="accountToolsDropdownStyle"
                    @click.stop
                  >
                    <div class="overflow-y-auto p-2" :style="{ maxHeight: `${accountToolsDropdownPosition.maxHeight}px` }">
                      <div class="px-2 py-2">
                        <div class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">
                          {{ t('admin.accounts.dataActions') }}
                        </div>
                      </div>
                      <button v-if="rolePermissions.can('accounts.authorize')" class="account-tools-menu-item" @click="openSyncFromCrs">
                        <span class="account-tools-menu-icon bg-blue-50 text-blue-600 dark:bg-blue-900/30 dark:text-blue-300">
                          <Icon name="sync" size="sm" />
                        </span>
                        <span class="flex-1 text-left">{{ t('admin.accounts.syncFromCrs') }}</span>
                      </button>
                      <button v-if="rolePermissions.can('accounts.authorize')" class="account-tools-menu-item" @click="openImportData">
                        <span class="account-tools-menu-icon bg-emerald-50 text-emerald-600 dark:bg-emerald-900/30 dark:text-emerald-300">
                          <Icon name="upload" size="sm" />
                        </span>
                        <span class="flex-1 text-left">{{ t('admin.accounts.dataImport') }}</span>
                      </button>
                      <button v-if="rolePermissions.isSuperAdmin" class="account-tools-menu-item" @click="openExportDataDialogFromMenu">
                        <span class="account-tools-menu-icon bg-violet-50 text-violet-600 dark:bg-violet-900/30 dark:text-violet-300">
                          <Icon name="download" size="sm" />
                        </span>
                        <span class="flex-1 text-left">
                          {{ selIds.length ? t('admin.accounts.dataExportSelected') : t('admin.accounts.dataExport') }}
                        </span>
                        <span
                          v-if="selIds.length"
                          class="rounded-full bg-primary-100 px-2 py-0.5 text-xs font-medium text-primary-700 dark:bg-primary-900/40 dark:text-primary-300"
                        >
                          {{ t('admin.accounts.selectedCount', { count: selIds.length }) }}
                        </span>
                      </button>

                      <div class="my-2 border-t border-gray-100 dark:border-dark-700"></div>
                      <div class="px-2 py-2">
                        <div class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">
                          {{ t('admin.accounts.toolActions') }}
                        </div>
                      </div>
                      <button v-if="rolePermissions.isSuperAdmin" class="account-tools-menu-item" @click="openErrorPassthrough">
                        <span class="account-tools-menu-icon bg-amber-50 text-amber-600 dark:bg-amber-900/30 dark:text-amber-300">
                          <Icon name="shield" size="sm" />
                        </span>
                        <span class="flex-1 text-left">{{ t('admin.errorPassthrough.title') }}</span>
                      </button>
                      <button v-if="rolePermissions.isSuperAdmin" class="account-tools-menu-item" @click="openTLSFingerprintProfiles">
                        <span class="account-tools-menu-icon bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-200">
                          <Icon name="lock" size="sm" />
                        </span>
                        <span class="flex-1 text-left">{{ t('admin.tlsFingerprintProfiles.title') }}</span>
                      </button>

                      <div class="my-2 border-t border-gray-100 dark:border-dark-700"></div>
                      <div class="px-2 py-2">
                        <div class="flex items-center justify-between gap-3">
                          <span class="text-xs font-semibold uppercase tracking-wide text-gray-400 dark:text-gray-500">
                            {{ t('admin.accounts.viewColumns') }}
                          </span>
                          <Icon name="grid" size="sm" class="text-gray-400" />
                        </div>
                      </div>
                      <div class="grid grid-cols-1 gap-1">
                        <button
                          v-for="col in toggleableColumns"
                          :key="col.key"
                          @click="toggleColumn(col.key)"
                          class="flex w-full items-center justify-between rounded-md px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700"
                        >
                          <span class="truncate">{{ col.label }}</span>
                          <Icon v-if="isColumnVisible(col.key)" name="check" size="sm" class="text-primary-500" />
                        </button>
                      </div>
                    </div>
                  </div>
                </Teleport>
              </div>
            </template>
          </AccountTableActions>
        </div>
        <div
          v-if="hasPendingListSync"
          class="mt-2 flex items-center justify-between rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-sm text-amber-800 dark:border-amber-700/40 dark:bg-amber-900/20 dark:text-amber-200"
        >
          <span>{{ t('admin.accounts.listPendingSyncHint') }}</span>
          <button
            class="btn btn-secondary px-2 py-1 text-xs"
            @click="syncPendingListChanges"
          >
            {{ t('admin.accounts.listPendingSyncAction') }}
          </button>
        </div>
      </template>
      <template #table>
        <AccountBulkActionsBar
          :selected-ids="selIds"
          :total-results="pagination.total"
          :selecting-all="selectingAllResults"
          :all-results-selected="allResultsSelected"
          @delete="handleBulkDelete"
          @reset-status="handleBulkResetStatus"
          @refresh-token="handleBulkRefreshToken"
          @probe-upstream-billing="handleBulkProbeUpstreamBilling"
          @edit-selected="openBulkEditSelected"
          @edit-filtered="openBulkEditFiltered"
          @clear="clearSelection"
          @select-page="selectPage"
          @select-all-results="handleSelectAllResults"
          @toggle-schedulable="handleBulkToggleSchedulable"
        />
        <div ref="accountTableRef" class="flex min-h-0 flex-1 flex-col overflow-hidden">
        <DataTable
          ref="dataTableRef"
          :columns="cols"
          :data="accounts"
          :loading="loading"
          row-key="id"
          :server-side-sort="true"
          @sort="handleSort"
          default-sort-key="name"
          default-sort-order="asc"
          :sort-storage-key="ACCOUNT_SORT_STORAGE_KEY"
          :estimate-row-height="156"
          :overscan="5"
          :virtualize-threshold="50"
        >
          <template #header-select>
            <input
              type="checkbox"
              class="h-4 w-4 cursor-pointer rounded border-gray-300 text-primary-600 focus:ring-primary-500"
              :checked="allVisibleSelected"
              @click.stop
              @change="toggleSelectAllVisible($event)"
            />
          </template>
          <template #cell-select="{ row }">
            <input type="checkbox" :checked="isSelected(row.id)" @change="toggleSel(row.id)" class="rounded border-gray-300 text-primary-600 focus:ring-primary-500" />
          </template>
          <template #cell-id="{ value }">
            <span class="font-mono text-xs text-gray-500 dark:text-gray-400">#{{ value }}</span>
          </template>
          <template #cell-name="{ row }">
            <AccountInfoCell :name="row.name" :id="row.id" :email="accountDisplayEmail(row)"
              :homepage="accountHomepageUrl(row)"
              @copy="copyToClipboard(accountDisplayEmail(row) || row.name)" :platform="row.platform" :type="row.type"
                  :auth-mode="getOpenAIAuthMode(row)"
                  :plan-type="getAccountPlanType(row)"
                  :method="accountBadgeString(row.extra?.vendor_auth_method)"
                  :billing-type="accountBadgeString(row.credentials?.billing_type)"
                  :authorization-modes="row.authorization_modes"
                  :compact="{ mode: row.extra?.openai_compact_mode, supported: row.extra?.openai_compact_supported, checkedAt: row.extra?.openai_compact_checked_at }"
                  :privacy-mode="accountBadgeString(row.extra?.privacy_mode, row.parent_privacy_mode)"
                  :subscription-expires-at="accountBadgeString(row.credentials?.subscription_expires_at, row.parent_subscription_expires_at)" >
              <template #details>
                <span
                  v-if="getAntigravityTierLabel(row)"
                  :class="['inline-block rounded px-1.5 py-0.5 text-[10px] font-medium', getAntigravityTierClass(row)]"
                >
                  {{ getAntigravityTierLabel(row) }}
                </span>
              </template>
              <BPSFailureBadge :account="row" :groups="accountGroupsForRow(row)" />
              <CodexDiagnosticBadge :summary="row.codex_diagnostic" @open="openCodexDiagnostic(row)" />
            </AccountInfoCell>
          </template>
          <template #cell-notes="{ value }">
            <span v-if="value" :title="value" class="block max-w-xs truncate text-sm text-gray-600 dark:text-gray-300">{{ value }}</span>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>
          <template #cell-capacity="{ row }">
            <AccountCapacityCell :account="row" />
          </template>
          <template #cell-status="{ row }">
            <div class="flex items-center gap-1.5">
              <AccountStatusIndicator :account="row" @show-temp-unsched="handleShowTempUnsched" />
            </div>
          </template>
          <template #cell-schedulable="{ row }">
            <button v-if="rolePermissions.can('accounts.manage')" @click="handleToggleSchedulable(row)" :disabled="togglingSchedulable.has(row.id)" class="relative inline-flex h-5 w-9 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none focus:ring-2 focus:ring-primary-500 focus:ring-offset-2 disabled:cursor-not-allowed disabled:opacity-50 dark:focus:ring-offset-dark-800" :class="[row.schedulable ? 'bg-primary-500 hover:bg-primary-600' : 'bg-gray-200 hover:bg-gray-300 dark:bg-dark-600 dark:hover:bg-dark-500']" :title="row.schedulable ? t('admin.accounts.schedulableEnabled') : t('admin.accounts.schedulableDisabled')">
              <span class="pointer-events-none inline-block h-4 w-4 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out" :class="[row.schedulable ? 'translate-x-4' : 'translate-x-0']" />
            </button>
          </template>
          <template #cell-today_stats="{ row }">
            <AccountTodayStatsCell
              :stats="todayStatsByAccountId[String(row.id)] ?? null"
              :loading="todayStatsLoading"
              :error="todayStatsError"
            />
          </template>
          <template #cell-groups="{ row }">
            <AccountGroupsCell :groups="accountGroupsForRow(row)" :max-display="4" />
          </template>
          <template #header-usage="{ column }">
            <div class="flex items-center">
              <span>{{ column.label }}</span>
              <HelpTooltip :content="t('admin.accounts.usageWindowsHint')" width-class="w-72" />
            </div>
          </template>
          <template #cell-usage="{ row }">
            <AccountUsageCell
              :account="row"
              :today-stats="todayStatsByAccountId[String(row.id)] ?? null"
              :today-stats-loading="todayStatsLoading"
              :manual-refresh-token="usageManualRefreshToken"
              :batched-usage="usageBatchByAccountId[String(row.id)] ?? null"
              :batched-usage-error="usageBatchErrorByAccountId[String(row.id)] ?? null"
              :batched-usage-loading="usageBatchLoadingByAccountId[String(row.id)] === true"
              :request-batched-usage="isDesktopViewport ? queueBatchedUsage : null"
              @account-updated="handleAccountUpdated"
              @usage-loaded="handleAccountUsageLoaded(row.id, $event)"
            />
          </template>
          <template #cell-proxy="{ row }">
            <div class="flex flex-col gap-1">
              <div v-if="row.proxy" class="flex items-center gap-2">
                <span class="text-sm text-gray-700 dark:text-gray-300">{{ row.proxy.name }}</span>
                <span v-if="row.proxy.country_code" class="text-xs text-gray-500 dark:text-gray-400">
                  ({{ row.proxy.country_code }})
                </span>
              </div>
              <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
              <div v-if="row.proxy && row.proxy.expires_at" class="flex items-center gap-2 text-xs">
                <span class="text-gray-600 dark:text-gray-300">{{ formatDateTime(row.proxy.expires_at) }}</span>
                <span :class="proxyExpiryBadge(row.proxy)">{{ proxyExpiryText(row.proxy) }}</span>
              </div>
              <div v-if="row.proxy_fallback_origin_id" class="flex items-center gap-1">
                <span class="inline-flex items-center px-1.5 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800 dark:bg-yellow-900 dark:text-yellow-200" :title="t('admin.accounts.fallbackActiveTip', { origin: row.proxy_fallback_origin_name })">
                  {{ t('admin.accounts.fallbackActive') }}
                </span>
                <button v-if="rolePermissions.can('accounts.manage')" class="text-xs px-1.5 py-0.5 rounded border border-gray-300 dark:border-dark-600 text-gray-600 dark:text-gray-300 hover:bg-gray-100 dark:hover:bg-dark-700" @click="onRevertFallback(row)">{{ t('admin.accounts.revertProxy') }}</button>
              </div>
            </div>
          </template>
          <template #cell-rate_multiplier="{ row }">
            <span class="inline-flex items-center gap-1 text-sm font-mono text-gray-700 dark:text-gray-300">
              <span>{{ formatMultiplier(row.rate_multiplier ?? 1) }}x</span>
              <span
                v-if="row.extra?.upstream_billing_rate_sync_enabled === true"
                class="inline-flex cursor-help text-emerald-600 dark:text-emerald-400"
                :aria-label="t('admin.accounts.upstreamBilling.syncedRateTooltip')"
                :title="t('admin.accounts.upstreamBilling.syncedRateTooltip')"
                data-testid="account-rate-sync-indicator"
              >
                <Icon name="sync" size="xs" />
              </span>
            </span>
          </template>
          <template #header-upstream_billing_rate="{ column }">
            <div class="flex items-center gap-1">
              <span>{{ column.label }}</span>
              <span @click.stop>
                <HelpTooltip :content="t('admin.accounts.upstreamBilling.trustWarning')" width-class="w-80" />
              </span>
            </div>
          </template>
          <template #cell-upstream_billing_rate="{ row }">
            <UpstreamBillingRateCell
              :account="row"
              :global-probe-enabled="upstreamBillingProbeGloballyEnabled"
              :now="upstreamBillingNow"
              :probing="probingUpstreamBilling.has(row.id)"
              @probe="handleProbeUpstreamBilling(row)"
            />
          </template>
          <template #cell-priority="{ row }">
            <AccountPriorityCell
              :account="row"
              :scope-key="accountActorScope"
              :can-edit="canEditAccountPriority"
              @updated="handleAccountUpdated"
              @error="(message: string) => appStore.showError(message)"
            />
          </template>
          <template #header-scheduler_score="{ column }">
            <div class="flex items-center">
              <span>{{ column.label }}</span>
              <HelpTooltip :content="t('admin.accounts.schedulerScore.hint')" width-class="w-80" />
            </div>
          </template>
          <template #cell-scheduler_score="{ row }">
            <div v-if="getSchedulerScoreRows(row).length" class="flex min-w-[7rem] flex-col gap-0.5 font-mono text-[11px] leading-4">
              <div
                v-for="score in getSchedulerScoreRows(row)"
                :key="String(score.group_id)"
                class="flex items-center gap-1 whitespace-nowrap text-gray-700 dark:text-gray-300"
                :title="`${formatSchedulerScoreGroup(score)} / ${formatSchedulerScore(score.base_score)}`"
              >
                <span class="max-w-[4.75rem] truncate text-gray-500 dark:text-dark-400">{{ formatSchedulerScoreGroup(score) }}</span>
                <span class="text-gray-300 dark:text-gray-600">/</span>
                <span>{{ formatSchedulerScore(score.base_score) }}</span>
              </div>
            </div>
            <span v-else class="text-sm text-gray-400 dark:text-dark-500">-</span>
          </template>
          <template #cell-last_used_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatRelativeTime(value) }}</span>
          </template>
          <template #cell-created_by_name="{ row }">
            <span :title="row.created_by ? '#' + row.created_by : undefined">{{ row.created_by_name || t('admin.accounts.creatorUnknown') }}</span>
          </template>
          <template #cell-created_by_role="{ value }">
            {{ value ? (value === 'system' ? t('admin.accounts.creatorSystem') : t('admin.users.roles.' + value)) : t('admin.accounts.creatorUnknown') }}
          </template>
          <template #cell-created_at="{ value }">
            <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatDateTime(value) }}</span>
          </template>
          <template #cell-expires_at="{ row, value }">
            <div class="flex flex-col items-start gap-1">
              <span class="text-sm text-gray-500 dark:text-dark-400">{{ formatExpiresAt(value) }}</span>
              <div v-if="isExpired(value) || (row.auto_pause_on_expired && value)" class="flex items-center gap-1">
                <span
                  v-if="isExpired(value)"
                  class="inline-flex items-center rounded-md bg-amber-100 px-2 py-0.5 text-xs font-medium text-amber-700 dark:bg-amber-900/30 dark:text-amber-300"
                >
                  {{ t('admin.accounts.expired') }}
                </span>
                <span
                  v-if="row.auto_pause_on_expired && value"
                  class="inline-flex items-center rounded-md bg-emerald-100 px-2 py-0.5 text-xs font-medium text-emerald-700 dark:bg-emerald-900/30 dark:text-emerald-300"
                >
                  {{ t('admin.accounts.autoPauseOnExpired') }}
                </span>
              </div>
            </div>
          </template>
          <template #cell-actions="{ row }">
            <div class="flex items-center gap-1">
              <button v-if="rolePermissions.can('accounts.manage') && ['anthropic', 'openai'].includes(row.platform)" type="button" data-testid="account-models" @click="openAccountModels(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700">
                <Icon name="brain" size="sm" />
                <span class="text-xs">{{ t('admin.accounts.modelWhitelist') }}</span>
              </button>
              <button v-if="rolePermissions.can('accounts.manage')" @click="handleEdit(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-primary-600 dark:hover:bg-dark-700 dark:hover:text-primary-400">
                <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M16.862 4.487l1.687-1.688a1.875 1.875 0 112.652 2.652L10.582 16.07a4.5 4.5 0 01-1.897 1.13L6 18l.8-2.685a4.5 4.5 0 011.13-1.897l8.932-8.931zm0 0L19.5 7.125M18 14v4.75A2.25 2.25 0 0115.75 21H5.25A2.25 2.25 0 013 18.75V8.25A2.25 2.25 0 015.25 6H10" /></svg>
                <span class="text-xs">{{ t('common.edit') }}</span>
              </button>
              <button v-if="rolePermissions.can('accounts.manage')" @click="handleDelete(row)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-red-50 hover:text-red-600 dark:hover:bg-red-900/20 dark:hover:text-red-400">
                <svg class="h-4 w-4" fill="none" stroke="currentColor" viewBox="0 0 24 24" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M14.74 9l-.346 9m-4.788 0L9.26 9m9.968-3.21c.342.052.682.107 1.022.166m-1.022-.165L18.16 19.673a2.25 2.25 0 01-2.244 2.077H8.084a2.25 2.25 0 01-2.244-2.077L4.772 5.79m14.456 0a48.108 48.108 0 00-3.478-.397m-12 .562c.34-.059.68-.114 1.022-.165m0 0a48.11 48.11 0 013.478-.397m7.5 0v-.916c0-1.18-.91-2.164-2.09-2.201a51.964 51.964 0 00-3.32 0c-1.18.037-2.09 1.022-2.09 2.201v.916m7.5 0a48.667 48.667 0 00-7.5 0" /></svg>
                <span class="text-xs">{{ t('common.delete') }}</span>
              </button>
              <button @click="openMenu(row, $event)" class="flex flex-col items-center gap-0.5 rounded-lg p-1.5 text-gray-500 transition-colors hover:bg-gray-100 hover:text-gray-900 dark:hover:bg-dark-700 dark:hover:text-white">
                <svg class="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5"><path stroke-linecap="round" stroke-linejoin="round" d="M6.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0zM12.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0zM18.75 12a.75.75 0 11-1.5 0 .75.75 0 011.5 0z" /></svg>
                <span class="text-xs">{{ t('common.more') }}</span>
              </button>
            </div>
          </template>
        </DataTable>
        </div>
      </template>
      <template #pagination><Pagination v-if="pagination.total > 0" :page="pagination.page" :total="pagination.total" :page-size="pagination.page_size" @update:page="handlePageChange" @update:pageSize="handlePageSizeChange" /></template>
    </TablePageLayout>
    <CreateAccountModal v-if="showCreate" :allow-rate-edit="rolePermissions.can('billing.rates.update')" :allow-system-settings="rolePermissions.isSuperAdmin" :show="showCreate" :proxies="proxies" :groups="groups" @close="showCreate = false" @created="reload" />
    <EditAccountModal v-if="showEdit" :allow-rate-edit="rolePermissions.can('billing.rates.update')" :show-cost="rolePermissions.can('billing.cost.read')" :allow-authorize="rolePermissions.can('accounts.authorize')" :allow-transport-edit="rolePermissions.isSuperAdmin" :show="showEdit" :account="edAcc" :proxies="proxies" :groups="groups" @close="closeEditModal" @updated="handleAccountUpdated" />
    <AccountModelReferencesDialog v-if="modelReferenceAccount" :show="true" :account-id="modelReferenceAccount.id" :platform="modelReferenceAccount.platform" @close="closeAccountModels" @saved="refreshAccountModelsRow" />
    <ReAuthAccountModal :allow-extra-edit="rolePermissions.isSuperAdmin" :show="showReAuth" :account="reAuthAcc" @close="closeReAuthModal" @reauthorized="handleAccountUpdated" />
    <AccountTestModal :show="showTest" :account="testingAcc" @close="closeTestModal" />
    <AccountStatsModal :show="showStats" :account="statsAcc" @close="closeStatsModal" />
    <ScheduledTestsPanel :show="showSchedulePanel" :account-id="scheduleAcc?.id ?? null" :model-options="scheduleModelOptions" @close="closeSchedulePanel" />
    <CodexDiagnosticModal :show="showCodexDiagnostic" :account="codexDiagnosticAcc" @close="showCodexDiagnostic = false" @completed="handleCodexDiagnosticCompleted" />
    <AccountActionMenu :can-diagnose="rolePermissions.can('accounts.diagnostics')" :can-manage="rolePermissions.can('accounts.manage')" @codex-diagnostic="openCodexDiagnostic" :show="menu.show" :account="menu.acc" :anchor-rect="menu.anchorRect" @close="menu.show = false" @test="handleTest" @stats="handleViewStats" @schedule="handleSchedule" @duplicate="handleDuplicateAccount" @reauth="handleReAuth" @refresh-token="handleRefresh" @recover-state="handleRecoverState" @reset-quota="handleResetQuota" @set-privacy="handleSetPrivacy" @create-spark-shadow="handleCreateSparkShadow" />
    <SyncFromCrsModal :show="showSync" @close="showSync = false" @synced="reload" />
    <ImportDataModal :show="showImportData" @close="showImportData = false" @imported="handleDataImported" />
    <BulkEditAccountModal
      v-if="showBulkEdit"
      :allow-rate-edit="rolePermissions.can('billing.rates.update')"
      :allow-authorize="rolePermissions.can('accounts.authorize')"
      :allow-transport-edit="rolePermissions.isSuperAdmin"
      :show="showBulkEdit"
      :account-ids="selIds"
      :selected-platforms="selPlatforms"
      :selected-types="selTypes"
      :target="bulkEditTarget ?? undefined"
      :proxies="proxies"
      :groups="groups"
      @close="showBulkEdit = false"
      @updated="handleBulkUpdated"
    />
    <TempUnschedStatusModal :show="showTempUnsched" :account="tempUnschedAcc" @close="showTempUnsched = false" @reset="handleTempUnschedReset" />
    <ConfirmDialog :show="showDeleteDialog" :title="t('admin.accounts.deleteAccount')" :message="t('admin.accounts.deleteConfirm', { name: deletingAcc?.name })" :confirm-text="t('common.delete')" :cancel-text="t('common.cancel')" :danger="true" @confirm="confirmDelete" @cancel="showDeleteDialog = false" />
    <ConfirmDialog :show="showCreateShadowDialog" :title="t('admin.accounts.createSparkShadow')" :message="t('admin.accounts.createSparkShadowConfirm', { name: creatingShadowAcc?.name })" @confirm="confirmCreateSparkShadow" @cancel="showCreateShadowDialog = false" />
    <ConfirmDialog :show="showExportDataDialog" :title="t('admin.accounts.dataExport')" :message="t('admin.accounts.dataExportConfirmMessage')" :confirm-text="t('admin.accounts.dataExportConfirm')" :cancel-text="t('common.cancel')" @confirm="handleExportData" @cancel="showExportDataDialog = false">
      <label class="flex items-center gap-2 text-sm text-gray-700 dark:text-gray-300">
        <input type="checkbox" class="h-4 w-4 rounded border-gray-300 text-primary-600 focus:ring-primary-500" v-model="includeProxyOnExport" />
        <span>{{ t('admin.accounts.dataExportIncludeProxies') }}</span>
      </label>
    </ConfirmDialog>
    <ErrorPassthroughRulesModal :show="showErrorPassthrough" @close="showErrorPassthrough = false" />
    <TLSFingerprintProfilesModal :show="showTLSFingerprintProfiles" @close="showTLSFingerprintProfiles = false" />
    <TotpStepUpDialog :controller="accountExportStepUp" />
</template>

<script setup lang="ts">
import { brandFilePrefix } from '@/shared/config/project'
import { createAccountDisplay } from './model/accountDisplay'
import { useAccountUsageBatch } from './queries/useAccountUsageBatch'
import { useAuthStore as useRolePermissions } from '@/features/identity'
const rolePermissions = useRolePermissions()

import { ref, shallowRef, shallowReactive, reactive, computed, onMounted, onBeforeUnmount, onUnmounted, toRaw, watch } from 'vue'
import { useIntervalFn } from '@vueuse/core'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/app/model/app'
import { useAuthStore } from '@/features/identity'
import { accountsAPI, proxiesAPI, type AccountListFilters } from '@/features/resources'
import { groupsAPI } from '@/features/access-policy'
import { CONCRETE_PLATFORM_OPTIONS } from '@/features/catalog'
import { useTableLoader } from '@/shared/lib/useTableLoader'
import { useSwipeSelect, type SwipeSelectVirtualContext } from '@/shared/lib/useSwipeSelect'
import { useTableSelection } from '@/shared/lib/useTableSelection'
import { useStepUp, isStepUpBlocked, isStepUpCancelled, stepUpBlockReason } from '@/features/identity'
import { TotpStepUpDialog as TotpStepUpDialog } from '@/features/identity'
import TablePageLayout from '@/shared/ui/TablePageLayout.vue'
import DataTable from '@/shared/ui/DataTable.vue'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import Pagination from '@/shared/ui/Pagination.vue'
import ConfirmDialog from '@/shared/ui/ConfirmDialog.vue'
import { SyncFromCrsModal, TempUnschedStatusModal } from '@/features/resources/panels'
import { loadCreateAccountModal, loadEditAccountModal, loadBulkEditAccountModal } from '@/features/resources/panelLoaders'
import { lazyPanel } from '../panelLoadingServices'
import { AccountTableActions } from '@/features/resources/managementPanels'
import { AccountTableFilters } from '@/features/resources/managementPanels'
import type { AccountTableFilterValues } from '@/features/resources/managementPanels'
import type { DataTableHandle } from '@/shared/ui/tableTypes'
import { AccountBulkActionsBar } from '@/features/resources/managementPanels'
import { AccountActionMenu } from '@/features/resources/managementPanels'
import { CodexDiagnosticModal } from '@/features/resources/managementPanels'
import { CodexDiagnosticBadge } from '@/features/resources/managementPanels'
import { AccountImportDataModal as ImportDataModal } from '@/features/resources/managementPanels'
import { ReAuthAccountModal } from '@/features/resources/panels'
import { AccountTestModal } from '@/features/resources/panels'
import { AccountStatsModal } from '@/features/resources/panels'
import { ScheduledTestsPanel } from '@/features/resources/managementPanels'
import type { SelectOption } from '@/shared/ui/Select.vue'
import { AccountStatusIndicator } from '@/features/resources/panels'
import { AccountUsageCell } from '@/features/resources/panels'
import { AccountTodayStatsCell } from '@/features/resources/panels'
import { AccountGroupsCell } from '@/features/resources/panels'
import { AccountCapacityCell } from '@/features/resources/panels'
import { BPSFailureBadge } from '@/features/resources/panels'
import { UpstreamBillingRateCell } from '@/features/resources/panels'
import { AccountPriorityCell } from '@/features/resources/panels'
import AccountInfoCell from '@/app/compositions/resource-workspace/components/AccountInfoCell.vue'
import { useClipboard } from '@/shared/lib/useClipboard'
import Icon from '@/shared/ui/icons/Icon.vue'
import { ErrorPassthroughRulesModal } from '@/features/resources/managementPanels'
import { TLSFingerprintProfilesModal } from '@/features/resources/managementPanels'
import { fetchAllAccountIds } from '@/features/resources'
import { buildGrokUsageRefreshKey, buildOpenAIUsageRefreshKey } from '@/features/resources'
import { formatDateTime, formatRelativeTime } from '@/shared/lib/format'
import { proxyExpiryBadgeClass, proxyExpiryLabelKey } from '@/features/resources'
import { extractApiErrorMessage, readApiError } from '@/shared/lib/apiError'
import { getFloatingPanelPosition } from '@/shared/lib/floatingPanel'
import { formatMultiplier } from '@/shared/lib/formatters'
import type { Account, AccountListItem, ClaudeModel } from '@/features/resources/api/accountTypes';
import type { AccountPlatform, AccountType } from '@/features/resources/api/platformTypes';
import type { AccountSchedulerGroupScore, UpstreamBillingProbeSnapshot } from '@/features/resources/api/accountObservationTypes';
import type { Proxy as AccountProxy } from '@/features/resources/api/proxyTypes';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import type { WindowStats } from '@/features/resources/api/accountUsageTypes';

const CreateAccountModal = lazyPanel(loadCreateAccountModal)
const EditAccountModal = lazyPanel(loadEditAccountModal)
const BulkEditAccountModal = lazyPanel(loadBulkEditAccountModal)
const AccountModelReferencesDialog = lazyPanel((): Promise<typeof import('./AccountModelReferencesDialog.vue')> => import('./AccountModelReferencesDialog.vue'))
const { t } = useI18n()
const { copyToClipboard } = useClipboard()
const appStore = useAppStore()
const authStore = useAuthStore()

let workspaceActive = true
interface AccountAction {
  readonly controller: AbortController
  readonly targets: readonly number[]
  current(): boolean
  wait<T>(work: () => Promise<T>): Promise<T>
  finish(): void
}
// Page-owned action lanes. Mutations are keyed by action + account, while
// detail intents replace only older detail reads. No shared command registry.
const accountActions = shallowReactive(new Map<string, AccountAction>())
const accountActorScope = () => JSON.stringify([
  authStore.identityScopeKey, authStore.user?.id, authStore.user?.role, authStore.user?.policy_version,
  authStore.isSuperAdmin, rolePermissions.can('accounts.manage'),
  rolePermissions.can('accounts.authorize'), rolePermissions.can('accounts.diagnostics')
])
const canEditAccountPriority = (): boolean => workspaceActive && authStore.isAuthenticated
  && rolePermissions.can('accounts.manage')
  && rolePermissions.canEditAdminField('account.update', 'priority')

function cancelAccountAction(key: string): void {
  accountActions.get(key)?.controller.abort()
  accountActions.delete(key)
}
function beginAccountAction(key: string, targets: readonly number[] = [], replace = false, readTarget?: () => string): AccountAction | null {
  if (!workspaceActive || (accountActions.has(key) && !replace)) return null
  cancelAccountAction(key)
  const scope = accountActorScope()
  const target = readTarget?.()
  const controller = new AbortController()
  const action: AccountAction = {
    controller, targets: [...targets],
    current: () => workspaceActive && !controller.signal.aborted && scope === accountActorScope()
      && accountActions.get(key) === action && (!readTarget || readTarget() === target),
    async wait<T>(work: () => Promise<T>): Promise<T> {
      if (!action.current()) throw new DOMException('Account action expired', 'AbortError')
      const result = await work()
      if (!action.current()) throw new DOMException('Account action expired', 'AbortError')
      return result
    },
    finish: () => { if (accountActions.get(key) === action) accountActions.delete(key) }
  }
  accountActions.set(key, action)
  return action
}
function activeActionTargets(prefix: string): Set<number> {
  return new Set([...accountActions.entries()].filter(([key]) => key.startsWith(prefix)).flatMap(([, action]) => [...action.targets]))
}
let accountSelectionRevision = 0
function applyActionSelection(revision: number, change: () => void): void {
  if (revision === accountSelectionRevision) change()
}
function invalidateAccountActions(): void {
  for (const key of [...accountActions.keys()]) cancelAccountAction(key)
  accountExportStepUp.onCancel()
  for (const shown of [showCreate, showEdit, showTest, showStats, showSchedulePanel, showDeleteDialog,
    showCreateShadowDialog, showExportDataDialog, showBulkEdit, showReAuth, showCodexDiagnostic]) shown.value = false
  edAcc.value = testingAcc.value = statsAcc.value = scheduleAcc.value = null
  deletingAcc.value = creatingShadowAcc.value = reAuthAcc.value = codexDiagnosticAcc.value = null
  scheduleModelOptions.value = []
  bulkEditTarget.value = null
  modelReferenceAccount.value = null
  menu.show = false
  menu.acc = null
  clearSelection()
}
watch(accountActorScope, invalidateAccountActions, { flush: 'sync' })
onBeforeUnmount(() => { workspaceActive = false; invalidateAccountActions() })

const proxies = ref<AccountProxy[]>([])
const groups = ref<AdminGroup[]>([])
const groupsByID = computed(() => new Map(groups.value.map(group => [group.id, group])))
const accountGroupsForRow = (account: Pick<AccountListItem, 'group_ids'>): AdminGroup[] => {
  const groupIDs = account.group_ids ?? []
  if (groupIDs.length === 0) return []
  return groupIDs.map(id => groupsByID.value.get(id)).filter((group): group is AdminGroup => Boolean(group))
}
const accountTableRef = ref<HTMLElement | null>(null)
const dataTableRef = shallowRef<DataTableHandle<AccountListItem> | null>(null)
type AccountBulkEditTarget = {
      mode: 'selected'
      accountIds: number[]
      selectedPlatforms: AccountPlatform[]
      selectedTypes: AccountType[]
}
const selPlatforms = computed<AccountPlatform[]>(() => {
  const platforms = new Set(
    accounts.value
      .filter(a => isSelected(a.id))
      .map(a => a.platform)
  )
  return [...platforms]
})
const selTypes = computed<AccountType[]>(() => {
  const types = new Set(
    accounts.value
      .filter(a => isSelected(a.id))
      .map(a => a.type)
  )
  return [...types]
})
const showCreate = ref(false)
const showEdit = ref(false)
const modelReferenceAccount = ref<Pick<AccountListItem, 'id' | 'platform'> | null>(null)
function openAccountModels(account: AccountListItem) {
  if (!authStore.isAuthenticated || !rolePermissions.can('accounts.manage') || !['anthropic', 'openai'].includes(account.platform)) return
  cancelAccountAction('model-reference-row')
  modelReferenceAccount.value = { id: account.id, platform: account.platform }
}
function closeAccountModels() {
  cancelAccountAction('model-reference-row')
  modelReferenceAccount.value = null
}
async function refreshAccountModelsRow(accountID: number) {
  if (!rolePermissions.can('accounts.manage') || modelReferenceAccount.value?.id !== accountID) return
  const action = beginAccountAction('model-reference-row', [accountID], true)
  if (!action) return
  try {
    const account = await action.wait(() => accountsAPI.getById(accountID, { signal: action.controller.signal }))
    if (modelReferenceAccount.value?.id === accountID && account.id === accountID && account.platform === modelReferenceAccount.value.platform) handleAccountUpdated(account)
  } catch (failure) {
    if (action.current()) appStore.showError(extractApiErrorMessage(failure, t('admin.accounts.modelReference.loadFailed')))
  } finally { action.finish() }
}
const showCodexDiagnostic = ref(false)
const codexDiagnosticAcc = ref<Account | null>(null)
function openCodexDiagnostic(account: Account) { codexDiagnosticAcc.value = account; showCodexDiagnostic.value = true }
async function handleCodexDiagnosticCompleted(accountID: number) {
    const action = beginAccountAction('diagnostic:' + accountID, [accountID], false);
    if (!action)
        return;
    try {
        try {
            handleAccountUpdated(await action.wait(() => accountsAPI.getById(accountID, { signal: action.controller.signal })));
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to refresh account after diagnostic', error);
        }
    }
    finally {
        action.finish();
    }
}
const showSync = ref(false)
const showImportData = ref(false)
const showExportDataDialog = ref(false)
const includeProxyOnExport = ref(true)
const showBulkEdit = ref(false)
const bulkEditTarget = ref<AccountBulkEditTarget | null>(null)
const showTempUnsched = ref(false)
const showDeleteDialog = ref(false)
const showCreateShadowDialog = ref(false)
const showReAuth = ref(false)
const showTest = ref(false)
const showStats = ref(false)
const showErrorPassthrough = ref(false)
const showTLSFingerprintProfiles = ref(false)
const edAcc = ref<Account | null>(null)
const tempUnschedAcc = ref<Account | null>(null)
const deletingAcc = ref<Account | null>(null)
const creatingShadowAcc = ref<Account | null>(null)
const reAuthAcc = ref<Account | null>(null)
const testingAcc = ref<Account | null>(null)
const statsAcc = ref<Account | null>(null)
const showSchedulePanel = ref(false)
const scheduleAcc = ref<Account | null>(null)
const scheduleModelOptions = ref<SelectOption[]>([])
const togglingSchedulable = computed(() => activeActionTargets('schedulable:'))
const menu = reactive<{show:boolean, acc:Account|null, anchorRect:DOMRect|null}>({ show: false, acc: null, anchorRect: null })
const probingUpstreamBilling = computed(() => activeActionTargets('probe:'))
const upstreamBillingProbeGloballyEnabled = ref<boolean | undefined>(undefined)
const upstreamBillingNow = ref(Date.now())
const upstreamBillingRateETag = ref<string | null>(null)
const upstreamBillingRateRefreshing = ref(false)
let upstreamBillingRateAbortController: AbortController | null = null
useIntervalFn(() => { upstreamBillingNow.value = Date.now() }, 60_000)

// Account tools dropdown
const showAccountToolsDropdown = ref(false)
const accountToolsDropdownRef = ref<HTMLElement | null>(null)
const accountToolsTriggerRef = ref<HTMLElement | null>(null)
const accountToolsDropdownPosition = reactive({
  top: null as number | null,
  bottom: null as number | null,
  left: 16,
  width: 320,
  maxHeight: 0
})
const accountToolsDropdownStyle = computed(() => ({
  top: accountToolsDropdownPosition.top == null ? 'auto' : `${accountToolsDropdownPosition.top}px`,
  bottom: accountToolsDropdownPosition.bottom == null ? 'auto' : `${accountToolsDropdownPosition.bottom}px`,
  left: `${accountToolsDropdownPosition.left}px`,
  width: `${accountToolsDropdownPosition.width}px`
}))
const hiddenColumns = reactive<Set<string>>(new Set())
const DEFAULT_HIDDEN_COLUMNS = ['today_stats', 'proxy', 'notes', 'scheduler_score', 'rate_multiplier']
const HIDDEN_COLUMNS_KEY = 'account-hidden-columns'
// One-time migration: hide scheduler score for existing admins too, because showing it opt-ins to heavy backend scoring.
const HIDDEN_COLUMNS_VERSION_KEY = 'account-hidden-columns-version'
const HIDDEN_COLUMNS_CURRENT_VERSION = 'scheduler-score-hidden-by-default'

const accountBadgeString = (value: unknown, fallback?: string): string | undefined =>
  typeof value === 'string' && value ? value : fallback

// Account filter settings
const ACCOUNT_FILTER_STORAGE_KEY = 'account-filter-values'
type AccountFilterValues = {
  platform: string
  status: string
}
const loadInitialAccountFilterValues = (): AccountFilterValues => {
  const fallback: AccountFilterValues = { platform: '', status: '' }
  try {
    const raw = localStorage.getItem(ACCOUNT_FILTER_STORAGE_KEY)
    if (!raw) return fallback
    const parsed: unknown = JSON.parse(raw)
    if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return fallback
    const values = parsed as Record<string, unknown>
    return {
      platform: typeof values.platform === 'string' && CONCRETE_PLATFORM_OPTIONS.some(option => option.value === values.platform)
        ? values.platform : '',
      status: typeof values.status === 'string' && ['active', 'inactive', 'error', 'rate_limited', 'temp_unschedulable', 'unschedulable'].includes(values.status)
        ? values.status : ''
    }
  } catch {
    return fallback
  }
}

// Sorting settings
const ACCOUNT_SORT_STORAGE_KEY = 'account-table-sort'
type AccountSortOrder = 'asc' | 'desc'
type AccountSortState = {
  sort_by: string
  sort_order: AccountSortOrder
}
const ACCOUNT_SORTABLE_KEYS = new Set([
  'id',
  'name',
  'status',
  'schedulable',
  'priority',
  'rate_multiplier',
  'upstream_billing_rate',
  'last_used_at',
  'created_at',
  'expires_at'
])
const loadInitialAccountSortState = (): AccountSortState => {
  const fallback: AccountSortState = { sort_by: 'name', sort_order: 'asc' }
  try {
    const raw = localStorage.getItem(ACCOUNT_SORT_STORAGE_KEY)
    if (!raw) return fallback
    const parsed = JSON.parse(raw) as { key?: string; order?: string }
    const key = typeof parsed.key === 'string' ? parsed.key : ''
    if (!ACCOUNT_SORTABLE_KEYS.has(key)) return fallback
    return {
      sort_by: key,
      sort_order: parsed.order === 'desc' ? 'desc' : 'asc'
    }
  } catch {
    return fallback
  }
}
const sortState = reactive<AccountSortState>(loadInitialAccountSortState())

// Auto refresh settings
const showAutoRefreshDropdown = ref(false)
const autoRefreshDropdownRef = ref<HTMLElement | null>(null)
const AUTO_REFRESH_STORAGE_KEY = 'account-auto-refresh'
const autoRefreshIntervals = [5, 10, 15, 30] as const
const autoRefreshEnabled = ref(false)
const autoRefreshIntervalSeconds = ref<(typeof autoRefreshIntervals)[number]>(30)
const autoRefreshCountdown = ref(0)
const autoRefreshETag = ref<string | null>(null)
const autoRefreshFetching = ref(false)
let autoRefreshAbortController: AbortController | null = null
const AUTO_REFRESH_SILENT_WINDOW_MS = 15000
const autoRefreshSilentUntil = ref(0)
const hasPendingListSync = ref(false)
const todayStatsByAccountId = ref<Record<string, WindowStats>>({})
const todayStatsLoading = ref(false)
const todayStatsError = ref<string | null>(null)
const todayStatsReqSeq = ref(0)
const pendingTodayStatsRefresh = ref(false)
const usageManualRefreshToken = ref(0)

const desktopViewportQuery = '(min-width: 768px)'
const isDesktopViewport = ref(
  typeof window === 'undefined' ? true : window.matchMedia(desktopViewportQuery).matches
)
let desktopViewportMediaQuery: MediaQueryList | null = null
let desktopViewportListener: ((event: MediaQueryListEvent) => void) | null = null

const { usageBatchByAccountId, usageBatchErrorByAccountId, usageBatchLoadingByAccountId, queueBatchedUsage, handleAccountUsageLoaded, retainVisibleUsage } = useAccountUsageBatch({
  isDesktopViewport, readScope: () => authStore.identityScopeKey, fetchUsage: accountsAPI.getBatchUsage
})

const buildDefaultTodayStats = (): WindowStats => ({
  requests: 0,
  tokens: 0,
  cost: 0,
  standard_cost: 0,
  user_cost: 0
})

const refreshTodayStatsBatch = async () => {
  // Why this checks both columns:
  // - today_stats column shows dedicated today's metrics.
  // - usage column also embeds today's stats for Key/Bedrock rows.
  // So we only skip fetching when BOTH columns are hidden.
  if (hiddenColumns.has('today_stats') && hiddenColumns.has('usage')) {
    todayStatsLoading.value = false
    todayStatsError.value = null
    return
  }

  const accountIDs = accounts.value.map(account => account.id)
  const reqSeq = ++todayStatsReqSeq.value
  if (accountIDs.length === 0) {
    todayStatsByAccountId.value = {}
    todayStatsError.value = null
    todayStatsLoading.value = false
    return
  }

  todayStatsLoading.value = true
  todayStatsError.value = null

  try {
    const result = await accountsAPI.getBatchTodayStats(accountIDs)
    if (reqSeq !== todayStatsReqSeq.value) return
    const serverStats = result.stats ?? {}
    const nextStats: Record<string, WindowStats> = {}
    for (const accountID of accountIDs) {
      const key = String(accountID)
      nextStats[key] = serverStats[key] ?? buildDefaultTodayStats()
    }
    todayStatsByAccountId.value = nextStats
  } catch (error) {
    if (reqSeq !== todayStatsReqSeq.value) return
    todayStatsError.value = 'Failed'
    console.error('Failed to load account today stats:', error)
  } finally {
    if (reqSeq === todayStatsReqSeq.value) {
      todayStatsLoading.value = false
    }
  }
}

const autoRefreshIntervalLabel = (sec: number) => {
  if (sec === 5) return t('admin.accounts.refreshInterval5s')
  if (sec === 10) return t('admin.accounts.refreshInterval10s')
  if (sec === 15) return t('admin.accounts.refreshInterval15s')
  if (sec === 30) return t('admin.accounts.refreshInterval30s')
  return `${sec}s`
}

const formatSchedulerScore = (value: unknown): string => {
  const num = Number(value)
  if (!Number.isFinite(num)) return '-'
  return num.toFixed(6).replace(/\.?0+$/, '')
}

const getSchedulerScoreRows = (account: Account): AccountSchedulerGroupScore[] => {
  const groupRows = Array.isArray(account.scheduler_scores)
    ? account.scheduler_scores.filter(score => score.group_id != null)
    : []
  if (groupRows.length) return groupRows
  // 未分组账号没有分组维度分数，回退展示后端返回的基础分
  if (account.scheduler_score) {
    return [{ group_id: null, ...account.scheduler_score }]
  }
  return []
}

const formatSchedulerScoreGroup = (score: AccountSchedulerGroupScore): string => {
  if ('group_name' in score && score.group_name) return score.group_name
  if ('group_id' in score && score.group_id != null) return `#${score.group_id}`
  return t('admin.accounts.schedulerScore.ungrouped')
}

const loadSavedColumns = () => {
  try {
    const saved = localStorage.getItem(HIDDEN_COLUMNS_KEY)
    if (saved) {
      const parsed = JSON.parse(saved) as string[]
      parsed.forEach(key => {
        hiddenColumns.add(key)
      })
      // Older saved column layouts may have scheduler_score visible; migrate them to the new safe default once.
      if (localStorage.getItem(HIDDEN_COLUMNS_VERSION_KEY) !== HIDDEN_COLUMNS_CURRENT_VERSION) {
        hiddenColumns.add('scheduler_score')
        localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
        localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
      }
    } else {
      DEFAULT_HIDDEN_COLUMNS.forEach(key => {
        hiddenColumns.add(key)
      })
      localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
    }
  } catch (e) {
    console.error('Failed to load saved columns:', e)
    DEFAULT_HIDDEN_COLUMNS.forEach(key => {
      hiddenColumns.add(key)
    })
  }
}

const saveColumnsToStorage = () => {
  try {
    localStorage.setItem(HIDDEN_COLUMNS_KEY, JSON.stringify([...hiddenColumns]))
    localStorage.setItem(HIDDEN_COLUMNS_VERSION_KEY, HIDDEN_COLUMNS_CURRENT_VERSION)
  } catch (e) {
    console.error('Failed to save columns:', e)
  }
}

const loadSavedAutoRefresh = () => {
  try {
    const saved = localStorage.getItem(AUTO_REFRESH_STORAGE_KEY)
    if (!saved) return
    const parsed = JSON.parse(saved) as { enabled?: boolean; interval_seconds?: number }
    autoRefreshEnabled.value = parsed.enabled === true
    const interval = Number(parsed.interval_seconds)
    const savedInterval = autoRefreshIntervals.find(value => value === interval)
    if (savedInterval !== undefined) autoRefreshIntervalSeconds.value = savedInterval
  } catch (e) {
    console.error('Failed to load saved auto refresh settings:', e)
  }
}

const saveAutoRefreshToStorage = () => {
  try {
    localStorage.setItem(
      AUTO_REFRESH_STORAGE_KEY,
      JSON.stringify({
        enabled: autoRefreshEnabled.value,
        interval_seconds: autoRefreshIntervalSeconds.value
      })
    )
  } catch (e) {
    console.error('Failed to save auto refresh settings:', e)
  }
}

if (typeof window !== 'undefined') {
  loadSavedColumns()
  loadSavedAutoRefresh()
}

const setAutoRefreshEnabled = (enabled: boolean) => {
  autoRefreshEnabled.value = enabled
  saveAutoRefreshToStorage()
  if (enabled) {
    autoRefreshCountdown.value = autoRefreshIntervalSeconds.value
    resumeAutoRefresh()
  } else {
    pauseAutoRefresh()
    autoRefreshCountdown.value = 0
  }
}

const setAutoRefreshInterval = (seconds: (typeof autoRefreshIntervals)[number]) => {
  autoRefreshIntervalSeconds.value = seconds
  saveAutoRefreshToStorage()
  if (autoRefreshEnabled.value) {
    autoRefreshCountdown.value = seconds
  }
}

const toggleColumn = (key: string) => {
  const wasHidden = hiddenColumns.has(key)
  if (hiddenColumns.has(key)) {
    hiddenColumns.delete(key)
  } else {
    hiddenColumns.add(key)
  }
  saveColumnsToStorage()
  if ((key === 'today_stats' || key === 'usage') && wasHidden) {
    refreshTodayStatsBatch().catch((error) => {
      console.error('Failed to load account today stats after showing column:', error)
    })
  }
  if (key === 'scheduler_score') {
    // The server only returns scheduler scores when this column is visible, so reload the current page immediately.
    syncAccountListDerivedParams()
    load().catch((error) => {
      console.error('Failed to reload accounts after toggling scheduler score column:', error)
    })
  }
}

const isColumnVisible = (key: string) => !hiddenColumns.has(key)
const shouldIncludeSchedulerScore = () => isColumnVisible('scheduler_score')
const syncAccountListDerivedParams = () => {
  // Keep every load path, including auto-refresh and sorting, aligned with the current column visibility.
  params.include_scheduler_score = shouldIncludeSchedulerScore() ? '1' : '0'
}

const {
  items: accounts,
  loading,
  error: loadError,
  params,
  pagination,
  load: baseLoad,
  cancel: cancelListLoad,
  reload: baseReload,
  debouncedReload: baseDebouncedReload,
  handlePageChange: baseHandlePageChange,
  handlePageSizeChange: baseHandlePageSizeChange
} = useTableLoader<AccountListItem, AccountListFilters & { search: string }>({
  fetchFn: accountsAPI.list,
  initialParams: {
    ...loadInitialAccountFilterValues(),
    type: '',
    privacy_mode: '',
    group: '',
    search: '',
    lite: '1',
    include_scheduler_score: shouldIncludeSchedulerScore() ? '1' : '0',
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
})

// Project only this panel's five filters; pagination/search/query-only params
// remain owned by the table loader. Keep the Select clear value (null) intact.
const accountFilterText = (value: unknown): string | null =>
  value === null || typeof value === 'string' ? value : ''
const accountTableFilters = computed<AccountTableFilterValues>(() => ({
  platform: accountFilterText(params.platform),
  type: accountFilterText(params.type),
  status: accountFilterText(params.status),
  privacy_mode: accountFilterText(params.privacy_mode),
  group: accountFilterText(params.group)
}))
const updateAccountTableFilters = (filters: AccountTableFilterValues): void => {
  Object.assign(params, filters)
}

const {
  selectedSet,
  selectedIds: selIds,
  allVisibleSelected,
  isSelected,
  setSelectedIds,
  select,
  deselect,
  toggle: toggleSel,
  clear: clearSelectedIds,
  removeMany: removeSelectedAccounts,
  toggleVisible,
  selectVisible: selectCurrentPage,
  batchUpdate
} = useTableSelection<AccountListItem>({
  rows: accounts,
  getId: (account) => account.id
})

watch(selIds, () => { accountSelectionRevision++ }, { flush: 'sync' })
const selectingAllResults = computed(() => accountActions.has('select-all'))
const selectedAllResultIDs = ref<Set<number> | null>(null)
const selectionRequestVersion = ref(0)
const allResultsSelected = computed(() => {
  const snapshot = selectedAllResultIDs.value
  if (!snapshot || snapshot.size === 0 || snapshot.size !== selectedSet.value.size) return false
  return Array.from(snapshot).every(id => selectedSet.value.has(id))
})

const clearSelection = () => {
  selectionRequestVersion.value++
  cancelAccountAction('select-all')
  selectedAllResultIDs.value = null
  clearSelectedIds()
}

const selectPage = () => {
  selectCurrentPage()
}

const swipeVirtualContext: SwipeSelectVirtualContext<Account> = {
  getVirtualizer: () => dataTableRef.value?.virtualizer ?? null,
  getSortedData: () => [...(dataTableRef.value?.sortedData ?? accounts.value)],
  getRowId: (row) => row.id,
}

useSwipeSelect(accountTableRef, {
  isSelected,
  select,
  deselect,
  batchUpdate
}, swipeVirtualContext)

const resetAutoRefreshCache = () => {
  autoRefreshAbortController?.abort()
  autoRefreshAbortController = null
  autoRefreshFetching.value = false
  upstreamBillingRateAbortController?.abort()
  upstreamBillingRateAbortController = null
  upstreamBillingRateRefreshing.value = false
  autoRefreshETag.value = null
  upstreamBillingRateETag.value = null
}
const accountQueryScope = () => JSON.stringify([
  authStore.identityScopeKey, pagination.page, pagination.page_size, { ...params }
])
watch(accountQueryScope, () => {
  cancelAccountAction('select-all')
  cancelAccountAction('bulk-edit-filtered')
  cancelListLoad()
  resetAutoRefreshCache()
}, { flush: 'sync' })

type AccountLoadOptions = {
  refreshTodayStats?: boolean
}

const load = async (options: AccountLoadOptions = {}) => {
  syncAccountListDerivedParams()
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = false
  params.lite = '1'
  // The table exposes the current failure beside its filters, including on
  // background refreshes. Do not turn a read failure into a mutation failure.
  await baseLoad().catch(() => undefined)
  if (loadError.value || !workspaceActive) return
  if (options.refreshTodayStats !== false) await refreshTodayStatsBatch()
}

const reload = async () => {
  syncAccountListDerivedParams()
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = false
  await baseReload().catch(() => undefined)
  if (loadError.value || !workspaceActive) return
  await refreshTodayStatsBatch()
}

const buildUpstreamBillingRateFilters = () => {
  const rawParams = toRaw(params) as Record<string, unknown>
  return {
    platform: typeof rawParams.platform === 'string' ? rawParams.platform : '',
    type: typeof rawParams.type === 'string' ? rawParams.type : '',
    status: typeof rawParams.status === 'string' ? rawParams.status : '',
    group: typeof rawParams.group === 'string' ? rawParams.group : '',
    search: typeof rawParams.search === 'string' ? rawParams.search : '',
    privacy_mode: typeof rawParams.privacy_mode === 'string' ? rawParams.privacy_mode : '',
    sort_by: sortState.sort_by,
    sort_order: sortState.sort_order
  }
}

const sameAccountIDOrder = (left: number[], right: number[]) =>
  left.length === right.length && left.every((id, index) => id === right[index])

const upstreamBillingRateContextKey = () => JSON.stringify({
  page: pagination.page,
  pageSize: pagination.page_size,
  filters: buildUpstreamBillingRateFilters()
})

const applyUpstreamBillingRateSnapshots = async (
  result: NonNullable<Awaited<ReturnType<typeof accountsAPI.getUpstreamBillingRatesWithEtag>>['data']>
) => {
  const nextIDs = result.items.map(item => item.account_id)
  const currentIDs = accounts.value.map(account => account.id)

  // The compact response cannot fill a row that crossed a page boundary.
  // Only that case needs the expensive, full account-list request.
  if (result.total !== pagination.total || !sameAccountIDOrder(nextIDs, currentIDs)) {
    try {
      await load({ refreshTodayStats: false })
    } catch (error) {
      console.error('Failed to reconcile upstream billing sort:', error)
    }
    return
  }

  const itemsByID = new Map(result.items.map(item => [item.account_id, item]))
  let changed = false
  const nextAccounts = accounts.value.map(account => {
    const item = itemsByID.get(account.id)
    if (!item) return account
    const nextSnapshot = item.snapshot ?? null
    const previousSnapshot = account.extra?.upstream_billing_probe ?? null
    if (JSON.stringify(previousSnapshot) === JSON.stringify(nextSnapshot)) return account

    const nextExtra = { ...(account.extra ?? {}) }
    if (nextSnapshot) nextExtra.upstream_billing_probe = nextSnapshot
    else delete nextExtra.upstream_billing_probe
    const nextAccount = {
      ...account,
      ...(typeof nextSnapshot?.synced_rate_multiplier === 'number'
        ? { rate_multiplier: nextSnapshot.synced_rate_multiplier }
        : {}),
      extra: nextExtra
    }
    syncAccountRefs(nextAccount)
    changed = true
    return nextAccount
  })

  if (changed) {
    accounts.value = nextAccounts
    upstreamBillingNow.value = Date.now()
  }
}

const refreshUpstreamBillingRates = async (force = false) => {
 if (!rolePermissions.can('billing.cost.read')) return
  if (upstreamBillingRateRefreshing.value || loading.value || accounts.value.length === 0) return
  if (!force && (
    probingUpstreamBilling.value.size > 0 ||
    isAnyModalOpen.value ||
    menu.show ||
    showAccountToolsDropdown.value ||
    showAutoRefreshDropdown.value ||
    (typeof document !== 'undefined' && document.hidden)
  )) return

  const controller = new AbortController()
  upstreamBillingRateAbortController = controller
  upstreamBillingRateRefreshing.value = true
  try {
    syncAccountListDerivedParams()
    const requestContextKey = upstreamBillingRateContextKey()
    const result = await accountsAPI.getUpstreamBillingRatesWithEtag(
      pagination.page,
      pagination.page_size,
      buildUpstreamBillingRateFilters(),
      { etag: force ? null : upstreamBillingRateETag.value, signal: controller.signal }
    )
    if (!workspaceActive || controller.signal.aborted || loading.value || requestContextKey !== upstreamBillingRateContextKey()) return
    if (result.etag) upstreamBillingRateETag.value = result.etag
    if (!result.notModified && result.data) await applyUpstreamBillingRateSnapshots(result.data)
  } catch (error) {
    if (!workspaceActive || controller.signal.aborted) return
    const refreshError = readApiError(error)
    if (refreshError.name !== 'AbortError' && refreshError.name !== 'CanceledError' && refreshError.code !== 'ERR_CANCELED') {
      console.error('Failed to refresh upstream billing rates:', error)
    }
  } finally {
    if (upstreamBillingRateAbortController === controller) {
      upstreamBillingRateAbortController = null
      upstreamBillingRateRefreshing.value = false
    }
  }
}

const refreshUpstreamBillingSortedList = async (force = false) => {
  if (!force && sortState.sort_by !== 'upstream_billing_rate') return
  await refreshUpstreamBillingRates(force)
}

useIntervalFn(() => { void refreshUpstreamBillingRates() }, 5 * 60_000, { immediate: false })

const saveAccountFilterValues = () => {
  try {
    localStorage.setItem(ACCOUNT_FILTER_STORAGE_KEY, JSON.stringify({
      platform: params.platform,
      status: params.status
    }))
  } catch {
    // Filtering must remain available when browser storage is unavailable.
  }
}

const debouncedReload = () => {
  saveAccountFilterValues()
  clearSelection()
  syncAccountListDerivedParams()
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = true
  void baseDebouncedReload().catch(() => undefined)
}

const handlePageChange = (page: number) => {
  syncAccountListDerivedParams()
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = true
  return baseHandlePageChange(page).catch(() => undefined)
}

const handlePageSizeChange = (size: number) => {
  syncAccountListDerivedParams()
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = true
  return baseHandlePageSizeChange(size).catch(() => undefined)
}

const handleSort = (key: string, order: AccountSortOrder) => {
  sortState.sort_by = key
  sortState.sort_order = order
  params.sort_by = key
  params.sort_order = order
  syncAccountListDerivedParams()
  pagination.page = 1
  hasPendingListSync.value = false
  resetAutoRefreshCache()
  pendingTodayStatsRefresh.value = true
  load()
}

watch(loading, (isLoading, wasLoading) => {
  if (wasLoading && !isLoading) {
    upstreamBillingNow.value = Date.now()
  }
  if (wasLoading && !isLoading && pendingTodayStatsRefresh.value) {
    pendingTodayStatsRefresh.value = false
    refreshTodayStatsBatch().catch((error) => {
      console.error('Failed to refresh account today stats after table load:', error)
    })
  }
})

watch(accounts, rows => retainVisibleUsage(rows.map(row => row.id)))

const isAnyModalOpen = computed(() => {
  return (
    showCreate.value ||
    showEdit.value ||
    modelReferenceAccount.value !== null ||
    showSync.value ||
    showImportData.value ||
    showExportDataDialog.value ||
    showBulkEdit.value ||
    showTempUnsched.value ||
    showDeleteDialog.value ||
    showReAuth.value ||
    showTest.value ||
    showStats.value ||
    showSchedulePanel.value ||
    showErrorPassthrough.value ||
    showTLSFingerprintProfiles.value
  )
})

const enterAutoRefreshSilentWindow = () => {
  autoRefreshSilentUntil.value = Date.now() + AUTO_REFRESH_SILENT_WINDOW_MS
  autoRefreshCountdown.value = autoRefreshIntervalSeconds.value
}

const inAutoRefreshSilentWindow = () => {
  return Date.now() < autoRefreshSilentUntil.value
}

const shouldReplaceAutoRefreshRow = (current: Account, next: Account) => {
  return (
    current.updated_at !== next.updated_at ||
    current.current_concurrency !== next.current_concurrency ||
    current.current_window_cost !== next.current_window_cost ||
    current.active_sessions !== next.active_sessions ||
    current.schedulable !== next.schedulable ||
    current.status !== next.status ||
    JSON.stringify(current.anthropic_status ?? null) !== JSON.stringify(next.anthropic_status ?? null) ||
    JSON.stringify(current.excel_bps_403_recovery ?? null) !== JSON.stringify(next.excel_bps_403_recovery ?? null) ||
    current.rate_limit_reset_at !== next.rate_limit_reset_at ||
    current.overload_until !== next.overload_until ||
    current.temp_unschedulable_until !== next.temp_unschedulable_until ||
    buildOpenAIUsageRefreshKey(current) !== buildOpenAIUsageRefreshKey(next) ||
    buildGrokUsageRefreshKey(current) !== buildGrokUsageRefreshKey(next)
  )
}

const syncAccountRefs = (nextAccount: Account) => {
  if (edAcc.value?.id === nextAccount.id) edAcc.value = nextAccount
  if (reAuthAcc.value?.id === nextAccount.id) reAuthAcc.value = nextAccount
  if (tempUnschedAcc.value?.id === nextAccount.id) tempUnschedAcc.value = nextAccount
  if (deletingAcc.value?.id === nextAccount.id) deletingAcc.value = nextAccount
  if (menu.acc?.id === nextAccount.id) menu.acc = nextAccount
}

const mergeAccountsIncrementally = (nextRows: Account[]) => {
  const currentRows = accounts.value
  const currentByID = new Map(currentRows.map(row => [row.id, row]))
  let changed = nextRows.length !== currentRows.length
  const mergedRows = nextRows.map((nextRow) => {
    const currentRow = currentByID.get(nextRow.id)
    if (!currentRow) {
      changed = true
      return nextRow
    }
    if (shouldReplaceAutoRefreshRow(currentRow, nextRow)) {
      changed = true
      syncAccountRefs(nextRow)
      return nextRow
    }
    return currentRow
  })
  if (!changed) {
    for (let i = 0; i < mergedRows.length; i += 1) {
      if (mergedRows[i].id !== currentRows[i]?.id) {
        changed = true
        break
      }
    }
  }
  if (changed) {
    accounts.value = mergedRows
  }
}

const refreshAccountsIncrementally = async () => {
  if (!workspaceActive || autoRefreshFetching.value) return
  syncAccountListDerivedParams()
  const requestScope = accountQueryScope()
  const controller = new AbortController()
  autoRefreshAbortController = controller
  const current = () => workspaceActive && !controller.signal.aborted && requestScope === accountQueryScope()
  autoRefreshFetching.value = true
  try {
    const result = await accountsAPI.listWithEtag(
      pagination.page,
      pagination.page_size,
      { ...toRaw(params) },
      { etag: autoRefreshETag.value, signal: controller.signal }
    )
    if (!current() || loading.value) return
    if (result.etag) {
      autoRefreshETag.value = result.etag
    }
    if (!result.notModified && result.data) {
      pagination.total = result.data.total || 0
      pagination.pages = result.data.pages || 0
      mergeAccountsIncrementally(result.data.items || [])
      hasPendingListSync.value = false
    }
    upstreamBillingNow.value = Date.now()

    await refreshTodayStatsBatch()
  } catch (error) {
    if (current()) console.error('Auto refresh failed:', error)
  } finally {
    if (autoRefreshAbortController === controller) {
      autoRefreshAbortController = null
      autoRefreshFetching.value = false
    }
  }
}

const handleManualRefresh = async () => {
  await Promise.all([load(), loadUpstreamBillingProbeGlobalState()])
  // Force usage cells to refetch /usage on explicit user refresh.
  usageManualRefreshToken.value += 1
}

const loadUpstreamBillingProbeGlobalState = async () => {
 if (!rolePermissions.isSuperAdmin) return
  const scope = authStore.identityScopeKey
  try {
    const settings = await accountsAPI.getUpstreamBillingProbeSettings()
    if (!workspaceActive || scope !== authStore.identityScopeKey) return
    upstreamBillingProbeGloballyEnabled.value = settings.enabled
  } catch (error) {
    if (workspaceActive && scope === authStore.identityScopeKey) console.error('Failed to load upstream billing probe settings:', error)
  }
}

const closeAccountToolsDropdown = () => {
  showAccountToolsDropdown.value = false
}

const updateAccountToolsDropdownPosition = () => {
  const trigger = accountToolsTriggerRef.value
  if (!trigger) return

  const position = getFloatingPanelPosition(
    trigger.getBoundingClientRect(),
    document.documentElement.clientWidth || window.innerWidth,
    window.innerHeight
  )
  Object.assign(accountToolsDropdownPosition, position)
}

const toggleAccountToolsDropdown = () => {
  const nextVisible = !showAccountToolsDropdown.value
  showAutoRefreshDropdown.value = false
  if (nextVisible) updateAccountToolsDropdownPosition()
  showAccountToolsDropdown.value = nextVisible
}

const openSyncFromCrs = () => {
  closeAccountToolsDropdown()
  showSync.value = true
}

const openImportData = () => {
  closeAccountToolsDropdown()
  showImportData.value = true
}

const openExportDataDialogFromMenu = () => {
  closeAccountToolsDropdown()
  openExportDataDialog()
}

const openErrorPassthrough = () => {
  closeAccountToolsDropdown()
  showErrorPassthrough.value = true
}

const openTLSFingerprintProfiles = () => {
  closeAccountToolsDropdown()
  showTLSFingerprintProfiles.value = true
}

const syncPendingListChanges = async () => {
  hasPendingListSync.value = false
  await load()
  // Keep behavior consistent with manual refresh.
  usageManualRefreshToken.value += 1
}

const { pause: pauseAutoRefresh, resume: resumeAutoRefresh } = useIntervalFn(
  async () => {
    if (!autoRefreshEnabled.value) return
    if (document.hidden) return
    if (loading.value || autoRefreshFetching.value) return
    if (isAnyModalOpen.value) return
    if (menu.show || showAccountToolsDropdown.value || showAutoRefreshDropdown.value) return
    if (inAutoRefreshSilentWindow()) {
      autoRefreshCountdown.value = Math.max(
        0,
        Math.ceil((autoRefreshSilentUntil.value - Date.now()) / 1000)
      )
      return
    }

    if (autoRefreshCountdown.value <= 0) {
      autoRefreshCountdown.value = autoRefreshIntervalSeconds.value
      await refreshAccountsIncrementally()
      return
    }

    autoRefreshCountdown.value -= 1
  },
  1000,
  { immediate: false }
)

const { getAccountPlanType, getOpenAIAuthMode, getAntigravityTierLabel, getAntigravityTierClass, accountDisplayEmail, accountHomepageUrl } = createAccountDisplay(t)

// All available columns
const allColumns = computed(() => {
  const c = [
    { key: 'select', label: '', sortable: false },
    { key: 'name', label: t('admin.accounts.columns.name'), sortable: true, class: 'w-[260px] max-w-[260px]' },
    { key: 'id', label: t('admin.accounts.columns.id'), sortable: true },
    { key: 'capacity', label: t('admin.accounts.columns.capacity'), sortable: false },
    { key: 'status', label: t('admin.accounts.columns.status'), sortable: true },
    { key: 'schedulable', label: t('admin.accounts.columns.schedulable'), sortable: true },
    { key: 'today_stats', label: t('admin.accounts.columns.todayStats'), sortable: false }
  ]
  if (!authStore.isSimpleMode) {
    c.push({ key: 'groups', label: t('admin.accounts.columns.groups'), sortable: false })
  }
  c.push({ key: 'usage', label: t('admin.accounts.columns.usageWindows'), sortable: false })
  c.push(
    { key: 'proxy', label: t('admin.accounts.columns.proxy'), sortable: false },
    { key: 'priority', label: t('admin.accounts.columns.priority'), sortable: true },
    { key: 'scheduler_score', label: t('admin.accounts.columns.schedulerScore'), sortable: false },
    { key: 'rate_multiplier', label: t('admin.accounts.columns.billingRateMultiplier'), sortable: true },
    { key: 'upstream_billing_rate', label: t('admin.accounts.columns.upstreamBillingRate'), sortable: true },
    { key: 'last_used_at', label: t('admin.accounts.columns.lastUsed'), sortable: true },
    { key: 'created_by_name', label: t('admin.accounts.columns.createdBy'), sortable: false },
    { key: 'created_by_role', label: t('admin.accounts.columns.createdByRole'), sortable: false },
    { key: 'created_at', label: t('admin.accounts.columns.createdAt'), sortable: true },
    { key: 'expires_at', label: t('admin.accounts.columns.expiresAt'), sortable: true },
    { key: 'notes', label: t('admin.accounts.columns.notes'), sortable: false },
    { key: 'actions', label: t('admin.accounts.columns.actions'), sortable: false }
  )
  return rolePermissions.can('billing.cost.read') ? c : c.filter(column => !['usage', 'today_stats', 'rate_multiplier', 'upstream_billing_rate'].includes(column.key))
})

// Columns that can be toggled (exclude select, name, and actions)
const toggleableColumns = computed(() =>
  allColumns.value.filter(col => col.key !== 'select' && col.key !== 'name' && col.key !== 'actions')
)

// Filtered columns based on visibility
const cols = computed(() =>
  allColumns.value.filter(col =>
    col.key === 'select' || col.key === 'name' || col.key === 'actions' || !hiddenColumns.has(col.key)
  )
)

async function openAccountDetails(accountId: number, mode: 'edit' | 'test' | 'stats'): Promise<void> {
  const action = beginAccountAction('detail', [accountId], true)
  if (!action) return
  try {
    const account = await loadAccountDetails(accountId, action)
    if (!account || !action.current()) return
    edAcc.value = mode === 'edit' ? account : null
    testingAcc.value = mode === 'test' ? account : null
    statsAcc.value = mode === 'stats' ? account : null
    showEdit.value = mode === 'edit'
    showTest.value = mode === 'test'
    showStats.value = mode === 'stats'
  } finally { action.finish() }
}

const loadAccountDetails = async (accountId: number, action: AccountAction): Promise<Account | null> => {
  try { return await action.wait(() => accountsAPI.getById(accountId, { signal: action.controller.signal })) }
  catch (error: unknown) {
    if (!action.current()) return null
    console.error('Failed to load account details:', error)
    appStore.showError(extractApiErrorMessage(error, t('common.error')))
    return null
  }
}

const handleEdit = (account: AccountListItem): Promise<void> => openAccountDetails(account.id, 'edit')
const openMenu = (a: Account, e: MouseEvent) => {
  menu.acc = a
  const target = e.currentTarget as HTMLElement
  menu.anchorRect = target.getBoundingClientRect()
  menu.show = true
}
const toggleSelectAllVisible = (event: Event) => {
  const target = event.target as HTMLInputElement
  toggleVisible(target.checked)
}
const handleBulkDelete = async () => {
    const action = beginAccountAction('bulk-delete', selIds.value, false);
    if (!action)
        return;
    const selectionRevision = accountSelectionRevision;
    try {
        const accountIds = [...action.targets];
        if (!confirm(t('admin.accounts.bulkActions.confirmDelete', { count: accountIds.length })))
            return;
        try {
            const result = await action.wait(() => accountsAPI.batchDelete(accountIds, { signal: action.controller.signal }));
            if (result.failed > 0) {
                appStore.showError(t('admin.accounts.bulkActions.partialSuccess', {
                    success: result.success,
                    failed: result.failed
                }));
                applyActionSelection(selectionRevision, () => setSelectedIds(result.failed_ids?.length ? result.failed_ids : accountIds));
            }
            else {
                appStore.showSuccess(t('admin.accounts.bulkActions.deleteSuccess', { count: result.success }));
                applyActionSelection(selectionRevision, () => clearSelection());
            }
            await action.wait(() => reload());
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to bulk delete accounts:', error);
            appStore.showError(String(error));
        }
    }
    finally {
        action.finish();
    }
};
const handleBulkResetStatus = async () => {
    const action = beginAccountAction('bulk-reset', selIds.value, false);
    if (!action)
        return;
    const selectionRevision = accountSelectionRevision;
    try {
        if (!confirm(t('common.confirm')))
            return;
        try {
            const result = await action.wait(() => accountsAPI.batchClearError([...action.targets], { signal: action.controller.signal }));
            if (result.failed > 0) {
                appStore.showError(t('admin.accounts.bulkActions.partialSuccess', { success: result.success, failed: result.failed }));
            }
            else {
                appStore.showSuccess(t('admin.accounts.bulkActions.resetStatusSuccess', { count: result.success }));
                applyActionSelection(selectionRevision, () => clearSelection());
            }
            await action.wait(() => reload());
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to bulk reset status:', error);
            appStore.showError(String(error));
        }
    }
    finally {
        action.finish();
    }
};
const handleBulkRefreshToken = async () => {
    const action = beginAccountAction('bulk-refresh', selIds.value, false);
    if (!action)
        return;
    const selectionRevision = accountSelectionRevision;
    try {
        if (!confirm(t('common.confirm')))
            return;
        const accountIds = [...action.targets];
        try {
            const result = await action.wait(() => accountsAPI.batchRefresh(accountIds, { signal: action.controller.signal }));
            if (result.failed > 0) {
                appStore.showError(t('admin.accounts.bulkActions.partialSuccess', { success: result.success, failed: result.failed }));
                const failedIds = result.errors?.map(error => error.account_id) ?? [];
                applyActionSelection(selectionRevision, () => setSelectedIds(failedIds.length > 0 ? failedIds : accountIds));
            }
            else {
                appStore.showSuccess(t('admin.accounts.bulkActions.refreshTokenSuccess', { count: result.success }));
                applyActionSelection(selectionRevision, () => clearSelection());
            }
            await action.wait(() => reload());
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to bulk refresh token:', error);
            appStore.showError(String(error));
        }
    }
    finally {
        action.finish();
    }
};
const handleBulkProbeUpstreamBilling = async () => {
    const action = beginAccountAction('probe:bulk', selIds.value, false);
    if (!action)
        return;
    try {
        const accountIDs = [...action.targets];
        if (accountIDs.length === 0) {
            appStore.showError(t('admin.accounts.upstreamBilling.noEligibleAccounts'));
            return;
        }
        if (accountIDs.length > 20) {
            appStore.showError(t('admin.accounts.upstreamBilling.batchLimit'));
            return;
        }
        try {
            const results = await action.wait(() => accountsAPI.probeUpstreamBillingBatch(accountIDs, { signal: action.controller.signal }));
            let patched = false;
            results.forEach(result => {
                if (result.snapshot) {
                    patchUpstreamBillingSnapshot(result.account_id, result.snapshot);
                    patched = true;
                }
            });
            if (patched)
                await action.wait(() => refreshAccountsAfterUpstreamBillingProbe());
            const failed = results.filter(result => result.error).length;
            if (failed > 0) {
                appStore.showError(t('admin.accounts.upstreamBilling.batchPartial', { success: results.length - failed, failed }));
            }
            else {
                appStore.showSuccess(t('admin.accounts.upstreamBilling.batchCompleted', { count: results.length }));
            }
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to probe upstream billing in batch:', error);
            appStore.showError(extractApiErrorMessage(error, t('admin.accounts.upstreamBilling.probeFailed')));
        }
    }
    finally {
        action.finish();
    }
};
const updateSchedulableInList = (accountIds: number[], schedulable: boolean) => {
  if (accountIds.length === 0) return
  const idSet = new Set(accountIds)
  accounts.value = accounts.value.map((account) => (idSet.has(account.id) ? { ...account, schedulable } : account))
}
const normalizeBulkSchedulableResult = (
  result: {
    success?: number
    failed?: number
    success_ids?: number[]
    failed_ids?: number[]
    results?: Array<{ account_id: number; success: boolean }>
  },
  accountIds: number[]
) => {
  const responseSuccessIds = Array.isArray(result.success_ids) ? result.success_ids : []
  const responseFailedIds = Array.isArray(result.failed_ids) ? result.failed_ids : []
  if (responseSuccessIds.length > 0 || responseFailedIds.length > 0) {
    return {
      successIds: responseSuccessIds,
      failedIds: responseFailedIds,
      successCount: typeof result.success === 'number' ? result.success : responseSuccessIds.length,
      failedCount: typeof result.failed === 'number' ? result.failed : responseFailedIds.length,
      hasIds: true,
      hasCounts: true
    }
  }

  const results = Array.isArray(result.results) ? result.results : []
  if (results.length > 0) {
    const successIds = results.filter(item => item.success).map(item => item.account_id)
    const failedIds = results.filter(item => !item.success).map(item => item.account_id)
    return {
      successIds,
      failedIds,
      successCount: typeof result.success === 'number' ? result.success : successIds.length,
      failedCount: typeof result.failed === 'number' ? result.failed : failedIds.length,
      hasIds: true,
      hasCounts: true
    }
  }

  const hasExplicitCounts = typeof result.success === 'number' || typeof result.failed === 'number'
  const successCount = typeof result.success === 'number' ? result.success : 0
  const failedCount = typeof result.failed === 'number' ? result.failed : 0
  if (hasExplicitCounts && failedCount === 0 && successCount === accountIds.length && accountIds.length > 0) {
    return {
      successIds: accountIds,
      failedIds: [],
      successCount,
      failedCount,
      hasIds: true,
      hasCounts: true
    }
  }

  return {
    successIds: [],
    failedIds: [],
    successCount,
    failedCount,
    hasIds: false,
    hasCounts: hasExplicitCounts
  }
}
const handleBulkToggleSchedulable = async (schedulable: boolean) => {
    const action = beginAccountAction('bulk-schedulable', selIds.value, false);
    if (!action)
        return;
    const selectionRevision = accountSelectionRevision;
    try {
        const accountIds = [...action.targets];
        try {
            const result = await action.wait(() => accountsAPI.bulkUpdate(accountIds, { schedulable }, undefined, { signal: action.controller.signal }));
            const { successIds, failedIds, successCount, failedCount, hasIds, hasCounts } = normalizeBulkSchedulableResult(result, accountIds);
            if (!hasIds && !hasCounts) {
                appStore.showError(t('admin.accounts.bulkSchedulableResultUnknown'));
                applyActionSelection(selectionRevision, () => setSelectedIds(accountIds));
                await action.wait(() => load());
                return;
            }
            if (successIds.length > 0) {
                updateSchedulableInList(successIds, schedulable);
            }
            if (successCount > 0 && failedCount === 0) {
                const message = schedulable
                    ? t('admin.accounts.bulkSchedulableEnabled', { count: successCount })
                    : t('admin.accounts.bulkSchedulableDisabled', { count: successCount });
                appStore.showSuccess(message);
            }
            if (failedCount > 0) {
                const message = hasCounts || hasIds
                    ? t('admin.accounts.bulkSchedulablePartial', { success: successCount, failed: failedCount })
                    : t('admin.accounts.bulkSchedulableResultUnknown');
                appStore.showError(message);
                applyActionSelection(selectionRevision, () => setSelectedIds(failedIds.length > 0 ? failedIds : accountIds));
            }
            else {
                if (hasIds)
                    applyActionSelection(selectionRevision, () => clearSelection());
                else
                    applyActionSelection(selectionRevision, () => setSelectedIds(accountIds));
            }
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to bulk toggle schedulable:', error);
            appStore.showError(t('common.error'));
        }
    }
    finally {
        action.finish();
    }
};
const buildBulkEditFilterSnapshot = () => {
  const rawParams = toRaw(params) as Record<string, unknown>
  const sortOrder: AccountSortOrder = rawParams.sort_order === 'desc' ? 'desc' : 'asc'
  return {
    platform: typeof rawParams.platform === 'string' ? rawParams.platform : '',
    type: typeof rawParams.type === 'string' ? rawParams.type : '',
    status: typeof rawParams.status === 'string' ? rawParams.status : '',
    group: typeof rawParams.group === 'string' ? rawParams.group : '',
    search: typeof rawParams.search === 'string' ? rawParams.search : '',
    privacy_mode: typeof rawParams.privacy_mode === 'string' ? rawParams.privacy_mode : '',
    sort_by: typeof rawParams.sort_by === 'string' ? rawParams.sort_by : '',
    sort_order: sortOrder
  }
}

const handleSelectAllResults = async () => {
    const action = beginAccountAction('select-all', [], false, accountQueryScope);
    if (!action)
        return;
    try {
        if (pagination.total === 0)
            return;
        const requestVersion = ++selectionRequestVersion.value;
        const filters = buildBulkEditFilterSnapshot();
        accountSelectionRevision++;
        try {
            const ids = await action.wait(() => fetchAllAccountIds((page, pageSize, requestFilters) => action.wait(() => accountsAPI.list(page, pageSize, requestFilters, { signal: action.controller.signal })), filters));
            if (requestVersion !== selectionRequestVersion.value)
                return;
            setSelectedIds(ids);
            selectedAllResultIDs.value = new Set(ids);
        }
        catch (error) {
            if (!action.current())
                return;
            if (requestVersion !== selectionRequestVersion.value)
                return;
            console.error('Failed to select all account results:', error);
            appStore.showError(t('admin.accounts.bulkActions.selectAllFailed'));
        }

    }
    finally {
        action.finish();
    }
};

const collectSelectionMetadata = (rows: Account[]) => {
  const selectedPlatforms = Array.from(new Set(rows.map(account => account.platform)))
  const selectedTypes = Array.from(new Set(rows.map(account => account.type)))
  return { selectedPlatforms, selectedTypes }
}

const openBulkEditSelected = () => {
  cancelAccountAction('bulk-edit-filtered')
  bulkEditTarget.value = {
    mode: 'selected',
    accountIds: [...selIds.value],
    selectedPlatforms: [...selPlatforms.value],
    selectedTypes: [...selTypes.value]
  }
  showBulkEdit.value = true
}

const openBulkEditFiltered = async () => {
    const action = beginAccountAction('bulk-edit-filtered', [], false, accountQueryScope);
    if (!action)
        return;
    try {
        // A checked selection always wins, including queued clicks from the old toolbar.
        if (selIds.value.length > 0) {
            openBulkEditSelected();
            return;
        }
        const filters = buildBulkEditFilterSnapshot();
        const requestVersion = selectionRequestVersion.value;
        try {
            // Freeze the complete preview as explicit IDs. Saving must never re-resolve
            // a broader filter or silently include accounts created while editing.
            const rows: Account[] = [];
            const accountIds = await action.wait(() => fetchAllAccountIds(async (page, pageSize, requestFilters) => {
                const result = await action.wait(() => accountsAPI.list(page, pageSize, requestFilters, { signal: action.controller.signal }));
                rows.push(...result.items);
                return result;
            }, filters));
            if (requestVersion !== selectionRequestVersion.value)
                return;
            if (selIds.value.length > 0) {
                openBulkEditSelected();
                return;
            }
            if (accountIds.length === 0) {
                appStore.showError(t('admin.accounts.bulkEdit.noSelection'));
                return;
            }
            const { selectedPlatforms, selectedTypes } = collectSelectionMetadata(rows);
            bulkEditTarget.value = {
                mode: 'selected',
                accountIds,
                selectedPlatforms,
                selectedTypes
            };
            showBulkEdit.value = true;
        }
        catch {
            if (!action.current())
                return;
            appStore.showError(t('admin.accounts.bulkActions.selectAllFailed'));
        }
    }
    finally {
        action.finish();
    }
};

const handleBulkUpdated = () => {
  showBulkEdit.value = false
  bulkEditTarget.value = null
  clearSelection()
  reload()
}
const handleDataImported = () => { showImportData.value = false; reload() }
const ACCOUNT_UNGROUPED_GROUP_QUERY_VALUE = 'ungrouped'
const ACCOUNT_PRIVACY_MODE_UNSET_QUERY_VALUE = '__unset__'
const buildAccountQueryFilters = () => ({
  platform: params.platform || '',
  type: params.type || '',
  status: params.status || '',
  group: params.group || '',
  privacy_mode: params.privacy_mode || '',
  search: params.search || '',
  sort_by: sortState.sort_by,
  sort_order: sortState.sort_order
})
const accountMatchesCurrentFilters = (account: Account) => {
  const filters = buildAccountQueryFilters()
  if (filters.platform && account.platform !== filters.platform) return false
  if (filters.type && account.type !== filters.type) return false
  if (filters.status) {
    const now = Date.now()
    const rateLimitResetAt = account.rate_limit_reset_at ? new Date(account.rate_limit_reset_at).getTime() : Number.NaN
    const isRateLimited = Number.isFinite(rateLimitResetAt) && rateLimitResetAt > now
    const tempUnschedUntil = account.temp_unschedulable_until ? new Date(account.temp_unschedulable_until).getTime() : Number.NaN
    const isTempUnschedulable = Number.isFinite(tempUnschedUntil) && tempUnschedUntil > now

    if (filters.status === 'active') {
      if (account.status !== 'active' || isRateLimited || isTempUnschedulable || !account.schedulable) return false
    } else if (filters.status === 'rate_limited') {
      if (account.status !== 'active' || !isRateLimited || isTempUnschedulable) return false
    } else if (filters.status === 'temp_unschedulable') {
      if (account.status !== 'active' || !isTempUnschedulable) return false
    } else if (filters.status === 'unschedulable') {
      if (account.status !== 'active' || account.schedulable || isRateLimited || isTempUnschedulable) return false
    } else if (account.status !== filters.status) {
      return false
    }
  }
  if (filters.group) {
    const groupIds = account.group_ids ?? account.groups?.map((group) => group.id) ?? []
    if (filters.group === ACCOUNT_UNGROUPED_GROUP_QUERY_VALUE) {
      if (groupIds.length > 0) return false
    } else if (!groupIds.includes(Number(filters.group))) {
      return false
    }
  }
  const privacyMode = typeof account.extra?.privacy_mode === 'string' ? account.extra.privacy_mode : ''
  if (filters.privacy_mode) {
    if (filters.privacy_mode === ACCOUNT_PRIVACY_MODE_UNSET_QUERY_VALUE) {
      if (privacyMode.trim() !== '') return false
    } else if (privacyMode !== filters.privacy_mode) {
      return false
    }
  }
  const search = String(filters.search || '').trim().toLowerCase()
  if (search && !account.name.toLowerCase().includes(search)) return false
  return true
}
const mergeRuntimeFields = (oldAccount: Account, updatedAccount: Account): Account => ({
  ...updatedAccount,
  current_concurrency: updatedAccount.current_concurrency ?? oldAccount.current_concurrency,
  current_window_cost: updatedAccount.current_window_cost ?? oldAccount.current_window_cost,
  active_sessions: updatedAccount.active_sessions ?? oldAccount.active_sessions
})

const syncPaginationAfterLocalRemoval = () => {
  const nextTotal = Math.max(0, pagination.total - 1)
  pagination.total = nextTotal
  pagination.pages = nextTotal > 0 ? Math.ceil(nextTotal / pagination.page_size) : 0

  const maxPage = Math.max(1, pagination.pages || 1)

  if (pagination.page > maxPage) {
    pagination.page = maxPage
  }
  // 行被本地移除后不立刻全量补页，改为提示用户手动同步。
  hasPendingListSync.value = nextTotal > 0
}

const patchAccountInList = (updatedAccount: Account) => {
  const index = accounts.value.findIndex(account => account.id === updatedAccount.id)
  if (index === -1) return
  const mergedAccount = mergeRuntimeFields(accounts.value[index], updatedAccount)
  if (!accountMatchesCurrentFilters(mergedAccount)) {
    accounts.value = accounts.value.filter(account => account.id !== mergedAccount.id)
    syncPaginationAfterLocalRemoval()
    removeSelectedAccounts([mergedAccount.id])
    if (menu.acc?.id === mergedAccount.id) {
      menu.show = false
      menu.acc = null
    }
    return
  }
  const nextAccounts = [...accounts.value]
  nextAccounts[index] = mergedAccount
  accounts.value = nextAccounts
  syncAccountRefs(mergedAccount)
}
const patchUpstreamBillingSnapshot = (accountID: number, snapshot: UpstreamBillingProbeSnapshot) => {
  const account = accounts.value.find(item => item.id === accountID)
  if (!account) return
  upstreamBillingNow.value = Date.now()
  patchAccountInList({
    ...account,
    ...(typeof snapshot.synced_rate_multiplier === 'number'
      ? { rate_multiplier: snapshot.synced_rate_multiplier }
      : {}),
    extra: { ...account.extra, upstream_billing_probe: snapshot }
  })
}
const refreshAccountsAfterUpstreamBillingProbe = async () => {
  await refreshUpstreamBillingSortedList(true)
}
const handleProbeUpstreamBilling = async (account: Account) => {
    const action = beginAccountAction('probe:' + account.id, [account.id], false);
    if (!action)
        return;
    try {
        try {
            const result = await action.wait(() => accountsAPI.probeUpstreamBilling(action.targets[0], { signal: action.controller.signal }));
            if (result.snapshot) {
                patchUpstreamBillingSnapshot(action.targets[0], result.snapshot);
                await action.wait(() => refreshAccountsAfterUpstreamBillingProbe());
            }
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to probe upstream billing:', error);
            appStore.showError(extractApiErrorMessage(error, t('admin.accounts.upstreamBilling.probeFailed')));
        }
    }
    finally {
        action.finish();
    }
};
const handleAccountUpdated = (updatedAccount: Account) => {
  patchAccountInList(updatedAccount)
  enterAutoRefreshSilentWindow()
}
const formatExportTimestamp = () => {
  const now = new Date()
  const pad2 = (value: number) => String(value).padStart(2, '0')
  return `${now.getFullYear()}${pad2(now.getMonth() + 1)}${pad2(now.getDate())}${pad2(now.getHours())}${pad2(now.getMinutes())}${pad2(now.getSeconds())}`
}
const openExportDataDialog = () => {
  includeProxyOnExport.value = true
  showExportDataDialog.value = true
}
const handleExportData = async (): Promise<void> => {
  const action = beginAccountAction('export', [...selIds.value])
  if (!action) return
  const includeProxies = includeProxyOnExport.value
  const request = action.targets.length > 0
    ? { ids: [...action.targets], includeProxies }
    : { includeProxies, filters: { ...buildAccountQueryFilters() } }
  const filePrefix = brandFilePrefix(appStore.siteName)
  showExportDataDialog.value = false
  try {
    // Step-up may invoke this closure twice. Both attempts keep the same intent,
    // and the page must still own it before either attempt starts a request.
    const dataPayload = await action.wait(() => accountExportStepUp.run(
      () => action.wait(() => accountsAPI.exportData(request, { signal: action.controller.signal }))
    ))
    const filename = filePrefix + '-account-' + formatExportTimestamp() + '.json'
    const blob = new Blob([JSON.stringify(dataPayload, null, 2)], { type: 'application/json' })
    const url = URL.createObjectURL(blob)
    try {
      const link = document.createElement('a')
      link.href = url
      link.download = filename
      link.click()
    } finally { URL.revokeObjectURL(url) }
    if (dataPayload.skipped_shadows && dataPayload.skipped_shadows > 0) {
      appStore.showWarning(t('admin.accounts.dataExportedSkippedShadows', { count: dataPayload.skipped_shadows }))
    } else appStore.showSuccess(t('admin.accounts.dataExported'))
  } catch (error: unknown) {
    if (!action.current() || isStepUpCancelled(error)) return
    if (isStepUpBlocked(error)) {
      appStore.showError(stepUpBlockReason(error) === 'STEP_UP_ADMIN_API_KEY_FORBIDDEN'
        ? t('stepUp.adminApiKeyForbidden') : t('stepUp.notEnabled'))
    } else appStore.showError(readApiError(error).message || t('admin.accounts.dataExportFailed'))
  } finally { action.finish() }
}
const accountExportStepUp = useStepUp()
const closeEditModal = () => { cancelAccountAction('detail'); showEdit.value = false; edAcc.value = null }
const closeTestModal = () => { cancelAccountAction('detail'); showTest.value = false; testingAcc.value = null }
const closeStatsModal = () => { cancelAccountAction('detail'); showStats.value = false; statsAcc.value = null }
const closeReAuthModal = () => { showReAuth.value = false; reAuthAcc.value = null }
const handleTest = (account: AccountListItem): Promise<void> => openAccountDetails(account.id, 'test')
const handleViewStats = (account: AccountListItem): Promise<void> => openAccountDetails(account.id, 'stats')
const handleSchedule = async (account: Account): Promise<void> => {
  const action = beginAccountAction('schedule', [account.id], true)
  if (!action) return
  scheduleAcc.value = account
  scheduleModelOptions.value = []
  showSchedulePanel.value = true
  try {
    const models = await action.wait(() => accountsAPI.getAvailableModels(action.targets[0], { signal: action.controller.signal }))
    scheduleModelOptions.value = models.map((model: ClaudeModel) => ({ value: model.id, label: model.display_name || model.id }))
  } catch {
    if (action.current()) scheduleModelOptions.value = []
  } finally { action.finish() }
}
const closeSchedulePanel = () => {
  cancelAccountAction('schedule')
  showSchedulePanel.value = false
  scheduleAcc.value = null
  scheduleModelOptions.value = []
}
const handleReAuth = (a: Account) => { reAuthAcc.value = a; showReAuth.value = true }
const handleDuplicateAccount = async (a: Account) => {
    const action = beginAccountAction('duplicate:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            const duplicate = await action.wait(() => accountsAPI.duplicate(action.targets[0], { signal: action.controller.signal }));
            appStore.showSuccess(t('admin.accounts.duplicateSuccess', { name: duplicate.name }));
            await action.wait(() => reload());
        }
        catch (error: unknown) {
            if (!action.current())
                return;
            console.error('Failed to duplicate account:', error);
            appStore.showError(readApiError(error).message || t('admin.accounts.duplicateFailed'));
        }
    }
    finally {
        action.finish();
    }
};
const handleRefresh = async (a: Account) => {
    const action = beginAccountAction('refresh:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            const result = await action.wait(() => accountsAPI.refreshCredentials(action.targets[0], { signal: action.controller.signal }));
            patchAccountInList(result.account);
            enterAutoRefreshSilentWindow();
            if (result.warning)
                appStore.showWarning(result.message);
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to refresh credentials:', error);
        }
    }
    finally {
        action.finish();
    }
};
const handleRecoverState = async (a: Account) => {
    const action = beginAccountAction('recover:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            const updated = await action.wait(() => accountsAPI.recoverState(action.targets[0], { signal: action.controller.signal }));
            patchAccountInList(updated);
            enterAutoRefreshSilentWindow();
            appStore.showSuccess(t('admin.accounts.recoverStateSuccess'));
        }
        catch (error: unknown) {
            if (!action.current())
                return;
            console.error('Failed to recover account state:', error);
            appStore.showError(readApiError(error).message || t('admin.accounts.recoverStateFailed'));
        }
    }
    finally {
        action.finish();
    }
};
const handleResetQuota = async (a: Account) => {
    const action = beginAccountAction('reset-quota:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            const updated = await action.wait(() => accountsAPI.resetAccountQuota(action.targets[0], { signal: action.controller.signal }));
            patchAccountInList(updated);
            enterAutoRefreshSilentWindow();
            appStore.showSuccess(t('common.success'));
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to reset quota:', error);
        }
    }
    finally {
        action.finish();
    }
};

const privacyResultMessageKey = (account: Account): { type: 'success' | 'error'; key: string } => {
  const mode = typeof account.extra?.privacy_mode === 'string' ? account.extra.privacy_mode : ''
  if (account.platform === 'openai') {
    switch (mode) {
      case 'training_off':
        return { type: 'success', key: 'admin.accounts.privacyTrainingOff' }
      case 'training_set_cf_blocked':
        return { type: 'error', key: 'admin.accounts.privacyCfBlocked' }
      default:
        return { type: 'error', key: 'admin.accounts.privacyFailed' }
    }
  }
  if (account.platform === 'antigravity') {
    if (mode === 'privacy_set') {
      return { type: 'success', key: 'admin.accounts.privacyAntigravitySet' }
    }
    return { type: 'error', key: 'admin.accounts.privacyAntigravityFailed' }
  }
  return { type: 'error', key: 'admin.accounts.privacyFailed' }
}

const handleSetPrivacy = async (a: Account) => {
    const action = beginAccountAction('privacy:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            const updated = await action.wait(() => accountsAPI.setPrivacy(action.targets[0], { signal: action.controller.signal }));
            patchAccountInList(updated);
            enterAutoRefreshSilentWindow();
            const result = privacyResultMessageKey(updated);
            if (result.type === 'success') {
                appStore.showSuccess(t(result.key));
            }
            else {
                appStore.showError(t(result.key));
            }
        }
        catch (error: unknown) {
            if (!action.current())
                return;
            console.error('Failed to set privacy:', error);
            appStore.showError(readApiError(error).response?.data?.message || t('admin.accounts.privacyFailed'));
        }
    }
    finally {
        action.finish();
    }
};
const onRevertFallback = async (a: Account) => {
    const action = beginAccountAction('revert-proxy:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        try {
            await action.wait(() => accountsAPI.revertProxyFallback(action.targets[0], { signal: action.controller.signal }));
            appStore.showSuccess(t('admin.accounts.revertProxySuccess'));
            await action.wait(() => reload());
        }
        catch (error: unknown) {
            if (!action.current())
                return;
            console.error('Failed to revert proxy fallback:', error);
            appStore.showError(readApiError(error).response?.data?.message || t('admin.accounts.revertProxyFailed'));
        }
    }
    finally {
        action.finish();
    }
};
const handleCreateSparkShadow = (a: Account) => {
  creatingShadowAcc.value = a
  showCreateShadowDialog.value = true
}
const confirmCreateSparkShadow = async (): Promise<void> => {
  const account = creatingShadowAcc.value
  if (!showCreateShadowDialog.value || !account) return
  const action = beginAccountAction('shadow:' + account.id, [account.id])
  if (!action) return
  const payload = { name: account.name + ' (Spark)' }
  showCreateShadowDialog.value = false
  creatingShadowAcc.value = null
  try {
    await action.wait(() => accountsAPI.createSparkShadow(action.targets[0], payload, { signal: action.controller.signal }))
    appStore.showSuccess(t('admin.accounts.createSparkShadowSuccess'))
    await action.wait(() => reload())
  } catch (error: unknown) {
    if (!action.current()) return
    console.error('Failed to create spark shadow:', error)
    appStore.showError(readApiError(error).response?.data?.message || t('admin.accounts.createSparkShadowFailed'))
  } finally { action.finish() }
}
const handleDelete = (a: Account) => { deletingAcc.value = a; showDeleteDialog.value = true }
const confirmDelete = async (): Promise<void> => {
  const account = deletingAcc.value
  if (!showDeleteDialog.value || !account) return
  const action = beginAccountAction('delete:' + account.id, [account.id])
  if (!action) return
  showDeleteDialog.value = false
  deletingAcc.value = null
  try {
    await action.wait(() => accountsAPI.delete(action.targets[0], { signal: action.controller.signal }))
    await action.wait(() => reload())
  } catch (error: unknown) {
    if (action.current()) console.error('Failed to delete account:', error)
  } finally { action.finish() }
}
const handleToggleSchedulable = async (a: Account) => {
    const action = beginAccountAction('schedulable:' + a.id, [a.id], false);
    if (!action)
        return;
    try {
        const nextSchedulable = !a.schedulable;
        try {
            const updated = await action.wait(() => accountsAPI.setSchedulable(action.targets[0], nextSchedulable, { signal: action.controller.signal }));
            updateSchedulableInList([action.targets[0]], updated?.schedulable ?? nextSchedulable);
            enterAutoRefreshSilentWindow();
        }
        catch (error) {
            if (!action.current())
                return;
            console.error('Failed to toggle schedulable:', error);
            appStore.showError(t('admin.accounts.failedToToggleSchedulable'));
        }
    }
    finally {
        action.finish();
    }
};
const handleShowTempUnsched = (a: Account) => { tempUnschedAcc.value = a; showTempUnsched.value = true }
const handleTempUnschedReset = async (updated: Account) => {
  showTempUnsched.value = false
  tempUnschedAcc.value = null
  patchAccountInList(updated)
  enterAutoRefreshSilentWindow()
}
const formatExpiresAt = (value: number | null) => {
  if (!value) return '-'
  return formatDateTime(
    new Date(value * 1000),
    {
      year: 'numeric',
      month: '2-digit',
      day: '2-digit',
      hour: '2-digit',
      minute: '2-digit',
      hour12: false
    },
    'sv-SE'
  )
}
const isExpired = (value: number | null) => {
  if (!value) return false
  return value * 1000 <= Date.now()
}
// 所绑定代理的有效期(逻辑同 /admin/proxies,见 utils/proxyExpiry)
const proxyExpiryBadge = (p: AccountProxy): string => proxyExpiryBadgeClass(p.expires_at, p.status)
const proxyExpiryText = (p: AccountProxy): string => {
  const { key, params } = proxyExpiryLabelKey(p.expires_at, p.status)
  return params ? t(key, params) : t(key)
}

// 表格滚动时关闭行操作菜单，并让顶部工具菜单继续贴紧触发按钮。
const handleScroll = (event: Event) => {
  if (event.target instanceof Element && event.target.closest('.action-menu-content')) return
  menu.show = false
  if (showAccountToolsDropdown.value) updateAccountToolsDropdownPosition()
}

const handleViewportResize = () => {
  if (showAccountToolsDropdown.value) updateAccountToolsDropdownPosition()
}

// 点击外部关闭顶部下拉菜单
const handleClickOutside = (event: MouseEvent) => {
  const target = event.target as HTMLElement
  if (accountToolsDropdownRef.value && !accountToolsDropdownRef.value.contains(target)) {
    showAccountToolsDropdown.value = false
  }
  if (autoRefreshDropdownRef.value && !autoRefreshDropdownRef.value.contains(target)) {
    showAutoRefreshDropdown.value = false
  }
}

onMounted(async () => {
  if (typeof window !== 'undefined') {
    desktopViewportMediaQuery = window.matchMedia(desktopViewportQuery)
    isDesktopViewport.value = desktopViewportMediaQuery.matches
    desktopViewportListener = (event: MediaQueryListEvent) => {
      isDesktopViewport.value = event.matches
    }
    if (typeof desktopViewportMediaQuery.addEventListener === 'function') {
      desktopViewportMediaQuery.addEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.addListener(desktopViewportListener)
    }
  }

  load()
  loadUpstreamBillingProbeGlobalState()
  const [proxiesResult, groupsResult] = await Promise.allSettled([
    proxiesAPI.getAll(),
    groupsAPI.getAll()
  ])
  if (!workspaceActive) return
  if (proxiesResult.status === 'fulfilled') {
    proxies.value = proxiesResult.value
  } else {
    console.error('Failed to load proxies:', proxiesResult.reason)
  }
  if (groupsResult.status === 'fulfilled') {
    groups.value = groupsResult.value
  } else {
    console.error('Failed to load groups:', groupsResult.reason)
  }
  window.addEventListener('scroll', handleScroll, true)
  window.addEventListener('resize', handleViewportResize)
  document.addEventListener('click', handleClickOutside)

  if (autoRefreshEnabled.value) {
    autoRefreshCountdown.value = autoRefreshIntervalSeconds.value
    resumeAutoRefresh()
  } else {
    pauseAutoRefresh()
  }
})

onUnmounted(() => {
  workspaceActive = false
  resetAutoRefreshCache()
  window.removeEventListener('scroll', handleScroll, true)
  window.removeEventListener('resize', handleViewportResize)
  document.removeEventListener('click', handleClickOutside)
  if (desktopViewportMediaQuery && desktopViewportListener) {
    if (typeof desktopViewportMediaQuery.removeEventListener === 'function') {
      desktopViewportMediaQuery.removeEventListener('change', desktopViewportListener)
    } else {
      desktopViewportMediaQuery.removeListener(desktopViewportListener)
    }
  }
  desktopViewportListener = null
  desktopViewportMediaQuery = null
})
</script>

<style scoped>
.account-tools-menu-item {
  @apply flex w-full items-center gap-3 rounded-md px-3 py-2 text-sm text-gray-700 transition-colors hover:bg-gray-100 dark:text-gray-200 dark:hover:bg-dark-700;
}

.account-tools-menu-icon {
  @apply inline-flex h-8 w-8 flex-shrink-0 items-center justify-center rounded-md;
}
</style>
