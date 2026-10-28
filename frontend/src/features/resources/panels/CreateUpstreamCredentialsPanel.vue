<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Toggle from '@/shared/ui/Toggle.vue'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { Props, CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  antigravityAccountType: UnwrapRef<Ref<"oauth" | "upstream", "oauth" | "upstream">>
  upstreamBaseUrl: UnwrapRef<Ref<string, string>>
  upstreamApiKey: UnwrapRef<Ref<string, string>>
  allowRateEdit: NonNullable<Props['allowRateEdit']>
  upstreamBillingAutoProbeEnabled: UnwrapRef<Ref<boolean, boolean>>
}>()

const emit = defineEmits<{
  'update:upstreamBaseUrl': [value: UnwrapRef<Ref<string, string>>]
  'update:upstreamApiKey': [value: UnwrapRef<Ref<string, string>>]
  'update:upstreamBillingAutoProbeEnabled': [value: UnwrapRef<Ref<boolean, boolean>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const antigravityAccountType = computed(() => props.antigravityAccountType)

const upstreamBaseUrl = usePanelField(() => props.upstreamBaseUrl, value => emit('update:upstreamBaseUrl', value))

const upstreamApiKey = usePanelField(() => props.upstreamApiKey, value => emit('update:upstreamApiKey', value))

const allowRateEdit = computed(() => props.allowRateEdit)

const upstreamBillingAutoProbeEnabled = usePanelField(() => props.upstreamBillingAutoProbeEnabled, value => emit('update:upstreamBillingAutoProbeEnabled', value))
</script>

<template>
<div v-if="form.platform === 'antigravity' && antigravityAccountType === 'upstream'" class="space-y-4">
        <div>
          <label class="input-label">{{ t('admin.accounts.upstream.baseUrl') }}</label>
          <input
            v-model="upstreamBaseUrl"
            type="text"
            required
            class="input"
            placeholder="https://cloudcode-pa.googleapis.com"
          />
          <p class="input-hint">{{ t('admin.accounts.upstream.baseUrlHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('admin.accounts.upstream.apiKey') }}</label>
          <input
            v-model="upstreamApiKey"
            type="password"
            required
            class="input font-mono"
            placeholder="sk-..."
          />
          <p class="input-hint">{{ t('admin.accounts.upstream.apiKeyHint') }}</p>
        </div>
        <!-- 上游倍率自动探测：antigravity upstream 也是 API-key 账号 -->
        <div v-if="allowRateEdit" class="flex items-center justify-between gap-4 border-t border-gray-200 pt-4 dark:border-dark-600">
          <div>
            <label class="input-label mb-0">{{ t('admin.accounts.upstreamBilling.autoProbe') }}</label>
            <p class="mt-1 text-xs text-gray-500 dark:text-gray-400">
              {{ t('admin.accounts.upstreamBilling.autoProbeHint') }}
            </p>
          </div>
          <Toggle
            v-model="upstreamBillingAutoProbeEnabled"
            data-testid="upstream-billing-auto-probe-antigravity"
            :aria-label="t('admin.accounts.upstreamBilling.autoProbe')"
          />
        </div>
      </div>
</template>
