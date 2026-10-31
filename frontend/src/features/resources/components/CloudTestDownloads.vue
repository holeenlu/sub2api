<template>
  <div v-if="result" class="flex flex-wrap gap-2" data-testid="cloud-test-downloads">
    <button type="button" class="btn btn-secondary" data-testid="cloud-download-result" @click="download('result.json', JSON.stringify(result, null, 2), 'application/json')">{{ t('admin.accounts.openai.cloud.downloadResult') }}</button>
    <button v-if="answer" type="button" class="btn btn-secondary" data-testid="cloud-download-answer" @click="download('answer.txt', answer, 'text/plain')">{{ t('admin.accounts.openai.cloud.downloadAnswer') }}</button>
  </div>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount } from 'vue'
import { useI18n } from 'vue-i18n'
import { cloudResultAnswer, type CloudTestResult } from '@/features/resources/model/openaiCloud'
const props = defineProps<{ result: CloudTestResult | null; successful: boolean }>()
const { t } = useI18n()
const answer = computed(() => props.successful ? cloudResultAnswer(props.result) : '')
const urls = new Set<string>()
function download(name: string, content: string, type: string) {
  const url = URL.createObjectURL(new Blob([content], { type: `${type};charset=utf-8` }))
  urls.add(url)
  const link = document.createElement('a')
  link.href = url; link.download = name
  document.body.appendChild(link); link.click(); link.remove()
  setTimeout(() => { if (urls.delete(url)) URL.revokeObjectURL(url) }, 1000)
}
onBeforeUnmount(() => { for (const url of urls) URL.revokeObjectURL(url); urls.clear() })
</script>
