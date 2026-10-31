import type { LocaleCode } from './localeUtils'

export interface LocaleMessages {
  [key: string]: string | LocaleMessages
}

export type LocaleMessageLoader = (locale: LocaleCode) => Promise<LocaleMessages>
export type LocaleModuleLoader = () => Promise<{ default: LocaleMessages }>
export type LocaleContributionLoaders = Readonly<Record<LocaleCode, LocaleModuleLoader>>

export interface LocaleContribution {
  owner: string
  messages: LocaleMessages
}

/** Shared containers are allowed; every message leaf has exactly one owner. */
export function mergeLocaleContributions(contributions: readonly LocaleContribution[]): LocaleMessages {
  const result: LocaleMessages = {}
  const owners = new Map<string, string>()

  function merge(target: LocaleMessages, source: LocaleMessages, owner: string, prefix: string): void {
    for (const [key, value] of Object.entries(source)) {
      const path = prefix ? `${prefix}.${key}` : key
      if (key === '__proto__' || key === 'prototype' || key === 'constructor') {
        throw new Error(`Invalid locale key: ${path}`)
      }
      const previous = target[key]
      if (typeof value === 'string') {
        if (Object.prototype.hasOwnProperty.call(target, key)) {
          throw new Error(`Duplicate locale message ${path}: ${owners.get(path) ?? 'container'} / ${owner}`)
        }
        target[key] = value
        owners.set(path, owner)
        continue
      }
      if (!value || typeof value !== 'object' || Array.isArray(value)) {
        throw new Error(`Invalid locale message: ${path}`)
      }
      if (typeof previous === 'string') {
        throw new Error(`Locale namespace conflicts with message: ${path}`)
      }
      const child = previous ?? {}
      target[key] = child
      merge(child, value, owner, path)
    }
  }

  for (const contribution of contributions) {
    merge(result, contribution.messages, contribution.owner, '')
  }
  return result
}
