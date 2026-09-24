# sub4api 1.1.4 实际代码合并记录

日期：2026-09-24。来源：本地 sub4api 的 `v1.1.4`，提交 `c2fbf1ba6eb38ad282ca3d464dbd9d6bede70cd4`。
公共基线：`ec51bb439`。使用普通 Git merge，保留来源提交历史；公共功能随后普通 merge 到 KDAN、TapModels。此次仅本地合并，不推送、不发布镜像、不部署、不操作生产数据库。

## 已合并功能

| 功能 | 实际行为 | 主要位置 |
| --- | --- | --- |
| ModelTrace 打票 | 随机挑战、模型指纹匹配；HTTP 200、匹配目标模型且收到 state 或 cookie 才保存票据 | `backend/internal/service/openai_codex_ticket_harvest.go`、`modeltrace_challenge.go`、`codex_fingerprint.go` |
| 指纹库 | 内置指纹数据；数据库缓存；管理员手动刷新及后台每小时刷新 | `codex_fingerprint_update.go`、`setting_gateway_runtime.go` |
| 票据生命周期 | 保存 generation、验证来源、state/cookie；按代际失效，取消固定 TTL 判活；记录打票、失效及统计 | `codex_ticket_observation.go`、`codex_ticket_invalidation.go`、`backend/internal/repository/codex_ticket_*` |
| 失效事务 | 已发送的当前代际 state 与 __oailb 同时变化才触发对应代际失效；防止旧响应删除新票；WebSocket 只观察真实握手 | `codex_ticket_observation.go`、`codex_ticket_ws_*_test.go` |
| 调度与参与策略 | 账号/模型参与开关；无票放行全局及账号/模型策略；受账号/模型配额限制；调度缓存存储脱敏就绪投影 | `openai_codex_ticket*.go`、`scheduler_cache.go` |
| 打票代理池和频率 | 所有可用代理或指定代理；失败重试区间、成功后刷新间隔；手工打票 | `codex_ticket_pool.go`、`codex_ticket_cadence.go`、前端代理池及票据看板 |
| 独立模型诊断 | 管理员自己的 API Key 经正常 `/v1/responses` 链路，检查分组和指定目标账号；产生正常用量/费用；不先打票 | `backend/internal/handler/admin/codex_ticket_diagnostic_handler.go`、`CodexDiagnosticModal.vue` |
| 共用 JSONL 模板 | 打票和诊断共用可编辑请求模板；验证、默认值恢复；模板含占位符和账号隔离身份 | `codex_probe_template.go`、`setting_codex_probe_template.go`、`docs/CODEX_PROBE_TEMPLATE.md` |
| 账号时区 | 新建/编辑账号选择请求时区；探测模板渲染日期；保留正常转发中的客户端日期，不强制注入 Accept-Language | `openai_request_timezone.go`、`OpenAIRequestTimezoneField.vue` |
| 管理界面 | 票据独立列、看板、事件筛选、失效详情、手工操作、诊断入口、设置与代理池 | `frontend/src/components/admin/account/Codex*`、`views/admin/AccountsView.vue` |
| 实验诊断命令 | 保留源项目的 codex-ticket-hypothesis 工具、测试、指纹资产和许可证 | `backend/cmd/codex-ticket-hypothesis/` |

## 升级时必须知道的行为

1. 迁移 `244_disable_codex_ticket_harvesting.sql` **首次执行会把已保存的全局打票开关设为 false**。迁移账本保证后续重启不覆盖管理员重新开启的选择。
2. 旧的只按长度/TTL 保存的票据不满足新格式，不能继续使用。重新开启前，在后台 IP 管理配置打票代理池。旧 `harvest_proxy_url` 不会自动转成代理池，也不再用于选代理。
3. 默认 `enabled=false`、`fail_closed=false`；管理端全局/账号/模型策略可覆盖无票放行行为。默认探测扫描 6 秒、单次超时 90 秒、失败重试 10–30 秒、成功刷新 1800 秒；刷新间隔 0 仅关闭主动刷新，不禁止缺票打票。
4. 模型范围由指纹库决定，按最终源码的 GPT-5.6 及以上规则筛选。指纹结果是代码实现的模型识别判断，不是上游模型身份保证。
5. 保留源代码最终时区集合：默认 `Asia/Singapore`；选项不包含中国大陆、香港、澳门、台湾。迁移 242 会清理已保存但不在允许集合的时区。
6. 诊断经过正常网关认证、调度限制和计费，不是免费直连探测。管理员需要自己的 API Key 及允许该账号的分组。
7. 239、241–244 为本次新增数据库迁移。240 已在公共基线存在。只在临时测试数据库验证；生产迁移仍属于后续部署范围。

## 保留的本项目差异及适配

- 保留 sub2api Go module、版本文件、README、更新渠道、安装地址、CI/发布流程及品牌配置；不引入 sub4api 的发布脚本、独立 main-release 工作流和预览稿。
- 保留现有敏感字段脱敏、上游失败状态码设置、compact 模型回退回归测试及 Docker 目录权限处理。
- 依赖注入使用 1.1.4 provider：历史存储、代理设置接好后再启动打票任务；移除旧构造函数提前启动的合并残留，重新生成 Wire。
- 新增界面统一接入现有 i18n；补齐日文，从简体生成繁体，保留品牌语言包。对话框关闭按钮继续使用本项目翻译。
- 修正合并造成的重复字段/重复方法及 API 契约期望；为满足本项目静态检查显式处理关闭操作和类型断言，移除新流程不再调用的旧长度/TTL 私有辅助函数。不改变最终 1.1.4 的判票规则。

## 验证记录

公共合并验证：

- `go test -tags=unit ./...`：全量后端 unit 回归通过。
- `golangci-lint run --new-from-rev=ec51bb439 --max-same-issues=0 --max-issues-per-linter=0 --timeout=10m`：新增差异 0 问题。未将历史已有的全库 lint 问题计作本次通过。
- `go test -tags=integration ./internal/repository -run 'Test(CodexTicket|Migration24[34]|ApplyMigrations)' -count=1 -v`：通过。使用临时 PostgreSQL/Redis，验证全迁移启动、一次性禁用、事务/代际失效和迁移账本。
- `vue-tsc --noEmit`、Vite production build：通过。
- 前端相关组件/页面/语言测试：87 个测试文件、821 个测试通过。首轮从仓库根目录启动导致 3 个读文件测试找错路径；在 frontend 正确目录重跑这 3 个文件后通过。
- 最后一次语言格式整理保留解析后的全部键值；13 个语言测试文件、85 个测试再次通过，`node tools/zh-tw/gen-locale.mjs --check` 通过。
- 新增前端组件与 API 的 ESLint 检查通过；Wire 重新生成，Git 差异无空白错误。

品牌分支合并后的类型、构建与相关回归结果见本次交付记录。未使用真实上游账号执行付费探测，未验证真实代理供应商或模型识别准确率。
