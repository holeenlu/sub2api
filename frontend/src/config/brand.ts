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
export const BRAND_NAME = 'TapModels'

/** Suffix appended to the site name in the document title (index.html, main.ts). */
export const BRAND_TITLE_SUFFIX_EN = 'Pick a model. Start building.'

/** Default `site_subtitle` shown on the auth pages and seeded in the settings form. */
export const BRAND_TAGLINE_EN = 'Pick a model. Start building.'

/**
 * Project home (header / footer "GitHub" link). Empty string hides the link:
 * TapModels has no public source repository to point at (marketing site is
 * https://tapmodels.ai, documentation is BRAND_DOCS_URL).
 */
export const BRAND_SITE_URL = ''

/** Base URL of the shipped documentation. */
export const BRAND_DOCS_URL = 'https://docs.tapmodels.ai'

/** Payment integration guide, per UI language. */
export const BRAND_PAYMENT_GUIDE_URL = {
  zh: `${BRAND_DOCS_URL}/zh/payment`,
  ja: `${BRAND_DOCS_URL}/ja/payment`,
  en: `${BRAND_DOCS_URL}/payment`
} as const

/** "Supported payment methods" section of the payment guide, per UI language. */
export const BRAND_PAYMENT_METHODS_URL = {
  zh: `${BRAND_DOCS_URL}/zh/payment#supported-payment-methods`,
  ja: `${BRAND_DOCS_URL}/ja/payment#supported-payment-methods`,
  en: `${BRAND_DOCS_URL}/payment#supported-payment-methods`
} as const

/**
 * Admin compliance agreement, per locale. Used only when the backend does not
 * return `document_url_zh` / `document_url_en`.
 */
export const BRAND_COMPLIANCE_DOCUMENT_URL = {
  zh: `${BRAND_DOCS_URL}/legal/admin-compliance.zh.md`,
  'zh-TW': `${BRAND_DOCS_URL}/legal/admin-compliance.zh-TW.md`,
  ja: `${BRAND_DOCS_URL}/legal/admin-compliance.ja.md`,
  en: `${BRAND_DOCS_URL}/legal/admin-compliance.en.md`
} as const

/** GitHub "owner/repo" whose releases the version badge follows (= service.DefaultReleaseRepo). */
export const RELEASE_REPO = 'erwinlin/TapModels'

/** GHCR image published by tapmodels-docker-image.yml (tags carry no "v" prefix, e.g. 1.0.0). */
export const RELEASE_DOCKER_IMAGE = 'ghcr.io/erwinlin/tapmodels'
