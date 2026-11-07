import type { Virtualizer } from '@tanstack/vue-virtual'

/** Presentation-only table contracts; business rows do not need an index signature. */
export type TableRowKey = string | number
export type TableSortOrder = 'asc' | 'desc'

export interface Column<TRow extends object = object> {
  /** May also name a display-only column, such as actions or a computed metric. */
  key: string
  label: string
  sortable?: boolean
  class?: string
  formatter?: (value: unknown, row: TRow) => string
}

export interface TableCellSlot<TRow extends object, TValue = unknown> {
  row: TRow
  /** Dynamic/display-only column keys do not assert a business field's value type. */
  value: TValue
  expanded: boolean
}

export interface TableHeaderSlot<TRow extends object> {
  column: Column<TRow>
  sortKey: string
  sortOrder: TableSortOrder
}

export type TableSlots<TRow extends object> = {
  [Name in `cell-${string}`]?: (props: TableCellSlot<TRow>) => unknown
} & {
  [Name in `header-${string}`]?: (props: TableHeaderSlot<TRow>) => unknown
} & {
  [Key in Extract<keyof TRow, string> as `cell-${Key}`]?: (props: TableCellSlot<TRow, TRow[Key]>) => unknown
} & {
  empty?: () => unknown
}

/** Vue unwraps these exposed refs on a component template ref. */
export interface DataTableHandle<TRow extends object> {
  readonly virtualizer: Virtualizer<HTMLElement, Element>
  readonly shouldVirtualize: boolean
  readonly sortedData: readonly TRow[]
  readonly resolveRowKey: (row: TRow, index: number) => TableRowKey
  readonly tableWrapperEl: HTMLElement | null
}
