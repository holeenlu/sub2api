<script setup lang="ts">
import { computed, defineAsyncComponent } from 'vue'
import { resolveChannelMonitorMode } from '../mode'
import { useOperationsServices } from '@/features/operations/services'
const services = useOperationsServices()
const ChannelStatusV1Page = defineAsyncComponent(() => import('./ChannelStatusV1Page.vue'))
const ChannelStatusV2Page = defineAsyncComponent(() => import('./ChannelStatusV2Page.vue'))
const isV1 = computed(() => services.publicSettings.value?.channel_monitor_enabled !== false && resolveChannelMonitorMode(services.publicSettings.value) === 'v1')
</script>
<template>
  <ChannelStatusV1Page v-if="isV1" />
  <ChannelStatusV2Page v-else />
</template>
