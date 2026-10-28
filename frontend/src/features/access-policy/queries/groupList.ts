import { ref } from 'vue';
import { groupsAPI } from '../api/groups';
import type { AdminGroup } from '@/features/access-policy/api/groupTypes';
import { useAccessPolicyRequest } from './requestScope';
export function useGroupListQuery() {
 const groups = ref<AdminGroup[]>([])
 const loading = ref(false)
 const total = ref(0)
 const pages = ref(0)
 const error = ref<unknown>(null)
 const request = useAccessPolicyRequest(() => { groups.value = []; loading.value = false; total.value = 0; pages.value = 0; error.value = null })
 async function load(page: number, pageSize: number, filters: Parameters<typeof groupsAPI.list>[2]) {
  const pending = request.begin(); loading.value = true; error.value = null
  try {
   const result = await groupsAPI.list(page, pageSize, filters, { signal: pending.signal })
   if (!pending.isCurrent()) return
   groups.value = result.items; total.value = result.total; pages.value = result.pages
   return result
  } catch (failure: unknown) {
   if (pending.isCurrent()) error.value = failure
  } finally { if (pending.isCurrent()) loading.value = false }
 }
 return { groups, loading, total, pages, error, load, scope: request.scope, clear: request.cancel }
}
