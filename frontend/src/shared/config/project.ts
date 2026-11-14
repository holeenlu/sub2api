/** Stable technical identifiers; never derive protocol keys from a site display name. */
export const PROJECT_ID = 'sub2api'
export const CLIENT_PROVIDER_ID = PROJECT_ID
export const CLIENT_API_KEY_ENV = 'SUB2API_API_KEY'
export const CLIENT_BASE_URL_ENV = 'SUB2API_BASE_URL'
export const BATCH_IMAGE_SKILL_NAME = PROJECT_ID + '-batch-image'

/** Neutral display fallback. Explicit server site settings take precedence. */
export const BRAND_NAME = PROJECT_ID

/** Public page fallback; deployment and release identities are not site branding. */
export const BRAND_TITLE_SUFFIX_EN = 'AI API Gateway'
export const BRAND_TAGLINE_EN = 'Subscription to API Conversion Platform'

/** Site settings may contain display punctuation; exported files need one safe name part. */
export function brandFilePrefix(siteName: string | null | undefined): string {
  const value = (siteName || BRAND_NAME).normalize('NFKC').trim().toLowerCase()
    .replace(/[^\p{L}\p{N}._-]+/gu, '-').replace(/^[._-]+|[._-]+$/g, '').slice(0, 80)
  return value || BRAND_NAME
}
