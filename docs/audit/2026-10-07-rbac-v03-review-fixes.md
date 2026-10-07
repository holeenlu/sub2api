# 三级角色 v0.3：独立复核修复与验证

> 发布说明：用户在本轮验收完成后已授权提交、推送。本报告的“未提交”描述保留为验收时状态；此文随共享功能提交纳入版本管理。正式交付使用 main → origin/main，再普通 merge 到 origin/TapModels、origin/tokensavy。推送不授权发版、部署或生产迁移。

日期：2026-10-07。对象：`codex/three-tier-rbac-v02` 工作树内的未提交实现。

## 结论及原报告更正

已经逐项阅读、核对独立复核报告。H1–H3 的安全漏洞属实，M1–M8 与 L1–L7 所述缺口或维护问题也成立。本轮完成以下修正并用源码审读、针对性测试、真实数据库、真实本机 HTTP 和浏览器交叉验证。此前实现报告把“一次测试通过”扩展为“字段授权完整、验收完成”，结论过早；原报告保留为历史记录，不再单独作为验收依据。

当前仍然**没有提交、推送、合并正式分支、发版或执行生产迁移**。代码在：

`/Users/luhonglin/.codex/worktrees/three-tier-rbac-v02/sub2api`

HEAD 仍为 `39d4e56db8fcdb4be4ff30e162a112bc835b15fc`。工作树名字沿用 v02，实际实现为 v0.3。没有采纳引用报告中“先提交快照”的建议，因为用户此前明确要求先不要提交；已经保存两份非 Git 恢复副本：修复前 `/private/tmp/sub2api-rbac-review-20261007/`，修复后 `/private/tmp/sub2api-rbac-review-final/`，包含二进制补丁、文件归档和统计。

## 逐项处理

| 编号 | 处理结果 | 验证重点 |
| --- | --- | --- |
| H1 | `auth` 类邮件模板统一由既有事件目录判定为超管专属。受限管理员不能列出、读取、修改、预览或恢复此类模板；内部发信渲染继续可用 | 服务回归；真实 GET/PUT/restore 请求 403 |
| H2 | 成本字段改为显式分类表，包含累计/今日账号成本、账号额度消耗、平均成本、渠道账号计价规则及 Ollama/OpenCode 快照；区分账号对象和嵌套用户/分组 | 财务字段源码清点测试；真实账号响应无成本字段、嵌套分组倍率仍可见 |
| H2 别名 | 取消按 account_id 查询时把 `actual_cost` 改装账号成本的逻辑；该字段统一表示客户实收。已有 `account_cost` / `total_account_cost` 保留成本语义 | 仓储 SQL 回归与真实仓储集成；没有给普通用户 DTO 增加成本字段 |
| H3 | 无 `redeem_codes.export` 时，列表和详情兑换码显示 `[redacted]`，不能复制；生成操作仍返回本次新码 | 实际生成返回新码、随后列表/详情掩码、超管列表仍能读取 |
| M1 | 14 类真实绑定结构体增加字段权限标签。分组限额/峰值/订阅类型、渠道计价来源、套餐币种/期限/所属组、批量和嵌套导入倍率在写入前校验；未分类字段默认超管 | 反射分类测试、批量预校验测试、真实越权请求 403；普通名称修改成功 |
| M2 | 模型上游预览改为 `accounts.authorize`，明确标记有副作用；只读不能让服务器携密钥请求任意目标 | 真实只读请求在预览处理前被拒 |
| M3 | 按 v0.3 已采用口径，内容风控自动封禁豁免 `IsStaff()`；面板限流仍只豁免超管 | 原有自动封禁测试扩展到 admin/super_admin |
| M4 | “可操作→只读”只撤去非 read 能力，不再先删除再补回 read；依赖 read 的成员管理、余额调整、退款等保持 | 权限编辑页回归；只有关闭模块时按依赖撤销 |
| M5 | 409 按 `reason=ADMIN_POLICY_CONFLICT` 判断，重新加载当前版本并提示用户 | 编辑页冲突测试；数据库并发 CAS 测试 |
| M6 | VersionBadge 仅超管查更新；用户渠道状态不再因 staff 身份走管理接口；公告/兑换码分组 lookup 放行；属性批量读取改为 users.read。账号/渠道表单也停止无权读取超管配置 | 前端回归、浏览器正常页面；属性批量读取再加超管对象保护 |
| M7 | 无导出权时用量列表分页最多 100，显式导出仍需独立权限/二次验证 | 真实接口发现 Gin 缓存参数绕过初版限制，已移到真正分页 handler；缓存条件回归与 HTTP 复测均通过 |
| M8 | 恢复原生路由注册语句。唯一静态权限表包含 454 条声明，按 method+path 匹配。调用真实 `router.go` 的覆盖测试检查遗漏、非 GET 的 Mutates/ReadOnly 和请求 schema | 管理路由文件相对基线仅 +4/+2 行；移除被替代的重复路由器夹具 |
| L1 | 创建人查询使用日志可见范围，不从超管审计泄露 ID | 人员与审计范围测试；浏览器超管创建的条目显示未知。审计过期后来源未知仍是预期限制 |
| L2 | `assigned_by_user` 投影为最少展示字段；若为超管，连同关联 ID 隐藏 | 响应投影回归 |
| L3 | 刷新/重置类权限对齐；导入非空代理需 proxies.manage，CRS 同步也校验；分组倍率需 groups.manage+rates；force 账号探测需 diagnostics | 路由与字段回归 |
| L4 | 以真实注册表做覆盖；插件 UI 签名能力 URL 作为明确的既有独立认证例外，断言该入口存在。不是把它当作匿名管理接口放开 | 真实 router 覆盖测试；普通管理员不能进入插件管理或签发入口 |
| L5 | 取消操作日志整表 UPDATE；在现有受保护策略 JSON 中记录 `legacy_audit_max_id`，读取时生成“升级前记录”展示标记 | 真实迁移/仓储测试；正常修改策略保留水位 |
| L6 | SSE 按事件鉴权，Header/Flush 不再重复查库；空闲 30 秒复查保留 | 分片写入+Flush 独立测试、撤权测试 |
| L7 | 登录落点统一使用可访问页面；补超管角色筛选；CORS 暴露策略版本头；403/409 本地化；zh-TW 重新生成 | 类型、前端测试、繁体生成一致性 |

