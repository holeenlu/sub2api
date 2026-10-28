import { describe, expect, it } from 'vitest'


import managedModelsEn from '@/features/catalog/components/managed-models/locales/en'
import managedModelsZh from '@/features/catalog/components/managed-models/locales/zh'
import managedModelsTW from '@/features/catalog/components/managed-models/locales/zh-TW'
import managedModelsJa from '@/features/catalog/components/managed-models/locales/ja'
import { messages as priceMessages } from '@/features/pricing/model-management/locales'
import workspaceMessages from '@/app/compositions/model-management/locales/messages'

// Local composers and their children resolve their own messages before the global fallback.
// Keep concrete ownership here so a missing local key still fails the production source check.
const localMessageScopes = [
  { path: '/features/catalog/components/managed-models/', messages: {
    en: { managedModels: managedModelsEn }, zh: { managedModels: managedModelsZh },
    'zh-TW': { managedModels: managedModelsTW }, ja: { managedModels: managedModelsJa }
  } },
  { path: '/features/pricing/model-management/', messages: priceMessages },
  { path: '/app/compositions/model-management/', messages: workspaceMessages }
]

import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledJaMessages = await loadApplicationLocale('ja')
const assembledZhMessages = await loadApplicationLocale('zh')
const assembledZhTWMessages = await loadApplicationLocale('zh-TW')
const en = assembledEnMessages
const ja = assembledJaMessages
const zh = assembledZhMessages
const zhTW = assembledZhTWMessages



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
  const keyReferences = /(?:keypath|titleKey|descriptionKey|metaDescriptionKey|labelKey|messageKey)\s*[:=]\s*(['"])([^'"\r\n]+)\1/g
  for (const match of source.matchAll(keyReferences)) {
    if (match[2].includes('.')) keys.add(match[2])
  }

  const i18nTKeypaths = /<i18n-t\b[^>]*\bkeypath\s*=\s*(['"])([^'"\r\n]+)\1/gi
  for (const match of source.matchAll(i18nTKeypaths)) {
    keys.add(match[2])
  }

  return [...keys]
}

function sourceKeys(): { path: string; keys: string[] }[] {
  const sourceFiles = import.meta.glob('../../**/*.{ts,vue}', {
    query: '?raw',
    import: 'default',
    eager: true
  }) as Record<string, string>

  return Object.entries(sourceFiles)
    .filter(([path]) => !path.includes('/__tests__/') && !/\.(spec|test)\.ts$/.test(path))
    .map(([path, source]) => ({ path, keys: collectStaticSourceKeys(source) }))
}

function missingKeys(usedKeys: string[], availableKeys: Set<string>): string[] {
  return usedKeys.filter((key) => !availableKeys.has(key)).sort()
}

describe('locale key completeness', () => {
  const enKeys = new Set(flattenLeafKeys(en))
  const zhKeys = new Set(flattenLeafKeys(zh))
  const twKeys = new Set(flattenLeafKeys(zhTW))
  const jaKeys = new Set(flattenLeafKeys(ja))
  const referencedSources = sourceKeys()
  const globalKeys = referencedSources.filter(source => !localMessageScopes.some(scope => source.path.includes(scope.path)))
  const usedKeys = [...new Set(globalKeys.flatMap(source => source.keys))].sort()

  it('resolves every linked message without missing targets or cycles', () => {
    const errors: string[] = []
    for (const [locale, messages] of Object.entries({ en, ja, zh, 'zh-TW': zhTW })) {
      const keys = new Set(flattenLeafKeys(messages))
      const links = new Map<string, string[]>()
      for (const key of keys) {
        const value = key.split('.').reduce((node, part) => (node as LocaleValue)[part], messages) as string
        const targets: string[] = []
        const visit = (node: unknown): void => {
          if (!node || typeof node !== 'object') return
          const record = node as Record<string, unknown>
          if (record.type === 6) {
            const target = (record.key as { value?: unknown })?.value
            if (typeof target === 'string') targets.push(target)
          }
          for (const [property, child] of Object.entries(record)) {
            if (property === 'loc') continue
            if (Array.isArray(child)) child.forEach(visit)
            else if (child && typeof child === 'object') visit(child)
          }
        }
        visit(baseCompile(value).ast)
        links.set(key, targets)
        for (const target of targets) {
          if (!keys.has(target)) errors.push(locale + ':' + key + ' -> missing ' + target)
        }
      }
      const checked = new Set<string>()
      const check = (key: string, active: Set<string>): void => {
        if (active.has(key)) { errors.push(locale + ': linked cycle at ' + key); return }
        if (checked.has(key)) return
        const next = new Set(active).add(key)
        for (const target of links.get(key) ?? []) check(target, next)
        checked.add(key)
      }
      for (const key of keys) check(key, new Set())
    }
    expect(errors).toEqual([])
  })

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
    for (const scope of localMessageScopes) {
      const localReferences = referencedSources.filter(source => source.path.includes(scope.path)).flatMap(source => source.keys)
      for (const [language, global] of Object.entries({ en: enKeys, zh: zhKeys, 'zh-TW': twKeys, ja: jaKeys })) {
        const local = scope.messages[language as keyof typeof scope.messages]
        const available = new Set([...global, ...flattenLeafKeys(local)])
        expect(missingKeys(localReferences, available), language + ':' + scope.path).toEqual([])
      }
    }
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
    for (const scope of localMessageScopes) {
      const referenceKeys = flattenLeafKeys(scope.messages.en).sort()
      for (const [language, messages] of Object.entries(scope.messages)) {
        expect(flattenLeafKeys(messages).sort(), language + ':' + scope.path + ' schema').toEqual(referenceKeys)
        for (const key of referenceKeys) {
          const value = valueAt(messages, key)
          expect(typeof value === 'string' && value.trim().length > 0, language + ':' + scope.path + ':' + key).toBe(true)
          const expected = signature(valueAt(scope.messages.en, key))
          if (JSON.stringify(signature(value)) !== JSON.stringify(expected)) errors.push(language + ':' + scope.path + ':' + key)
        }
      }
    }
    expect(errors).toEqual([])
  })
})
