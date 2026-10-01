# Codex 模型目录与默认配置

当前实现以 [模型目录与价格同步](MODEL_CATALOG.md) 为准。

- `/keys` 使用服务端按当前 Key 生成的 setup profile，打开弹窗立即生成配置，不等待上游请求。OpenAI 分组默认选中 Codex CLI (WebSocket)、API Key Mode、macOS / Linux；重新打开或切换密钥恢复默认选项。
- `config.toml` 默认在根级写入 `model_catalog_json = "~/.codex/codex-models.json"`。点击“获取目录”后下载同名 JSON 并保存到该路径，重启 Codex 即可在模型列表中看到当前 Key 可用型号。认证方式中的 `auth.json` 是登录凭据文件，与模型目录 JSON 不同。
- OpenAI/Composite 分组可按需切换远程目录，配置变为 provider 内 `model_catalog_url = "<站点>/v1/models"`；需要 Codex 0.156.0 及以上。切换 API Key 或分组后恢复本地文件模式。
- 智谱 API Key 分组同样提供 Claude Code、Codex CLI 与 OpenCode 配置；Codex 配置将 API Key 写入 `experimental_bearer_token`，不依赖 `KDAN_API_KEY`，并使用本地目录，`GLM-5.3` 系列目录与 `config.toml` 声明 1,000,000 token 上下文；`GLM-4.7` 使用上游的 200,000 token 限制。
- 模型事实、能力和价格由服务端目录任务同步并保存。上游新型号资料完整、账号与分组允许且价格完整后可发布；不复用其他型号的能力，也不把缺价当成免费；管理页面以候选清单维护为主，不展示原始能力 JSON。
- 模型清单集中维护候选、显示名、类型及停用状态，保留手工同步和自动同步开关。账号白名单声明供应（留空全部接受）；分组白名单决定对密钥开放的范围（留空不开放）。刷新只更新候选和能力资料，不改变两层勾选；别名路由和定价由原管理入口负责。
- 远程目录超过客户端 1 MiB 限制时自动保持本地文件模式，可继续下载 `codex-models.json`。
