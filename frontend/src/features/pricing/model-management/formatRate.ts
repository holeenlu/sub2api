/** Presentation only: avoid exposing binary conversion noise without rounding tiny known rates to zero. */
export function formatManagedRate(value: number, locale: string): string {
  return '$' + new Intl.NumberFormat(locale, { maximumSignificantDigits: 12, useGrouping: false }).format(value)
}
