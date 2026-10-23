# OpenAI 原生 / BPS 并行授权实施验收

日期：2026-10-17。归属：KDAN / TapModels / Tokensavy 共享业务。实现位于本任务的 `vendor-bps-authorization` 隔离工作树，基于 `b6598cafc178eea0e35fe4cdadd39630a922861f`；主工作区其他任务的 BPS 改动未覆盖。

## 已实现行为

- OpenAI 保持一个平台、一个供给账号 ID，原生与 BPS 两份授权独立维护。原生主凭证复用 `accounts.credentials`，独立 BPS 复用加密的 `openai_excel_grants`。
- 供应商模式白名单参加父子有效权限求交；Claude-only、原生-only、BPS-only、双模式均由服务端校验。撤权保留资源、凭证关联和历史，实际 HTTP/WS 转发重新检查权限。
- 新增、补充、重新授权、Token 导入和双凭证导出使用固定 profile。重复补充在访问上游前拒绝；BPS 主凭证转双授权原子完成，失败回滚。BPS 权限撤销后仍能保留旧凭证并接入获准的原生授权。
- 授权详情提供各模式的状态、到期时间、重新授权、刷新、启停、撤销及 BPS 模型范围。上级读取下级资源时无写按钮，伪造写请求同样被拒绝。账号归档继续保留两份凭证、历史及身份占用。
- 后台刷新按实际主授权模式检查权限；等待限流后、保存前再次核验。原生授权永久失效仅停用原生模式，不把整条账号置为异常或阻断独立 BPS；BPS 主授权同理。刷新结果通过凭证、代理、授权版本和实时父子权限的条件写入，避免覆盖并发撤权或重新授权。
- 用量按真实执行路径写入 `native` / `bps`；历史空值显示未记录。混合恢复拆分不可变执行事实，沿用原客户扣费总额、定价阈值和幂等规则，平台→一级、一级→二级各结算一次。聚合行可显示 `mixed`。
- 中、英、日、繁体中文文案，沿用浅色/深色/跟随系统；账号池双授权标记、授权模式筛选、归属列及固定操作列。

## 验证环境与证据

使用独立本地 PostgreSQL 数据库 `vendor_bps_review`、既有本地 Redis 的独立 DB 5、测试后端 `127.0.0.1:18089` 和供应商前端 `127.0.0.1:4189`。测试签名材料、登录资料和加密密钥均为本地夹具；界面供给账号设置为停用，没有有效上游凭证。

