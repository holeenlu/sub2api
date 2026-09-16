import type { DocsNavGroup, DocsNavItem } from './types'

export const apiNavGroups: DocsNavGroup[] = [
  {
    titleKey: 'docs.nav.start',
    items: [
      { path: '/docs', titleKey: 'docs.pages.overview.title', descriptionKey: 'docs.pages.overview.description', icon: 'home' },
      { path: '/docs/quickstart', articleId: 'quickstart', titleKey: 'docs.pages.quickstart.title', descriptionKey: 'docs.pages.quickstart.description', icon: 'bolt', exampleKind: 'responses' },
      { path: '/docs/authentication', articleId: 'authentication', titleKey: 'docs.pages.authentication.title', descriptionKey: 'docs.pages.authentication.description', icon: 'key' },
      { path: '/docs/api/protocols', articleId: 'api/protocols', titleKey: 'docs.pages.protocols.title', descriptionKey: 'docs.pages.protocols.description', icon: 'server' },
      { path: '/docs/pricing', articleId: 'pricing', titleKey: 'docs.pages.pricing.title', descriptionKey: 'docs.pages.pricing.description', icon: 'dollar', pricingKind: 'chat' },
      { path: '/docs/errors', articleId: 'errors', titleKey: 'docs.pages.errors.title', descriptionKey: 'docs.pages.errors.description', icon: 'exclamationTriangle' },
      { path: '/docs/limits', articleId: 'limits', titleKey: 'docs.pages.limits.title', descriptionKey: 'docs.pages.limits.description', icon: 'server' }
    ]
  },
  {
    titleKey: 'docs.nav.chat',
    items: [
      { path: '/docs/api/chat/openai-chat', articleId: 'api/chat/openai-chat', titleKey: 'docs.pages.openaiChat.title', descriptionKey: 'docs.pages.openaiChat.description', icon: 'chat', pricingKind: 'chat', exampleKind: 'chat' },
      { path: '/docs/api/chat/openai-responses', articleId: 'api/chat/openai-responses', titleKey: 'docs.pages.responses.title', descriptionKey: 'docs.pages.responses.description', icon: 'sparkles', pricingKind: 'chat', exampleKind: 'responses' },
      { path: '/docs/api/chat/anthropic-messages', articleId: 'api/chat/anthropic-messages', titleKey: 'docs.pages.messages.title', descriptionKey: 'docs.pages.messages.description', icon: 'chat', pricingKind: 'chat', exampleKind: 'messages' },
      { path: '/docs/api/chat/gemini-native', articleId: 'api/chat/gemini-native', titleKey: 'docs.pages.geminiNative.title', descriptionKey: 'docs.pages.geminiNative.description', icon: 'sparkles' },
      { path: '/docs/api/chat/grok-native', articleId: 'api/chat/grok-native', titleKey: 'docs.pages.grokNative.title', descriptionKey: 'docs.pages.grokNative.description', icon: 'chat' }
    ]
  },
  {
    titleKey: 'docs.nav.image',
    items: [
      { path: '/docs/api/image/openai-image', articleId: 'api/image/openai-image', titleKey: 'docs.pages.image.title', descriptionKey: 'docs.pages.image.description', icon: 'sparkles', pricingKind: 'image', exampleKind: 'image' }
    ]
  },
  {
    titleKey: 'docs.nav.models',
    items: [
      { path: '/docs/models', titleKey: 'docs.pages.models.title', descriptionKey: 'docs.pages.models.description', icon: 'cube' },
      { path: '/docs/api/query/models', articleId: 'api/query/models', titleKey: 'docs.pages.queryModels.title', descriptionKey: 'docs.pages.queryModels.description', icon: 'server', exampleKind: 'models' },
      { path: '/docs/api/query/token-count', articleId: 'api/query/token-count', titleKey: 'docs.pages.tokenCount.title', descriptionKey: 'docs.pages.tokenCount.description', icon: 'server' },
      { path: '/docs/api/query/embeddings', articleId: 'api/query/embeddings', titleKey: 'docs.pages.embeddings.title', descriptionKey: 'docs.pages.embeddings.description', icon: 'cube' }
    ]
  }
]

export const appsNavGroups: DocsNavGroup[] = [
  {
    titleKey: 'docs.nav.apps',
    items: [
      { path: '/apps', articleId: 'apps/index', titleKey: 'docs.pages.apps.title', descriptionKey: 'docs.pages.apps.description', icon: 'cube' },
      { path: '/apps/console', articleId: 'apps/console', titleKey: 'docs.pages.consoleGuide.title', descriptionKey: 'docs.pages.consoleGuide.description', icon: 'key' },
      { path: '/apps/codex', articleId: 'apps/codex', titleKey: 'docs.pages.codex.title', descriptionKey: 'docs.pages.codex.description', icon: 'bolt' },
      { path: '/apps/claude-code', articleId: 'apps/claude-code', titleKey: 'docs.pages.claudeCode.title', descriptionKey: 'docs.pages.claudeCode.description', icon: 'chat' },
      { path: '/apps/claude-desktop', articleId: 'apps/claude-desktop', titleKey: 'docs.pages.claudeDesktop.title', descriptionKey: 'docs.pages.claudeDesktop.description', icon: 'chat' },
      { path: '/apps/session-recovery', articleId: 'apps/session-recovery', titleKey: 'docs.pages.sessionRecovery.title', descriptionKey: 'docs.pages.sessionRecovery.description', icon: 'server' },
      { path: '/apps/image-skills', articleId: 'apps/image-skills', titleKey: 'docs.pages.imageSkills.title', descriptionKey: 'docs.pages.imageSkills.description', icon: 'sparkles' },
      { path: '/apps/downloads', articleId: 'downloads', titleKey: 'docs.pages.downloads.title', descriptionKey: 'docs.pages.downloads.description', icon: 'dollar' }
    ]
  }
]

export const docsNavGroups = [...apiNavGroups, ...appsNavGroups]
export const docsStaticItems: DocsNavItem[] = docsNavGroups.flatMap((group) => group.items)
export const docsItemByPath = new Map(docsStaticItems.map((item) => [item.path, item]))
export const isAppsPath = (path: string) => path === '/apps' || path.startsWith('/apps/')
export const navGroupsForPath = (path: string) => isAppsPath(path) ? appsNavGroups : apiNavGroups

// Preserve published bookmarks while giving applications their own top-level section.
export const legacyAppRedirects = appsNavGroups.flatMap(group => group.items)
  .filter(item => item.path !== '/apps/console')
  .map(item => ({ path: item.path === '/apps/downloads' ? '/docs/downloads' : item.path.replace('/apps', '/docs/apps'), redirect: item.path }))
