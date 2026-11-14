import { peakRatePresentation } from '@/features/pricing'
import { flushPromises, mount } from "@vue/test-utils";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { defineComponent, h, ref } from "vue";

import CodexManifestAccountsField from "../CodexManifestAccountsField.vue";

import type { CodexModelsManifestConfig } from "@/features/access-policy/api/groupTypes";

vi.mock("vue-i18n", async () => {
  const actual = await vi.importActual<typeof import("vue-i18n")>("vue-i18n");
  return {
    ...actual,
    useI18n: () => ({ t: (key: string) => key }),
  };
});

const accountsApiFixture = vi.hoisted(() => ({
      list: vi.fn(),
    }))
const usersApiFixture = vi.hoisted(() => ({ list: vi.fn(() => { throw new Error('Unexpected users.list request in this test') }) }))
const proxiesApiFixture = vi.hoisted(() => ({ getAll: vi.fn(() => { throw new Error('Unexpected proxies.getAll request in this test') }) }))
const settingsApiFixture = vi.hoisted(() => ({ getSettings: vi.fn(() => { throw new Error('Unexpected settings.getSettings request in this test') }), updateSettings: vi.fn(() => { throw new Error('Unexpected settings.updateSettings request in this test') }) }))
vi.mock('@/features/resources/accounts', async (importOriginal) => {
  const actual = await importOriginal<typeof import('@/features/resources/accounts')>()
  return { ...actual, default: accountsApiFixture }
});

const disabledConfig = (): CodexModelsManifestConfig => ({
  enabled: false,
  account_ids: [],
  fallback_to_scheduler: false,
});

const enabledConfig = (): CodexModelsManifestConfig => ({
  enabled: true,
  account_ids: [],
  fallback_to_scheduler: false,
});

// 挂载一个 v-model 双向绑定的宿主，验证完整的选择→移除流程。
const mountInteractive = (initial: CodexModelsManifestConfig, accountNames?: Record<number, string>) => {
  const host = defineComponent({
    setup() {
      const config = ref(initial);
      return () =>
        h(CodexManifestAccountsField, {
          groupId: 7,
          modelValue: config.value,
          accountNames,
          "onUpdate:modelValue": (value: CodexModelsManifestConfig) => {
            config.value = value;
          },
        });
    },
  });
  return mount(host, {
    global: {
      stubs: {
        Icon: { props: ["name"], template: '<i :data-icon="name" />' },
      },
    },
  });
};

const mountField = (modelValue: CodexModelsManifestConfig, accountNames?: Record<number, string>) =>
  mount(CodexManifestAccountsField, {
    props: {
      groupId: 7,
      modelValue,
      ...(accountNames ? { accountNames } : {}),
    },
    global: {
      stubs: {
        Icon: { props: ["name"], template: '<i :data-icon="name" />' },
      },
    },
  });

