# sub2api 全站功能规格书

最终前端源码已停写并冻结为 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`（2009文件、18,901,651 bytes）。c2维护35路径的退休、显式模型、metadata与类型修复已应用；完整测试/双构建的真实输入保留63fe，按唯一测试类型补丁的等价证明复用于e5，五项static直接绑定e5。当前catalog为753用例、92页面、51面板、10管理设置tab，业务定义全部not_run；后端及联合Docker/UI/外部验收尚未完成。

本规格书面向产品、前后端开发、测试及运维，说明当前重构项目的全站功能、角色边界、操作流程、数据合同与验收依据。范围包括公共站点、用户控制台、管理控制台和供应商门户；网关对外协议与服务端账务规则引用现有合同，不在前端重新定义。

版本1.4，2026-10-27；这是当前实现规格及验收要求，不是重构验收通过证明。规格以原始源码 6f48d6ab6fcc29763af84410690530aeaa957403 和当前 codex/sub2api-rebuild 工作区的实际实现为依据。源码存在、开发测试通过、真实页面验收通过分别记录；本资料不改变工作包状态、现行权限、接口、配置或交付授权。

最终前端：2009文件、18,901,651 bytes，SHA256 `e5a6080563419821daef5eb96e29a95b95f8d80c722b2f8af68dca762878277d`。6196测试/600文件及双build真实输入63fe，凭唯一测试类型补丁的等价证明复用于e5；五项static直接绑定e5。753业务定义均not_run，原f4fe/6005、旧性能和Docker保留历史归属，见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。

## 阅读顺序

1. [前端重构差异复核](../FRONTEND_REVIEW.md)：原始与当前实现差异、优化收益、已确认漏项和证据边界。
2. [全站总体规格](SPECIFICATION.md)：范围、角色、业务对象、跨领域流程及13项共同要求。
3. 按领域阅读下表四章，领取具体功能或设计相应验收。
4. 核对 [页面与权限](PAGES_AND_PERMISSIONS.md)、[设置与公共合同](SETTINGS_AND_CONTRACTS.md) 和 [机器可读追踪](traceability.json)。

| 章节 | 功能条目 | 覆盖范围 |
| --- | --- | --- |
| [01 公共站点、身份与Key](01-PUBLIC-IDENTITY-KEYS.md) | 44 | 网站/文档/公告/自定义页、登录注册与认证、个人安全、用户与角色、普通及管理Key |
| [02 资源、准入、目录、价格与订阅](02-RESOURCES-ACCESS-CATALOG.md) | 41 | 账号/代理/授权/导入导出、Group组合、风险规则、目录渠道、定价与订阅管理 |
| [03 支付、用量与仪表盘](03-PAYMENTS-USAGE.md) | 31 | 购买/恢复/退款、Provider与交易套餐、资金和用量展示、导出/清理、仪表盘 |
| [04 观测、作业、供应商与系统](04-OPERATIONS-SUPPLY-SYSTEM.md) | 46 | 运维/告警/审计、计划测试、批图、供应关系与结算、设置/邮件/备份/插件/setup |

共162项分域功能及13项共同要求。每项功能列角色/入口、前置、关键输入与校验、流程/结果、异常/取消、权威来源和既有验收引用。页面目录覆盖99条规范路由、2条alias及92个展开后的验收入口，当前管理设置为10个tab、51个面板，已移除独立bps tab及默认模板面板；全站BPS gateway开关在features中沿原CAS保存。

## 如何使用

- 产品评审：对照每项实际交互，不把API支持的字段或动作当作已暴露的界面。特别注意独立操作、只读面板、部分失败与结果未知。
- 开发协作：依据功能ID和源码链接定位owner，接口与字段规则仍由源DTO和既有CONTRACTS管理；跨领域保存不擅自拆事务。
- 测试设计：追踪文件引用当前753个case；旧752映射仅保留为历史，不复制其步骤或结果；章节关联不等于每项功能都有独立case或已测试，具体缺口按领域章节补证据。
- 缺陷收口：FR-01/02/05/06 与首页目录开关已完成源码修复并补开发回归；FR-04 角色目录旧误报已撤回，FR-03 规模/性能限制仍保留。逐页部署验收另行执行，不把实现或测试数量当整站验收。
- 维护输入：MAIN实时ls-remote确认c2a10f2；本资料owner已只读映射025945→c2的35条路径，实际应用待分工owner交接。此前6f→025945的54路径矩阵保留历史归属，不与35相加，也不把后续显式选择修订算作c2已实现。见[维护映射与限制](REVIEW-EVIDENCE.md#c2a10f2-维护输入与应用映射)。
- BPS模型选择：BPS只匹配最终账号映射后的明确勾选模型；缺失、空、null或畸形列表均不表示全选，不自动替历史账号补全。Cloud开启时仍优先并保留已保存BPS列表，关闭Cloud后按该列表恢复选路；具备原生授权的独立新请求可走Native，Excel-only不因此获得Native权限。这是本次用户修订，单列于source-review.current_user_requirements，不归入025945或c2a10f2维护已实现范围；前端e5已冻结，开发证据按实际输入作用域成立，业务验收未完成。
- BPS 边界：f4fe记录冷却隔离、BPSFailureBadge、隐藏配置保留、运行态过滤和显式停止意图。后续生产consumer已按MAIN确认迁至安全DTO的readBPSRecoveryPresentation，旧reader/专属测试删除无shim；七种state及严格时间parser的220项相关tests单列，不算旧6005覆盖。supplier安全投影、恢复权限的运行验收、陈旧enabled编辑与旧probe原子并发、最终源码验收仍未闭环，最终前端snapshot为e5，后端与业务验收仍待完成。见[资源章](02-RESOURCES-ACCESS-CATALOG.md)与[合同边界](SETTINGS_AND_CONTRACTS.md#5-服务端依赖边界)。

## 复核资料

- [技术证据与统计口径](REVIEW-EVIDENCE.md)；[可复查源码统计](source-review.json)。
- [文档核验摘要](verification.json)：本地链接、ID、目录覆盖、证据输入哈希、已固定e5前端与尚未完成的后端/联合runtime边界。
- [既有前端测试入口](../../../frontend/tests/acceptance/README.md)；[持续测试报告](../TEST_REPORT.md)；[工作包状态](../work-items.json)。

架构、编码及品牌规范仍分别归 [FRONTEND_ARCHITECTURE](../FRONTEND_ARCHITECTURE.md)、[CODING_STANDARDS](../CODING_STANDARDS.md)、[BRANDING](../BRANDING.md)。本规格不维护第二份实施状态或测试通过计数。
