import { describe, expect, it, vi, beforeEach } from 'vitest'

import { useHiddenColumns } from '../useHiddenColumns'

const store = new Map<string, string>()

beforeEach(() => {
  store.clear()
  vi.stubGlobal('localStorage', {
    getItem: vi.fn((key: string) => store.get(key) ?? null),
    setItem: vi.fn((key: string, value: string) => { store.set(key, value) }),
    removeItem: vi.fn((key: string) => { store.delete(key) }),
  })
})

const columns = [{ key: 'id' }, { key: 'name' }, { key: 'cost' }]

describe('useHiddenColumns', () => {
  it('starts with defaultHidden when nothing is saved and persists toggles', () => {
    const cols = useHiddenColumns('k', { defaultHidden: ['cost'] })
    cols.load()

    expect(cols.isVisible('cost')).toBe(false)
    expect(cols.visibleKeys(columns)).toEqual(['id', 'name'])

    cols.toggle('name')
    expect(cols.visibleKeys(columns)).toEqual(['id'])
    expect(JSON.parse(store.get('k')!)).toEqual(expect.arrayContaining(['cost', 'name']))

    cols.toggle('name')
    expect(cols.visibleKeys(columns)).toEqual(['id', 'name'])
  })

  it('restores a saved set and ignores alwaysVisible keys in it', () => {
    store.set('k', JSON.stringify(['id', 'cost']))
    const cols = useHiddenColumns('k', { alwaysVisible: ['id'], defaultHidden: ['name'] })
    cols.load()

    expect(cols.isVisible('id')).toBe(true)
    expect(cols.isVisible('cost')).toBe(false)
    // 有存档时不再套用 defaultHidden
    expect(cols.isVisible('name')).toBe(true)

    cols.toggle('id')
    expect(cols.isVisible('id')).toBe(true)
    expect(localStorage.setItem).not.toHaveBeenCalled()
  })

  it('falls back to defaults on corrupt or malformed storage without throwing', () => {
    for (const bad of ['{not json', '{"a":1}', '"str"', '[1, null, "cost"]']) {
      store.set('k', bad)
      const cols = useHiddenColumns('k', { defaultHidden: ['name'] })
      expect(() => cols.load()).not.toThrow()
      if (bad === '[1, null, "cost"]') {
        // 数组里只有字符串条目生效
        expect(cols.visibleKeys(columns)).toEqual(['id', 'name'])
      } else {
        expect(cols.visibleKeys(columns)).toEqual(['id', 'cost'])
      }
    }
  })

  it('keeps working when localStorage itself throws', () => {
    vi.stubGlobal('localStorage', {
      getItem: vi.fn(() => { throw new Error('denied') }),
      setItem: vi.fn(() => { throw new Error('denied') }),
      removeItem: vi.fn(),
    })
    vi.spyOn(console, 'error').mockImplementation(() => {})
    const cols = useHiddenColumns('k', { defaultHidden: ['cost'] })
    expect(() => cols.load()).not.toThrow()
    expect(cols.isVisible('cost')).toBe(false)
    expect(() => cols.toggle('cost')).not.toThrow()
    expect(cols.isVisible('cost')).toBe(true)
  })
})
