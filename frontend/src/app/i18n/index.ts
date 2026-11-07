import { mergeLocaleContributions, type LocaleContributionLoaders, type LocaleMessageLoader } from '@/i18n/messageAssembly'
import { localeLoaders as commonLocaleLoaders } from '@/i18n/locales'

type LocaleFacet = { localeLoaders: LocaleContributionLoaders }
type LocaleRegistry = Readonly<Record<string, LocaleContributionLoaders>>

// Facets are tiny, eagerly loaded public contracts; message chunks remain lazy.
const featureLocaleFacets = import.meta.glob<LocaleFacet>('../../features/*/locales.ts', { eager: true })
export const localeContributions: LocaleRegistry = {
  common: commonLocaleLoaders,
  ...Object.fromEntries(Object.entries(featureLocaleFacets).map(([owner, facet]) => [owner, facet.localeLoaders]))
}

export function createApplicationLocaleLoader(registry: LocaleRegistry): LocaleMessageLoader {
  return async function load(locale) {
    const selected = Object.entries(registry).sort(([left], [right]) => left.localeCompare(right))
    if (selected.length === 0) throw new Error(`Locale contributions are missing: ${locale}`)
    const contributions = await Promise.all(selected.map(async ([owner, loaders]) => {
      const loadModule = loaders[locale]
      if (typeof loadModule !== 'function') throw new Error(`Locale loader ${locale} is missing for ${owner}`)
      return { owner, messages: (await loadModule()).default }
    }))
    return mergeLocaleContributions(contributions)
  }
}

export const loadApplicationLocale: LocaleMessageLoader = createApplicationLocaleLoader(localeContributions)
