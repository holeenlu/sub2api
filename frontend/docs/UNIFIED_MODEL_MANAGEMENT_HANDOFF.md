# 统一模型管理迁入交接

日期：2026-10-28。目标工作树：`architecture-rebuild/sub2api`，分支 `codex/sub2api-rebuild`。来源是会话“移除充值记录选择项”的提交 `7771bc4b98327e185fb212274a3a40a8ec7eb3ef`，不是该提交全量移植。工作包状态仍由主集成的 `docs/architecture-rebuild/work-items.json` 管理；本文记录迁入合同与验收缺口，不替代任务索引。

## 当前前端交付

- 目录、策略、GPT 全局映射由 `features/catalog/management.ts` 提供公开入口。HTTP DTO 与 API、查询与命令、模型/Claude defaults/params/betas、图像模型开关、来源选择分开组织。
- 价格由 `features/pricing/management.ts` 提供公开面板，保留基础五项价格、tiers、fast/flex/priority、image 及价格覆盖恢复。读取的未知值、零价、删除覆盖、模式不可定价各按原合同表达。
- `app/compositions/model-management/ModelManagementPage.vue` 组合两域，只在进入计费分区时创建价格面板。领域不依赖 app store，app 计算读取/管理/来源/账务权限。
- Policy、mappings、pricing 使用独立权威版本。首次读取 0 后允许 CAS 写 0；任何后续写入必须使用最近读取/确认的版本，不能固定写 0。pricing 版本是全平台共享版本，切平台重新读取。
- 私有查询/命令绑定身份 epoch、权限和平台，切换或卸载取消旧请求并拒绝晚到结果。禁止卸载时保存，禁止自动重发写命令。
- 409 或提交结果不明时保留草稿并停止再次提交。目录/映射可读取最新快照、对照基线/服务端/本地修改，选择保留项后合并为新草稿；不自动保存。价格面板保留旧草稿并标记版本过期，要求基于新快照重新编辑。
- 不完整/重复映射行与价格弹窗草稿均计入平台切换、路由离开及浏览器关闭保护。`codex-auto-review` alias 固定，目标仍可编辑。
- 新模块只有管理专用公开入口，不放入全站 catalog/pricing 的启动 barrel。四语 messages 随管理组件加载。

2026-10-28 已注册正式 `/admin/models` 路由、管理导航和后端 12 条 API，统一模型策略、映射、价格已接入运行消费者。账号列表新增独立模型引用 CAS 弹窗，账号表单与批量保存保留原子用例；BPS 显式空列表与 Native 继承分开。具体真实 Docker、浏览器与失败修复证据见本轮 `.local/model-management-integration-20261028/`；源码接线与替身测试仍不等于真实上游或全站验收。

## 已接入的真实合同

所有路径前缀为 `/api/v1/admin/models`，成功响应使用现有共享 response envelope，前端 `shared/http` 解包。

| 方法、路径 | 输入 | data | 直接权限 |
| --- | --- | --- | --- |
| GET `/policies/:platform` | platform | 完整 ModelPolicy | models.read |
| PUT `/policies/:platform` | 完整策略与读取的 version | 完整新策略 | models.manage |
| POST `/policies/:platform/reset` | version | 完整新策略 | models.manage |
| POST `/policies/:platform/sync` | version、account_ids | 完整新策略 | models.manage + accounts.read |
| GET `/catalog` | platform query | ManagedModel[] | models.read / accounts.read / groups.read / risk.read 任一 |
| GET `/sources` | platform query | id/name/platform/type[] | models.manage + accounts.read |
| GET `/mappings` | 无 | version、updated_at?、mappings | models.read |
| PUT `/mappings` | version、mappings | 新映射快照 | models.manage |
| GET `/pricing` | platform query | version、synced_at?、entries | models.read + billing.cost.read |
| PUT `/pricing` | platform、model、version、price | 仅 version | models.manage + billing.rates.update |
| POST `/pricing/sync` | platform、version | 仅 version | models.manage + billing.rates.update |
| PUT `/accounts/:id/allowlist` | expected[]、models[] | models:string[] 或 null | accounts.manage |

