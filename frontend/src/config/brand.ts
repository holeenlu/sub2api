/**
 * Brand constants (white-label fallbacks).
 *
 * Brand branches override the VALUES in this file only; application code must
 * import from here instead of hardcoding the product name or its links.
 *
 * Keep this module dependency-free: it is imported by application code via the
 * `@/` alias. vite.config.ts keeps its own copy of the title suffix (see the
 * note there) so this file is never pulled into the Node-side config bundle.
 *
 * NOTE: every value here is only a *fallback*. Runtime settings coming from the
 * backend (`site_name` / `site_subtitle` / `site_logo` / `doc_url`) always win,
 * and copy in the i18n bundles refers to the site name through the
 * `@:common.siteName` linked message, which stores/app.ts overrides with the
 * configured value. The backend's equivalent lives in
 * backend/internal/service/brand.go.
 *
 * RELEASE_REPO / RELEASE_DOCKER_IMAGE mirror service.DefaultReleaseRepo and the
 * image published by the release pipeline; the version badge builds rollback
 * commands from them.
 */

/** Product name, used wherever the configured site name is missing. */
export const BRAND_NAME = 'Tokensavy'

/** Suffix appended to the site name in the document title (index.html, main.ts). */
export const BRAND_TITLE_SUFFIX_EN = 'Smart tokens. More possibilities.'

/** Default `site_subtitle` shown on the auth pages and seeded in the settings form. */
export const BRAND_TAGLINE_EN = 'Smart tokens. More possibilities.'

/**
 * Project home (header / footer "GitHub" link). Empty string hides the link:
 * Tokensavy is deployed from source; no separate public repository is configured.
 */
export const BRAND_SITE_URL = ''

/** Base URL of the shipped documentation. */
export const BRAND_DOCS_URL = 'https://tokensavy.ai/docs'

/** Payment integration guide, per UI language. */
export const BRAND_PAYMENT_GUIDE_URL = {
  zh: '/guides/payment.zh.md',
  ja: '/guides/payment.en.md',
  en: '/guides/payment.en.md'
} as const

/** "Supported payment methods" section of the payment guide, per UI language. */
export const BRAND_PAYMENT_METHODS_URL = {
  zh: BRAND_PAYMENT_GUIDE_URL.zh,
  ja: BRAND_PAYMENT_GUIDE_URL.ja,
  en: BRAND_PAYMENT_GUIDE_URL.en
} as const

/**
 * Admin compliance agreement, per locale. Used only when the backend does not
 * return `document_url_zh` / `document_url_en`.
 */
export const BRAND_COMPLIANCE_DOCUMENT_URL = {
  zh: '/legal/admin-compliance',
  'zh-TW': '/legal/admin-compliance',
  ja: '/legal/admin-compliance',
  en: '/legal/admin-compliance'
} as const

/** Releases from the tokensavy branch, isolated by the tokensavy/v* channel. */
export const RELEASE_REPO = 'holeenlu/sub2api'

/** GHCR image built manually from this repository’s tokensavy branch. */
export const RELEASE_DOCKER_IMAGE = 'ghcr.io/holeenlu/tokensavy'