## 额外发现及收敛方式

1. **`extra: {}` 不是无操作。** 原账号单条更新会整体替换 extra，空对象能清掉隐藏的成本/出口配置。受限账号编辑改用同一个组件与更新入口，只提交获准基础字段，不回传已投影的整份 extra；重新授权只更新凭据，保留现有配置。高级整体替换留给超管。新建账号的 provider extra 是非结构化 map，无法用绑定结构体反射分类，因此单独登记已知创建字段，未知键默认超管，财务键仍需 rates 权限。
2. **不让前端再维护一份财务字段规则。** 现有 `/auth/me` 授权快照增加只读 `admin_write_fields`，直接由同一组绑定声明生成。分组、渠道、套餐表单据此禁用字段并组装 payload。该数据只是 UI 提示，后端仍独立逐字段校验；它不增加权限、不落入 JWT，也不新增端点。浏览器验证无定价权仍能修改名称。
3. **测试脚本自身也需可重复。** 测试 API 重启时，首版临时启动脚本遗漏恢复仅在进程环境中的 TOTP 加密密钥，导致一次黑盒复跑的 MFA 步骤失败。只在独立测试容器重新登记合成超管 MFA 后，完整 8 组黑盒检查通过；没有修改产品的 MFA 校验来让测试通过。
4. **保留已有测试风险事实。** `TestInflightEstimate_AccountMappingNoDBAndBoundedMemory` 未修改生产逻辑或 8 MiB 阈值。本轮后端全量通过，独立重复 10/10 通过；独立审计曾复现 4 次中 2 次失败，因此仍记录为既有不稳定测试，不能宣布已修好。

## 验证结果与准确范围

| 检查 | 实际结果 |
| --- | --- |
| 实现树后端 | 全量 `go test -tags unit -p 2 -timeout 10m ./...` 通过，service 186.944s；构建和 vet 通过。最后分页修复另跑完整 admin handler/middleware，移除重复测试后另跑 server/routes，均通过 |
| 真实数据库 | `CI=true go test -tags integration ./internal/repository -count=1 -timeout 15m` 通过，17.363s；真实 PostgreSQL/Redis 和迁移，不因缺 Docker 跳过 |
| 前端 | vue-tsc、ESLint、生产构建通过；最终全套 **356 个文件、2741 项测试通过** |
| 三角色基本 HTTP 流程 | 超管独立建用户/个人 Key；管理员同权互改；保护超管与财务；策略修改二次验证；同一令牌即时撤权/恢复；日志隔离，4 组通过 |
| 本轮漏洞黑盒 | 邮件模板、兑换码、模型预览、字段提示、定价及嵌套导入、成本投影、属性目标、分页与导出，8 组通过 |
| 浏览器 | 实际管理员登录、用户列表不含超管；账号页面不显示成本；基础编辑不显示倍率/出口配置且保存成功；无定价权限时分组价格控件不可操作，名称保存成功 |
| 品牌快照 | TapModels `b06532840289c33ae5f52cbab160d3c6d15b4071`、tokensavy `b4d7693d3b9a2a2359e9854d02e5fbb097d87bb5`：临时副本可应用补丁，后端完整构建与权限/路由/管理 handler/初始化回归通过；前端类型/构建通过。本轮未重跑品牌前端全套或真实供应商调用 |
| 当前 main 兼容 | `7c9d3607f5ceb2a3450b53ed3c91bb00cc43f7d3` 临时副本可应用补丁，后端构建及权限回归、前端类型通过；设置/权限/Key/Codex 5 文件 116 项回归通过。保留并验证了其他任务新增的 Key/Codex 功能，没有修改正式 main |
| 品牌保留 | 两品牌 `frontend/src/config/brand.ts`、`backend/internal/service/brand.go`、`scripts/release/channels.json` 与各自快照逐字节一致 |
| 生成物/发布脚本 | zh-TW --check、下载包和 SHA256 检查通过；发布矩阵 15 项；自动更新 27 项，1 项可选场景跳过；release-images.sh 语法通过。未运行发版工作流 |

