/** Read-only pricing fields consumed by payments, subscriptions, catalog and access-policy. */
export interface PeakRateFields {
  readonly peak_rate_enabled?: boolean
  readonly peak_start?: string
  readonly peak_end?: string
  readonly peak_rate_multiplier?: number
}

export function hasPeakRate(fields?: PeakRateFields | null): boolean {
  return Boolean(fields?.peak_rate_enabled && fields.peak_start && fields.peak_end)
}

/** Display the server's window unchanged; never interpret it in the browser's timezone. */
export function formatPeakRateWindow(
  fields: PeakRateFields | null | undefined,
  serverUtcOffset?: string | null
): string {
  if (!hasPeakRate(fields) || !fields) return ''
  const window = `${fields.peak_start}-${fields.peak_end} ×${fields.peak_rate_multiplier ?? 1}`
  return serverUtcOffset ? `${window} (UTC${serverUtcOffset})` : window
}

/** App supplies this pricing-owned contract through each consumer's existing port. */
export interface PeakRatePresentation {
  readonly hasPeakRate: typeof hasPeakRate
  readonly formatPeakRateWindow: typeof formatPeakRateWindow
}

export const peakRatePresentation: PeakRatePresentation = { hasPeakRate, formatPeakRateWindow }
