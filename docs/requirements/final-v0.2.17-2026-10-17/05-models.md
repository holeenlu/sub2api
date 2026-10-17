# 05 · 模型权限、分组候选与目录顺序

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`fix(models): preserve restrictions catalogs and group selection`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. Anthropic OAuth、OAuth token 与 Setup Token 账号可以编辑模型限制；限制规则与原生授权、调度资格一致，不能以 UI 候选代替服务端准入。
2. 账号创建默认值、编辑候选、分组内置目录和批量最新模型同步复用后端目录。移除已经被统一目录替代的前端列表，simple mode 沿用 id=0 候选入口。
3. 分组和账号候选包含经来源核对的各平台模型；保留已保存规则、显式映射和人工输入。候选改名不成为运行时隐式模型别名。
4. 批量同步只添加各目标共同支持的模型。目录请求失败保留草稿，切换平台后丢弃旧响应，不用空列表覆盖用户选择。
5. 所有 Codex discovery 模式服从保存的分组模型顺序；排序后生成 ETag，Key 使用说明、模型目录下载和缓存保持一致。
6. BPS 的映射后模型权限、冷却与过滤发现由第 07 组补充，最终调度仍复用原规则。

官方 v0.2.17 已包含的 GPT-5.5 订阅退役、Gemini 4 Argon 及提供方模型目录行为属于基线；本地清单只统计相对该基线的差异。Responses 选号诊断使用实际映射后的 forward model，客户端错误仍保留请求模型别名，避免诊断误报 model_not_found。

## 入口与配置归属

沿用下列方案和完整文件清单中的现有入口。

## 最终实现方案

1. 后端内置目录与既有模型同步入口承载候选、默认值和批量交集；移除重复前端规则，保存值/显式映射保持。

2. 所有 discovery 分支先应用授权和保存顺序，再计算 ETag；客户端配置沿既有下载链消费结果。

3. 表单、列表、共享 DTO/模型服务可能同时被其他功能修改；以跨组关联表定位最终共同实现，不做重复补丁。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

模型限制组件、simple mode 候选、批量同步失败/过期响应、Codex 顺序与 ETag 测试。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 1 | 555 | 0 | 0 |
| source | 2 | 80 | 384 | 0 |
| test | 8 | 299 | 150 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 16 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | test | 10 | 0 | 05 | [backend/internal/handler/admin/group_handler_simple_mode_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/group_handler_simple_mode_test.go) |
| M | test | 3 | 3 | 05 | [backend/internal/pkg/claude/constants_model_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/claude/constants_model_test.go) |
| M | test | 83 | 11 | 05 | [backend/internal/service/admin_service_composite_group_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_service_composite_group_test.go) |
| M | test | 56 | 0 | 05 | [backend/internal/service/openai_codex_models_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_codex_models_service_test.go) |
| A | documentation | 555 | 0 | 05 | [docs/GROUP_MODEL_CANDIDATES.md](https://github.com/holeenlu/sub2api/blob/main/docs/GROUP_MODEL_CANDIDATES.md) |
| M | source | 73 | 24 | 05 | [frontend/src/components/account/ModelWhitelistSelector.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/ModelWhitelistSelector.vue) |
| M | test | 97 | 2 | 05 | [frontend/src/components/account/__tests__/ModelWhitelistSelector.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/account/__tests__/ModelWhitelistSelector.spec.ts) |
| M | test | 4 | 130 | 05 | [frontend/src/composables/__tests__/useModelWhitelist.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/__tests__/useModelWhitelist.spec.ts) |
| M | source | 7 | 360 | 05,19 | [frontend/src/composables/useModelWhitelist.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/composables/useModelWhitelist.ts) |
| M | test | 34 | 3 | 05 | [frontend/src/views/admin/__tests__/ChannelsView.modelSync.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/ChannelsView.modelSync.spec.ts) |
| M | test | 12 | 1 | 05 | [frontend/src/views/admin/__tests__/groupModelAllowlist.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/__tests__/groupModelAllowlist.spec.ts) |
