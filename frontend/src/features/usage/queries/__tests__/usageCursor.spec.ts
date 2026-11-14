import { describe, expect, it } from 'vitest'
import { createUsageCursorPager } from '@/features/usage/queries/usageCursor'
describe('usage cursor history', () => {
  it('uses returned cursors and resets on filter changes', () => {
    const pager = createUsageCursorPager()
    expect(pager.prepare({ page: 1, page_size: 20 }).cursor).toBe('start')
    pager.accept(1, { items: [], total: 21, page: 1, page_size: 20, pages: 2, next_cursor: 'next' })
    expect(pager.prepare({ page: 2, page_size: 20 }).cursor).toBe('next')
    expect(() => pager.prepare({ page: 3, page_size: 20 })).toThrow()
    expect(pager.prepare({ page: 1, page_size: 20, model: 'new' }).cursor).toBe('start')
    expect(() => pager.prepare({ page: 2, page_size: 20, model: 'new' })).toThrow()
  })
})
