# 重构工作包与依赖

任务ID、依赖、写集、状态的唯一来源是[work-items.json](work-items.json)。本表是其阅读视图，不是第二份任务状态。全部为计划任务，尚未开始业务开发。任务状态变化只更新JSON及准备状态，不在聊天中另建相互矛盾的清单。

## 工作包表

| ID | 交付物 | 角色 | 前置依赖 |
| --- | --- | --- | --- |
| WP-00 | 实施基线与合同证据冻结 | integrator | 无 |
| WP-01 | 共享值对象与可编译合同 | contract_owner | WP-00 |
| WP-02 | 纯协议与wire事实 | protocol_worker | WP-01 |
| WP-03 | 授权范围与发送前复核 | access_worker | WP-01 |
| WP-04 | 调度资格与容量租约 | routing_worker | WP-01, WP-03 |
| WP-05 | 会话归属与owner状态 | session_worker | WP-01, WP-03 |
| WP-06 | 账务用例与幂等恢复 | billing_worker | WP-01 |
| WP-07 | 能力与配置快照编译 | catalog_worker | WP-01 |
| WP-08 | 统一执行与恢复引擎 | execution_worker | WP-01, WP-03, WP-04, WP-05, WP-06, WP-07 |
| WP-09 | 原生OpenAI单次尝试 | native_worker | WP-01, WP-02, WP-07 |
| WP-10 | 原生Responses完整切片集成 | integrator | WP-08, WP-09 |
| WP-11 | BPS模式适配 | bps_worker | WP-02, WP-05, WP-06, WP-10 |
| WP-12 | Cloud模式适配 | cloud_worker | WP-02, WP-05, WP-06, WP-10 |
| WP-13 | Anthropic与TypeSafe适配 | anthropic_worker | WP-02, WP-10 |
| WP-14 | Gemini与Antigravity适配 | gemini_worker | WP-02, WP-10 |
| WP-15 | 其余OpenAI兼容供给适配 | compatible_worker | WP-02, WP-10 |
| WP-16 | 媒体 文件与异步作业 | jobs_worker | WP-02, WP-06, WP-10 |
| WP-17 | 双向实时与语音 | realtime_worker | WP-03, WP-04, WP-05, WP-06, WP-10 |
| WP-18 | BPS Cloud垂直切片集成 | integrator | WP-11, WP-12 |
| WP-19 | 其他平台与操作完整集成 | integrator | WP-13, WP-14, WP-15, WP-16, WP-17, WP-18 |
| WP-20 | 控制面与领域配置 | control_worker | WP-03, WP-06, WP-07 |
| WP-21 | 前端跨域装配与最终收敛 | frontend_integrator | WP-20, WP-28, WP-29, WP-30, WP-31, WP-32, WP-33, WP-34, WP-35 |
| WP-22 | Host与观测边界 | host_worker | WP-01 |
| WP-23 | 生命周期及控制装配集成 | integrator | WP-19, WP-20, WP-22, WP-27 |
| WP-24 | 全功能旧路径与数据读者收敛 | integrator | WP-19, WP-21, WP-23 |
| WP-25 | 整体风险与真实证据验收 | reviewer | WP-24 |
| WP-26 | 品牌交付与开发收尾 | release_integrator | WP-25 |
| WP-27 | 统一项目身份与部署品牌参数 | deployment_worker | WP-01, WP-20, WP-21, WP-22 |
| WP-28 | 前端基础合同与技术机制 | frontend_foundation_worker | WP-01 |
| WP-29 | 身份安全与API Key切片 | frontend_identity_worker | WP-20, WP-28 |
| WP-30 | 资源凭据与访问策略切片 | frontend_resources_worker | WP-20, WP-28 |
| WP-31 | 目录定价与订阅切片 | frontend_catalog_billing_worker | WP-20, WP-28 |
| WP-32 | 支付订单与使用量切片 | frontend_payments_usage_worker | WP-20, WP-28 |
| WP-33 | 运行观测与异步作业切片 | frontend_operations_jobs_worker | WP-20, WP-28 |
| WP-34 | 供应商关系与结算切片 | frontend_supply_worker | WP-20, WP-28 |
| WP-35 | 网站品牌与系统设置切片 | frontend_site_system_worker | WP-20, WP-28 |

## 开发波次

- 波次0：WP-00核对实施时基线与合同来源，WP-01先产出可编译共享类型。
- 波次1：WP-02/03/05/06/07/22按各自依赖并行；WP-04等待access接口；WP-09可独立于engine实现。
- 波次2：WP-08接入已验收端口，WP-10完成原生完整切片；WP-20可在其依赖满足后并行整理控制用例。
- 波次3：WP-11至17在各自目录并行，WP-28在共享合同固定后处理基础；WP-29至35在WP-20/28完成后并行领域，WP-21最终跨域收敛。
- 波次4：WP-27在其前置完成后统一部署参数，汇入WP-23；WP-18→WP-19→WP-23串行修改共享旧入口和装配；每个集成点完成对应旧路径删除。
- 波次5：WP-24全功能收敛，WP-25实际验收，WP-26按现行规则交付。

