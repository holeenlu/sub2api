# 15 · 客户端配置、下载包与恢复工具

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(clients): preserve setup downloads and recovery tools`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. API Key 使用说明包含 Codex/OpenCode 等当前支持的客户端配置。Codex 目录来源服从该 Key 可见模型与服务端顺序；保留自动获取、手动刷新、可用模型默认值及推理强度选择。

2. 生成的 Codex 配置、认证文件和模型目录按当前 UI 提供复制/下载；字段处于正确 TOML 层级，目录未就绪、失败或为空时采用明确的既有行为，不捏造模型能力。原生搜索配置继续使用当前客户端规则。

3. CC-Switch 导入入口及对应可见设置和失效工具代码已移除，不能因重排旧提交再带回。存在于上游的模型目录服务不计为新造目录治理系统。

4. Claude/Codex 会话修复、恢复脚本、图片技能和安装脚本的源码、压缩包和 SHA256 清单配套保留；三个品牌下载名、文档示例及供应商标识维持各自最终版本。

5. 密钥列表返回后立即显示，统计独立填入并显示加载缺失状态；翻页/卸载取消旧统计请求。用户 Key 的鉴权、并发与额度继续由原后端控制。

下载源码、zip 包、SHA256SUMS 与安装文档作为一个交付单元保存；按最终分支的 blob 和 mode 比对，不重新压缩从而制造二进制差异。官方原有客户端功能不重复算作本地新增。两品牌命名和默认服务地址在各自唯一品牌提交中保留。

## 入口与配置归属

/keys 使用说明与下载；frontend/public/downloads、frontend/public/install；tools/build_docs_downloads.py。

## 最终实现方案

1. Key 可见目录、顺序与能力决定生成配置；TOML 层级、认证文件、默认模型与 effort 使用现有生成逻辑。

2. 修复脚本、图片技能、zip 与 SHA256 清单对应同一品牌最终版本；删除的 CC-Switch 入口保持删除。

3. Key 列表不等待统计，统计失败显式缺失；实际鉴权/额度/并发仍由原生后端控制。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

客户端配置解析、文档例子、downloads --check 与 SHA256；各品牌分别验证包。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 3 | 27 | 22 | 0 |
| generated | 22 | 1934 | 0 | 4 |
| source | 9 | 430 | 344 | 0 |
| test | 11 | 1360 | 182 | 0 |
| tooling | 2 | 67 | 3 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 4 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | documentation | 11 | 11 | 15 | [backend/internal/pkg/openai/instructions_gpt6_astra.txt](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/openai/instructions_gpt6_astra.txt) |
| A | generated | 4 | 0 | 15 | [frontend/public/downloads/SHA256SUMS.txt](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/SHA256SUMS.txt) |
| A | generated | - | - | 15 | [frontend/public/downloads/gpt-image-flare.zip](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/gpt-image-flare.zip) |
| A | generated | - | - | 15 | [frontend/public/downloads/gpt-image-sunburst.zip](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/gpt-image-sunburst.zip) |
| A | generated | - | - | 15 | [frontend/public/downloads/kdan-claude-session-recovery.zip](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-claude-session-recovery.zip) |
| A | generated | 22 | 0 | 15 | [frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1) |
| A | generated | 60 | 0 | 15 | [frontend/public/downloads/kdan-claude-session-recovery/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-claude-session-recovery/README.md) |
| A | generated | 11 | 0 | 15 | [frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh) |
| A | generated | 213 | 0 | 15 | [frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py) |
| A | generated | - | - | 15 | [frontend/public/downloads/kdan-codex-session-repair.zip](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair.zip) |
| A | generated | 78 | 0 | 15 | [frontend/public/downloads/kdan-codex-session-repair/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair/README.md) |
| A | generated | 40 | 0 | 15 | [frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1) |
| A | generated | 11 | 0 | 15 | [frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh) |
| A | generated | 714 | 0 | 15 | [frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py) |
| A | generated | 125 | 0 | 15 | [frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py) |
| A | generated | 38 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md) |
| A | generated | 4 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml) |
| A | generated | 1 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt) |
| A | generated | 285 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py) |
| A | generated | 38 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md) |
| A | generated | 4 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml) |
| A | generated | 1 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt) |
| A | generated | 285 | 0 | 15 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py) |
| A | source | 11 | 0 | 15 | [frontend/public/install/README.md](https://github.com/holeenlu/sub2api/blob/main/frontend/public/install/README.md) |
| A | source | 23 | 0 | 15 | [frontend/public/install/claude-code.sh](https://github.com/holeenlu/sub2api/blob/main/frontend/public/install/claude-code.sh) |
| A | source | 25 | 0 | 15 | [frontend/public/install/codex.sh](https://github.com/holeenlu/sub2api/blob/main/frontend/public/install/codex.sh) |
| A | source | 138 | 0 | 15 | [frontend/public/install/update-codex-models.py](https://github.com/holeenlu/sub2api/blob/main/frontend/public/install/update-codex-models.py) |
| M | source | 167 | 101 | 15 | [frontend/src/components/keys/UseKeyModal.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/keys/UseKeyModal.vue) |
| M | test | 205 | 21 | 15 | [frontend/src/components/keys/__tests__/UseKeyModal.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/keys/__tests__/UseKeyModal.spec.ts) |
| D | test | 0 | 22 | 15 | `frontend/src/utils/__tests__/ccswitchImport.antigravityUrl.spec.ts` |
| D | test | 0 | 138 | 15 | `frontend/src/utils/__tests__/ccswitchImport.spec.ts` |
| M | test | 16 | 0 | 15 | [frontend/src/utils/__tests__/codexCatalogConfig.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/__tests__/codexCatalogConfig.spec.ts) |
| D | source | 0 | 115 | 15 | `frontend/src/utils/ccswitchImport.ts` |
| M | source | 16 | 0 | 15 | [frontend/src/utils/codexCatalogConfig.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/utils/codexCatalogConfig.ts) |
| M | source | 6 | 122 | 15 | [frontend/src/views/user/KeysView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/KeysView.vue) |
| M | test | 35 | 1 | 15 | [frontend/src/views/user/__tests__/KeysView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/user/__tests__/KeysView.spec.ts) |
| M | documentation | 7 | 7 | 15 | [skills/sub2api-admin/SKILL.md](https://github.com/holeenlu/sub2api/blob/main/skills/sub2api-admin/SKILL.md) |
| M | tooling | 3 | 3 | 15 | [skills/sub2api-admin/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/main/skills/sub2api-admin/agents/openai.yaml) |
| M | documentation | 9 | 4 | 15 | [skills/sub2api-admin/references/admin-cli.md](https://github.com/holeenlu/sub2api/blob/main/skills/sub2api-admin/references/admin-cli.md) |
| M | source | 44 | 6 | 15 | [skills/sub2api-admin/scripts/sub2api-admin.js](https://github.com/holeenlu/sub2api/blob/main/skills/sub2api-admin/scripts/sub2api-admin.js) |
| A | tooling | 64 | 0 | 15 | [tools/build_docs_downloads.py](https://github.com/holeenlu/sub2api/blob/main/tools/build_docs_downloads.py) |
| A | test | 90 | 0 | 15 | [tools/tests/test_claude_session_recovery.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_claude_session_recovery.py) |
| A | test | 78 | 0 | 15 | [tools/tests/test_codex_catalog_updater.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_codex_catalog_updater.py) |
| A | test | 101 | 0 | 15 | [tools/tests/test_docs_downloads.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_docs_downloads.py) |
| A | test | 258 | 0 | 15 | [tools/tests/test_image_skills.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_image_skills.py) |
| A | test | 137 | 0 | 15 | [tools/tests/test_session_recovery_commands.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_session_recovery_commands.py) |
| A | test | 440 | 0 | 15 | [tools/tests/test_session_repair.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_session_repair.py) |
