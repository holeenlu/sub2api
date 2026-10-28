import { extractI18nErrorMessage, readApiError } from '@/shared/lib/apiError'

type TranslatePayment = ((key: string, params?: Record<string, unknown>) => string) & {
  te?: (key: string) => boolean
}

/** Provider config field names belong to payment errors, not shared transport errors. */
export function extractPaymentErrorMessage(error: unknown, t: TranslatePayment, fallback: string): string {
  const details = readApiError(error)
  if (!details.metadata) return extractI18nErrorMessage(error, t, 'payment.errors', fallback)

  function fieldLabel(field: string): string {
    const key = 'admin.settings.payment.field_' + field
    const translated = t(key)
    return translated === key || (t.te && !t.te(key)) ? field : translated
  }

  const metadata = { ...details.metadata }
  if (typeof metadata.key === 'string') metadata.key = fieldLabel(metadata.key)
  if (typeof metadata.keys === 'string') metadata.keys = metadata.keys.split('/').map(fieldLabel).join(' / ')
  return extractI18nErrorMessage({ ...details, metadata }, t, 'payment.errors', fallback)
}