## 操作范围归属

| 当前能力族 | 实现与集成责任 | 边界 |
| --- | --- | --- |
| 原生Responses/Chat/Messages转换及compact/input_tokens | WP-09、WP-10 | 包括WS多路复用及跨流续链，保留现有端点限制 |
| Embeddings、alpha/search | WP-09、WP-10 | 依当前账号能力；不扩展为所有平台/模式通用能力 |
| 模型目录、Codex manifest和能力声明 | WP-07；WP-10/19接入 | unknown模型与未知工具声明分开，价格别名不参与选号 |
| Anthropic/TypeSafe、Gemini/Antigravity、其他兼容平台 | WP-13/14/15；WP-19接入 | 逐平台实际合同，composite是路由聚合不是新provider |
| 图片、网关async image、Gemini/Vertex图片batch、Grok视频、Seedance任务 | WP-16；WP-19接入 | 内部文件/产物与下载归属；不虚构公共通用Files或音频batch |
| Grok TTS/STT/音色/Realtime、Codex Live | WP-17；WP-19接入 | REST音频与双向实时语义分开，每轮用户容量和voice归属保留 |
| Gemini cachedContents及相应内部附件 | WP-14；作业产物由WP-16 | 明确数据读者/owner，不在两个包重复写缓存规则 |
| usage/Key账务读接口、用户/管理员/供应商/支付订阅控制面 | WP-06/20；WP-23接入 | 读模型与扣款权限分开，复用已有领域能力 |
| 观测、审计、worker、插件、启动关闭 | WP-22、WP-23 | 可靠性分类，不赋予新插件权限 |

入口精确清单以PROTOCOLS为准，数据owner以DATA_AND_CONFIG为准。发现未分配入口先补该表和JSON，不允许worker自行扩大对外能力。

所有工作包同时遵守[编码规范1.3](CODING_STANDARDS.md)，适用范围与检查方式以任务实际影响确定；交接记录适用CS规则和证据。

## 领取与写集解释

write_paths是worker可直接修改的范围，部分为尚不存在的目标目录；legacy_read_paths仅供定位当前实现；integration_paths由集成者在相关集成工作包串行处理，不属于worker隐式写权限。共享契约和装配保护规则优先于宽目录通配符。

对于已有代码迁移，worker交付提取实现及精确的旧文件替换/删除建议。集成者在同一垂直切片中接入并删除旧路径，未接入的新包不能单独作为功能完成发布。无需为了避免合并冲突永久保留wrapper。

一个工作包若过大，可由集成者在保持合同和需求追踪的前提下拆成显式子任务，并修改JSON依赖。不能让多个agent自行把同一个WP复制成平行实现。

## 每个工作包的完成条件

范围和合同一致、独立风险有真实证据、旧路径同时处理、依赖消费者已核对、无越界文件、资源收尾有记录。review不等于accepted，文档ready不等于运行ready。按照[协作协议](COLLABORATION.md)和[交接模板](templates/HANDOFF.md)提交。

WP-27为新增任务，不表示排在发布之后；依赖图要求它先于WP-23/24/25/26完成。品牌目标见BRANDING，当前三分支仅为迁移期交付安排。

## 前端切片集成约定

WP-29至35的integration_paths属于各包交付过程，由同一前端集成者串行写入，不赋予worker直接修改权限。WP-28是可编译基础依赖，其入口切换与WP-29同一个纵向切片完成，不单独发布；各领域包先接入可独立工作的路由/面板并清除其旧实现再accepted；跨域组合和仍有消费者的共享旧路径由WP-21最终处理。WP-21依赖的是全部领域已验收，不要求领域等待WP-21才第一次接线，因此不存在循环。组编辑/供应商门户等跨域页面在全部必要公开出口可用时再切换，不先用兼容wrapper发布。细节见[前端架构](FRONTEND_ARCHITECTURE.md)。

## 工作包可领取资料

每个任务的deliverables列明实际应交接的结果，evidence_refs链接VALIDATION的AT场景，reviewer/review_mode/handoff_record记录真实评审方式与交接位置。当前均未分配、未实施；null不是已经安排的人员。领取前按[实施启动手册](IMPLEMENTATION_PLAYBOOK.md)核对依赖、输入、写集、风险和最小检查；不能从“文档完整”推导“所有任务同时ready”。

evidence_refs是相关场景索引，不代表每个包都必须先跑完这些场景的所有证据层级；按实际风险与基础包/运行切片边界选择，完整端到端及外部证据由相应集成/验收包完成。未运行部分保持真实状态，不能为了accepted填造结果。
