<script setup lang="ts">
import { computed, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAuthStore } from '@/stores/auth'
import { vendorAPI } from '../api'
import { errorText } from '../helpers'
import type { IdentityReconciliationRow } from '../types'
const auth = useAuthStore(),
  { t } = useI18n()
const afterId = ref(0),
  limit = ref(10),
  busy = ref(false),
  error = ref(''),
  done = ref(false)
const rows = ref<IdentityReconciliationRow[]>([])
const failures = computed(
  () => rows.value.filter((row) => row.status !== 'claimed').length,
)
let active = true
async function run(cursor = afterId.value, size = limit.value, advance = true) {
  if (!auth.isSuperAdmin || busy.value) return
  if (
    !Number.isSafeInteger(cursor) ||
    cursor < 0 ||
    !Number.isInteger(size) ||
    size < 1 ||
    size > 25
  ) {
    error.value = t('vendor.reconcileBounds')
    return
  }
  busy.value = true
  error.value = ''
  try {
    const response = await vendorAPI.reconcileIdentities({
      after_id: cursor,
      limit: size,
    })
    if (!active || !auth.isSuperAdmin) return
    const results = new Map(rows.value.map((row) => [row.account_id, row]))
    for (const row of response.rows) results.set(row.account_id, row)
    rows.value = [...results.values()].sort(
      (a, b) => a.account_id - b.account_id,
    )
    if (advance) {
      afterId.value = response.next_after_id
      done.value = response.done
    }
  } catch (e) {
    if (active && auth.isSuperAdmin) error.value = errorText(e)
  } finally {
    busy.value = false
  }
}
onUnmounted(() => {
  active = false
})
</script>
<template>
  <section
    v-if="auth.isSuperAdmin"
    class="space-y-4 rounded-xl border border-amber-200 bg-amber-50/40 p-5 dark:border-amber-900 dark:bg-dark-800"
  >
    <h2 class="font-semibold">{{ t('vendor.reconcileTitle') }}</h2>
    <p class="text-sm text-gray-600 dark:text-gray-300">
      {{ t('vendor.reconcileHelp') }}
    </p>
    <form class="flex flex-wrap items-end gap-3" @submit.prevent="run()">
      <label
        >{{ t('vendor.reconcileCursor')
        }}<input
          v-model.number="afterId"
          class="input mt-1"
          type="number"
          min="0"
          step="1"
          required
          :disabled="busy"
      /></label>
      <label
        >{{ t('vendor.reconcileLimit')
        }}<input
          v-model.number="limit"
          class="input mt-1"
          type="number"
          min="1"
          max="25"
          step="1"
          required
          :disabled="busy"
      /></label>
      <button class="btn btn-primary" :disabled="busy">
        {{ t(busy ? 'vendor.loading' : 'vendor.reconcileRun') }}
      </button>
      <button
        type="button"
        class="btn btn-secondary"
        :disabled="busy"
        @click="
          afterId = 0
          done = false;
        "
      >
        {{ t('vendor.reconcileReset') }}
      </button>
    </form>
    <p v-if="busy" role="status" class="text-sm">
      {{ t('vendor.reconcileRunning') }}
    </p>
    <p v-if="error" role="alert" class="text-red-600">{{ error }}</p>
    <p v-if="done" role="status" class="text-sm">
      {{ t('vendor.reconcileDone') }}
    </p>
    <p v-if="failures" class="text-sm text-amber-700 dark:text-amber-300">
      {{ t('vendor.reconcileFailures', { count: failures }) }}
    </p>
    <div v-if="rows.length" class="max-h-96 overflow-auto rounded-lg border">
      <table class="w-full text-left text-sm">
        <thead>
          <tr>
            <th class="p-3">{{ t('vendor.account') }}</th>
            <th class="p-3">{{ t('vendor.status') }}</th>
            <th class="p-3">{{ t('vendor.results') }}</th>
            <th class="p-3">{{ t('vendor.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="row in rows" :key="row.account_id" class="border-t">
            <td class="p-3">#{{ row.account_id }}</td>
            <td class="p-3">
              {{
                t(
                  row.status === 'claimed'
                    ? 'vendor.reconcileClaimed'
                    : 'vendor.failed',
                )
              }}
            </td>
            <td class="p-3">{{ row.message || '—' }}</td>
            <td class="p-3">
              <button
                v-if="row.status !== 'claimed'"
                class="btn btn-secondary btn-sm"
                :disabled="busy"
                @click="run(row.account_id - 1, 1, false)"
              >
                {{ t('vendor.retry') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </section>
</template>
