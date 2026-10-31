import { describe, expect, it } from 'vitest'
import { defaultAllowImageGenerationForPlatform } from '../groupPolicyDraft'
describe('existing Group admission defaults', () => {
  it("defaults image generation on for Grok and Gemini-family groups only", () => {
    expect(defaultAllowImageGenerationForPlatform("grok")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("gemini")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("antigravity")).toBe(true);
    expect(defaultAllowImageGenerationForPlatform("composite")).toBe(false);
    expect(defaultAllowImageGenerationForPlatform("openai")).toBe(false);
  });

})
