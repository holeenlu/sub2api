<script lang="ts">
export interface ProxyFormValue {
  name: string
  protocol: string
  host: string
  port: number
  username: string
  password: string
  expires_at: string
  fallback_mode?: 'none' | 'proxy' | 'direct'
  backup_proxy_id?: number | null
}

export const EXPIRY_PRESETS = [7, 30, 90, 180]
const baseDateOrToday = (base: string): Date => {
  const date = base ? new Date(`${base}T00:00:00`) : new Date()
  date.setHours(0, 0, 0, 0)
  return date
}
export const addDaysToBase = (base: string, value: number | null): string => {
  const days = Number(value)
  if (!Number.isFinite(days) || days <= 0) return ''
  const date = baseDateOrToday(base)
  date.setDate(date.getDate() + days)
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
}
export const daysFromBase = (base: string, target: string): number | null =>
  target ? Math.round((new Date(`${target}T00:00:00`).getTime() - baseDateOrToday(base).getTime()) / 86400000) : null
</script>

<script setup lang="ts">
import { computed, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Select from '@/shared/ui/Select.vue'
import Icon from '@/shared/ui/icons/Icon.vue'

const props = withDefaults(defineProps<{
  disabled?: boolean
  showCredentials?: boolean
  showFallback?: boolean
  fallbackDisabled?: boolean
  allowDirectFallback?: boolean
  backupOptions?: { label: string; value: number }[]
}>(), { showCredentials: true, showFallback: true, allowDirectFallback: true, backupOptions: () => [] })
const form = defineModel<ProxyFormValue>({ required: true })
const { t } = useI18n()
const id = useId()
const passwordVisible = ref(false)
watch(() => props.showCredentials, () => { passwordVisible.value = false })
const protocolOptions = computed(() => ['http', 'https', 'socks5', 'socks5h'].map(value => ({
  value, label: t(`admin.proxies.protocols.${value}`)
})))
const expiresDays = computed<number | null>({
  get: () => daysFromBase('', form.value.expires_at),
  set: value => { form.value.expires_at = addDaysToBase('', value) }
})
</script>

<template>
  <div>
    <label :for="`${id}-name`" class="input-label">{{ t('admin.proxies.name') }}</label>
    <input :id="`${id}-name`" v-model="form.name" type="text" required class="input"
      :disabled="disabled" :placeholder="t('admin.proxies.enterProxyName')" />
  </div>
  <div>
    <label class="input-label">{{ t('admin.proxies.protocol') }}</label>
    <Select v-model="form.protocol" :options="protocolOptions" :disabled="disabled"
      :aria-label="t('admin.proxies.protocol')" />
  </div>
  <div class="grid grid-cols-2 gap-4">
    <div>
      <label :for="`${id}-host`" class="input-label">{{ t('admin.proxies.host') }}</label>
      <input :id="`${id}-host`" v-model="form.host" type="text" required class="input"
        :disabled="disabled" :placeholder="t('admin.proxies.form.hostPlaceholder')" />
    </div>
    <div>
      <label :for="`${id}-port`" class="input-label">{{ t('admin.proxies.port') }}</label>
      <input :id="`${id}-port`" v-model.number="form.port" type="number" required min="1" max="65535"
        class="input" :disabled="disabled" :placeholder="t('admin.proxies.form.portPlaceholder')" />
    </div>
  </div>
  <slot name="credentials" />
  <template v-if="showCredentials">
    <div>
      <label :for="`${id}-username`" class="input-label">{{ t('admin.proxies.username') }}</label>
      <input :id="`${id}-username`" v-model="form.username" type="text" autocomplete="off" class="input"
        :disabled="disabled" :placeholder="t('admin.proxies.optionalAuth')" />
    </div>
    <div>
      <label :for="`${id}-password`" class="input-label">{{ t('admin.proxies.password') }}</label>
      <div class="relative">
        <input :id="`${id}-password`" v-model="form.password" :type="passwordVisible ? 'text' : 'password'"
          autocomplete="new-password" class="input pr-10" :disabled="disabled" :placeholder="t('admin.proxies.optionalAuth')" />
        <button type="button" :disabled="disabled" :aria-label="t('admin.proxies.password')" :aria-pressed="passwordVisible"
          class="absolute right-3 top-1/2 -translate-y-1/2 text-gray-400 hover:text-gray-600 dark:hover:text-gray-300"
          @click="passwordVisible = !passwordVisible">
          <Icon :name="passwordVisible ? 'eyeOff' : 'eye'" size="md" />
        </button>
      </div>
    </div>
  </template>
  <slot name="expiry">
    <div>
      <label :for="`${id}-expiry`" class="input-label">{{ t('admin.proxies.expiresAt') }}</label>
      <div class="mb-2 flex flex-wrap gap-2">
        <button v-for="days in EXPIRY_PRESETS" :key="days" type="button" class="btn btn-sm" :disabled="disabled"
          :class="form.expires_at === addDaysToBase('', days) ? 'btn-primary' : 'btn-secondary'" @click="expiresDays = days">
          {{ t('admin.proxies.nDays', { days }) }}
        </button>
      </div>
      <input v-model.number="expiresDays" type="number" min="0" class="input mb-2" :disabled="disabled"
        :aria-label="t('admin.proxies.expiryDaysPlaceholder')" :placeholder="t('admin.proxies.expiryDaysPlaceholder')" />
      <input :id="`${id}-expiry`" v-model="form.expires_at" type="date" max="9999-12-31" class="input" :disabled="disabled" />
    </div>
  </slot>
  <template v-if="showFallback">
    <div>
      <label class="input-label">{{ t('admin.proxies.fallbackMode') }}</label>
      <Select v-model="form.fallback_mode" :disabled="disabled || fallbackDisabled" :aria-label="t('admin.proxies.fallbackMode')"
        :options="[
          { label: t('admin.proxies.fallbackNone'), value: 'none' },
          { label: t('admin.proxies.fallbackProxy'), value: 'proxy' },
          { label: t('admin.proxies.fallbackDirect'), value: 'direct', disabled: !allowDirectFallback }
        ]" />
    </div>
    <div v-if="form.fallback_mode === 'proxy'">
      <label class="input-label">{{ t('admin.proxies.backupProxy') }}</label>
      <Select v-model="form.backup_proxy_id" :options="backupOptions" :disabled="disabled || fallbackDisabled"
        :aria-label="t('admin.proxies.backupProxy')" />
    </div>
  </template>
</template>
