# 当前系统与证据基线

开发准备基线：750d4a16c7adaf558cf2c6ce7101975602832d7b；官方父提交：37067e8e340e9440d173a92b4faa05e8ce58368f。原始架构分析基于d1e6e924debeaaced1dc816f779198577703aee4。本轮已审查两者路径差异并纳入已完成的Cloud parser、能力计划、原生Responses严格兼容、多路复用及相应测试更新。没有同步或升级官方来源。

[source-inventory.json](source-inventory.json)记录39个当前源码/文档位置及2个历史输入，文件带Git blob和SHA256。它是研究输入清单，不是运行验证回执，也不表示所有源码均审计通过。目录项仅定位模块，实施时要展开实际受影响文件形成验证输入。

## 系统范围与重构处置

| 领域 | 当前事实与证据 | 目标处置 | 对应工作包 |
| --- | --- | --- | --- |
| 装配与运行 | [wire.go](../../backend/cmd/server/wire.go)组装server、Cloud、审计、插件和多种worker | 生命周期显式，保留现有技术栈和必要依赖；公共装配单写者 | WP-22、WP-23 |
| 客户端网关 | [gateway routes](../../backend/internal/server/routes/gateway.go) + middleware + handlers | 保留协议和入口安全，执行控制迁入唯一引擎 | WP-02、WP-08至19 |
| 平台目录 | [platforms.go](../../backend/internal/domain/platforms.go)登记13个具体平台及网关族，composite另有语义 | 保持单一注册事实，编译能力/模型/策略，不复制平台列表 | WP-07、WP-13至15 |
| 原生OpenAI | [Forward](../../backend/internal/service/openai_gateway_forward.go)、原生HTTP/WS及bridge | 迁移完整首轮/工具续接/取消/结算，不能只抽一次HTTP调用 | WP-09、WP-10 |
| BPS | [既有需求](../bps/REQUIREMENTS.md)；工具目录、图片附件、独立Excel授权和恢复 | 单次尝试适配器，复用核心，保留独有合同 | WP-11、WP-18 |
| Cloud | [最新parser设计](../ENGINEERING.md#req-cloud)、[已交付记录](../cloud-parser/implementation-20261025.md) | 在已完成parser/ingress/capability plan上迁移，不退回旧解析器 | WP-12、WP-18 |
| Anthropic与TypeSafe | platform registry、各自handler/service与协议 | 共用执行能力，保持Messages及TypeSafe专有入口 | WP-13 |
| Gemini与Antigravity | 平台族和对应服务、缓存等原合同 | 分别处理实际能力，不推导两者所有操作等价 | WP-14 |
| 其他OpenAI族 | Grok、Kimi、Zhipu、DeepSeek、MiniMax、OpenCode、CommandCode、Cline | 保留各供给/协议差异；普通provider不得修改核心平台分支 | WP-15 |
| 媒体与异步任务 | 图片、batch、文件/产物、视频、语音相关路由与service | 作业生命周期和双向会话使用窄端口；仍共享授权账务 | WP-16、WP-17 |
| 身份与用户 | [auth routes](../../backend/internal/server/routes/auth.go)、[user routes](../../backend/internal/server/routes/user.go)、JWT/Key中间件 | 保留登录、密钥、用户及订阅合同；新access消费权威身份，避免复制认证体系 | WP-03、WP-20 |
| RBAC与安全 | [authz](../../backend/internal/authz)、[securityaudit](../../backend/internal/securityaudit)、admin规则 | 复用既有角色/scope、step-up、审计、安全措施，形成领域调用边界 | WP-03、WP-20、WP-23 |
| 供应商 | [vendor routes](../../backend/internal/server/routes/vendor.go)、[统一平台验收](../ENGINEERING.md#req-vendor) | 双层归属、归档和双边账务保留，不能仅前端过滤 | WP-03、WP-06、WP-20、WP-21 |
| 账务、订阅与支付 | [账务事务](../../backend/internal/repository/usage_billing_repo.go)、[payment](../../backend/internal/payment)、[支付路由](../../backend/internal/server/routes/payment.go) | 重构网关账务调用；现有支付/兑换/订阅功能保持领域职责，未受影响代码不强制重写 | WP-06、WP-20 |
| 管理、运营与通知 | [admin routes](../../backend/internal/server/routes/admin.go)、Ops、邮件/通知等worker | 保留功能与权限，分离控制用例/生命周期/只读观测 | WP-20、WP-22、WP-23 |
| 前端与品牌 | [frontend/src](../../frontend/src)、主站/供应商构建、brand入口 | 接着现有features整理领域，不新建第二个前端；品牌通过现有配置差异化 | WP-21 |
| 持久化 | [Ent schema](../../backend/ent/schema)、[SQL migrations](../../backend/migrations)、repository | 权威来源/幂等/旧数据明确；不改历史迁移，不预造新表 | WP-05、WP-06、WP-20 |
| 部署及交付 | [新API Compose](../../deploy/api/compose.yaml)、[交付规范](../CHANGE_DELIVERY.md) | 旧部署/品牌发布链已纳入退出；当前独立分支和API角色见IMPLEMENTATION，源码交付不等于上线 | WP-26 |

系统重构覆盖职责和调用边界，不要求把已经独立、正确且不受影响的支付、认证、审计、通知等模块全部重写。目标目录名称是职责；当前已有authz/payment/securityaudit等模块优先复用，禁止新建一套同能力模块后长期并存。

## 基线与并发工作

任何开发者开始前读取最新正式main及当前交付规范，比较本清单中相关文件和后续已交付修复。只把审查后的相关变化合入自己的工作包，不重置其他任务现场。基线更新要记录旧新SHA、受影响合同/夹具、复用或补充验证；不以SHA变化本身要求全项目重测。

本轮引入的都是资料和精确的Git忽略白名单。source-inventory的源码指纹不包含后来文档提交自身，以免形成自引用；代码输入未变时可以沿用。

## 外部参考

CPR、vm2api、New API的固定本地版本及核查范围记录在source-inventory。现有分析支持架构选择，不表示这些仓库整体可复制。本次vm2api架构对照不推进来源review水位；既有授权及新增范围按现行来源说明和交付规范处理。本轮没有复制第三方业务代码。

## 尚需收集的真实材料

脱敏的客户端版本与完整交互报文、代表性部署的性能/资源基线、真实上游及多实例故障证据由WP-00/WP-25记录。现有fixture只能证明其自身来源与覆盖；未收集材料不得以合成JSON冒充。材料中的token、会话凭据及用户内容不得进入公共仓库。

## 并发文档发布的整合

750d4a16c7adaf558cf2c6ce7101975602832d7b将199个文档/导航路径整理为当前工程基线，运行代码相对ce58未变。本包审查并保留其结果：当前已实现行为见ENGINEERING，本包描述尚未实施的目标；退役文档引用按documentation-audit映射到对应需求锚点。交付规范的导航和既有vm2api授权澄清一并保留，不刷新lease覆盖该任务。旧资料的SHA/blob保存在source-inventory的historical_inputs中，可按原提交追索。

## 品牌重构的新目标

本节现状表和快照继续描述已实现系统。用户后续要求已将重构目标明确为统一sub2api项目、部署标识参数化、网站品牌设置化和无关推广清理；具体以BRANDING为准。不得把现状中的三品牌常量/渠道照搬为永久源码分叉，也不得将新目标写成当前运行实现已经完成。

## 实施输入冻结（2026-10-25）

独立重构分支 codex/sub2api-rebuild 从已完成维护提交 6f48d6ab6fcc29763af84410690530aeaa957403 创建。相对前轮资料提交 b7b6a440 的12文件变化已核对，属于人工发布固定选定源的修正、对应测试和交付资料；网关业务代码没有增量。没有同步新上游、升级依赖或移动 main 工作树。source-inventory 原研究指纹保留，implementation_baseline 另记实际开发输入。

前端早期隔离规划起点为b7b6a440；当前前后端已统一在codex/sub2api-rebuild共同工作树开发，Git索引由后端集成者单写。公开API保持已有合同，本轮后端独立构建不需要前端完成。此前“本轮仅资料”“三品牌交付”等描述仅记录研究阶段，本次实施以根AGENTS独立分支条款为准。
