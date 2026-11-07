import { describe, expect, it } from 'vitest'
import { listPlatformIds } from '@/features/catalog'
import { normalizePlatformQuotasMap, sanitizePlatformQuotasMap } from '../platformQuotas'

/** 与后端 AllowedQuotaPlatforms 一致的全部具体平台（平台清单）。 */
const quotaPlatforms = [
  "anthropic", "openai", "gemini", "antigravity", "grok",
  "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "typesafe", "command_code", "cline",
];

describe("normalizePlatformQuotasMap", () => {
  it('isolates explicit platform lists without process-global configuration', () => {
    const first = normalizePlatformQuotasMap(['acme_router'], { acme_router: { daily: 0, weekly: null, monthly: 9 } });
    const second = normalizePlatformQuotasMap(['openai']);
    expect(first).toEqual({ acme_router: { daily: 0, weekly: null, monthly: 9 } });
    expect(Object.keys(second)).toEqual(['openai']);
    expect(normalizePlatformQuotasMap([])).toEqual({});
  });
  it("填充缺失的平台为全 null 三档", () => {
    const result = normalizePlatformQuotasMap(listPlatformIds(), { anthropic: { daily: 5, weekly: null, monthly: null } });
    expect(result.anthropic).toEqual({ daily: 5, weekly: null, monthly: null });
    expect(result.openai).toEqual({ daily: null, weekly: null, monthly: null });
    expect(result.gemini).toEqual({ daily: null, weekly: null, monthly: null });
    expect(result.antigravity).toEqual({ daily: null, weekly: null, monthly: null });
    expect(result.grok).toEqual({ daily: null, weekly: null, monthly: null });
    expect(result.kimi).toEqual({ daily: null, weekly: null, monthly: null });
    expect(result.opencode_go).toEqual({ daily: null, weekly: null, monthly: null });
  });

  it("无参数时返回平台清单中的全部平台全 null", () => {
    const result = normalizePlatformQuotasMap(listPlatformIds());
    expect(listPlatformIds()).toEqual(quotaPlatforms);
    expect(Object.keys(result)).toEqual(quotaPlatforms);
    for (const v of Object.values(result)) {
      expect(v).toEqual({ daily: null, weekly: null, monthly: null });
    }
  });

  it("非 number 类型的值归一化为 null", () => {
    const result = normalizePlatformQuotasMap(listPlatformIds(), {
      anthropic: { daily: "50" as unknown as number, weekly: undefined as unknown as number, monthly: null },
    });
    expect(result.anthropic).toEqual({ daily: null, weekly: null, monthly: null });
  });
});

describe("sanitizePlatformQuotasMap", () => {
  it("保留合法的正数和零值", () => {
    const result = sanitizePlatformQuotasMap(listPlatformIds(), {
      anthropic: { daily: 10.5, weekly: 0, monthly: null },
    });
    expect(result.anthropic?.daily).toBe(10.5);
    expect(result.anthropic?.weekly).toBe(0);
    expect(result.anthropic?.monthly).toBe(null);
  });

  it("空字符串（v-model.number 空输入）清洗为 null", () => {
    const result = sanitizePlatformQuotasMap(listPlatformIds(), {
      anthropic: { daily: "" as unknown as number, weekly: null, monthly: null },
    });
    expect(result.anthropic?.daily).toBe(null);
  });

  it("负数清洗为 null", () => {
    const result = sanitizePlatformQuotasMap(listPlatformIds(), {
      openai: { daily: -1, weekly: null, monthly: null },
    });
    expect(result.openai?.daily).toBe(null);
  });

  it("NaN/Infinity 清洗为 null", () => {
    const result = sanitizePlatformQuotasMap(listPlatformIds(), {
      gemini: { daily: NaN, weekly: Infinity, monthly: null },
    });
    expect(result.gemini?.daily).toBe(null);
    expect(result.gemini?.weekly).toBe(null);
  });

  it("缺失平台填充为全 null", () => {
    const result = sanitizePlatformQuotasMap(listPlatformIds(), {});
    expect(Object.keys(result)).toEqual(quotaPlatforms);
    for (const v of Object.values(result)) {
      expect(v).toEqual({ daily: null, weekly: null, monthly: null });
    }
  });
});
