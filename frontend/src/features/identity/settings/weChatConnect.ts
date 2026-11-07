import type { WeChatConnectMode, WeChatConnectModeOption } from '@/features/system/api'

const WECHAT_CONNECT_MODE_OPTIONS: WeChatConnectModeOption[] = [
  { value: "open", labelKey: "ui.pcApp", },
  {
    value: "mp",
    labelKey: "ui.officialAccount",
  },
  {
    value: "mobile",
    labelKey: "ui.mobileApp",
  },
];
const WECHAT_CONNECT_MODE_ALIASES: Record<string, WeChatConnectMode> = {
  open: "open",
  open_platform: "open",
  official: "open",
  wx_open: "open",
  mp: "mp",
  official_account: "mp",
  wechat_mp: "mp",
  mini_program: "mp",
  mobile: "mobile",
  mobile_app: "mobile",
  native_app: "mobile",
};

export function getWeChatConnectModeOptions(): WeChatConnectModeOption[] {
  return WECHAT_CONNECT_MODE_OPTIONS;
}

export function normalizeWeChatConnectMode(source: unknown): WeChatConnectMode {
  if (typeof source !== "string") return "open";

  const normalized = source.trim().toLowerCase();
  if (!normalized) return "open";

  return WECHAT_CONNECT_MODE_ALIASES[normalized] ?? "open";
}

export function defaultWeChatConnectScopesForMode(mode: unknown): string {
  switch (normalizeWeChatConnectMode(mode)) {
    case "mp":
      return "snsapi_userinfo";
    case "mobile":
      return "";
    default:
      return "snsapi_login";
  }
}

export function resolveWeChatConnectModeCapabilities(
  openEnabled: unknown,
  mpEnabled: unknown,
  mobileEnabled: unknown,
  legacyMode: unknown,
): { openEnabled: boolean; mpEnabled: boolean; mobileEnabled: boolean } {
  if (
    typeof openEnabled === "boolean" ||
    typeof mpEnabled === "boolean" ||
    typeof mobileEnabled === "boolean"
  ) {
    return {
      openEnabled: openEnabled === true,
      mpEnabled: mpEnabled === true,
      mobileEnabled: mobileEnabled === true,
    };
  }

  switch (normalizeWeChatConnectMode(legacyMode)) {
    case "mp":
      return { openEnabled: false, mpEnabled: true, mobileEnabled: false };
    case "mobile":
      return { openEnabled: false, mpEnabled: false, mobileEnabled: true };
    default:
      return { openEnabled: true, mpEnabled: false, mobileEnabled: false };
  }
}

export function deriveWeChatConnectStoredMode(
  openEnabled: boolean,
  mpEnabled: boolean,
  mobileEnabled: boolean,
  legacyMode: unknown,
): WeChatConnectMode {
  if (mpEnabled) return "mp";
  if (mobileEnabled) return "mobile";
  if (openEnabled) return "open";
  return normalizeWeChatConnectMode(legacyMode);
}

