import { describe, expect, it } from 'vitest'
import { localeContributions } from '@/app/i18n'

type Modules = Record<string, Record<string, unknown>>

// Containers such as admin/settings may be shared; message leaves may not.
function leaves(value: unknown, prefix = '', result: Record<string, unknown> = {}): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) {
    if (prefix) result[prefix] = value
    return result
  }
  for (const [key, child] of Object.entries(value)) {
    leaves(child, prefix ? prefix + '.' + key : key, result)
  }
  return result
}

function collisions(modules: Modules): string[] {
  const seen = new Map<string, string>()
  const out: string[] = []
  for (const [name, mod] of Object.entries(modules)) {
    for (const key of Object.keys(mod)) {
      const previous = seen.get(key)
      if (previous) out.push(key + ' in both ' + previous + ' and ' + name)
      else seen.set(key, name)
    }
  }
  return out
}

const roots: Record<string, Modules> = {}
const admins: Record<string, Modules> = {}
for (const locale of ['en', 'zh', 'zh-TW', 'ja'] as const) {
  const entries = await Promise.all(Object.entries(localeContributions)
    .map(async ([owner, loaders]) => [owner, (await loaders[locale]()).default] as const))
  roots[locale] = Object.fromEntries(entries.map(([owner, messages]) => [owner, leaves(messages)]))
  admins[locale] = Object.fromEntries(entries.map(([owner, messages]) => [owner, leaves(messages.admin ?? {})]))
}

it('keeps every message leaf in the same owner across all languages', () => {
  function ownership(locale: string): [string, string][] {
    return Object.entries(roots[locale]).flatMap(([owner, messages]) =>
      Object.keys(messages).map(key => [key, owner] as [string, string])
    ).sort(([left], [right]) => left.localeCompare(right))
  }
  const expected = ownership('en')
  for (const locale of ['zh', 'zh-TW', 'ja']) expect(ownership(locale)).toEqual(expected)
})

it('keeps only common technical namespaces in the core contribution', async () => {
  for (const locale of ['en', 'zh', 'zh-TW', 'ja'] as const) {
    const messages = (await localeContributions.common[locale]()).default
    expect(Object.keys(messages).sort()).toEqual(['common', 'dates', 'empty', 'errors', 'pagination', 'table', 'ui'])
  }
})

describe.each(Object.keys(roots))('locale %s contribution assembly', locale => {
  it('root modules have no overlapping message leaves', () => {
    expect(collisions(roots[locale])).toEqual([])
  })

  it('root modules do not shadow the admin namespace with a message', () => {
    for (const [name, mod] of Object.entries(roots[locale])) {
      expect(Object.keys(mod), 'module ' + name + ' must not define admin as a leaf').not.toContain('admin')
    }
  })

  it('admin modules have no overlapping message leaves', () => {
    expect(collisions(admins[locale])).toEqual([])
  })
})