| 验证 | 结果与范围 |
| --- | --- |
| 完整 service 包测试 | 4554 项顶层测试通过，3 项因缺外部条件跳过；`/tmp/vendor-bps-service-final.jsonl`。跳过项为 TypeSafe 在线测试、真实 OpenAI Token 估算对照、外部插件进程包。此轮后补的刷新隔离及旧邮箱归属回归由下列最终定向测试覆盖 |
| repository / handler / middleware / routes / authz | 首轮整包运行均通过；`/tmp/vendor-bps-full-tests.log`。同轮 service 的两处失败已修复，随后完整 service 重跑通过 |
| 实际模式与恢复 | 221 项定向测试通过；真实本地 PostgreSQL 用量、双边结算与模式权限 36 项通过。覆盖 BPS-only 429 的零原生请求、WS 撤权、缓存权限复核、历史未知值、混合恢复定价总和及重放防重 |
| 双授权事务 | 16 项定向测试通过，另补重复 attach 提前拒绝、BPS 撤权后原生提升/事务回滚，以及旧账号凭证缺少邮箱时仍校验已验证邮箱的回归；无跳过。最终事务与归属检查见 `/tmp/vendor-bps-final-identity-tests.log` |
| BPS 身份与刷新 | RSA/RS256 本地签名、issuer/client/audience/user/workspace/有效期、可选 ID Token 关联与篡改场景；公开 OpenAI JWKS 返回 4 个 RSA 公钥。最后补修的旧 ID Token 区分/手动刷新合并等 31 项 targeted 和 race 测试均通过。无真实用户 OAuth 换码声明 |
| 后台刷新隔离 | 最终 service 定向 67 项、repository 定向 23 项通过，无失败或跳过；`/tmp/vendor-bps-refresh-isolation-unit.jsonl`、`/tmp/vendor-bps-refresh-isolation-postgres.jsonl`。覆盖排队后撤权、换码期间重新授权、永久/临时错误不阻断另一模式、持久状态缓存及 outbox 回滚 |
| 最终并发竞态检查 | service / repository 共 106 项顶层测试通过，零失败、零跳过；`/tmp/vendor-bps-final-race.jsonl`。使用 `go test -race -tags=unit` 覆盖授权、身份、事务和后台刷新 |
| 本地真实 HTTP | 45 次检查通过；`/tmp/vendor-bps-e2e/api-results.json`、`api-lifecycle-results.json` 和 `api-admin-results.json`。覆盖独立开户、权限子集、固定 OAuth profile、伪造 client 拒绝、模式筛选、启停、模型范围、一级只读、父级撤权和旧会话失效；单独解除原生后保留 BPS，归档仍在列表/数据库中且保留 BPS 凭证关联，归档后写入被拒绝；超管跨供应商列表及详情均正确返回模式与归属 |
| 前端 | 23 个文件、292 项供应商测试通过，另 4 项翻译键完整性检查通过；类型检查与修改文件 ESLint 通过。包含模式继承/撤权、只读授权管理、模式过滤、多语与混合模式显示；普通前端及供应商独立前端均完成生产构建 |
| 最终构建与接口复核 | 后端最终构建成功并启动独立测试服务；8 项只读 HTTP 冒烟检查通过，含健康检查、双授权详情、两种模式筛选、下级默认列表、超管全部供应商列表/详情和归档保留；`/tmp/vendor-bps-e2e/final-smoke-results.json`。修改的 Go 文件格式及 `git diff --check` 通过 |
| 浏览器人工交互验收 | 新增原生/BPS切换、双授权详情、模型范围、英文表单、浅色/深色显示、下级默认列表及归属列；二级详情的重新授权、刷新、撤销按钮均为 0。使用当前供应商组件和样式，不声称本轮重新操作过外部参考站 |

截图：`/tmp/vendor-bps-e2e/dual-authorization-dark.png`、`/tmp/vendor-bps-e2e/authorization-form-en.png`。本地源码 API 会话文件不进入版本控制。

## 接口与配置

新增 1 个公开业务操作“按模式解除授权”，映射到 2 条现有权限边界下的路由：

- `DELETE /api/v1/vendor/accounts/:id/authorizations/:mode`
- `DELETE /api/v1/admin/accounts/:id/authorizations/:mode?vendor_id=...`

原开户、OAuth start/complete、账号更新/刷新/筛选、导入导出及用量接口扩展字段；未开放管理员通用授权 API 给供应商。新增部署配置项 0，新增供应商策略项 1（`openai_authorization_modes`），新增公开执行入口 1；授权验证、凭证转换、模式调度及混合用量分摊属于既有执行链扩展。复用现有数据库、Redis、后台刷新服务、账号并发和网关。

## 改动规模

相对于工作树基线，含新增文件：业务代码及迁移 80 个文件，增加 2752 行、删除 397 行；测试 34 个文件，增加 3315 行、删除 34 行。Ent 自动生成代码另计 12 个文件，增加 475 行、删除 68 行。新增数据库迁移 2 个；公开接口、配置和入口数量见上节。

## 迁移与发布边界

- `278_vendor_openai_authorization_modes.sql`：既有供应商 OpenAI 默认只授予原生权限，不自动开通 BPS；Claude-only 模式为空。若发现存量供应商 BPS 主凭证/独立授权且尚无模式列，先报错要求核对映射，避免迁移静默停供。
- `279_openai_usage_authorization_mode.sql`：增加可空执行模式和不可变保护；历史不回填。集成测试包含迁移重入及拒绝篡改历史模式。
- 本轮迁移只用于独立本地测试库。没有执行生产迁移、部署、版本发布，也没有使用真实用户授权材料完成 OpenAI OAuth/BPS 请求。真实上游登录与计费流量验收仍需对应有效授权。

提交/推送若另行执行，应遵守当前 `docs/CHANGE_DELIVERY.md` 的 main→TapModels/tokensavy 普通合并规则，并保留其他工作区改动。
