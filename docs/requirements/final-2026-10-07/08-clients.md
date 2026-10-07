# 08 · API Key 客户端配置与下载工具

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. API Key 使用说明包含 Codex/OpenCode 等当前支持的客户端配置。Codex 目录来源服从该 Key 可见模型与服务端顺序；保留自动获取、手动刷新、可用模型默认值及推理强度选择。

2. 生成的 Codex 配置、认证文件和模型目录按当前 UI 提供复制/下载；字段处于正确 TOML 层级，目录未就绪、失败或为空时采用明确的既有行为，不捏造模型能力。原生搜索配置继续使用当前客户端规则。

3. CC-Switch 导入入口及对应可见设置和失效工具代码已移除，不能因重排旧提交再带回。存在于上游的模型目录服务不计为新造目录治理系统。

4. Claude/Codex 会话修复、恢复脚本、图片技能和安装脚本的源码、压缩包和 SHA256 清单配套保留；三个品牌下载名、文档示例及供应商标识维持各自最终版本。

5. 密钥列表返回后立即显示，统计独立填入并显示加载缺失状态；翻页/卸载取消旧统计请求。用户 Key 的鉴权、并发与额度继续由原后端控制。

## 入口与配置归属

/keys 使用说明与下载；frontend/public/downloads、frontend/public/install；tools/build_docs_downloads.py。

## 验收边界

