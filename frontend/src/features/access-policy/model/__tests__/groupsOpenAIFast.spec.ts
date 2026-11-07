import { describe, expect, it } from "vitest";

import {
  normalizeGroupOpenAIFast,
  supportsGroupOpenAIFast,
} from "../groupsOpenAIFast";
import { loadApplicationLocale } from '@/app/i18n'
const assembledEnMessages = await loadApplicationLocale('en')
const assembledZhMessages = await loadApplicationLocale('zh')
const en = assembledEnMessages.admin
const zh = assembledZhMessages.admin


describe("groupsOpenAIFast", () => {
  it("supports OpenAI and composite groups", () => {
    expect(supportsGroupOpenAIFast("openai")).toBe(true);
    expect(supportsGroupOpenAIFast("composite")).toBe(true);
    expect(supportsGroupOpenAIFast("anthropic")).toBe(false);
  });

  it("clears stale enabled state on unsupported platforms", () => {
    expect(normalizeGroupOpenAIFast("openai", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("composite", true)).toBe(true);
    expect(normalizeGroupOpenAIFast("anthropic", true)).toBe(false);
    expect(normalizeGroupOpenAIFast("openai", false)).toBe(false);
  });

  it("provides localized switch labels", () => {
    expect(zh.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
    expect(en.groups.openaiFast).toMatchObject({
      title: expect.any(String),
      force: expect.any(String),
      hint: expect.stringContaining("service_tier=priority"),
      free: expect.any(String),
      freeHint: expect.stringContaining("Standard"),
    });
  });
});
