<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, useId, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import Select from '@/components/common/Select.vue'
import { platformDisplayName } from '@/constants/platformCatalog'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import GroupSelector from '@/components/common/GroupSelector.vue'
import GroupBadge from '@/components/common/GroupBadge.vue'
import { platformAccountTabClass, platformAccountSelectionClass } from '@/utils/platformColors'
import { useClipboard } from '@/composables/useClipboard'
import ProxyForm from './ProxyForm.vue'
import { resourceAPI, vendorAPI } from '../api'
import { accountOpenAIModes, errorText, proxyAvailable, vendorOpenAIModes } from '../helpers'
import type {
  OAuthLease,
  OpenAIAuthorizationMode,
  AccountAuthorizationAction,
  ResourceScope,
  Vendor,
  VendorAccount,
  VendorPlatform,
  VendorProxy
} from '../types'
const props = defineProps<{
  vendor: Vendor
  scope: ResourceScope
  account?: VendorAccount
  reauthorize?: boolean
  authorizationMode?: OpenAIAuthorizationMode
}>()
const emit = defineEmits<{ close: []; saved: [] }>()
const { t } = useI18n()
const formId = useId()
const { copyToClipboard } = useClipboard()
const authorizedId = ref<number>()
const busy = ref(false),
  error = ref(''),
  code = ref(''),
  token = ref(''),
  lease = ref<OAuthLease>(),
  quickProxy = ref(false)
const proxies = ref<VendorProxy[]>([]),
  proxyLoading = ref(false)
let generation = 0
const form = reactive({
  name: props.account?.name || '',
  vendor_notes: props.account?.vendor_notes || '',
  platform: props.account?.platform || props.vendor.platforms[0] || ('anthropic' as VendorPlatform),
  method: props.account?.method === 'setup-token-manual' || props.account?.method === 'refresh_token'
    ? 'manual' : props.account?.method || 'oauth',
  authorization_mode: props.authorizationMode ||
    (props.account ? accountOpenAIModes(props.account).find(mode => vendorOpenAIModes(props.vendor, props.account).includes(mode)) : undefined) ||
    vendorOpenAIModes(props.vendor, props.account)[0] || 'native' as OpenAIAuthorizationMode,
  group_ids: [
    ...(props.account?.group_ids ||
      props.vendor.groups?.filter((g) => g.is_default).map((g) => g.id) ||
      [])
  ],
  proxy_id: props.account
    ? props.account.proxy_id
      ? String(props.account.proxy_id)
      : props.vendor.allow_direct
        ? 'direct'
        : ''
    : ''
})
const editing = computed(() => !!props.account && !props.reauthorize)
const authorizationModes = computed(() => vendorOpenAIModes(props.vendor, props.account))
const authorizationAction = computed<AccountAuthorizationAction>(() => {
  if (!props.account) return 'create'
  return accountOpenAIModes(props.account).includes(form.authorization_mode) ? 'reauthorize' : 'attach'
})
const title = computed(() => editing.value ? t('vendor.edit') : props.account
  ? t(form.platform === 'openai' && authorizationAction.value === 'attach' ? 'vendor.authorization.attach' : 'vendor.reauthorize')
  : t('vendor.addAccount'))
