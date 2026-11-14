export function formatUsageLocalDate(date: Date): string {
  return [date.getFullYear(), String(date.getMonth() + 1).padStart(2, '0'), String(date.getDate()).padStart(2, '0')].join('-')
}
export function getLast24HoursRangeDates(): { start: string; end: string } {
  const end = new Date(), start = new Date(end.getTime() - 24 * 60 * 60 * 1000)
  return { start: formatUsageLocalDate(start), end: formatUsageLocalDate(end) }
}
export function getGranularityForRange(start: string, end: string): 'day' | 'hour' {
  return Math.ceil((Date.parse(end + 'T00:00:00') - Date.parse(start + 'T00:00:00')) / 86400000) <= 1 ? 'hour' : 'day'
}
