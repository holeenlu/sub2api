import type { DocsNavGroup, DocsNavItem } from './types'

export const docsNavGroups: DocsNavGroup[] = [
  {
    titleKey: 'docs.nav.start',
    items: [
      { path: '/docs', titleKey: 'docs.pages.overview.title', descriptionKey: 'docs.pages.overview.description', icon: 'home' },
      { path: '/docs/quickstart', articleId: 'quickstart', titleKey: 'docs.pages.quickstart.title', descriptionKey: 'docs.pages.quickstart.description', icon: 'bolt', exampleKind: 'responses' },
      { path: '/docs/authentication', articleId: 'authentication', titleKey: 'docs.pages.authentication.title', descriptionKey: 'docs.pages.authentication.description', icon: 'key' },
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
      { path: '/docs/api/chat/anthropic-messages', articleId: 'api/chat/anthropic-messages', titleKey: 'docs.pages.messages.title', descriptionKey: 'docs.pages.messages.description', icon: 'chat', pricingKind: 'chat', exampleKind: 'messages' }
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
      { path: '/docs/api/query/models', articleId: 'api/query/models', titleKey: 'docs.pages.queryModels.title', descriptionKey: 'docs.pages.queryModels.description', icon: 'server', exampleKind: 'models' }
    ]
  },
  {
    titleKey: 'docs.nav.apps',
    items: [
      { path: '/docs/apps', articleId: 'apps/index', titleKey: 'docs.pages.apps.title', descriptionKey: 'docs.pages.apps.description', icon: 'cube' },
      { path: '/docs/apps/codex', articleId: 'apps/codex', titleKey: 'docs.pages.codex.title', descriptionKey: 'docs.pages.codex.description', icon: 'bolt' },
      { path: '/docs/apps/claude-code', articleId: 'apps/claude-code', titleKey: 'docs.pages.claudeCode.title', descriptionKey: 'docs.pages.claudeCode.description', icon: 'chat' },
      { path: '/docs/apps/claude-desktop', articleId: 'apps/claude-desktop', titleKey: 'docs.pages.claudeDesktop.title', descriptionKey: 'docs.pages.claudeDesktop.description', icon: 'chat' },
      { path: '/docs/apps/session-recovery', articleId: 'apps/session-recovery', titleKey: 'docs.pages.sessionRecovery.title', descriptionKey: 'docs.pages.sessionRecovery.description', icon: 'server' },
      { path: '/docs/apps/image-skills', articleId: 'apps/image-skills', titleKey: 'docs.pages.imageSkills.title', descriptionKey: 'docs.pages.imageSkills.description', icon: 'sparkles' },
      { path: '/docs/downloads', articleId: 'downloads', titleKey: 'docs.pages.downloads.title', descriptionKey: 'docs.pages.downloads.description', icon: 'dollar' }
    ]
  }
]

export const docsStaticItems: DocsNavItem[] = docsNavGroups.flatMap((group) => group.items)
export const docsItemByPath = new Map(docsStaticItems.map((item) => [item.path, item]))
