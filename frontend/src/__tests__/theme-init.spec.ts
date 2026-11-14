import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { runInNewContext } from 'node:vm'
import { describe, expect, it } from 'vitest'

const script = readFileSync(resolve(__dirname, '../../public/theme-init.js'), 'utf8')

describe('initial display mode', () => {
  it.each([
    ['dark', false, true], ['light', true, false],
    ['auto', true, true], ['auto', false, false],
    [null, true, true], ['unknown', true, true],
  ])('resolves saved mode %s before application startup', (mode, systemDark, expected) => {
    let applied = false
    runInNewContext(script, {
      localStorage: { getItem: () => mode },
      window: { matchMedia: () => ({ matches: systemDark }) },
      document: { documentElement: { classList: { toggle: (_: string, dark: boolean) => { applied = dark } } } },
    })
    expect(applied).toBe(expected)
  })

  it('uses the system preference when browser storage is unavailable', () => {
    let applied = false
    runInNewContext(script, {
      localStorage: { getItem: () => { throw new Error('storage denied') } },
      window: { matchMedia: () => ({ matches: true }) },
      document: { documentElement: { classList: { toggle: (_: string, dark: boolean) => { applied = dark } } } },
    })
    expect(applied).toBe(true)
  })
})