const methods = computed(() => {
  const supported = props.vendor.supported_auth_methods?.[form.platform]
  const available =
    form.platform === 'anthropic'
      ? [
          {
            value: 'oauth',
            wire: 'oauth',
            label: t('vendor.authMethods.oauth', 'OAuth')
          },
          {
            value: 'setup-token',
            wire: 'setup-token',
            label: t('vendor.authMethods.oauthToken', 'OAuth Token')
          },
          {
            value: 'manual',
            wire: 'setup-token-manual',
            label: t('vendor.authMethods.setupToken', 'Setup Token')
          }
        ]
      : [
          {
            value: 'oauth',
            wire: 'oauth',
            label: t('vendor.authMethods.oauth', 'OAuth')
          },
          {
            value: 'manual',
            wire: 'refresh_token',
            label: t('vendor.authMethods.refreshToken', 'Refresh Token')
          }
        ]
  return supported ? available.filter((method) => supported.includes(method.wire)) : available
})
watch(
  methods,
  (values) => {
    if (!values.some((m) => m.value === form.method)) form.method = values[0]?.value || ''
  },
  { immediate: true }
)
const permittedGroups = computed(() =>
  (props.vendor.groups || []).filter((g) => !g.platform || g.platform === form.platform)
)
const valid = computed(
  () =>
    (editing.value || methods.value.some((m) => m.value === form.method)) &&
    (editing.value || (props.vendor.platforms.includes(form.platform) &&
      (form.platform !== 'openai' || authorizationModes.value.includes(form.authorization_mode)))) &&
    (!editing.value || !!form.name.trim()) &&
    form.group_ids.length > 0 &&
    form.group_ids.every((id) => permittedGroups.value.some((g) => g.id === id)) &&
    (form.proxy_id === 'direct'
      ? props.vendor.allow_direct
      : proxies.value.some(
          (p) =>
            p.id === Number(form.proxy_id) &&
            (proxyAvailable(p) || (editing.value && p.id === props.account?.proxy_id))
        ))
)
if (!props.account) {
  form.group_ids = form.group_ids.filter((id) =>
    permittedGroups.value.some((group) => group.id === id)
  )
  if (!form.group_ids.length && permittedGroups.value[0])
    form.group_ids = [permittedGroups.value[0].id]
}
const manual = computed(() => form.method === 'manual')
function resetLease() {
  generation++
  lease.value = undefined
  code.value = ''
  token.value = ''
}
watch(
  () => [form.platform, form.method, form.authorization_mode],
  () => {
    resetLease()
    form.group_ids = form.group_ids.filter((id) => permittedGroups.value.some((g) => g.id === id))
    if (!props.account && !form.group_ids.length && permittedGroups.value[0])
      form.group_ids = [permittedGroups.value[0].id]
  }
)
watch(authorizationModes, modes => {
  if (editing.value || modes.includes(form.authorization_mode)) return
  resetLease()
  form.authorization_mode = modes[0] || 'native'
})
function proxyBody() {
  return {
    proxy_id: form.proxy_id === 'direct' ? null : Number(form.proxy_id),
    direct: form.proxy_id === 'direct'
  }
}
async function changeProxy(value: string) {
  const old = form.proxy_id
  form.proxy_id = value
  if (!lease.value || authorizedId.value) return
  busy.value = true
  error.value = ''
  try {
    await vendorAPI.rebindOAuth(form.platform, lease.value.session_id, proxyBody(), props.scope)
  } catch (e) {
    form.proxy_id = old
    error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
async function loadProxies() {
  proxyLoading.value = true
  try {
    const list: VendorProxy[] = []
    let page = 1,
      total = 0
    do {
      const data = await resourceAPI(props.scope).proxies({
        page,
        page_size: 50
      })
      list.push(...data.items)
      total = data.total
      page++
    } while (list.length < total && page <= 200)
    proxies.value = list
  } catch (e) {
    error.value = errorText(e)
  } finally {
    proxyLoading.value = false
  }
}
async function proxyCreated(proxy: VendorProxy) {
  proxies.value.push(proxy)
  quickProxy.value = false
  await changeProxy(String(proxy.id))
}
async function submit() {
  if (busy.value || props.scope.readOnly || props.account?.archived) return
  if (!valid.value) {
    error.value = t('vendor.requiredGroups')
    return
  }
  busy.value = true
  error.value = ''
  const epoch = generation
  try {
    const metadata = {
      ...(form.name.trim() ? { name: form.name.trim() } : {}),
      vendor_notes: form.vendor_notes.trim(),
      group_ids: [...form.group_ids],
      ...proxyBody()
    }
    const authorization = {
      ...(form.platform === 'openai' ? {
        authorization_mode: form.authorization_mode,
        authorization_action: authorizationAction.value
      } : {}),
      ...(props.account ? { target_account_id: props.account.id } : {})
    }
    if (editing.value && props.account) {
      await resourceAPI(props.scope).updateAccount(props.account.id, {
        ...metadata
      })
      emit('saved')
    } else if (authorizedId.value) {
      await resourceAPI(props.scope).updateAccount(authorizedId.value, metadata)
      emit('saved')
    } else if (manual.value) {
      await resourceAPI(props.scope).createAccount({
        ...metadata,
        platform: form.platform,
        method: form.platform === 'openai' ? 'refresh_token' : 'setup-token-manual',
        token: token.value,
        ...authorization
      })
      token.value = ''
      emit('saved')
    } else if (lease.value) {
      const account = await vendorAPI.completeOAuth(
        form.platform,
        lease.value.session_id,
        code.value,
        props.scope
      )
      if (epoch !== generation) return
      authorizedId.value = account.id
      code.value = ''
      await resourceAPI(props.scope).updateAccount(account.id, metadata)
      emit('saved')
    } else {
      const result = await vendorAPI.startOAuth(
        form.platform,
        {
          ...metadata,
          method: form.method,
          ...authorization
        },
        props.scope
      )
      if (epoch === generation) lease.value = result
    }
  } catch (e) {
    const reason = typeof e === 'object' && e && 'reason' in e ? e.reason : undefined
    if (reason === 'VENDOR_OAUTH_RESTART_REQUIRED') {
      resetLease()
      error.value = t('vendor.oauthRestartRequired')
    } else if (reason === 'VENDOR_IDENTITY_KEY_MISMATCH') {
      error.value = t('vendor.identityKeyMismatch')
    } else {
      error.value = errorText(e)
    }
  } finally {
    busy.value = false
  }
}
onMounted(loadProxies)
onUnmounted(() => {
  resetLease()
})
</script>
<template>
  <BaseDialog
    show
    :title="title"
    :close-on-escape="!busy"
    :show-close-button="!busy"
    width="wide"
    content-class="!max-w-2xl"
    @close="emit('close')"
  >
    <form :id="formId" class="space-y-4" @submit.prevent="submit">
      <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
      <p class="text-sm text-gray-500">{{ t('vendor.accountFormDescription') }}</p>
      <fieldset :disabled="!!account || busy || !!authorizedId">
        <legend class="mb-2 text-sm font-medium">{{ t('vendor.platform') }}</legend>
        <div class="flex gap-2 rounded-xl bg-gray-100 p-1 dark:bg-dark-700">
          <label
            v-for="platform in vendor.platforms"
            :key="platform"
            class="flex flex-1 cursor-pointer items-center justify-center gap-2 rounded-lg px-3 py-2 text-sm"
            :class="
              form.platform === platform
                ? platformAccountTabClass(platform)
                : 'text-gray-500'
            "
            ><input
              v-model="form.platform"
              type="radio"
              :value="platform"
              :aria-label="platformDisplayName(platform)"
              class="sr-only peer"
            /><PlatformIcon :platform="platform" size="sm" /><span
              class="peer-focus-visible:ring-2 peer-focus-visible:ring-current"
              >{{ platformDisplayName(platform) }}</span
            ></label
          >
        </div>
      </fieldset>
      <fieldset v-if="form.platform === 'openai' && !editing" :disabled="busy || !!authorizedId" data-testid="account-authorization-mode">
        <legend class="mb-2 text-sm font-medium">{{ t('vendor.authorization.mode') }}</legend>
        <div class="grid grid-cols-2 gap-2">
          <label v-for="mode in authorizationModes" :key="mode" class="cursor-pointer rounded-xl border p-3 text-sm"
            :class="form.authorization_mode === mode ? platformAccountSelectionClass(form.platform) : 'border-gray-200 dark:border-dark-600'">
            <span class="flex items-center gap-2 font-medium"><input v-model="form.authorization_mode" type="radio" :value="mode" :aria-label="t('vendor.authorization.' + mode)" />{{ t('vendor.authorization.' + mode) }}</span>
            <span class="mt-1 block text-xs text-gray-500 dark:text-dark-400">{{ t('vendor.authorization.' + mode + 'Help') }}</span>
          </label>
        </div>
        <p v-if="!authorizationModes.length" role="alert" class="mt-2 text-sm text-amber-700 dark:text-amber-400">{{ t('vendor.authorization.unavailable') }}</p>
        <p v-if="account" class="mt-2 text-xs text-gray-500">{{ t('vendor.authorization.sameAccountHelp', { id: account.id }) }}</p>
      </fieldset>
      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label :for="formId + '-name'" class="input-label">{{ t('vendor.accountName') }}</label>
          <input :id="formId + '-name'" v-model="form.name" data-testid="account-name" :required="editing" :disabled="busy"
            :placeholder="t(editing ? 'admin.accounts.enterAccountName' : 'vendor.accountNamePlaceholder')"
            :aria-describedby="formId + '-name-help'" maxlength="100" class="input"
          />
          <p :id="formId + '-name-help'" class="input-hint">{{ t(account ? 'vendor.accountNameEditHelp' : 'vendor.accountNameHelp') }}</p>
        </div>
        <div>
          <label :for="formId + '-notes'" class="input-label">{{ t('vendor.notes') }}</label>
          <textarea
            :id="formId + '-notes'"
            v-model="form.vendor_notes"
            :disabled="busy"
            :aria-describedby="formId + '-notes-help'"
            rows="1"
            maxlength="2000"
            :placeholder="t('vendor.accountNotesPlaceholder')"
            class="input"
          />
          <p :id="formId + '-notes-help'" class="input-hint">{{ t('vendor.accountNotesHelp') }}</p>
        </div>
      </div>
      <div class="flex flex-wrap items-end gap-2">
        <label class="min-w-0 basis-full sm:flex-1 sm:basis-auto"
          >{{ t('vendor.proxy')
          }}<Select
            :model-value="form.proxy_id"
            :options="[
              { value: '', label: t('vendor.select'), disabled: true },
              ...(vendor.allow_direct ? [{ value: 'direct', label: t('vendor.direct') }] : []),
              ...proxies
                .filter((p) => proxyAvailable(p) || p.id === account?.proxy_id)
                .map((p) => ({
                  value: String(p.id),
                  label:
                    p.name +
                    ' · ' +
                    p.protocol.toUpperCase() +
                    ' · ' +
                    (p.endpoint || p.protocol + '://' + p.host + ':' + p.port)
                }))
            ]"
            :aria-label="t('vendor.proxy')"
            class="mt-1 min-w-0"
            :disabled="busy || proxyLoading"
            :loading="proxyLoading"
            @update:model-value="changeProxy(String($event))" /></label
        ><button
          class="btn btn-secondary"
          type="button"
          :disabled="busy"
          @click="quickProxy = true"
        >
          {{ t('vendor.quickProxy') }}
        </button>
      </div>
      <fieldset :disabled="busy">
        <details class="rounded-xl border border-gray-200 p-3 dark:border-dark-600">
          <summary class="cursor-pointer text-sm">
            <span class="font-medium">{{ t('vendor.groups') }}</span
            ><span class="ml-3 inline-flex flex-wrap gap-1 align-middle">
              <GroupBadge v-for="group in permittedGroups.filter(group => form.group_ids.includes(group.id))"
                :key="group.id" :name="group.name" :platform="group.platform || form.platform" :show-rate="false" />
              <span v-if="!form.group_ids.length" class="text-gray-500">{{ t('vendor.select') }}</span>
            </span>
          </summary>
          <GroupSelector class="mt-3" v-model="form.group_ids" :groups="permittedGroups" />
        </details>
      </fieldset>
      <p class="text-xs text-gray-500">{{ t('vendor.oauthPreserved') }}</p>
      <fieldset v-if="!editing" :disabled="busy || !!authorizedId">
        <legend class="mb-2 text-sm font-medium">{{ t('vendor.method') }}</legend>
        <div class="flex flex-wrap gap-1 rounded-xl bg-gray-100 p-1 dark:bg-dark-700">
          <label
            v-for="method in methods"
            :key="method.value"
            class="flex-1 cursor-pointer whitespace-nowrap rounded-lg px-3 py-2 text-center text-sm"
            :class="
              form.method === method.value
                ? platformAccountTabClass(form.platform)
                : 'text-gray-500'
            "
            ><input
              v-model="form.method"
              type="radio"
              :value="method.value"
              :aria-label="method.label"
              class="sr-only peer"
            /><span class="peer-focus-visible:ring-2 peer-focus-visible:ring-current">{{
              method.label
            }}</span></label
          >
        </div>
      </fieldset>
      <div
        v-if="!editing && !manual && !lease && !authorizedId"
        class="space-y-3 rounded-xl border border-gray-200 bg-gray-50 p-4 dark:border-dark-600 dark:bg-dark-900"
      >
        <p class="text-sm text-gray-500">{{ t('vendor.authorizationHelp') }}</p>
        <p class="text-sm font-medium">{{ t('vendor.authorizationLink') }}</p>
        <button
          type="button"
          class="btn btn-secondary w-full"
          :disabled="busy || !valid"
          @click="submit"
        >
          <Icon name="link" size="sm" />{{ t('vendor.startOAuth') }}
        </button>
        <label class="block text-sm font-medium"
          >{{ t('vendor.callback') }}<textarea disabled class="input mt-2" rows="3" />
        </label>
      </div>
      <label v-if="manual" class="block"
        >{{ form.platform === 'openai' ? t('vendor.authorization.refreshTokenFor', { mode: t('vendor.authorization.' + form.authorization_mode) }) : t('vendor.token')
        }}<textarea
          v-model="token"
          required
          autocomplete="off"
          spellcheck="false"
          class="input mt-1"
        />
        <span v-if="form.platform === 'openai'" class="mt-1 block text-xs text-gray-500">{{ t('vendor.authorization.tokenHelp') }}</span>
      </label>
      <div
        v-if="lease && !authorizedId"
        class="space-y-3 rounded-xl bg-primary-50 p-4 dark:bg-dark-700"
      >
        <a
          :href="lease.auth_url"
          target="_blank"
          rel="noopener noreferrer"
          class="font-semibold text-primary-600 underline"
          >{{ t('vendor.openOAuth') }}</a
        >
        <div class="flex gap-2">
          <input
            class="input flex-1"
            readonly
            :value="lease.auth_url"
            :aria-label="t('vendor.openOAuth')"
          /><button
            type="button"
            class="btn btn-secondary"
            @click="copyToClipboard(lease.auth_url)"
          >
            {{ t('vendor.copy') }}
          </button>
        </div>
        <p class="text-xs">
          {{ t('vendor.leaseExpires') }}:
          {{ formatDateTime(lease.expires_at) || t('vendor.unknown') }}
        </p>
        <label class="block"
          >{{ t('vendor.callback')
          }}<textarea
            v-model="code"
            required
            spellcheck="false"
            autocomplete="off"
            class="input mt-1"
          /></label
        ><button type="button" class="btn btn-secondary" :disabled="busy" @click="resetLease">
          {{ t('vendor.restartOAuth') }}
        </button>
      </div>
      <p v-if="authorizedId" role="status" class="text-sm text-amber-700">
        {{ t('vendor.authorizationSaved') }}
      </p>
      <p class="text-xs text-gray-500">
        {{ t('vendor.policyReadOnly') }} {{ t('vendor.concurrency') }}:
        {{ account?.concurrency ?? vendor.account_defaults.concurrency }} · {{ t('vendor.priority') }}:
        {{ account?.priority ?? vendor.account_defaults.priority }}
      </p>
    </form>
    <template #footer>
      <div class="flex justify-end gap-2">
        <button type="button" class="btn btn-secondary" :disabled="busy" @click="emit('close')">
          {{ t('vendor.cancel') }}</button
        ><button
          type="submit"
          :form="formId"
          class="btn btn-primary"
          :disabled="
            busy ||
            (!editing && !manual && !lease && !authorizedId) ||
            !valid ||
            (manual && !token.trim()) ||
            (!!lease && !authorizedId && !code.trim())
          "
        >
          {{
            t(
              busy
                ? 'vendor.loading'
                : editing || manual || authorizedId
                  ? 'vendor.save'
                  : 'vendor.completeOAuth'
            )
          }}
        </button>
      </div>
    </template>
  </BaseDialog>
  <ProxyForm v-if="quickProxy" :scope="scope" @close="quickProxy = false" @saved="proxyCreated" />
</template>
