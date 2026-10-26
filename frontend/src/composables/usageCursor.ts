import type { UsageQueryParams, PaginatedResponse } from '@/types'

// Cursor history supports back/next navigation without rescanning skipped rows.
export function createUsageCursorPager() {
  let filterKey = ''
  const cursors = new Map<number, string>([[1, 'start']])
  return {
    prepare<T extends UsageQueryParams>(params: T): T {
      const { page = 1, cursor: _cursor, ...filters } = params
      const key = JSON.stringify(filters)
      if (key !== filterKey || page === 1) {
        filterKey = key
        cursors.clear()
        cursors.set(1, 'start')
      }
      const cursor = cursors.get(page)
      if (!cursor) throw new Error('Page has no usage cursor; refresh from the first page')
      return { ...params, cursor }
    },
    accept<T>(page: number, result: PaginatedResponse<T>) {
      if (result.next_cursor) cursors.set(page + 1, result.next_cursor)
      else cursors.delete(page + 1)
    }
  }
}
