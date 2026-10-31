import { mount } from '@vue/test-utils'
import { Virtualizer } from '@tanstack/vue-virtual'
import { defineComponent, h, ref } from 'vue'
import { afterEach, describe, it, expect, expectTypeOf, vi } from 'vitest'

import { findRowIndexByDomPosition, useSwipeSelect, type SwipeSelectVirtualContext } from '@/shared/lib/useSwipeSelect'

/**
 * Build a scroll element whose `tbody tr[data-index]` rows expose stubbed
 * vertical rects, so we can exercise findRowIndexByDomPosition without a real DOM.
 * `index` is the value placed in the row's data-index attribute (its absolute
 * position in the sorted data), which need not equal the array position.
 */
function makeScrollEl(rows: Array<{ index: number; top: number; bottom: number }>): Element {
  const table = document.createElement('table')
  const body = table.createTBody()
  for (const row of rows) {
    const tr = body.insertRow()
    tr.dataset.index = String(row.index)
    tr.getBoundingClientRect = () => new DOMRect(0, row.top, 100, row.bottom - row.top)
  }
  return table
}

describe('findRowIndexByDomPosition (swipe-select full-render fallback)', () => {
  // Variable row heights on purpose — the third row is taller.
  const rows = [
    { index: 0, top: 100, bottom: 200 },
    { index: 1, top: 200, bottom: 300 },
    { index: 2, top: 300, bottom: 450 }
  ]
  const el = makeScrollEl(rows)

  it('returns -1 when no rows are rendered', () => {
    expect(findRowIndexByDomPosition(makeScrollEl([]), 250)).toBe(-1)
  })

  it('locates the row whose rect contains the Y coordinate', () => {
    expect(findRowIndexByDomPosition(el, 150)).toBe(0)
    expect(findRowIndexByDomPosition(el, 250)).toBe(1)
    expect(findRowIndexByDomPosition(el, 400)).toBe(2) // inside the tall row
  })

  it('clamps to the first/last row when Y is outside the rendered range', () => {
    expect(findRowIndexByDomPosition(el, 50)).toBe(0) // above the first row
    expect(findRowIndexByDomPosition(el, 999)).toBe(2) // below the last row
  })

  it('picks the closer row when Y falls in a gap between rows', () => {
    const gapped = makeScrollEl([
      { index: 0, top: 100, bottom: 180 },
      { index: 1, top: 220, bottom: 300 }
    ])
    expect(findRowIndexByDomPosition(gapped, 190)).toBe(0) // 10px from row0.bottom vs 30px from row1.top
    expect(findRowIndexByDomPosition(gapped, 215)).toBe(1) // 35px from row0.bottom vs 5px from row1.top
  })

  it('returns the data-index attribute value, not the array position', () => {
    const remapped = makeScrollEl([
      { index: 5, top: 100, bottom: 200 },
      { index: 9, top: 200, bottom: 300 }
    ])
    expect(findRowIndexByDomPosition(remapped, 150)).toBe(5)
    expect(findRowIndexByDomPosition(remapped, 250)).toBe(9)
  })
})

describe('useSwipeSelect virtual data', () => {
  let unmount: (() => void) | undefined
  afterEach(() => {
    unmount?.()
    unmount = undefined
    vi.restoreAllMocks()
    vi.useRealTimers()
  })

  interface Row { id: number; name: string }

  it('preserves the row type between sorted data and row identity', () => {
    expectTypeOf<SwipeSelectVirtualContext<Row>['getSortedData']>().returns.toEqualTypeOf<readonly Row[]>()
    expectTypeOf<SwipeSelectVirtualContext<Row>['getRowId']>().parameter(0).toEqualTypeOf<Row>()
  })

  it.each([true, false])('selects offscreen sorted rows and restores the snapshot when shrinking (batch: %s)', async batch => {
    vi.useFakeTimers()
    vi.spyOn(document.documentElement, 'clientWidth', 'get').mockReturnValue(1024)
    vi.spyOn(document.documentElement, 'clientHeight', 'get').mockReturnValue(768)
    const rows: readonly Row[] = [
      { id: 50, name: 'First' }, { id: 10, name: 'Second' }, { id: 40, name: 'Third' },
      { id: 20, name: 'Fourth' }, { id: 30, name: 'Last' },
    ]
    const selected = new Set([30])
    const identities: Row[] = []
    const scrollEl = document.createElement('div')
    scrollEl.getBoundingClientRect = () => new DOMRect(0, 0, 500, 400)
    const virtualizer = new Virtualizer<HTMLElement, Element>({
      count: rows.length,
      getScrollElement: () => scrollEl,
      estimateSize: () => 40,
      scrollToFn: () => {},
      observeElementRect: () => {},
      observeElementOffset: () => {},
    })
    virtualizer.scrollElement = scrollEl
    vi.spyOn(virtualizer, 'getVirtualItems').mockReturnValue([
      { index: 1, key: 10, start: 40, end: 80, size: 40, lane: 0 },
      { index: 2, key: 40, start: 80, end: 120, size: 40, lane: 0 },
    ])
    const wrapper = mount(defineComponent({
      setup() {
        const container = ref<HTMLElement | null>(null)
        useSwipeSelect(container, {
          isSelected: id => selected.has(id),
          select: id => { selected.add(id) },
          deselect: id => { selected.delete(id) },
          batchUpdate: batch ? update => update(selected) : undefined,
        }, {
          getVirtualizer: () => virtualizer,
          getSortedData: () => rows,
          getRowId: row => {
            expectTypeOf(row).toEqualTypeOf<Row>()
            identities.push(row)
            return row.id
          },
        })
        return () => h('div', { ref: container })
      },
    }), { attachTo: document.body })
    unmount = () => wrapper.unmount()

    await wrapper.trigger('mousedown', { button: 0, clientX: 10, clientY: 10 })
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 10, clientY: 190 }))
    expect([...selected].sort((a, b) => a - b)).toEqual([10, 20, 30, 40, 50])
    expect(identities.every(row => rows.includes(row))).toBe(true)

    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 10, clientY: 60 }))
    await vi.advanceTimersByTimeAsync(20)
    expect([...selected].sort((a, b) => a - b)).toEqual([10, 30, 50])
    document.dispatchEvent(new MouseEvent('mouseup'))
    document.dispatchEvent(new MouseEvent('mousemove', { clientX: 10, clientY: 190 }))
    await vi.advanceTimersByTimeAsync(20)
    expect([...selected].sort((a, b) => a - b)).toEqual([10, 30, 50])
  })
})
