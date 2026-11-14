/** Parse an API RFC3339 timestamp without JavaScript's invalid-date normalization. */
export function parseRfc3339Timestamp(value: unknown): Date | null {
  if (typeof value !== 'string') return null
  const fields = /^(?<year>\d{4})-(?<month>\d{2})-(?<day>\d{2})T(?<hour>\d{2}):(?<minute>\d{2}):(?<second>\d{2})(?:\.\d{1,9})?(?:Z|[+-](?<zoneHour>\d{2}):(?<zoneMinute>\d{2}))$/.exec(value)?.groups
  if (!fields) return null
  const year = Number(fields.year), month = Number(fields.month), day = Number(fields.day)
  const hour = Number(fields.hour), minute = Number(fields.minute), second = Number(fields.second)
  const zoneHour = Number(fields.zoneHour ?? 0), zoneMinute = Number(fields.zoneMinute ?? 0)
  if (month < 1 || month > 12 || hour > 23 || minute > 59 || second > 59 || zoneHour > 23 || zoneMinute > 59) return null
  const leap = year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0)
  const days = [31, leap ? 29 : 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31]
  if (day < 1 || day > (days[month - 1] ?? 0)) return null
  const date = new Date(value)
  return Number.isFinite(date.getTime()) ? date : null
}
