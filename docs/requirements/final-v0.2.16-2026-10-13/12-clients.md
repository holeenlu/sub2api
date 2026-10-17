# 12 · 客户端配置、模型目录下载与修复工具

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。

## 最终需求

1. API Key 使用说明包含 Codex/OpenCode 等当前支持的客户端配置。Codex 目录来源服从该 Key 可见模型与服务端顺序；保留自动获取、手动刷新、可用模型默认值及推理强度选择。

2. 生成的 Codex 配置、认证文件和模型目录按当前 UI 提供复制/下载；字段处于正确 TOML 层级，目录未就绪、失败或为空时采用明确的既有行为，不捏造模型能力。原生搜索配置继续使用当前客户端规则。

3. CC-Switch 导入入口及对应可见设置和失效工具代码已移除，不能因重排旧提交再带回。存在于上游的模型目录服务不计为新造目录治理系统。

4. Claude/Codex 会话修复、恢复脚本、图片技能和安装脚本的源码、压缩包和 SHA256 清单配套保留；三个品牌下载名、文档示例及供应商标识维持各自最终版本。

5. 密钥列表返回后立即显示，统计独立填入并显示加载缺失状态；翻页/卸载取消旧统计请求。用户 Key 的鉴权、并发与额度继续由原后端控制。

## 入口与配置归属

/keys 使用说明与下载；frontend/public/downloads、frontend/public/install；tools/build_docs_downloads.py。

## 最终实现方案

1. Key 可见目录、顺序与能力决定生成配置；TOML 层级、认证文件、默认模型与 effort 使用现有生成逻辑。

2. 修复脚本、图片技能、zip 与 SHA256 清单对应同一品牌最终版本；删除的 CC-Switch 入口保持删除。

3. Key 列表不等待统计，统计失败显式缺失；实际鉴权/额度/并发仍由原生后端控制。

## 边界、失败路径与依赖

没有额外文件归属组关联。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。上面描述现有最终行为，本次不重新实现业务。

## 验收要求

客户端配置解析、文档例子、downloads --check 与 SHA256；各品牌分别验证包。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | --- | --- | --- | --- |
| asset | 4 | 0 | 0 | 4 |
| documentation | 11 | 258 | 22 | 0 |
| source | 17 | 2125 | 344 | 0 |
| test | 11 | 1360 | 182 | 0 |
| tooling | 4 | 75 | 3 | 0 |

这是相对 v0.2.16 的累计定制差异，不是本次新写代码。删除/新增重命名分别计数；生成代码与测试单列。

## 来源提交

- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `d75e372d49d61ba00e2afb6f7a77f090eda0d694`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | --- | --- | --- | --- |
| M | documentation | 11 | 11 | 12 | [backend/internal/pkg/openai/instructions_gpt6_astra.txt](../../../backend/internal/pkg/openai/instructions_gpt6_astra.txt) |
| A | documentation | 4 | 0 | 12 | [frontend/public/downloads/SHA256SUMS.txt](../../../frontend/public/downloads/SHA256SUMS.txt) |
| A | asset | - | - | 12 | [frontend/public/downloads/gpt-image-flare.zip](../../../frontend/public/downloads/gpt-image-flare.zip) |
| A | asset | - | - | 12 | [frontend/public/downloads/gpt-image-sunburst.zip](../../../frontend/public/downloads/gpt-image-sunburst.zip) |
| A | asset | - | - | 12 | [frontend/public/downloads/kdan-claude-session-recovery.zip](../../../frontend/public/downloads/kdan-claude-session-recovery.zip) |
| A | source | 22 | 0 | 12 | [frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1](../../../frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1) |
| A | documentation | 60 | 0 | 12 | [frontend/public/downloads/kdan-claude-session-recovery/README.md](../../../frontend/public/downloads/kdan-claude-session-recovery/README.md) |
| A | source | 11 | 0 | 12 | [frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh](../../../frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh) |
| A | source | 213 | 0 | 12 | [frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py](../../../frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py) |
| A | asset | - | - | 12 | [frontend/public/downloads/kdan-codex-session-repair.zip](../../../frontend/public/downloads/kdan-codex-session-repair.zip) |
| A | documentation | 78 | 0 | 12 | [frontend/public/downloads/kdan-codex-session-repair/README.md](../../../frontend/public/downloads/kdan-codex-session-repair/README.md) |
| A | source | 40 | 0 | 12 | [frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1](../../../frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1) |
| A | source | 11 | 0 | 12 | [frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh](../../../frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh) |
| A | source | 714 | 0 | 12 | [frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py](../../../frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py) |
| A | source | 125 | 0 | 12 | [frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py](../../../frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py) |
| A | documentation | 38 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md) |
| A | tooling | 4 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml) |
| A | documentation | 1 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt) |
| A | source | 285 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py) |
| A | documentation | 38 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md) |
| A | tooling | 4 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml) |
| A | documentation | 1 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt) |
| A | source | 285 | 0 | 12 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py) |
| A | documentation | 11 | 0 | 12 | [frontend/public/install/README.md](../../../frontend/public/install/README.md) |
| A | source | 23 | 0 | 12 | [frontend/public/install/claude-code.sh](../../../frontend/public/install/claude-code.sh) |
| A | source | 25 | 0 | 12 | [frontend/public/install/codex.sh](../../../frontend/public/install/codex.sh) |
| A | source | 138 | 0 | 12 | [frontend/public/install/update-codex-models.py](../../../frontend/public/install/update-codex-models.py) |
| M | source | 167 | 101 | 12 | [frontend/src/components/keys/UseKeyModal.vue](../../../frontend/src/components/keys/UseKeyModal.vue) |
| M | test | 205 | 21 | 12 | [frontend/src/components/keys/__tests__/UseKeyModal.spec.ts](../../../frontend/src/components/keys/__tests__/UseKeyModal.spec.ts) |
| D | test | 0 | 22 | 12 | [frontend/src/utils/__tests__/ccswitchImport.antigravityUrl.spec.ts](../../../frontend/src/utils/__tests__/ccswitchImport.antigravityUrl.spec.ts) |
| D | test | 0 | 138 | 12 | [frontend/src/utils/__tests__/ccswitchImport.spec.ts](../../../frontend/src/utils/__tests__/ccswitchImport.spec.ts) |
| M | test | 16 | 0 | 12 | [frontend/src/utils/__tests__/codexCatalogConfig.spec.ts](../../../frontend/src/utils/__tests__/codexCatalogConfig.spec.ts) |
| D | source | 0 | 115 | 12 | [frontend/src/utils/ccswitchImport.ts](../../../frontend/src/utils/ccswitchImport.ts) |
| M | source | 16 | 0 | 12 | [frontend/src/utils/codexCatalogConfig.ts](../../../frontend/src/utils/codexCatalogConfig.ts) |
| M | source | 6 | 122 | 12 | [frontend/src/views/user/KeysView.vue](../../../frontend/src/views/user/KeysView.vue) |
| M | test | 35 | 1 | 12 | [frontend/src/views/user/__tests__/KeysView.spec.ts](../../../frontend/src/views/user/__tests__/KeysView.spec.ts) |
| M | documentation | 7 | 7 | 12 | [skills/sub2api-admin/SKILL.md](../../../skills/sub2api-admin/SKILL.md) |
| M | tooling | 3 | 3 | 12 | [skills/sub2api-admin/agents/openai.yaml](../../../skills/sub2api-admin/agents/openai.yaml) |
| M | documentation | 9 | 4 | 12 | [skills/sub2api-admin/references/admin-cli.md](../../../skills/sub2api-admin/references/admin-cli.md) |
| M | source | 44 | 6 | 12 | [skills/sub2api-admin/scripts/sub2api-admin.js](../../../skills/sub2api-admin/scripts/sub2api-admin.js) |
| A | tooling | 64 | 0 | 12 | [tools/build_docs_downloads.py](../../../tools/build_docs_downloads.py) |
| A | test | 90 | 0 | 12 | [tools/tests/test_claude_session_recovery.py](../../../tools/tests/test_claude_session_recovery.py) |
| A | test | 78 | 0 | 12 | [tools/tests/test_codex_catalog_updater.py](../../../tools/tests/test_codex_catalog_updater.py) |
| A | test | 101 | 0 | 12 | [tools/tests/test_docs_downloads.py](../../../tools/tests/test_docs_downloads.py) |
| A | test | 258 | 0 | 12 | [tools/tests/test_image_skills.py](../../../tools/tests/test_image_skills.py) |
| A | test | 137 | 0 | 12 | [tools/tests/test_session_recovery_commands.py](../../../tools/tests/test_session_recovery_commands.py) |
| A | test | 440 | 0 | 12 | [tools/tests/test_session_repair.py](../../../tools/tests/test_session_repair.py) |
