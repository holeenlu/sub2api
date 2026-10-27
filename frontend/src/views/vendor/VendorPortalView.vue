<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import VendorLayout from '@/features/vendor/components/VendorLayout.vue'
import AccountWorkbench from '@/features/vendor/components/AccountWorkbench.vue'
import ProxyWorkbench from '@/features/vendor/components/ProxyWorkbench.vue'
import UsageWorkbench from '@/features/vendor/components/UsageWorkbench.vue'
import SettlementWorkbench from '@/features/vendor/components/SettlementWorkbench.vue'
import VendorAccountSettings from '@/features/vendor/components/VendorAccountSettings.vue'
import VendorManagement from '@/features/vendor/components/VendorManagement.vue'
import VendorChildrenView from '@/features/vendor/components/VendorChildrenView.vue'
import VendorsView from '@/views/admin/VendorsView.vue'
import { useVendorStore } from '@/features/vendor/store'
const route = useRoute(), router = useRouter(), auth = useAuthStore(), store = useVendorStore()
const tab = computed(() => String(route.params.tab || 'accounts'))
function openChild(target: 'accounts' | 'proxies' | 'usage' | 'settlements', id: number) {
  router.push({ path: '/vendor/' + (target === 'settlements' ? 'payables' : 'child-' + target), query: { vendor_id: id } })
}
</script>
<template>
  <VendorLayout>
    <VendorAccountSettings v-if="tab === 'profile'" />
    <VendorsView v-else-if="auth.isSuperAdmin && tab === 'platform'" embedded />
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
