<template>
  <div v-if="availability !== 'available'" class="min-h-[240px] bg-gray-50 dark:bg-dark-950">
    <PlazaNavBar v-if="!presentation.embedded" />
    <div class="mx-auto max-w-7xl space-y-3 p-6 text-sm text-gray-500" role="status">
      <p>{{ t(availability === 'disabled' ? 'modelPlaza.catalog.disabled' : availability === 'authentication_required' ? 'modelPlaza.catalog.loginRequired' : 'modelPlaza.catalog.settingsUnavailable') }}</p>
      <RouterLink v-if="availability === 'authentication_required'" :to="presentation.loginTarget" class="text-primary-600 hover:underline">{{ t('home.login') }}</RouterLink>
      <button v-else-if="availability === 'unknown'" type="button" class="text-primary-600 hover:underline" @click="reload">{{ t('common.tryAgain') }}</button>
    </div>
  </div>
  <ModelPlazaContent v-else-if="presentation.embedded" :response="data" :loading="loading" :error="loadFailed" embedded />
  <div v-else class="min-h-screen bg-gray-50 dark:bg-dark-950">
    <PlazaNavBar />
    <main class="mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8 lg:py-8">
      <ModelPlazaContent :response="data" :loading="loading" :error="loadFailed" />
    </main>
  </div>
</template>
<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { RouterLink } from 'vue-router'
import PlazaNavBar from '../components/PlazaNavBar.vue'
import ModelPlazaContent from '../components/ModelPlazaContent.vue'
import { catalogPresentationKey, requireCatalogPort } from '../ports'
import { useModelPlazaQuery } from '../queries/modelPlaza'
const presentation = requireCatalogPort(catalogPresentationKey)
const { t } = useI18n()
const { data, loading, loadFailed, availability, reload } = useModelPlazaQuery()
</script>
