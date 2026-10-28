import { getIntlLocale } from '@/i18n/localeUtils'
export function publicUsageUsd(value: number | null | undefined): string {
  return value == null || !Number.isFinite(value) || value < 0 ? '-' : '$' + value.toFixed(2)
}
export function publicUsageNumber(value: number | null | undefined): string { return value == null || !Number.isFinite(value) ? '-' : value.toLocaleString() }
export function publicUsageDate(value: string | null | undefined, locale: string): string {
  if (!value) return '-'
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date.toLocaleDateString(getIntlLocale(locale), { year: 'numeric', month: 'long', day: 'numeric' }) : '-'
}
export function publicUsageResetTime(value: string | null | undefined, now: number, resetNow: string): string {
  if (!value) return ''
  const diff = new Date(value).getTime() - now
  if (!Number.isFinite(diff)) return ''
  if (diff <= 0) return resetNow
  const days = Math.floor(diff / 86400000), hours = Math.floor((diff % 86400000) / 3600000), mins = Math.floor((diff % 3600000) / 60000)
  return days > 0 ? days + 'd ' + hours + 'h' : hours > 0 ? hours + 'h ' + mins + 'm' : mins + 'm'
}
export interface PublicUsageRing { title: string; pct: number; amount: string; isBalance?: boolean; iconType: 'clock' | 'calendar' | 'dollar'; resetAt?: string | null }

export function publicUsageColor(pct: number): string {
  if (pct > 90) return 'text-rose-500'
  if (pct > 70) return 'text-amber-500'
  return 'text-emerald-500'
}

