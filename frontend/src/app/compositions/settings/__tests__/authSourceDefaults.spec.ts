import { describe, expect, it } from 'vitest'
import type { UpdateSettingsRequest, DefaultPlatformQuotasMap } from '@/features/system/api'
import { listPlatformIds } from '@/features/catalog'
import { createAuthSourceDefaultsServices } from '../authSourceDefaults'
const { build: buildAuthSourceDefaultsState, append: appendAuthSourceDefaultsToUpdateRequest } = createAuthSourceDefaultsServices(listPlatformIds)

/** 与后端 AllowedQuotaPlatforms 一致的全部具体平台（平台清单）。 */
const quotaPlatforms = [
  "anthropic", "openai", "gemini", "antigravity", "grok",
  "kimi", "zhipu", "deepseek", "minimax", "opencode_go", "typesafe", "command_code", "cline",
];

/** 全部平台全 null 的 map，用于断言归一化默认值 */
const allNullQuotas: DefaultPlatformQuotasMap = Object.fromEntries(
  quotaPlatforms.map((platform) => [platform, { daily: null, weekly: null, monthly: null }]),
)

describe("admin settings auth source defaults helpers", () => {
  it("builds auth source defaults state from flat settings fields", () => {
    const state = buildAuthSourceDefaultsState({
      auth_source_default_email_balance: 9.5,
      auth_source_default_email_concurrency: 3,
      auth_source_default_email_subscriptions: [
        { group_id: 1, validity_days: 30 },
      ],
      auth_source_default_email_grant_on_signup: false,
      auth_source_default_email_grant_on_first_bind: true,
      auth_source_default_linuxdo_balance: 6,
      auth_source_default_linuxdo_concurrency: 8,
      auth_source_default_linuxdo_subscriptions: [
        { group_id: 2, validity_days: 60 },
      ],
      auth_source_default_linuxdo_grant_on_signup: true,
      auth_source_default_linuxdo_grant_on_first_bind: false,
    });

    expect(state.email).toEqual({
      balance: 9.5,
      concurrency: 3,
      subscriptions: [{ group_id: 1, validity_days: 30 }],
      grant_on_signup: false,
      grant_on_first_bind: true,
      platform_quotas: allNullQuotas,
    });
    expect(state.linuxdo).toEqual({
      balance: 6,
      concurrency: 8,
      subscriptions: [{ group_id: 2, validity_days: 60 }],
      grant_on_signup: true,
      grant_on_first_bind: false,
      platform_quotas: allNullQuotas,
    });
    expect(state.oidc).toEqual({
      balance: 0,
      concurrency: 5,
      subscriptions: [],
      grant_on_signup: false,
      grant_on_first_bind: false,
      platform_quotas: allNullQuotas,
    });
    expect(state.wechat).toEqual({
      balance: 0,
      concurrency: 5,
      subscriptions: [],
      grant_on_signup: false,
      grant_on_first_bind: false,
      platform_quotas: allNullQuotas,
    });
  });

  it("defaults grant-on-signup to disabled when settings are missing", () => {
    const state = buildAuthSourceDefaultsState({});

    expect(state.email.grant_on_signup).toBe(false);
    expect(state.linuxdo.grant_on_signup).toBe(false);
    expect(state.oidc.grant_on_signup).toBe(false);
    expect(state.wechat.grant_on_signup).toBe(false);
  });

  it("reads nested platform_quotas from settings into auth source state", () => {
    const state = buildAuthSourceDefaultsState({
      auth_source_default_email_platform_quotas: {
        anthropic: { daily: 10, weekly: 50, monthly: 200 },
        openai:    { daily: null, weekly: null, monthly: null },

      } as DefaultPlatformQuotasMap,
    });

    // anthropic 填写的值应被保留
    expect(state.email.platform_quotas.anthropic).toEqual({ daily: 10, weekly: 50, monthly: 200 });
    // openai 全 null 应被保留
    expect(state.email.platform_quotas.openai).toEqual({ daily: null, weekly: null, monthly: null });
    // 未出现的平台（gemini/antigravity）归一化为 null
    expect(state.email.platform_quotas.gemini).toEqual({ daily: null, weekly: null, monthly: null });
    expect(state.email.platform_quotas.antigravity).toEqual({ daily: null, weekly: null, monthly: null });
  });

  it("appends auth source defaults back onto update payload", () => {
    const payload: UpdateSettingsRequest = { revision: "revision",
      site_name: "Sub2API",
    };

    appendAuthSourceDefaultsToUpdateRequest(payload, {
      email: {
        balance: 1.25,
        concurrency: 2,
        subscriptions: [{ group_id: 3, validity_days: 7 }],
        grant_on_signup: true,
        grant_on_first_bind: false,
        platform_quotas: {},
      },
      linuxdo: {
        balance: 0,
        concurrency: 6,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: true,
        platform_quotas: {},
      },
      oidc: {
        balance: 4,
        concurrency: 9,
        subscriptions: [{ group_id: 9, validity_days: 90 }],
        grant_on_signup: true,
        grant_on_first_bind: true,
        platform_quotas: {},
      },
      wechat: {
        balance: 2,
        concurrency: 5,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: false,
        platform_quotas: {},
      },
      github: {
        balance: 0,
        concurrency: 5,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: false,
        platform_quotas: {},
      },
      google: {
        balance: 0,
        concurrency: 5,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: false,
        platform_quotas: {},
      },
      dingtalk: {
        balance: 0,
        concurrency: 5,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: false,
        platform_quotas: {},
      },
    });

    expect(payload).toMatchObject({
      site_name: "Sub2API",
      auth_source_default_email_balance: 1.25,
      auth_source_default_email_concurrency: 2,
      auth_source_default_email_subscriptions: [
        { group_id: 3, validity_days: 7 },
      ],
      auth_source_default_email_grant_on_signup: true,
      auth_source_default_email_grant_on_first_bind: false,
      auth_source_default_linuxdo_balance: 0,
      auth_source_default_linuxdo_concurrency: 6,
      auth_source_default_linuxdo_subscriptions: [],
      auth_source_default_linuxdo_grant_on_signup: false,
      auth_source_default_linuxdo_grant_on_first_bind: true,
      auth_source_default_oidc_balance: 4,
      auth_source_default_oidc_concurrency: 9,
      auth_source_default_oidc_subscriptions: [
        { group_id: 9, validity_days: 90 },
      ],
      auth_source_default_oidc_grant_on_signup: true,
      auth_source_default_oidc_grant_on_first_bind: true,
      auth_source_default_wechat_balance: 2,
      auth_source_default_wechat_concurrency: 5,
      auth_source_default_wechat_subscriptions: [],
      auth_source_default_wechat_grant_on_signup: false,
      auth_source_default_wechat_grant_on_first_bind: false,
      // 嵌套 platform_quotas 字段
      auth_source_default_email_platform_quotas: allNullQuotas,
      auth_source_default_linuxdo_platform_quotas: allNullQuotas,
      auth_source_default_oidc_platform_quotas: allNullQuotas,
      auth_source_default_wechat_platform_quotas: allNullQuotas,
      auth_source_default_github_platform_quotas: allNullQuotas,
      auth_source_default_google_platform_quotas: allNullQuotas,
      auth_source_default_dingtalk_platform_quotas: allNullQuotas,
    });
  });

  it("appends sanitized nested platform_quotas with non-null values in update payload", () => {
    const payload: UpdateSettingsRequest = { revision: "revision" };
    appendAuthSourceDefaultsToUpdateRequest(payload, {
      email: {
        balance: 0,
        concurrency: 5,
        subscriptions: [],
        grant_on_signup: false,
        grant_on_first_bind: false,
        platform_quotas: {
          anthropic: { daily: 10, weekly: 50, monthly: 200 },
          openai:    { daily: 0, weekly: null, monthly: null },
        },
      },
      linuxdo: { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
      oidc:    { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
      wechat:  { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
      github:  { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
      google:  { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
      dingtalk: { balance: 0, concurrency: 5, subscriptions: [], grant_on_signup: false, grant_on_first_bind: false, platform_quotas: {} },
    });

    const emailQuotas = payload.auth_source_default_email_platform_quotas;
    if (!emailQuotas) throw new Error('Expected the command to contain email platform quotas');
    expect(emailQuotas.anthropic).toEqual({ daily: 10, weekly: 50, monthly: 200 });
    // 0 是合法值（不限额=0 与"不设"不同，保留）
    expect(emailQuotas.openai?.daily).toBe(0);
    // 缺失平台归一化为全 null
    expect(emailQuotas.gemini).toEqual({ daily: null, weekly: null, monthly: null });
    expect(emailQuotas.antigravity).toEqual({ daily: null, weekly: null, monthly: null });
  });
});

