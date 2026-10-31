export const formatDashboardTokens = (value: number | undefined): string => {
  if (value === undefined || value === null || !Number.isFinite(value)) return '—'
  if (value >= 1_000_000_000) {
    return `${(value / 1_000_000_000).toFixed(2)}B`
  } else if (value >= 1_000_000) {
    return `${(value / 1_000_000).toFixed(2)}M`
  } else if (value >= 1_000) {
    return `${(value / 1_000).toFixed(2)}K`
  }
  return value.toLocaleString()
}

const toFiniteNumber = (value: unknown): number => {
  const numberValue = typeof value === 'number' ? value : NaN
  return numberValue
}

export const formatDashboardNumber = (value: number | null | undefined): string => {
  return Number.isFinite(toFiniteNumber(value)) ? toFiniteNumber(value).toLocaleString() : '—'
}

export const formatDashboardCost = (value: number | null | undefined): string => {
  const safeValue = toFiniteNumber(value)
  if (!Number.isFinite(safeValue)) return '—'
  if (safeValue >= 1000) {
    return (safeValue / 1000).toFixed(2) + 'K'
  } else if (safeValue >= 1) {
    return safeValue.toFixed(2)
  } else if (safeValue >= 0.01) {
    return safeValue.toFixed(3)
  }
  return safeValue.toFixed(4)
}

export const formatDashboardDuration = (ms: number | null | undefined): string => {
  if (ms == null || !Number.isFinite(ms)) return '—'
  if (ms >= 1000) {
    return `${(ms / 1000).toFixed(2)}s`
  }
  return `${Math.round(ms)}ms`
}

