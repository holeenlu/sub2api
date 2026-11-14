import type { DefaultPlatformQuotasMap, PlatformType } from '@/features/system/api'

/** 归一化为全部平台 × 3 窗口（缺失填 null），供模板非空绑定 */
export function normalizePlatformQuotasMap(platformIds: readonly PlatformType[], input?: DefaultPlatformQuotasMap | null): DefaultPlatformQuotasMap {
  const result: DefaultPlatformQuotasMap = {}
  for (const p of platformIds) {
    const src = input?.[p]
    result[p] = {
      daily:   typeof src?.daily === "number" ? src.daily : null,
      weekly:  typeof src?.weekly === "number" ? src.weekly : null,
      monthly: typeof src?.monthly === "number" ? src.monthly : null,
    }
  }
  return result
}

/** 提交前清洗：非有限数/负数/空字符串 → null（保留 0 = 显式禁用），返回全部平台嵌套 map */
export function sanitizePlatformQuotasMap(platformIds: readonly PlatformType[], input?: DefaultPlatformQuotasMap | null): DefaultPlatformQuotasMap {
  const clean = (v: unknown): number | null => (typeof v === "number" && Number.isFinite(v) && v >= 0 ? v : null)
  const result: DefaultPlatformQuotasMap = {}
  for (const p of platformIds) {
    const src = input?.[p]
    result[p] = { daily: clean(src?.daily), weekly: clean(src?.weekly), monthly: clean(src?.monthly) }
  }
  return result
}

