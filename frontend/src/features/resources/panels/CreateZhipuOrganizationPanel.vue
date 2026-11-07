<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import HelpTooltip from '@/shared/ui/HelpTooltip.vue'
import { type UnwrapRef, type Ref } from 'vue'
import { usePanelField } from '../model/usePanelField'
import type { CreateResourceForm } from '../model/createEditorTypes'


const props = defineProps<{
  form: Pick<CreateResourceForm, "platform">
  accountMode: UnwrapRef<Ref<string, string>>
  zhipuOrganization: UnwrapRef<Ref<string, string>>
  zhipuProject: UnwrapRef<Ref<string, string>>
}>()

const emit = defineEmits<{
  'update:zhipuOrganization': [value: UnwrapRef<Ref<string, string>>]
  'update:zhipuProject': [value: UnwrapRef<Ref<string, string>>]
}>()

const { t } = useI18n()

const form = computed(() => props.form)

const accountMode = computed(() => props.accountMode)

const zhipuOrganization = usePanelField(() => props.zhipuOrganization, value => emit('update:zhipuOrganization', value))

const zhipuProject = usePanelField(() => props.zhipuProject, value => emit('update:zhipuProject', value))
</script>

<template>
<div v-if="form.platform === 'zhipu' && accountMode === 'coding'" class="mt-4">
        <div class="flex items-center">
          <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.title') }}</label>
          <HelpTooltip trigger="click" width-class="w-80">
            <p class="mb-1 font-medium">{{ t('admin.accounts.cnProviders.zhipuTeam.help.title') }}</p>
            <ol class="list-decimal space-y-1 pl-4">
              <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step1') }}</li>
              <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step2') }}</li>
              <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step3') }}</li>
              <li>{{ t('admin.accounts.cnProviders.zhipuTeam.help.step4') }}</li>
            </ol>
            <p class="mt-2 break-all rounded bg-black/20 p-1.5 font-mono text-[11px] leading-relaxed">
              {{ t('admin.accounts.cnProviders.zhipuTeam.help.example') }}
            </p>
          </HelpTooltip>
        </div>
        <div class="mt-2 grid gap-4 sm:grid-cols-2">
          <div>
            <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.organization') }}</label>
            <input v-model="zhipuOrganization" type="text" class="input" :placeholder="t('admin.accounts.cnProviders.zhipuTeam.organizationPlaceholder')" />
          </div>
          <div>
            <label class="input-label">{{ t('admin.accounts.cnProviders.zhipuTeam.project') }}</label>
            <input v-model="zhipuProject" type="text" class="input" :placeholder="t('admin.accounts.cnProviders.zhipuTeam.projectPlaceholder')" />
          </div>
        </div>
        <p class="input-hint mt-2">{{ t('admin.accounts.cnProviders.zhipuTeam.hint') }}</p>
      </div>
</template>