这些结论仅针对本轮源码与明确的快照，不代表生产环境、真实上游 OAuth、扣费/退款或所有历史功能均已被验收。

## 变更统计

统计包含未跟踪的新源码，业务代码包含迁移、生成 wire 和语言文件；不含被忽略的设计/审计文档、临时文件或构建输出。

| 范围 | 类型 | 文件数 | 新增行 | 删除行 |
| --- | --- | ---: | ---: | ---: |
| 本轮复核修复（相对修复前恢复副本） | 业务代码 | 56 | 2024 | 1075 |
| 本轮复核修复（相对修复前恢复副本） | 测试 | 19 | 485 | 128 |
| 三级角色累计（相对 HEAD 39d4e56） | 业务代码 | 170 | 6523 | 1662 |
| 三级角色累计（相对 HEAD 39d4e56） | 测试 | 73 | 1545 | 187 |

- **本轮新增 HTTP 接口 0、持久化配置键 0、环境变量 0、独立业务执行流程 0**；字段声明、表单提示和成本投影均收敛在既有授权流程中。增加一个只读响应字段 `admin_write_fields`，现有策略 JSON 增加内部迁移水位 `legacy_audit_max_id`。
- **三级角色累计**：新增 HTTP 方法 3 个、URL 2 条（权限 GET/PUT、用户批量删除 POST）；新增受保护 settings 键 1 个 `admin_role_policy`；新增表 0、列 2（审计可见级别、定时任务发起身份）；独立执行流程仍按原报告口径为 7 条：统一管理授权、策略编辑/修复、响应投影、人员事务保护与通知、整批删除、多步/长连接复核、一次性角色迁移。
- 请求字段声明直接附于真实绑定结构体，只有 provider extra 因本来就是无类型 map 需创建字段清单；没有增加第二套人员 CRUD、永久兼容旧授权路径或独立财务系统。

## 未消除的边界与后续交付

- 有读取权限就能够逐页收集已获准的数据；`usage.export` 区分批量操作，100 条限制不宣称能阻止抓取。
- 内存测试的历史偶发失败仍待独立定位，不提高阈值、不删除测试。
- 品牌/main 测的是记录的本地提交快照；正式整合时仍要以当时分支状态合并复核。本轮没有移动正式分支指针。
- 迁移在隔离容器跑过，生产仍需按设计维护窗口执行，不能新旧二值角色实例混跑。本轮未进行生产动作。
- 设计及本报告属于 gitignored 的 docs，本地有副本。任何正式交付必须显式决定是否纳入版本管理，不能把本机报告当作远端已经包含。

主要证据保留于 `/private/tmp/`：

- `sub2api-rbac-review-unit-final.log`、`sub2api-rbac-review-vet-final.log`
- `sub2api-rbac-review-pagination-test.log`、`sub2api-rbac-review-route-final.log`
- `sub2api-rbac-review-db-accepted.log`
- `sub2api-rbac-review-frontend-accepted.log`、`sub2api-rbac-review-types-accepted.log`、`sub2api-rbac-review-lint-accepted.log`、`sub2api-rbac-review-ui-build.log`
- `sub2api-rbac-review-http-smoke.log`、`sub2api-rbac-review-adversarial-accepted.log`、`sub2api-rbac-review-pagination-http.log`
- `sub2api-rbac-review-TapModels-*.log`、`sub2api-rbac-review-tokensavy-*.log`、`sub2api-rbac-review-main-snapshot-*.log`
- `sub2api-rbac-review-final/statistics.json`、`implementation.patch`、`working-files.tar.gz`

修复前后恢复副本另已持久保存到主仓库忽略目录 `docs/audit/rbac-v03-recovery-20261007/`，含前后源码归档、补丁与统计；不依赖临时目录长期保留，也没有创建备份分支或提交。

收尾：已停止本轮临时 API、前端及专用 PostgreSQL/Redis 容器，移除临时品牌/main 源码副本、测试密钥配置和测试二进制。保留实现工作树、修复前后持久恢复副本、补丁、报告及测试日志。
