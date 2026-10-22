import { describe, expect, it } from 'vitest'

import en from '../locales/en'
import ja from '../locales/ja'
import zh from '../locales/zh'
import zhTW from '../locales/zh-TW'
import { baseCompile } from '@intlify/message-compiler'

type LocaleValue = Record<string, unknown>

function flattenLeafKeys(value: unknown, prefix = ''): string[] {
  if (value === null || typeof value !== 'object' || Array.isArray(value)) {
    return prefix ? [prefix] : []
  }

  return Object.entries(value as LocaleValue).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key
    return flattenLeafKeys(child, path)
  })
}

function collectStaticSourceKeys(source: string): string[] {
  const keys = new Set<string>()

  // Covers useI18n().t(), the global $t(), and direct i18n.t() calls in both
  // TypeScript and Vue script/template source. Dynamic suffixes are checked by
  // the runtime type of the value and cannot be proven from source text alone.
  const translationCalls = /(?:\bi18n\.t|\$t|\bt)\s*\(\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(translationCalls)) {
    const key = match[2]
    if (!key.endsWith('.')) {
      keys.add(key)
    }
  }

  // Router metadata and i18n-t are key references without a t() call.
  const keyReferences = /(?:keypath|titleKey|descriptionKey|metaDescriptionKey|labelKey)\s*[:=]\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(keyReferences)) {
    if (match[2].includes('.')) keys.add(match[2])
  }

  const i18nTKeypaths = /<i18n-t\b[^>]*\bkeypath\s*=\s*(['"])([^'"\r\n]+)\1/gi
  for (const match of source.matchAll(i18nTKeypaths)) {
    keys.add(match[2])
  }

  return [...keys]
}

function sourceKeys(): string[] {
  const sourceFiles = import.meta.glob('../../**/*.{ts,vue}', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>

  return Object.entries(sourceFiles)
    .filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))
    .flatMap(([, source]) => collectStaticSourceKeys(source))
}

function missingKeys(usedKeys: string[], availableKeys: Set<string>): string[] {
  return usedKeys.filter((key) => !availableKeys.has(key)).sort()
}

describe('locale key completeness', () => {
  const enKeys = new Set(flattenLeafKeys(en))
  const zhKeys = new Set(flattenLeafKeys(zh))
  const twKeys = new Set(flattenLeafKeys(zhTW))
  const jaKeys = new Set(flattenLeafKeys(ja))
  const usedKeys = [...new Set(sourceKeys())].sort()

  it('keeps English and Chinese locale schemas identical', () => {
    expect([...enKeys].filter((key) => !zhKeys.has(key)).sort()).toEqual([])
    expect([...zhKeys].filter((key) => !enKeys.has(key)).sort()).toEqual([])
    expect([...twKeys].sort()).toEqual([...zhKeys].sort())
    expect([...jaKeys].sort()).toEqual([...enKeys].sort())
  })

  it('contains a non-empty message for every locale leaf', () => {
    for (const [locale, messages] of Object.entries({ en, ja, zh, 'zh-TW': zhTW })) {
      const emptyKeys = flattenLeafKeys(messages).filter((key) => {
        let current: unknown = messages
        for (const segment of key.split('.')) {
          current = (current as LocaleValue)[segment]
        }
        return typeof current !== 'string' || current.trim() === ''
      })
      expect(emptyKeys, `${locale} has empty or non-string messages`).toEqual([])
    }
  })

  it('contains every statically referenced production key', () => {
    expect(missingKeys(usedKeys, enKeys), 'English locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, zhKeys), 'Chinese locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, twKeys), 'Taiwan locale is missing referenced keys').toEqual([])
    expect(missingKeys(usedKeys, jaKeys), 'Japanese locale is missing referenced keys').toEqual([])
  })

  it('preserves interpolation parameters and linked message targets in all languages', () => {
    function valueAt(messages: unknown, key: string): string {
      return key.split('.').reduce((node, part) => (node as LocaleValue)[part], messages) as string
    }
    function signature(message: string): string[] {
      const tokens = new Set<string>()
      function walk(node: unknown): void {
        if (!node || typeof node !== 'object') return
        const record = node as Record<string, unknown>
        if (record.type === 4) tokens.add(`named:${record.key}`)
        if (record.type === 5) tokens.add(`list:${record.index}`)
        if (record.type === 6) tokens.add(`linked:${(record.key as { value: string }).value}`)
        for (const [key, value] of Object.entries(record)) {
          if (key !== 'loc') {
            if (Array.isArray(value)) value.forEach(walk)
            else if (value && typeof value === 'object') walk(value)
          }
        }
      }
      walk(baseCompile(message).ast)
      return [...tokens].sort()
    }
    const errors: string[] = []
    for (const key of zhKeys) {
      const expected = signature(valueAt(zh, key))
      for (const [locale, messages] of Object.entries({ en, ja, 'zh-TW': zhTW })) {
        const actual = signature(valueAt(messages, key))
        if (JSON.stringify(actual) !== JSON.stringify(expected)) {
          errors.push(`${locale}:${key}: ${actual.join(',')} != ${expected.join(',')}`)
        }
      }
    }
    expect(errors).toEqual([])
  })
})