客户端配置解析、目录顺序、默认模型/推理强度、下载文件内容、没有 CC-Switch 旧路径；生成包 --check 验证与 SHA256；这些检查不等同于在所有真实客户端完成端到端测试。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 业务代码 | 21 | 2142 | 355 | 0 |
| 图像/下载资源 | 4 | 0 | 0 | 4 |
| 已有文档/许可 | 7 | 241 | 11 | 0 |
| 工具/构建/配置 | 4 | 75 | 3 | 0 |
| 测试/夹具 | 11 | 1360 | 185 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 业务代码 | 11 | 11 | [backend/internal/pkg/openai/instructions_gpt6_astra.txt](../../../backend/internal/pkg/openai/instructions_gpt6_astra.txt) |
| A | 业务代码 | 4 | 0 | [frontend/public/downloads/SHA256SUMS.txt](../../../frontend/public/downloads/SHA256SUMS.txt) |
| A | 图像/下载资源 | - | - | [frontend/public/downloads/gpt-image-flare.zip](../../../frontend/public/downloads/gpt-image-flare.zip) |
| A | 图像/下载资源 | - | - | [frontend/public/downloads/gpt-image-sunburst.zip](../../../frontend/public/downloads/gpt-image-sunburst.zip) |
| A | 图像/下载资源 | - | - | [frontend/public/downloads/kdan-claude-session-recovery.zip](../../../frontend/public/downloads/kdan-claude-session-recovery.zip) |
| A | 业务代码 | 22 | 0 | [frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1](../../../frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1) |
| A | 已有文档/许可 | 60 | 0 | [frontend/public/downloads/kdan-claude-session-recovery/README.md](../../../frontend/public/downloads/kdan-claude-session-recovery/README.md) |
| A | 业务代码 | 11 | 0 | [frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh](../../../frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh) |
| A | 业务代码 | 213 | 0 | [frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py](../../../frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py) |
| A | 图像/下载资源 | - | - | [frontend/public/downloads/kdan-codex-session-repair.zip](../../../frontend/public/downloads/kdan-codex-session-repair.zip) |
| A | 已有文档/许可 | 78 | 0 | [frontend/public/downloads/kdan-codex-session-repair/README.md](../../../frontend/public/downloads/kdan-codex-session-repair/README.md) |
| A | 业务代码 | 40 | 0 | [frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1](../../../frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1) |
| A | 业务代码 | 11 | 0 | [frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh](../../../frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh) |
| A | 业务代码 | 714 | 0 | [frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py](../../../frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py) |
| A | 业务代码 | 125 | 0 | [frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py](../../../frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py) |
| A | 已有文档/许可 | 38 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md) |
| A | 工具/构建/配置 | 4 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml) |
| A | 业务代码 | 1 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt) |
| A | 业务代码 | 285 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py](../../../frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py) |
| A | 已有文档/许可 | 38 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md) |
| A | 工具/构建/配置 | 4 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml) |
| A | 业务代码 | 1 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt) |
| A | 业务代码 | 285 | 0 | [frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py](../../../frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py) |
| A | 已有文档/许可 | 11 | 0 | [frontend/public/install/README.md](../../../frontend/public/install/README.md) |
| A | 业务代码 | 23 | 0 | [frontend/public/install/claude-code.sh](../../../frontend/public/install/claude-code.sh) |
| A | 业务代码 | 25 | 0 | [frontend/public/install/codex.sh](../../../frontend/public/install/codex.sh) |
| A | 业务代码 | 138 | 0 | [frontend/public/install/update-codex-models.py](../../../frontend/public/install/update-codex-models.py) |
| M | 业务代码 | 167 | 101 | [frontend/src/components/keys/UseKeyModal.vue](../../../frontend/src/components/keys/UseKeyModal.vue) |
| M | 测试/夹具 | 205 | 24 | [frontend/src/components/keys/__tests__/UseKeyModal.spec.ts](../../../frontend/src/components/keys/__tests__/UseKeyModal.spec.ts) |
| D | 测试/夹具 | 0 | 22 | `frontend/src/utils/__tests__/ccswitchImport.antigravityUrl.spec.ts`（已删除） |
| D | 测试/夹具 | 0 | 138 | `frontend/src/utils/__tests__/ccswitchImport.spec.ts`（已删除） |
| M | 测试/夹具 | 16 | 0 | [frontend/src/utils/__tests__/codexCatalogConfig.spec.ts](../../../frontend/src/utils/__tests__/codexCatalogConfig.spec.ts) |
| D | 业务代码 | 0 | 115 | `frontend/src/utils/ccswitchImport.ts`（已删除） |
| M | 业务代码 | 16 | 0 | [frontend/src/utils/codexCatalogConfig.ts](../../../frontend/src/utils/codexCatalogConfig.ts) |
| M | 业务代码 | 6 | 122 | [frontend/src/views/user/KeysView.vue](../../../frontend/src/views/user/KeysView.vue) |
| M | 测试/夹具 | 35 | 1 | [frontend/src/views/user/__tests__/KeysView.spec.ts](../../../frontend/src/views/user/__tests__/KeysView.spec.ts) |
| M | 已有文档/许可 | 7 | 7 | [skills/sub2api-admin/SKILL.md](../../../skills/sub2api-admin/SKILL.md) |
| M | 工具/构建/配置 | 3 | 3 | [skills/sub2api-admin/agents/openai.yaml](../../../skills/sub2api-admin/agents/openai.yaml) |
| M | 已有文档/许可 | 9 | 4 | [skills/sub2api-admin/references/admin-cli.md](../../../skills/sub2api-admin/references/admin-cli.md) |
| M | 业务代码 | 44 | 6 | [skills/sub2api-admin/scripts/sub2api-admin.js](../../../skills/sub2api-admin/scripts/sub2api-admin.js) |
| A | 工具/构建/配置 | 64 | 0 | [tools/build_docs_downloads.py](../../../tools/build_docs_downloads.py) |
| A | 测试/夹具 | 90 | 0 | [tools/tests/test_claude_session_recovery.py](../../../tools/tests/test_claude_session_recovery.py) |
| A | 测试/夹具 | 78 | 0 | [tools/tests/test_codex_catalog_updater.py](../../../tools/tests/test_codex_catalog_updater.py) |
| A | 测试/夹具 | 101 | 0 | [tools/tests/test_docs_downloads.py](../../../tools/tests/test_docs_downloads.py) |
| A | 测试/夹具 | 258 | 0 | [tools/tests/test_image_skills.py](../../../tools/tests/test_image_skills.py) |
| A | 测试/夹具 | 137 | 0 | [tools/tests/test_session_recovery_commands.py](../../../tools/tests/test_session_recovery_commands.py) |
| A | 测试/夹具 | 440 | 0 | [tools/tests/test_session_repair.py](../../../tools/tests/test_session_repair.py) |
