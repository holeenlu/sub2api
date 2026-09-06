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
 * Release-channel identifiers (GitHub repo, container image) are intentionally
 * not here: the version badge follows the backend's release repository constant.
 */

/** Product name, used wherever the configured site name is missing. */
export const BRAND_NAME = 'Sub2API'

/** Suffix appended to the site name in the document title (index.html, main.ts). */
export const BRAND_TITLE_SUFFIX_EN = 'AI API Gateway'

/** Default `site_subtitle` shown on the auth pages and seeded in the settings form. */
export const BRAND_TAGLINE_EN = 'Subscription to API Conversion Platform'

/** Project home (header / footer link). Empty string hides the link. */
export const BRAND_SITE_URL = 'https://github.com/Wei-Shaw/sub2api'

/** Base URL of the shipped documentation. */
export const BRAND_DOCS_URL = 'https://github.com/Wei-Shaw/sub2api/blob/main/docs'

/** Payment integration guide, per UI language. */
export const BRAND_PAYMENT_GUIDE_URL = {
  zh: `${BRAND_DOCS_URL}/PAYMENT_CN.md`,
  en: `${BRAND_DOCS_URL}/PAYMENT.md`
} as const

/** "Supported payment methods" section of the payment guide, per UI language. */
export const BRAND_PAYMENT_METHODS_URL = {
  zh: `${BRAND_DOCS_URL}/PAYMENT_CN.md#支持的支付方式`,
  en: `${BRAND_DOCS_URL}/PAYMENT.md#supported-payment-methods`
} as const

/**
 * Admin compliance agreement, per locale. Used only when the backend does not
 * return `document_url_zh` / `document_url_en`.
 */
export const BRAND_COMPLIANCE_DOCUMENT_URL = {
  zh: `${BRAND_DOCS_URL}/legal/admin-compliance.zh.md`,
  'zh-TW': 'https://github.com/holeenlu/sub2api/blob/main/docs/legal/admin-compliance.zh-TW.md',
  en: `${BRAND_DOCS_URL}/legal/admin-compliance.en.md`
} as const