后端继续负责角色/对象鉴权、敏感操作 step-up、持久化 CAS、通知失效、运行时策略与计费消费者。不能为页面补一份独立浏览器授权、目录解释器或计费引擎。页面本轮范围为 Anthropic/OpenAI；不把此 UI 范围误作后端支持平台全集。

普通冲突是 HTTP 409，`code:409`、`reason:"MODEL_POLICY_CONFLICT"`。本轮 `/pricing/sync` 已保留 CAS 冲突的 409；公开价格目录抓取失败仍按依赖错误处理，前端不能把所有 502 当作冲突。

普通价格保存要求 input/output/cache_read 三项有限、非 null 且在 0..1e9，0 有效。可选 cache_5m/cache_1h 缺省/null 由服务端补值。`price:null` 删除 override；modes 的 null 和 tier.unpriced 才有不可定价语义，不能将任意 token null 当作合法保存值。

## 切换与删除边界

1. 主集成登记任务 owner/依赖，实现策略、映射、价格持久化/CAS、12 条管理 API、权限及页面目录，并核对真实运行消费者。
2. 确认后端合同可用后，由前端 app/router 单写者懒加载本组合页，使用 shell=admin、domain=catalog 及同一 route meta 投影菜单。目录基础模块保持独立，不让全站入口提前加载编辑器。
3. 分组/账号候选改为统一 `/catalog` 的投影。新选择只允许启用模型，已有失效引用按合同保留；目录包含禁用项，不能不加区分直接当可新选列表。分组白名单保持独立附加限制，不能扩大授权。
4. 账号勾选接入独立 `/accounts/:id/allowlist`，用最近读取的 expected 数组 CAS。`models:null` 表示继承，不是读取失败。保存引用成功后仍须保持真实上游改写 `from != to`；不能删除整个 credentials.model_mapping。
5. 后端引用、调度及模型输出读者接管后，同切片删除账号本地自定义模型/同步目录入口、被替代候选端点消费者和专属测试。当前旧入口仍服务请求，不能提前删除，也不能宣称已全量清理。
6. 旧迁移 `298_unified_account_model_references.sql` 只作为语义来源；本分支新增 `306_account_model_references.sql`，保留现有 298，不改已应用迁移、不运行生产迁移。

beta_policy 的账号 scope/block、上游错误后的 thinking/signature rectifier、model fallback、BPS 显式模型授权、compact mapping、其他平台映射和分组/渠道定价均有独立读者，不属于可按名称批量删除的内容。GET seed 能读到策略不代表运行参数已经接管；旧运行时 version=0 时仍保持原请求行为。

## 验证与实际页面验收

前一轮前端切片证据保留在 `frontend/.local/unified-model-management-20261027/`；本轮完整接入的自动化、manifest 与运行证据在 `frontend/.local/model-management-integration-20261028/` 和根 `.local/model-management-integration-20261028/`；目录内报告分别标记类型/边界/替身测试、真实 Docker 与浏览器结果。历史 6482 项不能自动计作本轮新增需求验收。

本轮需要按以下范围记录实际结果，未执行项仍保留为未执行：

- 超管、可读管理员、无页面管理员、缺 accounts.read、缺 billing.cost.read、缺 billing.rates.update 的真实服务端身份。
- 两平台首读 seed=0、各域首次保存及刷新；政策、映射、价格版本互不冒用；跨平台价格并发冲突。
- 两个真实会话并发修改、409、最新值比较、逐项选择合并、再次显式保存；断网/结果不明不自动重发。
- 来源目录失败/空/无权限、只同步勾选来源、后台拒绝越权来源、来源抓取失败与 CAS 冲突分别呈现。
- 模型/图像启用、Claude 高级字段、全局别名、分组和账号统一目录接管、继承/null、已有失效引用、真实改写保留。
- 完整价格覆盖/删除、tier/mode/image、step-up 成功/取消、未定价和零价展示及服务端报价/账务读取。
- 平台切换/路由/退出/权限收回/卸载的草稿和晚到响应；四语言、键盘焦点、手机/平板/桌面。

每项以真实服务端回执、镜像/源摘要和实际电脑操作记录验收；没有后端或夹具时登记阻塞，不能用组件替身代替。
