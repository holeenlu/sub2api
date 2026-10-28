<script setup lang="ts">
import DataTable from '../../DataTable.vue'
import type { Column, TableRowKey, TableSortOrder } from '../../tableTypes'

interface Row { id: number; name: string; amount: number | null }
const rows: readonly Row[] = [{ id: 7, name: 'Seven', amount: null }]
const columns: Column<Row>[] = [
  { key: 'name', label: 'Name', formatter: (_value, row) => row.name },
  { key: 'amount', label: 'Amount' },
  { key: 'summary', label: 'Computed summary' }
]
const text = (value: string): string => value
const number = (value: number): number => value
const nullableNumber = (value: number | null): number | null => value
const rowClick = (row: Row): string => row.name
const wrongRow = (row: { unrelated: boolean }): boolean => row.unrelated
const selectKeys = (keys: TableRowKey[]): number => keys.length
const sortOrder = (order: TableSortOrder): TableSortOrder => order
</script>

<template>
  <DataTable :data="rows" :columns="columns" row-key="id" :selection-label="row => row.name"
             @row-click="rowClick" @selection-change="selectKeys">
    <template #cell-name="{ row, value, expanded }">
      {{ text(row.name) }} {{ text(value) }} {{ expanded ? 'open' : 'closed' }}
      <!-- @vue-expect-error known string field cannot become a numeric slot value -->
      {{ number(value) }}
      <!-- @vue-expect-error generic rows expose only their actual fields -->
      {{ row.unrelated }}
    </template>
    <template #cell-amount="{ value }">
      {{ nullableNumber(value) }}
      <!-- @vue-expect-error nullable fields must retain their nullability -->
      {{ number(value) }}
    </template>
    <template #cell-summary="{ row, value }">
      {{ text(row.name) }} {{ value }}
      <!-- @vue-expect-error display-only columns have no asserted field type -->
      {{ text(value) }}
    </template>
    <template #header-name="{ column, sortOrder: order }">
      {{ text(column.label) }} {{ sortOrder(order) }}
    </template>
  </DataTable>
  <!-- @vue-expect-error row events carry Row, not an unrelated consumer shape -->
  <DataTable :data="rows" :columns="columns" @row-click="wrongRow" />
  <!-- @vue-expect-error field row keys must exist on the supplied row type -->
  <DataTable :data="rows" :columns="columns" row-key="unrelated" />
  <!-- @vue-expect-error callback row keys receive the actual row type -->
  <DataTable :data="rows" :columns="columns" :row-key="row => row.unrelated" />
</template>
