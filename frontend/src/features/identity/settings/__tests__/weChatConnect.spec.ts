import { describe, expect, it } from "vitest";

import {
  defaultWeChatConnectScopesForMode,
  normalizeWeChatConnectMode,
  resolveWeChatConnectModeCapabilities,
  deriveWeChatConnectStoredMode,
} from "../weChatConnect";

describe("admin settings wechat connect helpers", () => {
  it("normalizes legacy or noisy mode values to the backend contract", () => {
    expect(normalizeWeChatConnectMode("OPEN")).toBe("open");
    expect(normalizeWeChatConnectMode(" open_platform ")).toBe("open");
    expect(normalizeWeChatConnectMode("mp")).toBe("mp");
    expect(normalizeWeChatConnectMode("official_account")).toBe("mp");
    expect(normalizeWeChatConnectMode("unknown")).toBe("open");
  });

  it("maps each mode to the backend default scopes", () => {
    expect(defaultWeChatConnectScopesForMode("open")).toBe("snsapi_login");
    expect(defaultWeChatConnectScopesForMode("mp")).toBe("snsapi_userinfo");
  });

  it('keeps explicit disabled flags authoritative over legacy mode', () => {
    expect(resolveWeChatConnectModeCapabilities(false, undefined, undefined, 'mp')).toEqual({ openEnabled: false, mpEnabled: false, mobileEnabled: false });
    expect(resolveWeChatConnectModeCapabilities(undefined, undefined, undefined, 'official_account')).toEqual({ openEnabled: false, mpEnabled: true, mobileEnabled: false });
    expect(resolveWeChatConnectModeCapabilities(undefined, undefined, undefined, 'native_app')).toEqual({ openEnabled: false, mpEnabled: false, mobileEnabled: true });
    expect(defaultWeChatConnectScopesForMode('mobile')).toBe('');
  });

  it('retains stored-mode precedence and legacy fallback', () => {
    expect(deriveWeChatConnectStoredMode(true, true, true, 'open')).toBe('mp');
    expect(deriveWeChatConnectStoredMode(true, false, true, 'mp')).toBe('mobile');
    expect(deriveWeChatConnectStoredMode(true, false, false, 'mp')).toBe('open');
    expect(deriveWeChatConnectStoredMode(false, false, false, 'official_account')).toBe('mp');
  });
});
