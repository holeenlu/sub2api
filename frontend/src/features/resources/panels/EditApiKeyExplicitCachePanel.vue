<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import Icon from '@/shared/ui/icons/Icon.vue'
import { isOfficialGeminiBaseUrl } from '@/features/resources/model/geminiExplicitCache'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props } from '../model/editEditorTypes'

const props = defineProps<{
  account: NonNullable<Props['account']>
  editBaseUrl: UnwrapRef<Ref<string, string>>
  explicitCacheUpstreamEnabled: UnwrapRef<Ref<boolean, boolean>>
  explicitCacheUpstreamMaxTTL: UnwrapRef<Ref<string | number, string | number>>
}>()

const emit = defineEmits<{
  'update:explicitCacheUpstreamEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
  'update:explicitCacheUpstreamMaxTTL': [value: UnwrapRef<Ref<string | number, string | number>>]
}>()

const { t } = useI18n()

const account = computed(() => props.account)

const editBaseUrl = computed(() => props.editBaseUrl)

const explicitCacheUpstreamEnabled = usePanelField(() => props.explicitCacheUpstreamEnabled, value => emit('update:explicitCacheUpstreamEnabled', value))

const explicitCacheUpstreamMaxTTL = usePanelField(() => props.explicitCacheUpstreamMaxTTL, value => emit('update:explicitCacheUpstreamMaxTTL', value))
</script>
<template>
<div
          v-if="account.platform === 'gemini' && !isOfficialGeminiBaseUrl(editBaseUrl)"
          class="border-t border-gray-200 pt-4 dark:border-dark-600"
          data-testid="explicit-cache-upstream-section"
        >
          <div class="flex items-center justify-between gap-4">
            <div>
              <label class="input-label mb-0">{{ t('admin.accounts.explicitCacheUpstream') }}</label>
              <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
                {{ t('admin.accounts.explicitCacheUpstreamHint') }}
              </p>
            </div>
            <Toggle
              v-model="explicitCacheUpstreamEnabled"
              data-testid="explicit-cache-upstream-toggle"
              :aria-label="t('admin.accounts.explicitCacheUpstream')"
            />
          </div>
          <div v-if="explicitCacheUpstreamEnabled" class="mt-3 rounded-lg bg-blue-50 p-3 dark:bg-blue-900/20">
            <p class="text-xs text-blue-700 dark:text-blue-400">
              <Icon name="exclamationCircle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.explicitCacheUpstreamInfo') }}
            </p>
          </div>
          <div v-if="explicitCacheUpstreamEnabled" class="mt-3">
            <label class="input-label">{{ t('admin.accounts.explicitCacheUpstreamMaxTTL') }}</label>
            <input
              v-model="explicitCacheUpstreamMaxTTL"
              data-testid="explicit-cache-upstream-max-ttl"
              type="number"
              min="1"
              step="1"
              class="input"
              placeholder="86400"
            />
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.explicitCacheUpstreamMaxTTLHint') }}
            </p>
          </div>
        </div>
</template>
