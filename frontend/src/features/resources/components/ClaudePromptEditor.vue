<template>
  <section class="space-y-3" data-testid="claude-prompt-editor">
    <label class="block text-sm font-medium text-gray-700 dark:text-gray-300">
      {{ t(`${prefix}claudePromptPreset`) }}
      <select v-model="preset" class="input mt-1 w-full" data-testid="claude-prompt-preset" @change="choosePreset">
        <option v-for="value in presets" :key="value" :value="value">{{ t(`${prefix}claudePrompt${presetLabels[value]}`) }}</option>
      </select>
    </label>
    <p class="text-xs text-gray-500 dark:text-gray-400" data-testid="claude-prompt-hint">{{ t(`${prefix}claudePrompt${presetLabels[preset]}Hint`) }}</p>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}claudePromptBoundaryHint`) }}</p>

    <div v-for="(block, index) in blocks" :key="index" class="rounded-lg border border-gray-200 bg-gray-50 p-3 dark:border-dark-700 dark:bg-dark-800/60" data-testid="claude-prompt-block">
      <div class="mb-3 flex flex-wrap items-center gap-2">
        <label class="mr-auto flex items-center gap-2 text-sm font-medium">
          <input v-model="block.enabled" type="checkbox" @change="editBlocks" />
          {{ t(`${prefix}systemBlockTitle`, { index: index + 1 }) }}
        </label>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="index === 0" :aria-label="t(`${prefix}claudePromptMoveUp`)" @click="move(index, -1)">↑</button>
        <button type="button" class="btn btn-secondary btn-sm" :disabled="index === blocks.length - 1" :aria-label="t(`${prefix}claudePromptMoveDown`)" @click="move(index, 1)">↓</button>
        <button type="button" class="btn btn-secondary btn-sm" :aria-label="t('common.delete')" @click="remove(index)">{{ t('common.delete') }}</button>
      </div>
      <label class="block text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t(`${prefix}systemBlockPreset`) }}
        <select class="input mt-1 w-full" :value="variableValue(block)" @change="setVariable(index, ($event.target as HTMLSelectElement).value)">
          <option value="">{{ t(`${prefix}systemBlockPresetCustom`) }}</option>
          <option v-for="variable in claudePromptVariables" :key="variable" :value="variable">{{ variable }}</option>
        </select>
      </label>
      <label class="mt-3 block text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t(prefix + 'claudePromptPlacement') }}
        <select :value="block.placement ?? 'system'" class="input mt-1 w-full" @change="changePlacement(index, ($event.target as HTMLSelectElement).value)">
          <option value="system">{{ t(prefix + 'claudePromptPlacementSystem') }}</option>
          <option value="overlay_body">{{ t(prefix + 'claudePromptPlacementBody') }}</option>
          <option value="overlay_wrapper">{{ t(prefix + 'claudePromptPlacementWrapper') }}</option>
        </select>
      </label>
      <label v-if="block.placement === 'overlay_body'" class="mt-3 block text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t(prefix + 'claudePromptMatch') }}
        <input v-model="block.match" class="input mt-1 w-full font-mono text-xs" @input="editBlocks" />
      </label>
      <label class="mt-3 block text-xs font-medium text-gray-600 dark:text-gray-300">
        {{ t(`${prefix}systemBlockText`) }}
        <textarea v-model="block.text" rows="4" class="input mt-1 w-full resize-y font-mono text-xs leading-5" data-testid="claude-prompt-text" @input="editBlocks" />
      </label>
      <div class="mt-3 flex flex-wrap items-center gap-4 text-xs">
        <label class="flex items-center gap-2">
          <input type="checkbox" :checked="block.drop_if_empty !== false" @change="block.drop_if_empty = ($event.target as HTMLInputElement).checked; editBlocks()" />
          {{ t(`${prefix}claudePromptDropIfEmpty`) }}
        </label>
        <label class="flex items-center gap-2">
          <input type="checkbox" :checked="!!block.cache_control" :disabled="isCaller(block) || (!!block.placement && block.placement !== 'system')" @change="setCache(index, ($event.target as HTMLInputElement).checked)" />
          {{ t(`${prefix}systemBlockCacheControl`) }}
        </label>
        <select v-if="block.cache_control" class="input w-auto" :aria-label="t(`${prefix}systemBlockCacheControl`)" :value="cacheTTL(block)" @change="setTTL(index, ($event.target as HTMLSelectElement).value)">
          <option value="">{{ t(`${prefix}claudePromptDefaultTTL`) }} (5m)</option>
          <option value="5m">{{ t(`${prefix}cacheTTL5m`) }}</option>
          <option value="1h">{{ t(`${prefix}cacheTTL1h`) }}</option>
        </select>
      </div>
    </div>

    <div class="flex flex-wrap gap-2">
      <button type="button" class="btn btn-secondary btn-sm" data-testid="claude-prompt-add" @click="add">{{ t(`${prefix}addSystemBlock`) }}</button>
      <button type="button" class="btn btn-secondary btn-sm" data-testid="claude-prompt-reset" @click="reset">{{ t(`${prefix}resetSystemBlocks`) }}</button>
    </div>
    <p class="text-xs text-gray-500 dark:text-gray-400">{{ t(`${prefix}claudePromptVariablesHint`) }}</p>
    <div class="flex flex-wrap gap-1">
      <code v-for="variable in claudePromptVariables" :key="variable" class="rounded bg-gray-100 px-1 text-xs dark:bg-dark-800">{{ variable }}</code>
    </div>
    <p v-if="invalid" role="alert" class="text-sm text-red-600 dark:text-red-400" data-testid="claude-prompt-error">{{ t(`${prefix}claudePromptInvalid`) }}</p>
    <details>
      <summary class="cursor-pointer text-sm">{{ t(`${prefix}claudePromptJSON`) }}</summary>
      <textarea :value="modelValue" rows="10" class="input mt-2 w-full font-mono text-xs" :aria-label="t(`${prefix}claudePromptJSON`)" data-testid="claude-prompt-json" @input="editJSON(($event.target as HTMLTextAreaElement).value)" />
    </details>
    <details>
      <summary class="cursor-pointer text-sm">{{ t(`${prefix}claudePromptPreview`) }}</summary>
      <pre class="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-words rounded bg-gray-50 p-3 text-xs dark:bg-dark-800" data-testid="claude-prompt-preview">{{ preview }}</pre>
    </details>
  </section>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import {
  claudePromptPresetBlocks, claudePromptValidationError, claudePromptVariables,
  defaultClaudePromptBlocks, detectClaudePromptPreset, parseClaudePromptBlocks, serializeClaudePromptBlocks,
  type ClaudePromptBlock, type ClaudePromptPreset
} from '@/features/resources/model/claudePrompt'

const props = defineProps<{ modelValue: string; enabled: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: string]; 'update:enabled': [value: boolean] }>()
const { t } = useI18n()
const prefix = 'admin.settings.gatewayForwarding.'
const presets: ClaudePromptPreset[] = ['official', 'official_full', 'minimal', 'zero', 'custom']
const presetLabels = { official: 'Official', official_full: 'OfficialFull', minimal: 'Minimal', zero: 'Zero', custom: 'Custom' } as const
const preset = ref<ClaudePromptPreset>('custom')
const blocks = ref<ClaudePromptBlock[]>([])
const parseError = ref(false)
let lastEmitted: string | undefined
let customDraft: ClaudePromptBlock[] | undefined

watch(() => props.modelValue, raw => {
  if (raw === lastEmitted) return
  try {
    blocks.value = parseClaudePromptBlocks(raw).map(block => ({ ...block, enabled: block.enabled !== false }))
    preset.value = detectClaudePromptPreset(blocks.value)
    if (preset.value === 'custom') customDraft = JSON.parse(JSON.stringify(blocks.value)) as ClaudePromptBlock[]
    parseError.value = false
  } catch {
    blocks.value = []
    preset.value = 'custom'
    parseError.value = true
  }
}, { immediate: true })

const invalid = computed(() => parseError.value || !!claudePromptValidationError(blocks.value))
// This is the editable template, not a fabricated wire request or token estimate.
const preview = computed(() => invalid.value ? '' : JSON.stringify(
  props.enabled ? blocks.value.filter(block => block.enabled !== false) : [], null, 2
))

function publish() {
  parseError.value = false
  lastEmitted = serializeClaudePromptBlocks(blocks.value)
  emit('update:modelValue', lastEmitted)
}
function editBlocks() {
  preset.value = 'custom'
  customDraft = JSON.parse(JSON.stringify(blocks.value)) as ClaudePromptBlock[]
  publish()
}
function choosePreset() {
  blocks.value = preset.value === 'custom'
    ? customDraft ?? blocks.value
    : claudePromptPresetBlocks(preset.value)
  blocks.value = blocks.value.map(block => ({ ...block, enabled: block.enabled !== false }))
  emit('update:enabled', true)
  publish()
}
function reset() {
  blocks.value = preset.value === 'custom' ? defaultClaudePromptBlocks() : claudePromptPresetBlocks(preset.value)
  blocks.value = blocks.value.map(block => ({ ...block, enabled: block.enabled !== false }))
  if (preset.value === 'custom') customDraft = JSON.parse(JSON.stringify(blocks.value)) as ClaudePromptBlock[]
  publish()
}
function add() {
  blocks.value.push({ enabled: true, type: 'text', text: '', drop_if_empty: true })
  editBlocks()
}
function remove(index: number) { blocks.value.splice(index, 1); editBlocks() }
function move(index: number, offset: number) {
  const next = index + offset
  if (next < 0 || next >= blocks.value.length) return
  ;[blocks.value[index], blocks.value[next]] = [blocks.value[next], blocks.value[index]]
  editBlocks()
}
function isCaller(block: ClaudePromptBlock) { return block.text?.trim() === '{caller_system}' }
function variableValue(block: ClaudePromptBlock) {
  return claudePromptVariables.find(variable => variable === block.text) ?? ''
}
function setVariable(index: number, value: string) {
  if (!value) return
  blocks.value[index].text = value
  if (isCaller(blocks.value[index])) { delete blocks.value[index].cache_control; blocks.value[index].drop_if_empty = true }
  editBlocks()
}
function changePlacement(index: number, placement: string) {
  if (placement === 'system') delete blocks.value[index].placement
  else blocks.value[index].placement = placement as 'overlay_body' | 'overlay_wrapper'
  if (placement !== 'system') delete blocks.value[index].cache_control
  if (blocks.value[index].placement !== 'overlay_body') delete blocks.value[index].match
  editBlocks()
}
function setCache(index: number, enabled: boolean) {
  if (enabled) blocks.value[index].cache_control = { type: 'ephemeral', ttl: '5m' }
  else delete blocks.value[index].cache_control
  editBlocks()
}
function cacheTTL(block: ClaudePromptBlock) {
  return block.cache_control === true ? '5m' : typeof block.cache_control === 'object' ? block.cache_control?.ttl ?? '' : ''
}
function setTTL(index: number, ttl: string) {
  const cache = blocks.value[index].cache_control
  const updated: Exclude<ClaudePromptBlock['cache_control'], boolean | null | undefined> = { ...(typeof cache === 'object' ? cache : {}), type: 'ephemeral' }
  if (ttl === '5m' || ttl === '1h') updated.ttl = ttl
  else delete updated.ttl
  blocks.value[index].cache_control = updated
  editBlocks()
}
function editJSON(raw: string) {
  lastEmitted = undefined
  emit('update:modelValue', raw)
}
defineExpose({ validate: () => !invalid.value })
</script>
