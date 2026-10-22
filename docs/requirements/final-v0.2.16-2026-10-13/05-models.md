# 05 · 账号模型权限、分组候选与目录顺序

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. Anthropic OAuth、OAuth token 与 Setup Token 账号可以编辑模型限制；限制规则与原生授权、调度资格一致，不能以 UI 候选代替服务端准入。
2. 账号创建默认值、编辑候选、分组内置目录和批量最新模型同步复用后端目录。移除已经被统一目录替代的前端列表，simple mode 沿用 id=0 候选入口。
3. 分组和账号候选包含经来源核对的各平台模型；保留已保存规则、显式映射和人工输入。候选改名不成为运行时隐式模型别名。
4. 批量同步只添加各目标共同支持的模型。目录请求失败保留草稿，切换平台后丢弃旧响应，不用空列表覆盖用户选择。
5. 所有 Codex discovery 模式服从保存的分组模型顺序；排序后生成 ETag，Key 使用说明、模型目录下载和缓存保持一致。
6. BPS 的映射后模型权限、冷却与过滤发现由第 07 组补充，最终调度仍复用原规则。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. 后端内置目录与既有模型同步入口承载候选、默认值和批量交集；移除重复前端规则，保存值/显式映射保持。

2. 所有 discovery 分支先应用授权和保存顺序，再计算 ETag；客户端配置沿既有下载链消费结果。

3. 表单、列表、共享 DTO/模型服务可能同时被其他功能修改；以跨组关联表定位最终共同实现，不做重复补丁。

## 边界、失败路径与依赖

关联功能组：16。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

模型限制组件、simple mode 候选、批量同步失败/过期响应、Codex 顺序与 ETag 测试。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| documentation | 1 | 555 | 0 | 0 |
| source | 2 | 80 | 356 | 0 |
| test | 8 | 267 | 140 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `3bd5cd406dd37b332f01a446718caca3a8a4222f`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `bb7c4d0a169b5ba3dbd5aa3cec957d52170d0265`
- `be0749922538fe576b5e7329609e4f1d7716699f`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | test | 10 | 0 | 05 | [backend/internal/handler/admin/group_handler_simple_mode_test.go](../../../backend/internal/handler/admin/group_handler_simple_mode_test.go) |
| M | test | 3 | 3 | 05 | [backend/internal/pkg/claude/constants_model_test.go](../../../backend/internal/pkg/claude/constants_model_test.go) |
| M | test | 83 | 11 | 05 | [backend/internal/service/admin_service_composite_group_test.go](../../../backend/internal/service/admin_service_composite_group_test.go) |
| M | test | 56 | 0 | 05 | [backend/internal/service/openai_codex_models_service_test.go](../../../backend/internal/service/openai_codex_models_service_test.go) |
| A | documentation | 555 | 0 | 05 | [docs/GROUP_MODEL_CANDIDATES.md](../../../docs/GROUP_MODEL_CANDIDATES.md) |
| M | source | 73 | 24 | 05 | [frontend/src/components/account/ModelWhitelistSelector.vue](../../../frontend/src/components/account/ModelWhitelistSelector.vue) |
| M | test | 97 | 2 | 05 | [frontend/src/components/account/__tests__/ModelWhitelistSelector.spec.ts](../../../frontend/src/components/account/__tests__/ModelWhitelistSelector.spec.ts) |
| M | test | 3 | 123 | 05 | [frontend/src/composables/__tests__/useModelWhitelist.spec.ts](../../../frontend/src/composables/__tests__/useModelWhitelist.spec.ts) |
| M | source | 7 | 332 | 05,16 | [frontend/src/composables/useModelWhitelist.ts](../../../frontend/src/composables/useModelWhitelist.ts) |
| M | test | 3 | 0 | 05 | [frontend/src/views/admin/__tests__/ChannelsView.modelSync.spec.ts](../../../frontend/src/views/admin/__tests__/ChannelsView.modelSync.spec.ts) |
| M | test | 12 | 1 | 05 | [frontend/src/views/admin/__tests__/groupModelAllowlist.spec.ts](../../../frontend/src/views/admin/__tests__/groupModelAllowlist.spec.ts) |

## 关联实现（由其他组提交）

这些最终文件同时被本需求涉及，但按主职责只落在下列其他提交中。主归属不是排他功能边界；完整对应关系在 files-main.tsv。

| 主归属组 | 类别 | 文件 |
| --- | --- | --- |
| 15 | tooling | [.gitignore](../../../.gitignore) |
| 02 | source | [backend/internal/handler/admin/group_handler.go](../../../backend/internal/handler/admin/group_handler.go) |
| 02 | source | [backend/internal/service/admin_group.go](../../../backend/internal/service/admin_group.go) |
| 16 | source | [backend/internal/service/openai_codex_models_service.go](../../../backend/internal/service/openai_codex_models_service.go) |
| 16 | source | [backend/internal/service/openai_models_list.go](../../../backend/internal/service/openai_models_list.go) |
| 04 | source | [frontend/src/components/account/BulkEditAccountModal.vue](../../../frontend/src/components/account/BulkEditAccountModal.vue) |
| 04 | source | [frontend/src/components/account/CreateAccountModal.vue](../../../frontend/src/components/account/CreateAccountModal.vue) |
| 04 | source | [frontend/src/components/account/EditAccountModal.vue](../../../frontend/src/components/account/EditAccountModal.vue) |
| 04 | test | [frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/BulkEditAccountModal.spec.ts) |
| 04 | test | [frontend/src/components/account/__tests__/CreateAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/CreateAccountModal.spec.ts) |
| 04 | test | [frontend/src/components/account/__tests__/EditAccountModal.spec.ts](../../../frontend/src/components/account/__tests__/EditAccountModal.spec.ts) |
| 14 | source | [frontend/src/i18n/locales/en/admin/accounts.ts](../../../frontend/src/i18n/locales/en/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/en/admin/overview.ts](../../../frontend/src/i18n/locales/en/admin/overview.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/admin/accounts.ts](../../../frontend/src/i18n/locales/ja/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/ja/admin/overview.ts](../../../frontend/src/i18n/locales/ja/admin/overview.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](../../../frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| 14 | generated | [frontend/src/i18n/locales/zh-TW/admin/overview.ts](../../../frontend/src/i18n/locales/zh-TW/admin/overview.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/admin/accounts.ts](../../../frontend/src/i18n/locales/zh/admin/accounts.ts) |
| 14 | source | [frontend/src/i18n/locales/zh/admin/overview.ts](../../../frontend/src/i18n/locales/zh/admin/overview.ts) |