describe("CodexManifestAccountsField", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.clearAllMocks();
  });

  it("hides account controls while disabled and reveals them after enabling", async () => {
    const wrapper = mountField(disabledConfig());

    expect(wrapper.find('[data-testid="codex-manifest-search"]').exists()).toBe(false);
    expect(wrapper.find('[data-testid="codex-manifest-fallback-toggle"]').exists()).toBe(false);
    expect(wrapper.text()).toContain("admin.groups.codexModelsManifest.disabledHint");

    await wrapper.find('[data-testid="codex-manifest-toggle"]').trigger("click");

    const emitted = wrapper.emitted("update:modelValue");
    expect(emitted).toHaveLength(1);
    expect(emitted?.[0]?.[0]).toMatchObject({ enabled: true });

    const enabled = mountField(enabledConfig());
    expect(enabled.find('[data-testid="codex-manifest-search"]').exists()).toBe(true);
    expect(enabled.find('[data-testid="codex-manifest-fallback-toggle"]').exists()).toBe(true);
    expect(enabled.text()).toContain("admin.groups.codexModelsManifest.enabledHint");
  });

  it("selects accounts from search results and removes them via tags", async () => {
    vi.mocked(accountsApiFixture.list).mockResolvedValue({
      items: [
        { id: 5, name: "oauth-five" },
        { id: 6, name: "apikey-six" },
      ],
    } as never);
    const wrapper = mountInteractive(
      { ...enabledConfig(), account_ids: [5] },
      { 5: "oauth-five" },
    );

    const initialTags = wrapper.find('[data-testid="codex-manifest-selected-tags"]');
    expect(initialTags.text()).toContain("oauth-five");

    const search = wrapper.find('[data-testid="codex-manifest-search"]');
    await search.trigger("focus");
    await search.setValue("oauth");
    vi.advanceTimersByTime(300);
    await flushPromises();

    expect(accountsApiFixture.list).toHaveBeenCalledWith(
      1,
      20,
      { search: "oauth", platform: "openai", group: "7" },
      expect.anything(),
    );

    const dropdown = wrapper.find('[data-testid="codex-manifest-dropdown"]');
    expect(dropdown.exists()).toBe(true);
    expect(dropdown.text()).toContain("apikey-six");

    // 账号 6 未选中：点击加入。
    const option = dropdown.findAll("button").find((b) => b.text().includes("apikey-six"));
    await option!.trigger("click");
    await flushPromises();

    expect(wrapper.text()).toContain("apikey-six", "新选账号立即以名称展示");

    // 移除账号 5：点击其标签上的删除按钮。
    const removeButton = wrapper
      .find('[data-testid="codex-manifest-selected-tags"]')
      .findAll("button")
      .find((b) => b.attributes("aria-label") === "remove account 5");
    await removeButton!.trigger("click");
    await flushPromises();

    const tags = wrapper.find('[data-testid="codex-manifest-selected-tags"]');
    expect(tags.text()).toContain("apikey-six");
    expect(tags.text()).not.toContain("oauth-five");
  });

  it("fails validation when enabled without accounts", async () => {
    const wrapper = mountField(enabledConfig());

    expect(wrapper.vm.validate()).toBe(false);
    await flushPromises();
    expect(
      wrapper.find('[data-testid="codex-manifest-validation-error"]').text(),
    ).toBe("admin.groups.codexModelsManifest.selectAtLeastOne");

    // GroupsView 关闭对话框时通过可选链调用 resetValidation，必须真实存在且生效。
    expect(typeof wrapper.vm.resetValidation).toBe("function");
    wrapper.vm.resetValidation();
    await flushPromises();
    expect(
      wrapper.find('[data-testid="codex-manifest-validation-error"]').exists(),
    ).toBe(false);

    await wrapper.setProps({
      modelValue: { ...enabledConfig(), account_ids: [9] },
    });
    expect(wrapper.vm.validate()).toBe(true);
    await flushPromises();
    expect(
      wrapper.find('[data-testid="codex-manifest-validation-error"]').exists(),
    ).toBe(false);
  });

  it("closes the dropdown on outside click and shows the empty state", async () => {
    vi.mocked(accountsApiFixture.list).mockResolvedValue({ items: [] } as never);
    const wrapper = mountField(enabledConfig());

    const search = wrapper.find('[data-testid="codex-manifest-search"]');
    await search.trigger("focus");
    await search.setValue("nonexistent");
    vi.advanceTimersByTime(300);
    await flushPromises();

    // 有关键词但无结果：展示空状态提示而非静默无反应。
    const dropdown = wrapper.find('[data-testid="codex-manifest-dropdown"]');
    expect(dropdown.exists()).toBe(true);
    expect(wrapper.find('[data-testid="codex-manifest-search-empty"]').text()).toBe(
      "admin.groups.codexModelsManifest.searchEmpty",
    );

    // 点击组件外部：收起下拉。
    document.body.dispatchEvent(new MouseEvent("click", { bubbles: true }));
    await flushPromises();
    expect(wrapper.find('[data-testid="codex-manifest-dropdown"]').exists()).toBe(false);

    // 点击输入框内部：不收起。
    await search.trigger("click");
    await search.trigger("focus");
    expect(wrapper.find('[data-testid="codex-manifest-dropdown"]').exists()).toBe(true);
    wrapper.unmount();
  });
});


import { config as testConfig } from '@vue/test-utils'
import { ref as testRef, computed as testComputed } from 'vue'
import { groupPresentationKey } from '@/features/access-policy/presentation/context'
import { platformLabel } from '@/features/catalog'

import { useAppStore as mockedApp } from '@/app/model/app'
import { accessPolicyScopeKey, accessPolicyNotificationsKey, accessPolicyPermissionsKey, accessPolicyAccountsKey, accessPolicyUsersKey, riskControlProxiesKey, riskControlSettingsKey, accessPolicyModelSelectorKey } from '@/features/access-policy/services'


const permissionFallback = { isSuperAdmin: true, isSimpleMode: false, can: () => true, canEditAdminField: () => true, editableAdminFields: (_schema: string, payload: object) => payload }
const mockedPermissions = new Proxy(permissionFallback, { get(target, key) { return readPermissions()[key as keyof typeof target] ?? target[key as keyof typeof target] } })
Object.assign(testConfig.global.provide, {
 [groupPresentationKey as symbol]: { peakRate: peakRatePresentation, platformLabel, serverUtcOffset: testRef(undefined), isSimpleMode: testComputed(() => mockedPermissions.isSimpleMode) },
 [accessPolicyScopeKey as symbol]: testRef('session:admin:test'),
 [accessPolicyNotificationsKey as symbol]: new Proxy({}, { get(_target, key) { return mockedApp()[key as 'showError' | 'showSuccess'] } }),
 [accessPolicyPermissionsKey as symbol]: mockedPermissions,
 [accessPolicyAccountsKey as symbol]: { search: async (keyword: string, params: object, signal: AbortSignal) => (await accountsApiFixture.list(1, 20, { search: keyword, ...params }, { signal })).items, getById: (id: number) => accountsApiFixture.getById(id) },
 [accessPolicyUsersKey as symbol]: { search: async (keyword: string) => (await usersApiFixture.list(1, 10, { search: keyword })).items },
 [riskControlProxiesKey as symbol]: () => proxiesApiFixture.getAll(),
 [riskControlSettingsKey as symbol]: { enable: async () => { const current = await settingsApiFixture.getSettings(); await settingsApiFixture.updateSettings({ risk_control_enabled: true, revision: current.revision || '' }) } },
 [accessPolicyModelSelectorKey as symbol]: { name: 'ModelWhitelistSelector', props: ['modelValue'], emits: ['update:modelValue'], template: '<div />' }
})
function readPermissions() { return permissionFallback }
