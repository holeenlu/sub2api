<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/features/identity'
import VendorLayout from '@/app/compositions/vendor-portal/components/VendorLayout.vue'
import AccountWorkbench from '@/app/compositions/vendor-portal/components/AccountWorkbench.vue'
import ProxyWorkbench from '@/app/compositions/vendor-portal/components/ProxyWorkbench.vue'
import UsageWorkbench from '@/app/compositions/vendor-portal/components/UsageWorkbench.vue'
import { SettlementWorkbench } from '@/features/supply/panels'
import VendorAccountSettings from '@/app/compositions/vendor-portal/components/VendorAccountSettings.vue'
import { VendorManagement } from '@/features/supply/panels'
import VendorChildrenView from '@/app/compositions/vendor-portal/components/VendorChildrenView.vue'
import VendorPlatformWorkspace from './VendorPlatformWorkspace.vue'
import { useSupplyStore } from '@/features/supply'
import { useSupplyServices } from '@/features/supply/services'
const supplyServices = useSupplyServices()
const portalScopeKey = computed(() => supplyServices.scopeKey.value)
const route = useRoute(), router = useRouter(), auth = useAuthStore(), store = useSupplyStore()
const tab = computed(() => String(route.params.tab || 'accounts'))
function openChild(target: 'accounts' | 'proxies' | 'usage' | 'settlements', id: number) {
  router.push({ path: '/vendor/' + (target === 'settlements' ? 'payables' : 'child-' + target), query: { vendor_id: id } })
}
</script>
<template>
  <VendorLayout :key="portalScopeKey ?? 'signed-out'">
    <VendorAccountSettings v-if="tab === 'profile'" />
    <VendorPlatformWorkspace v-else-if="auth.isSuperAdmin && tab === 'platform'" />
    <template v-else-if="store.vendor">
      <AccountWorkbench v-if="tab === 'accounts'" :scope="{ admin: false }" :vendor="store.vendor" />
      <ProxyWorkbench v-else-if="tab === 'proxies'" :scope="{ admin: false }" />
      <UsageWorkbench v-else-if="tab === 'usage'" :vendor-id="store.vendor.id" :groups="store.vendor.groups" />
      <SettlementWorkbench v-else-if="tab === 'settlements'" />
      <template v-else-if="auth.user?.role === 'vendor_level1'">
        <VendorManagement v-if="tab === 'children'" :parent="store.vendor" @navigate="openChild" />
        <VendorChildrenView v-else-if="tab === 'child-accounts' || tab === 'child-proxies' || tab === 'child-usage'" :parent="store.vendor" :kind="tab === 'child-accounts' ? 'accounts' : tab === 'child-proxies' ? 'proxies' : 'usage'" />
        <SettlementWorkbench v-else-if="tab === 'payables'" payer :vendor-id="Number(route.query.vendor_id) || undefined" />
      </template>
    </template>
  </VendorLayout>
</template>
