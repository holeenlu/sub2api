<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import type { GrokMediaEligibilityMode, GrokMediaEligibilityState } from '@/features/resources/api/accountCredentialTypes';
import Icon from '@/shared/ui/icons/Icon.vue'
import { type UnwrapRef, type Ref, type ComputedRef } from 'vue'
import { usePanelField } from '../model/usePanelField'


const props = defineProps<{
  isGrokOAuthAccount: UnwrapRef<ComputedRef<boolean>>
  grokMediaEligibilityMode: UnwrapRef<Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>>
  grokMediaEligibilityLoading: UnwrapRef<Ref<boolean, boolean>>
  grokMediaEligibilityError: UnwrapRef<Ref<string, string>>
  grokMediaEligibilityState: UnwrapRef<Ref<{ account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null, GrokMediaEligibilityState | { account_id: number; mode: GrokMediaEligibilityMode; eligible: boolean; reason: string } | null>>
}>()

const emit = defineEmits<{
  'update:grokMediaEligibilityMode': [value: UnwrapRef<Ref<GrokMediaEligibilityMode, GrokMediaEligibilityMode>>]
}>()

const { t } = useI18n()

const isGrokOAuthAccount = computed(() => props.isGrokOAuthAccount)

const grokMediaEligibilityMode = usePanelField(() => props.grokMediaEligibilityMode, value => emit('update:grokMediaEligibilityMode', value))

const grokMediaEligibilityLoading = computed(() => props.grokMediaEligibilityLoading)

const grokMediaEligibilityError = computed(() => props.grokMediaEligibilityError)

const grokMediaEligibilityState = computed(() => props.grokMediaEligibilityState)
</script>

<template>
<div
        v-if="isGrokOAuthAccount"
        class="border-t border-gray-200 pt-4 dark:border-dark-600"
        data-testid="grok-media-eligibility-card"
      >
        <div class="space-y-3">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.grokMediaEligibility.title') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.grokMediaEligibility.hint') }}
            </p>
          </div>
          <select
            v-model="grokMediaEligibilityMode"
            class="input"
            data-testid="grok-media-eligibility-mode"
            :disabled="grokMediaEligibilityLoading"
          >
            <option value="auto">{{ t('admin.accounts.grokMediaEligibility.auto') }}</option>
            <option value="enabled">{{ t('admin.accounts.grokMediaEligibility.enabled') }}</option>
            <option value="disabled">{{ t('admin.accounts.grokMediaEligibility.disabled') }}</option>
          </select>
          <p v-if="grokMediaEligibilityLoading" class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.grokMediaEligibility.loading') }}
          </p>
          <p v-else-if="grokMediaEligibilityError" class="text-xs text-red-600 dark:text-red-400">
            {{ grokMediaEligibilityError }}
          </p>
          <div v-else-if="grokMediaEligibilityState" class="rounded-lg bg-gray-50 p-3 text-xs dark:bg-dark-700">
            <span class="font-medium">{{ t('admin.accounts.grokMediaEligibility.current') }}</span>
            <span class="ml-1" data-testid="grok-media-eligibility-status">
              {{ grokMediaEligibilityState.eligible ? t('admin.accounts.grokMediaEligibility.eligible') : t('admin.accounts.grokMediaEligibility.ineligible') }}
              · {{ t(`admin.accounts.grokMediaEligibility.reasons.${grokMediaEligibilityState.reason}`) }}
            </span>
          </div>
          <div
            v-if="grokMediaEligibilityMode === 'enabled'"
            class="rounded-lg bg-amber-50 p-3 dark:bg-amber-900/20"
          >
            <p class="text-xs text-amber-700 dark:text-amber-400">
              <Icon name="exclamationTriangle" size="sm" class="mr-1 inline" :stroke-width="2" />
              {{ t('admin.accounts.grokMediaEligibility.forceEnableWarning') }}
            </p>
          </div>
          <p v-else-if="grokMediaEligibilityMode === 'auto'" class="text-xs text-gray-500 dark:text-gray-400">
            {{ t('admin.accounts.grokMediaEligibility.autoHint') }}
          </p>
        </div>
      </div>
</template>
