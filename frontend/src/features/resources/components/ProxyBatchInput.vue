<script lang="ts">
import type { ProxyProtocol } from '@/features/resources/api/proxyTypes';
// Parse proxy URL: protocol://user:pass@host:port or protocol://host:port
// Host may be a domain, IPv4, or bracketed IPv6 ([2001:db8::1]).
export const parseProxyUrl = (
  line: string
): {
  protocol: ProxyProtocol
  host: string
  port: number
  username: string
  password: string
} | null => {
  const trimmed = line.trim()
  if (!trimmed) return null

  // Regex to parse proxy URL (supports http, https, socks5, socks5h).
  // Host alternatives: [bracketed-IPv6] | hostname/IPv4 (colon-free, so the
  // match stops before the final :port).
  const regex =
    /^(https?|socks5h?):\/\/(?:([^:@\[\]]+):([^@\[\]]+)@)?(\[[0-9a-f:.]+\]|[^:\[\]]+):(\d+)$/i
  const match = trimmed.match(regex)

  if (!match) return null

  const [, protocol, username, password, rawHost, port] = match
  const portNum = parseInt(port, 10)

  if (portNum < 1 || portNum > 65535) return null

  // Strip brackets from IPv6 literals; the backend re-brackets via net.JoinHostPort.
  const host = rawHost.replace(/^\[|\]$/g, '').trim()

  return {
    protocol: protocol.toLowerCase() as ProxyProtocol,
    host,
    port: portNum,
    username: username || '',
    password: password || ''
  }
}

export const parseProxyBatch = (input: string) => {
  const lines = input.split('\n').filter((l) => l.trim())
  const seen = new Set<string>()
  const proxies: NonNullable<ReturnType<typeof parseProxyUrl>>[] = []
  let invalid = 0
  let duplicate = 0

  for (const line of lines) {
    const parsed = parseProxyUrl(line)
    if (!parsed) {
      invalid++
      continue
    }

    // Check for duplicates (same host:port:username:password)
    const key = JSON.stringify([parsed.host, parsed.port, parsed.username, parsed.password])
    if (seen.has(key)) {
      duplicate++
      continue
    }
    seen.add(key)
    proxies.push(parsed)
  }

  return { total: lines.length, valid: proxies.length, invalid, duplicate, proxies }
}
export type ProxyBatchParseResult = ReturnType<typeof parseProxyBatch>
</script>
<script setup lang="ts">
import { computed, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/shared/ui/icons/Icon.vue'
defineProps<{ disabled?: boolean }>()
const input = defineModel<string>({ required: true })
const emit = defineEmits<{ parsed: [result: ProxyBatchParseResult] }>()
const { t } = useI18n()
const batchParseResult = computed(() => parseProxyBatch(input.value))
watch(batchParseResult, result => emit('parsed', result), { immediate: true, flush: 'sync' })
</script>
<template>
  <div class="space-y-5">
    <div>
      <label class="input-label">{{ t('admin.proxies.batchInput') }}</label>
      <textarea
        v-model="input"
        rows="10"
        class="input font-mono text-sm"
        :placeholder="t('admin.proxies.batchInputPlaceholder')"
        :disabled="disabled"
        :aria-label="t('admin.proxies.batchInput')"
        autocomplete="off"
        spellcheck="false"
      ></textarea>
      <p class="input-hint mt-2">
        {{ t('admin.proxies.batchInputHint') }}
      </p>
    </div>
    <!-- Parse Result -->
    <div v-if="batchParseResult.total > 0" class="rounded-lg bg-gray-50 p-4 dark:bg-dark-700">
        <div class="flex items-center gap-4 text-sm">
          <div class="flex items-center gap-1.5">
          <Icon name="checkCircle" size="sm" :stroke-width="2" class="text-primary-500" />
          <span class="text-gray-700 dark:text-gray-300">
            {{ t('admin.proxies.parsedCount', { count: batchParseResult.valid }) }}
          </span>
        </div>
        <div v-if="batchParseResult.invalid > 0" class="flex items-center gap-1.5">
          <Icon
            name="exclamationCircle"
            size="sm"
            :stroke-width="2"
            class="text-amber-500"
          />
          <span class="text-amber-600 dark:text-amber-400">
            {{ t('admin.proxies.invalidCount', { count: batchParseResult.invalid }) }}
          </span>
        </div>
        <div v-if="batchParseResult.duplicate > 0" class="flex items-center gap-1.5">
          <Icon name="copy" size="sm" class="text-gray-400" />
          <span class="text-gray-500 dark:text-gray-400">
            {{ t('admin.proxies.duplicateCount', { count: batchParseResult.duplicate }) }}
          </span>
        </div>
      </div>
    </div>
  </div>
</template>
