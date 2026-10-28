# 重构验收矩阵

日期：2026-10-25。源码基线：`750d4a16c7adaf558cf2c6ce7101975602832d7b`；合同版本 `0.3`，已包含 Cloud parser 重构。

需求/接口/取舍分别以 [REQUIREMENTS](REQUIREMENTS.md)、[CONTRACTS](CONTRACTS.md)、[DECISIONS](DECISIONS.md) 为唯一来源；任务状态/依赖/写集以 [work-items.json](work-items.json) 为准，[WORK_PACKAGES](WORK_PACKAGES.md) 是阅读视图。遵守 [COLLABORATION](COLLABORATION.md)、[根 AGENTS](../../AGENTS.md) 和 [交付政策](../CHANGE_DELIVERY.md)。本文只维护验收追踪，不复制接口或改变任务授权。

本验收矩阵的场景为目标设计；业务/测试代码增删为0，未因编写场景运行应用、调用上游、部署或迁移。文档Git交付状态以实际交付回执为准。现状仅指已读源码/夹具；目标断言须由相应 WP 实现后验证，不把设计签名/目标目录说成已有实现。

## 1. 证据状态

所有 U/S/R/M 证据均为未运行 `not_run`；C/P/O 均为未采集 `not_collected`。文档检查不升级任何业务证据。AT-25/V20 按当前策略容量信号替代需求更新；页首历史源码基线与既有来源/验收证据不改写，当前实现的实际检查结果由对应任务另报。

| 等级 | 实际环境 | 不能据此宣称 |
| --- | --- | --- |
| U 单测 | 纯函数/内存对象/mock 仓储 | 网络、真实存储或部署通过 |
| S 替身组合 | httptest、RPC double、miniredis | 真实客户端/上游或独立进程 |
| R 真实 DB/Redis | 隔离 PG 事务、Redis Lua/锁/持久化 | 完整多网关进程 |
| M 多实例 | 两个独立网关进程共享隔离 PG/Redis | 真实上游或线上环境 |
| C 真实客户端 | 指定版本对本地网关的完整报文/交互 | 替身上游的结果能证明真实能力 |
| P 真实上游 | 指定模式、账号、模型、传输的真实调用 | 假 store 可证明真实账本/多进程 |
| O 线上 | 指定品牌、部署 SHA、revision 与时间窗 | 所有版本/模式/品牌均通过 |

后续使用 [HANDOFF](templates/HANDOFF.md) 记录 WP/REQ/CT/AT、源码/依赖/fixture 输入、合同版本、命令/cwd、非敏感环境、实际测试与 Skip、输入/动作/结果/限制；不另建平台。测试存在、进程退出 0、零命中或 Skip 均不等于通过。

## 2. REQ—CT—AT 追踪

需求含义不在此复制；主要 CT 与 REQUIREMENTS 对齐。所有 AT 状态继承第 1 节。

| REQ | 主要 CT | AT |
| --- | --- | --- |
| REQ-01 | CT-03/04/09/11 | AT-01、02、03、26、27、32、33、AT-35、AT-36、AT-37 |
| REQ-02 | CT-02/03 | AT-04、05、06、31、AT-36 |
| REQ-03 | CT-02/03/04 | AT-05、06、07、08、AT-35、AT-36 |
| REQ-04 | CT-01/05/08 | AT-09、10、28、AT-36 |
| REQ-05 | CT-06 | AT-10、11、12、23、AT-36 |
| REQ-06 | CT-05/06 | AT-09、12、13、24、AT-36 |
| REQ-07 | CT-04 | AT-01、02、03、14、31、AT-35 |
| REQ-08 | CT-07 | AT-08、15、16、17、26、AT-36 |
| REQ-09 | CT-01/05/07/08 | AT-17、18、28、AT-37 |
| REQ-10 | CT-11 | AT-19、20、22、AT-44、47、48、55 |
| REQ-11 | CT-11 | AT-20、30、31、AT-44至55 |
| REQ-12 | CT-10 | AT-08、12、21、34、AT-36 |
| REQ-13 | CT-03/04/05/06/11 | AT-03、11、19、22、23、24、25、AT-36、AT-45、46、48、49、52 |
| REQ-14 | CT-09 | AT-24、26、27 |
| REQ-15 | CT-08 | AT-09、18、20、28、AT-35、AT-37 |
| REQ-16 | CT-06/07/08 | AT-15、17、29 |
| REQ-17 | CT-10/11 | AT-30、34、40、43 |
| REQ-18 | CT-01至11 | AT-14、21、31、AT-36、AT-44、51、55 |
| REQ-19 | CT-08/10/11 | AT-38、39、40、41、42、43 |

## 3. WP 验收入口

这是场景索引，不是第二份任务状态或写授权；仍按 work-items 的依赖和共享文件单写者规则执行。表中数字均为 AT 后缀；集成包验证完整切片，不能以单元成功代替。

| WP | 场景 |
| --- | --- |
| WP-00 | 01、02、29、30、32、34；增量 35、36 |
| WP-01 | 04、14、18、31；增量 36 |
| WP-02 | 01、02、03、07、14；增量 35、37 |
| WP-03 | 09、10、11、19、22；增量 36 |
| WP-04 | 04、09、13、18、28；增量 36 |
| WP-05 | 10、11、12、13、29；增量 36 |
| WP-06 | 08、15、16、17、18、29 |
| WP-07 | 18、28、31；增量 35、37 |
| WP-08 | 04–13、15、16、31；增量 36 |
| WP-09 | 01、04–07、11、23；增量 35、36 |
| WP-10 | 01、04–18、22、23、31、32；增量 35、36 |
| WP-11 | 03–08、11、15、16、32、33；增量 36 |
| WP-12 | 02、03、05–17、29、32、33；增量 36 |
| WP-13 | 01、04、06、07、14、17、25、32 |
| WP-14 | 01、07、11、14、17、26、32 |
| WP-15 | 01、04、14、17、18、22、32 |
| WP-16 | 22、26、29 |
| WP-17 | 08–10、13、24、27 |
| WP-18 | 02–17、22、23、31–33；增量 36 |
| WP-19 | 01、04、08、14、17、22–27、31、32 |
| WP-20 | 09、17–20、28、29；增量 35、37；品牌 38、39、41、42 |
| WP-21 | 19、20、30、31、38、39、42、43、44–55 |
| WP-22 | 08、16、21；品牌 40、41、43 |
| WP-23 | 19、21、28、31；增量 36；品牌 38–43 |
| WP-24 | 14、29、31；品牌 38、41、43 |
| WP-25 | 01–55 |
| WP-26 | 29、30、31、34；品牌 38、40、41、43 |
| WP-27 | 38–43；先于WP-23集成，精确依赖/写集以work-items.json为准 |

| 前端工作包 | 增量AT |
| --- | --- |
| WP-28 | 44、45、46、52、53、54 |
| WP-29 | 45、46、48、51、55 |
| WP-30 | 44、45、47、48、50、51、55 |
| WP-31 | 44、47、50、51、55 |
| WP-32 | 45、49、50、51、55 |
| WP-33 | 45、50、51、54、55 |
| WP-34 | 45、48、51、55 |
| WP-35 | 47、51、52、53、55 |

## 4. 场景操作与观察

以下均为目标验收；现有测试只是可复用起点，不能自动覆盖未来 execution、SendPermit、DeliverySink、全域 revision 或 host。V 为第 6 节现有命令菜单，不是新增 Make target。

### 协议与执行

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果 | 现有起点 / 命令 |
| --- | --- | --- | --- |
| AT-01 外部协议与同协议保真 | 按实际注册的 Responses/Chat/Messages/Gemini 与 HTTP JSON/SSE/WS 入口送扩展字段、工具 call id、reasoning/refusal/usage；反例按各自现有合同选择重复键、无效字段、截断 SSE，不为透传协议新增全局拒绝规则。 | 同协议未知扩展不无故丢失；跨协议具名转换并记录损失。具体协议要求拒绝时在生成前拒绝，透传路径保持现有语义；工具身份/事件/发送次数可对上，截断不能伪造成功。 | [apicompat/anthropic_responses_test.go](../../backend/internal/pkg/apicompat/anthropic_responses_test.go) 的 `TestAnthropicToResponses_ToolUse`；V01、V06 |
| AT-02 Cloud parser 与完整客户端 envelope | 复用第 5 节 Cloud JSON；正例包含默认字段、additional tools、namespace/custom tools、迁移历史、reasoning、stream options、图片及第二轮；反例为放错位置的执行字段、冲突重复键、不可映射执行语义。加入超过旧工具/历史项数限制的合法输入。 | 原始输入与出站投影分开，适配诊断保留；合法中性字段不触发旧白名单拒绝；不能静默丢弃执行字段。相同有效设置生成相同 digest，变化影响对应 digest；校验错误指明字段。 | [openai_cloud_parser_plan_test.go](../../backend/internal/service/openai_cloud_parser_plan_test.go) 的 `TestCloudParserPlanCompleteCodexFixtures`、`TestCloudParserPlanNeverDropsMisplacedExecutionFields`、`TestCloudEarlyProjectionRetainsPriorAdaptationDecisions`；V02 |
| AT-03 BPS/Cloud 工具、图片和授权模式 | BPS/Cloud 分别执行 function/custom 工具、工具截图、多图、附件复用与回传；正例覆盖 HTTP/SSE/客户端 WS 的第二轮；反例为错误工具身份、跨主体 file id、上传失败、BPS 明确 403、429 与不确定网络失败。Excel-only 账号同时作为负例，确认不会落到原生 Codex。 | call id、工具名、内容次序与图片字节/引用关联不变；已成功附件不重复上传；未完成上传不计为生成成功；403 仅按现有授权恢复合同处理；BPS 可用性不影响独立授权事实，Cloud 仍复用原生 OAuth。 | [BPS attachments](../../backend/internal/service/openai_excel_bps_attachments_test.go) 的 `TestExcelBPSFileOwnershipFailure`、`TestExcelBPSAttachment429RetainsSuccessfulImagesAndSharesRetry`；V02、V03、V04、V05、V08 |
| AT-04 唯一执行者与 attempt 总预算 | 一轮中依次触发准备失败、凭据刷新、同账号重试、模式切换和候选失败；正例在预算内完成；反例穷尽 attempt/目标/等待预算并再次触发底层恢复。 CT-03 设计新增断言：Prepare 不生成；同 SendPermit 重复消费、换 sealed identity/grant/正文均不得发送。 | 每个可能产生生成的出站动作对应唯一 attempt；准备重试不冒充生成，也不重置总预算；handler、adapter、transport 的计数可对上，预算耗尽后发送数不再增加。新 Provider 不新增核心平台分支。 | [gateway_turn_recovery_test.go](../../backend/internal/service/gateway_turn_recovery_test.go) 的 `TestTurnRecoveryActualTargetsShareBudget`、`TestTurnRecoveryPreparationAndSelectionAreIndependent`、`TestTurnRecoveryRepairDoesNotResetOwnerLimit`；V04、V06 |
| AT-05 可证明 NotSent 的受限重选 | 在拨号前、授权校验或明确拒绝入队处制造失败；正例是已证实未发送且交付未提交、范围和预算仍有效；反例是已写请求后超时，或仅凭“没有输出”声称 NotSent。 | 正例可以按合同重选并登记新 attempt；反例进入 AT-06，不能伪造未发送事实。检查远端替身计数、入队记录和 writer 交付状态，防止重复生成。 | [gateway_turn_recovery.go](../../backend/internal/service/gateway_turn_recovery.go) 的 `requestDefinitelyUnsent`；V03、V06 |
| AT-06 Sent/Ambiguous 且未交付 | 上游已经接收请求但响应丢失、写入结果不明或 Redis command 入队超时；正例为有证据且符合模式合同的同一生成恢复；反例为切另一账号/模式重新生成，特别是 hosted/server tool 已可能执行。 | 不以 Uncommitted 推断可重放；没有恢复证据时明确失败或待核对，保留原 owner/root。现有 Native 明确允许的恢复条件需逐条对照 `NativeUncertainReplaySafe`，不能用“禁止一切恢复”替代合同。 | [gateway_turn_recovery_test.go](../../backend/internal/service/gateway_turn_recovery_test.go) 的 `TestTurnRecoveryNativeHostedToolDoesNotRepeatUnknownExecution`；V03、V04、V06 |
| AT-07 下游 Committed 与写出失败 | 分别输出 keepalive、暂存 metadata、response 身份、文本/tool delta、完整终态后触发超时/Write/Flush 失败；正例为协议允许的原归属续接；反例为已交付后拼接另一份生成。 | 记录 writer 实际不可撤回边界，不能仅用非空文本判断；Committed 后不换生成，不重复终态，不对已关闭客户端制造新的网关错误输出；已观测 usage 仍进入同一账务根。 | [gateway_turn_recovery_test.go](../../backend/internal/service/gateway_turn_recovery_test.go) 的 `TestTurnRecoveryHeaderDeadlineAndCommittedStream`；V03、V04、V06、V07 |
| AT-08 取消、断线与有界 drain | 在排队、拨号、thread/start、首个输出后、工具等待及终态竞态中取消；正例确认取消后 drain 到可信用量；反例为无终态证据、drain 超时或子 transport 自身失败被误标用户取消。 | 取消后不启动新 attempt，取消生产者、有界收集、持久化、释放自己资源；取消不污染账号健康。经上游确认 interrupted/failed 且有实测 usage，依现有规则单根幂等结算并标 lower_bound，不等于 Final；结束未确认即使有水位仍 unknown，不收费或补零。 | [Cloud boundaries](../../backend/internal/service/openai_cloud_boundaries_test.go) 的 `TestCloudCancelledPreparationDoesNotStartTurn`；V03、V04、V11 |

### 授权、会话与租约

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果 | 现有起点 / 命令 |
| --- | --- | --- | --- |
| AT-09 排队期间撤销与发送前复核 | 先占满用户/资源槽，使请求进入等待；分别禁用 Key、变更 Key 组、移除资源池成员、归档账号、撤销供应商授权、改变代理归属，再释放槽。正例保持授权不变；反例覆盖“快照仍旧、PG 已撤销”以及复核存储失败。 | 正例只在取得有效租约后发送；反例在出站之前阻断，生成次数为零、无新增生成扣费、槽位释放。必须在等待返回及每次实际发送前复核；已被接收的旧生成如何收尾由 AT-08/16 覆盖，不混为新发送。 | [scheduler_snapshot_vendor_test.go](../../backend/internal/service/scheduler_snapshot_vendor_test.go) 的 `TestDispatchRecheckUsesCurrentOwnershipAndPreservesSendGuard`、`TestVendorHTTPDispatchRechecksEveryRetryAndDoesNotInterruptStream`；V07、V08 |
| AT-10 WS 每轮准入与凭据代际 | 同一 WS 完成第一轮后，在第二轮之前改变 Key 权限/组/有效期、余额或凭据 generation；正例每轮取得新准入但第一轮价格保持不变；反例长期连接复用初次许可。 | 第二轮被正确阻断或采用新一轮许可；旧凭据不得拨号/发送；前一轮 usage 不被新一轮 key/价格对象覆盖，失败准入不延长 sticky 生命周期。 | [openai_gateway_ws_group_pricing_test.go](../../backend/internal/handler/openai_gateway_ws_group_pricing_test.go) 的 `TestOpenAIResponsesWebSocket_KeyMovedToAnotherGroupRejectsNextTurn`、`TestOpenAIResponsesWebSocket_UngroupedKeyContinuesAndRevokes`、`TestOpenAIWSTurnBillingAPIKeysKeepPreviousTurnUntilItIsRecorded`；V06、V07、V08 |
| AT-11 跨 Key/用户的强续接隔离 | 用户 A 的 Key A1 建立 response/thread；A1 正常继续；同用户 A2、另一用户 B1、换组/换账号/换 grant 的请求携带相同 session id、previous_response_id 或 tool call id。 | 只有完整 owner 匹配时可强续接；其他主体不能读取缓存响应、恢复工具状态或借用账务根。普通 sticky miss 可按规则重选，强续接 miss 不得降级为任意新生成。 | [openai_gateway_service_session_isolation_test.go](../../backend/internal/service/openai_gateway_service_session_isolation_test.go) 的 `TestIsolateOpenAISessionID`；V09、V18、V09-R |
| AT-12 owner 失联、epoch fencing 与交接 | A 持有会话，B 转交工具结果；正例 A 存活完成；反例 A 租约过期/进程退出后 B claim，旧 A 恢复发送命令、输出或结算事实，另测迟到旧序号和响应流关闭。 | 旧 owner 的生成/状态写入被 fence；可恢复合同才允许重建，不能声称 socket 已迁移；Unknown 事实留存，必要迟到 usage 进入幂等账务而非因 owner 过期丢弃。 | [gateway_cache_cloud_test.go](../../backend/internal/repository/gateway_cache_cloud_test.go) 的 `TestCloudStoreLeaseFencesOldOwnerAndStaleSequence`；V09、V09-R |
| AT-13 Redis 租约丢失与容量守恒 | 正例并发申请到硬上限、续租、重复释放；反例删除 lease member、TTL 到期、错误 owner 释放、Redis 断连跨 TTL，再恢复旧持有者。分别观察用户容量、账号容量、WS 连接容量与纯统计槽。 | 有效持有者数不超限；明确丢租约后停止新的业务发送并按协议关闭/取消；旧持有者不能释放新租约；暂态错误只按既定租约时限处理。统计降级不能升级为硬容量旁路。 | [concurrency_service_test.go](../../backend/internal/service/concurrency_service_test.go) 的 `TestOpenAIWSIngressLeaseRefreshLoss`、`TestOpenAIWSIngressLeaseReleaseWaitsForInFlightRefresh`；V10、V10-R |
| AT-14 纯转换与适配诊断 | 相同原始报文和显式规则调用两次转换；正例结果确定且不改原输入；反例将网络/token/config/DB/调度依赖接入协议包，或工具 schema/call id/图片损失无诊断。 | 输出与具名步骤、限制、诊断一致；无发送、选号、扣费或全局配置读取。保留有依据的当前修复，不能以“纯协议”删除仍有效的工具兼容能力。 CT-01 还须验证帧寿命/有界复制、金额与时间单位、unknown/absent/zero；新类型是设计，不是已有断言。 | [apicompat](../../backend/internal/pkg/apicompat)；V01、V02、V05、V25 |

### 账务、控制与生命周期

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果 | 现有起点 / 命令 |
| --- | --- | --- | --- |
| AT-15 幂等事件、重复请求与 Unknown | 同一账务根/旧 `(request_id, api_key_id)` 重复投递相同事实；反例同键不同指纹、重复 ACK、Redis 标记过期后恢复，以及 usage 缺失。明确实测零作为独立正例。 | 相同事实只扣一次、只产生对应唯一结算；冲突可见且不覆盖原事实；Unknown 持久化等待核对，不按零结算；删除 Key/账号不能使已发生消费逃账。 | [usage_billing_repo_integration_test.go](../../backend/internal/repository/usage_billing_repo_integration_test.go) 的 `TestUsageBillingRepositoryApply_DeduplicatesBalanceBilling`、`TestUsageBillingRepositoryApply_RequestFingerprintConflict`、`TestUsageBillingRepositoryApply_DeduplicatesAgainstArchivedKey`；V11、V12 |
| AT-16 late usage、累计/增量与下界 | 同一 turn 累计用量依次 10、12、12，再迟到 11；跨 turn 基线另行保存；增加缺字段、负数、错误 model/turn、Unknown 后补齐、取消后迟到、终态与重复事件乱序。数字为合成测试输入，非观测数据。 | 累计不相加，重复不多扣，回退/冲突有诊断；增量口径须明确。确认提前结束且实测 usage 可按 CT-07 单根 lower_bound 结算，但不是 Final，也不证明健康/重放安全。未确认结束有水位仍 unknown；迟到冲突按现有幂等/归档合同处理，不新增独立补差路径。 | [Cloud protocol](../../backend/internal/service/openai_cloud_protocol.go) 的 `cloudParseUsage`、`cloudUsageDifference`；V02、V11、V12 |
| AT-17 事务崩溃、双边归属与余额豁免 | 在事件持久化、余额/配额更新、usage log、提交、ACK、缓存失效之间失败并重启；正例用户/供应商各自归属固定；反例角色中途变化、重复消费、删除 Key、供应商范围变化。 | PG 金额、Key/订阅/平台配额与结算记录守恒；回滚不留下半份账，提交后重试不再扣费。`BalanceExempt` 仅豁免个人余额，不豁免 Key/平台限制、订阅或供应商成本；恢复按冻结归属。 | [usage_billing_events_integration_test.go](../../backend/internal/repository/usage_billing_events_integration_test.go) 的 `TestUsageBillingRecoveryCrashAndDuplicate`、`TestSuperAdminKeyBillingIsDurableWithoutBalanceDebit`；V11、V12 |
| AT-18 资源范围、模型身份与价格来源 | 同一资源供给不同价格组；requested/routed/sent/returned/billed 分别记录；正例 unknown 目录项按现有权限决议、明确免费价格保留；反例把 unknown 当 unsupported、价格缺失当零、按当前全局价格重算旧轮。 | 候选只在授权 ResourceScope 内；定价使用冻结规则与可信模型事实；五种模型身份及来源可核对，倍数及图像/搜索附加费不重复。管理上仍保留有效 Group/Account API。 | [upstream_response_model_test.go](../../backend/internal/service/upstream_response_model_test.go) 的 `TestUpstreamResponseModelObservationAttemptReset`；V11、V13 |
| AT-19 控制面 RBAC 与供应商范围 | 超级管理员、普通管理员、用户、供应商分别请求列表/详情/导出/修改及敏感字段；正例只返回授权对象与字段；反例猜 ID、分页绕过范围、批量混入保护对象、权限撤销后继续管理流。 缺失或过期 step-up 也列负例。 | 后端过滤发生在分页/聚合之前；前端隐藏无关权限；越权无部分写入，流撤销可观察。保留已准入普通管理请求的既有完成语义，不把它与新生成发送前复核混为一条规则。 后端 step-up 与关键审计仍为安全边界。 | [admin_authorization_test.go](../../backend/internal/server/middleware/admin_authorization_test.go) 的 `TestAdminAuthorizationHTTPProjectionAndRevocation`、`TestIdleManagementStreamClosesWhenPermissionRevoked`、`TestAdminAuthorizationAllowsAdmittedRequestToFinish`；V14、V14-R、V15 |
| AT-20 前端领域表单与配置并发 | 按角色访问账号/路由/计费/供应商领域；正例按服务端字段权限和能力渲染、按域提交；反例两个窗口同 revision 保存冲突、撤销后旧 store 继续提交、任意布尔组合导致前后端解释不一致。 | 未授权字段不出现在 payload，冲突提示且不覆盖另一次保存；保存/错误/加载状态归属清楚；旧组件、API/store/locale 的专属规则在切换后删除。未知能力不由前端擅自扩大。 | [auth.permissions.spec.ts](../../frontend/src/stores/__tests__/auth.permissions.spec.ts) 的 `three fixed management roles`；V15 |
| AT-21 host 生命周期与观测归属 | 正例按 Start→接收请求→Drain→Stop 关闭 worker/连接池/actor；反例中途关闭、重复 Stop、恢复队列积压、日志输出失败、usage 已持久化但尚未 ACK。 | 新构造函数不隐式启动任务；Drain 拒绝新生成、已有工作有界退出，持久化待核对可恢复；operation/attempt/root/owner/发送/交付/用量状态相关联。日志不默认暴露 token、prompt、工具内容；可丢观测不承载账务。 保留内容审计及插件信任边界；插件失败不能绕过发送事实另起生成。 | [usage_record_worker_pool.go](../../backend/internal/service/usage_record_worker_pool.go) 的 `Run`、`Stop`、`ProvideUsageRecordWorkerPool`；V11、V16 |

### 安全、操作与收敛

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果 | 现有起点 / 命令 |
| --- | --- | --- | --- |
| AT-22 可信 IP 与图片出口保护 | 正例来自配置可信代理的 IP 链，图片解析到公网且 TLS Host 正确；反例伪造 forwarded header、私网/混合 DNS、重定向到私网、代理绕过校验和跨主体文件引用。 | 访问限制使用可信链；下载每次解析/重定向保持出口约束，禁止私网拨号；合法代理行为仍有效。Cloud 不做本地图片解码限制，不代表网关主动下载可绕过 SSRF。 | [ip_test.go](../../backend/internal/pkg/ip/ip_test.go) 的 `TestGetSecurityClientIPSwitchEnabledStillUsesTrustedProxy`；V17 |
| AT-23 WS 客户端限制与 response 归属 | 合法客户端从 HTTP→WS 或 WS→HTTP 续接；反例未许可客户端、他人 response id、旧连接关闭误删新绑定、租约丢失后再次创建轮次。 | 客户端能力/限制仍在入口执行，响应归属跨 transport 保持；清理带身份/代际，不误删替换连接；丢租约按协议结束，不开放第二条执行路径。 | [allowed_client_test.go](../../backend/internal/pkg/openai/allowed_client_test.go)；V09、V18 |
| AT-24 Grok 语音并发与音色所有权 | 正例用户在限额内使用自己的音色或合法内置音色；反例容量满、他人音色、嵌套参数引用越权音色、音色资源账号不匹配。 | 用户并发在上游前执行；所有引用均检查归属，失败发送数为零；不能在账户切换时失去音色硬归属，也不能只保护列表接口。 | [grok_audio_billing_test.go](../../backend/internal/handler/grok_audio_billing_test.go) 的 `TestGrokVoiceAndRealtimeEnforceUserConcurrencyBeforeUpstream`；V19 |
| AT-25 策略容量被动标记与 Fable 独立阈值 | 正例结构化错误精确命中、Cloud `codexErrorInfo=policyCapacityUnavailable` 规范化、归属检查后的重试错误通知标记、原子最大值续期、full/lite 同期限、最后命中 +60 分钟到期；反例纯文本/普通过载误命中、跨归属通知、陈旧编辑覆盖、新建账号继承输入标记、乱序缩短期限、正常成功提前清除、关闭刷新后不过期、卸载遗留计时器、旧诊断行进入普通定时测试。 | 按 [R15](../ENGINEERING.md#req-diagnostics) 展示“疑似降智”产品信号，不证明能力下降；标记本身不影响调度/计费或 Cloud 终态/重放合同，不恢复设置/探测/分类器/指纹库/队列/历史 API。原生 Codex 指纹和 Fable 独立阈值保留，Fable 不暂停整个账号或覆盖并发 429；264 及更早迁移与历史数据保留。 | [policy_capacity_test.go](../../backend/internal/service/policy_capacity_test.go)、[DTO 投影](../../backend/internal/handler/dto/policy_capacity_test.go)、[PolicyCapacityBadge.spec.ts](../../frontend/src/components/admin/account/__tests__/PolicyCapacityBadge.spec.ts)、[AccountsView.lite.spec.ts](../../frontend/src/views/admin/__tests__/AccountsView.lite.spec.ts)；V20，实际执行状态另报 |
| AT-26 异步 batch/图片/视频生命周期 | 正例提交→排队→派发→查询→结算；反例重复提交、worker 重启、取消与完成竞争、孤儿预留、错误用户查询视频任务、余额 hold 与实际费用冲突。 | job 身份与 operation/attempt 分开关联；交接再校验授权；有界 lease/心跳保护，取消不误退款已消费，重启/重复消费不重扣，其他任务 hold 不受影响。 Submit/Poll/Cancel/GetArtifact 分别鉴权；批次父子费用按既有合同合并，不重复收费。 | [batch_image_public_test.go](../../backend/internal/service/batch_image_public_test.go) 的 `TestBatchImagePublicService_StatusItemsAndCancel`；V21 |
| AT-27 实时双向音频生命周期 | 正例持续双向音频、二进制帧及晚到音频；反例只有连接成功无音频、客户端取消、上游结束与下游关闭竞争、超限和归属失败。 | 实时端口不强制文本 SSE 终态；只有观测到的音频事实进入既有计费规则，连接 id 不充当可重复使用的 billing root；终止后不重开生成，晚到帧有界处理。 | [grok_audio_test.go](../../backend/internal/service/grok_audio_test.go) 的 `TestProxyGrokRealtimeConnWaitsForLateAudioBeforeReturning`、`TestProxyGrokRealtimeConnPreservesBinaryAudioFrames`；V19 |
| AT-28 配置 revision、发布失败与即时撤销 | 正例同轮固定策略/价格 revision，新轮采用新 revision；反例并发写入同 revision、PG 提交后发布通知丢失、Redis 重启、过期 rebuild 写回、等待时发生撤销。 | CAS 拒绝丢更新；提交后从持久化 revision 恢复收敛；旧 epoch 不能覆盖新配置；紧急撤销不依赖普通通知或下一次全量重建；一轮不混用两套价格。 | [scheduler_snapshot_group_lifecycle_test.go](../../backend/internal/service/scheduler_snapshot_group_lifecycle_test.go) 的 `TestSchedulerGroupLifecycleEpochPreventsABA`；V07、V22、V22-R |
| AT-29 数据迁移与历史幂等键兼容 | 只在隔离 DB 中装入既有 Group/Account/授权/usage/归档数据，运行拟议迁移再重放旧请求；正例旧 API/数据读写保持产品合同；反例新 root 绕过旧去重、两实例同时迁移、旧版本无法读新格式。 | 历史结算不重扣、归属不变化、已应用 migration 校验值保持；幂等运行和并发锁可靠；回退是否可读必须用实际旧版本/数据证明，不能仅声明“回滚代码”。 | [migrations_runner_checksum_test.go](../../backend/internal/repository/migrations_runner_checksum_test.go) 的 `TestIsMigrationChecksumCompatible`；V12、V23 |
| AT-30 三品牌 UI、来源与渠道 | 分别核对 KDAN/main、TapModels/TapModels、Tokensavy/tokensavy 的品牌配置与实际界面：登录、管理、供应商、账号/路由/计费、语言与 logo/favicon；正例共享行为一致；反例混用资产/渠道、品牌分支另生执行逻辑、来源说明丢失。 | 品牌资产/文案/已授权差异各归其主；用户/管理员/供应商看到对应权限界面，安全 logo URL 规则保留；渠道仍与 [channels.json](../../scripts/release/channels.json) 的 `kdan`、`tapmodels`、`tokensavy` 一致；来源与累计变更对实际树可追溯。 | [branding.spec.ts](../../frontend/src/utils/__tests__/branding.spec.ts) 的 `updateFavicon`；V15、V24 |
| AT-31 旧路径删除、依赖方向与单链切换 | 每一垂直切片核对旧/新名称、字段、注册、路由、Wire、配置、动态注册、前端 store/locale/测试；正例只有新链可产生副作用；反例旧路由、旧恢复 loop、过渡开关或新旧双发仍存在。 | 一个请求只有一个执行者和幂等结算用例；删除失效入口及专属资源，有效公开兼容保留具体调用方/数据依据与退出条件；核心不 import Gin/Ent/Redis/具体 provider，model 不反向依赖 execution。 | [ARCHITECTURE.md](ARCHITECTURE.md)“旧代码替换与删除清单”；V25 |

### 真实取证

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果 | 现有起点 / 命令 |
| --- | --- | --- | --- |
| AT-32 真实客户端→本地网关的完整回合 | 记录真实客户端名称/版本/配置/入口，采集其实际序列化首轮、工具回传、图片、第二个用户轮、取消、断线/恢复、错误后的下一轮；Native/BPS/Cloud 逐项区分。正例合法完整 envelope；反例真实产生的不支持形态，不删字段制造成功。 | 客户端可见工具调用、输出、错误和取消符合协议；入口请求、出站模式与 billing root 能关联。材料先脱敏，不保存凭据；已有 fixture 只作对照，不充当此次实际报文。 | 第 5 节 fixture；V02 |
| AT-33 真实 Native/BPS/Cloud 上游 | 在授权测试账号/预算内，按 AT-32 的完整回合验证；正例工具/图片/续接，反例受控取消、真实拒绝与恢复；记录 ingress transport 和 upstream mode/transport，避免仅凭开关推断。 | 可对照真实协议事件、模型、用量、终态和归属；“真实上游+假 store”只证明这一组合。未经证据支持不宣称所有模式兼容或 exactly-once 生成。 | [openai_cloud_live_integration_test.go](../../backend/internal/service/openai_cloud_live_integration_test.go) 的 `TestCloudLiveParserCompleteClientContract`、`TestCloudLiveAdapterToolPNGAcrossManagers`；V26 |
| AT-34 指定部署版本的线上及性能验收 | 独立授权后，在确定品牌、部署 SHA、配置 revision、客户端/模型/负载/拓扑下执行选定场景；正例正常流程；反例只做已授权且受控的故障/恢复，不默认注入生产故障。 | 分别记录 TTFT、吞吐、内存、取消排空/恢复耗时、重复发送/结算计数与归属异常；前后版本必须同口径，不用本地 fake 的计时承诺上游延迟收益。日志脱敏且区分入口 WS 与上游 WS/BPS/Cloud。 | [openai_cloud_observation.go](../../backend/internal/service/openai_cloud_observation.go)；C/P/O：待采集 |

### ce58 只读增量验收（不替换文首工作区基线）

已通过 `git show` / `git diff` 核对增量提交 `ce58a16ee67305575a704cef304b9c2b1959cb75`；当前工作区仍为文首 3e 基线，本轮不 reset/切换。下面链接指向 ce58 已存在的仓库相对路径，其中新增文件尚未出现在当前检出；这与尚未实现的新架构包不同。V27–29 仅在主集成者纳入增量的源码树上运行，当前不声称命令可用或测试通过。基线统一更新由主集成者完成。

| AT / 风险 | 输入与动作：正例及反例 | 可观察结果与现有符号 | 最小验证/缺口 |
| --- | --- | --- | --- |
| AT-35 原生 strict 被泛化为全协议拒绝 | native 与 relay/BPS/Cloud 分开；测试默认关闭、配置/环境开启、请求头开启；native HTTP/bridge 与 ctx_pool/passthrough 分别送显式约束、aliases、null/invalid 原生字段 | [strict](../../backend/internal/service/openai_responses_strict.go) `strictResponsesError` / `detectOpenAIResponsesCompatibilityLoss` 是所选 native Codex 的损失检测，不是通用 JSON Schema 校验。`gateway.strict_responses_compatibility` 默认 false，`CODEX_STRICT_RESPONSES_COMPAT` 绑定配置；`X-Codex2API-Strict-Compatibility: true` 可开启，false 不关闭已启用全局配置。不可保留约束以 unsupported_feature/字段清单在派发前报错；不对 relay/BPS/Cloud 新增此门槛。alias 只补缺失 native 值，保留显式 null/invalid 交给对应 provider | [strict tests](../../backend/internal/service/openai_responses_strict_test.go) `TestResponsesStrictSelectedAccount` / `TestResponsesStrictHTTPAndWebSocketFailBeforeDispatch`；[兼容测试](../../backend/internal/service/openai_responses_ingress_compat_test.go) `TestResponsesNativeFieldsWinAliases`；V27，U/S=`not_run` |
| AT-36 WS 并发轮互相污染 | 同连接 A/B 命名流并发、同流 FIFO；response_id 错配取消、排队 create 前取消、单流失败/共享授权撤销、队列/活动上限、跨流 fork、终态写失败；每轮分别权限/容量/费用 | [multiplexer](../../backend/internal/service/openai_ws_multiplex.go) `RunOpenAIWSMultiplexer` 按 stream_id 分流，响应携 stream_id，控制错误关联 event_id；cancel/interrupt 还校验活动 response_id。物理连接不是单 operation/账务根。常量为 16 活动轮、32 命名流、每流16等待、连接64等待，并有连接级字节预算；这些是 ce58 实现常量，不是新增配置。append/steer 返回不支持；单流失败不击落健康流，共享授权失效关闭整连接。跨流只复用已 committed 且 account/key/group/credential generation 一致的不可变历史，不借 live socket；写终态失败不提交历史 | [mux tests](../../backend/internal/service/openai_ws_multiplex_test.go) `TestOpenAIWSMultiplexFIFOAndIndependentFailure`、`TestOpenAIWSMultiplexCancellationBypassesQueuedCreate`、`TestOpenAIWSMultiplexForkRequiresCommittedOwnedHistory`；[handler](../../backend/internal/handler/openai_gateway_handler_test.go) `TestOpenAIResponsesWebSocketMultiplexIndependentBilling`；V28，S=`not_run`。每轮资格/容量撤销组合需补齐；[lane writer](../../backend/internal/handler/openai_gateway_ws_writer.go) `newOpenAIWSLaneWriter` 仅隔离 Gin header/status，实际输出由 downstream Write 判断 |
| AT-37 metadata 夸大能力/串身份 | 候选支持交集、缺失/空/撤回 experimental_supported_tools；同 OAuth 身份刷新 token 与更换账号/平台/baseURL/身份/relay key 分别测试 | [metadata](../../backend/internal/service/openai_codex_model_metadata.go) `decodeCodexExperimentalTools` / `codexCapabilityScope` / `restrictCodexExperimentalToolsManifest` 校验列表、按候选交集和 scope 限制广告，缺失不假称支持；live manifest 也须收窄且 ETag 对应最终内容。Cloud 现有 capability 声明为空列表，不能从名称推断支持任意工具或变成新模型准入 | [metadata tests](../../backend/internal/service/openai_codex_model_metadata_test.go) `TestExperimentalToolsIntersectionAndScope` / `TestExperimentalToolsPersistPartialMetadataAndLiveWithdrawal`；[Cloud capability](../../backend/internal/service/openai_cloud_capabilities.go) `cloudCapabilities`；V29，U=`not_run`，真实目录/客户端仍 `not_collected` |

AT-10/11/13/21/23 同时消费 AT-36：连接、命名流、每轮 operation、Provider Turn 与 Billing Root 不合并；[execution scope](../../backend/internal/service/openai_ws_execution_scope.go) 的 `resolveOpenAIWSExecutionLane` 将 connection/stream 纳入对应作用域，主 turn/prewarm/compaction 与 memory/独立 subagent 的既有隔离仍须保留。

## 5. 现状、夹具与证据层级限制

- **Cloud 当前实现**：[ingress](../../backend/internal/service/openai_cloud_ingress.go) 的 `PreserveCloudResponsesInput` / `projectCloudIngressSource` 与 [protocol](../../backend/internal/service/openai_cloud_protocol.go) 的 `compileCloudProtocolRequest` / `cloudUsageDifference` 是现状。合法中性字段、大工具目录/历史、图片不做本地 codec 政策按现有 parser 测试保留，不恢复旧限制。
- **发送与账务现状**：[gateway_turn_recovery.go](../../backend/internal/service/gateway_turn_recovery.go) 已有 `NativeUncertainReplaySafe` / `MarkTurnOutputCommitted` 与共享预算；[Cloud session store](../../backend/internal/service/openai_cloud_session_store.go) 的 `ErrOpenAICloudCommandRejected` 才表示可证明未入队，普通入队错误不证明 NotSent；nil usage 不等于实测零。[usage_billing_repo.go](../../backend/internal/repository/usage_billing_repo.go) 的 `applyUsageBillingTransaction` 保留 request/key 幂等及事务，[worker](../../backend/internal/service/usage_record_worker_pool.go) 的 `Run` 首次同步执行。
- **CT-07 已定边界**：上游确认 interrupted/failed 且有实测 usage，按既有规则单根 lower_bound 结算，但不是 Final，不证明健康/重放安全。结束未确认，即使有水位仍 unknown，不收费/补零；迟到冲突按既有幂等/归档处理，不新增独立补差。不能把它继续列作待决定业务规则。

| 材料 | 实际位置/使用点 | 证据边界 |
| --- | --- | --- |
| F1 完整 envelope | [0.159.3](../../backend/internal/service/testdata/cloud_codex_0_159_3.json) → `TestCloudCapturedCodexClientEnvelopes`；[0.164 catalog](../../backend/internal/service/testdata/cloud_codex_0_164_catalog_shapes.json) → `TestCloudParserPlanCompleteCodexFixtures` | 既有 fixture，不代表本轮已采集的真实版本/报文 |
| F2 历史/图片/目录 | [desktop](../../backend/internal/service/testdata/cloud_codex_0_164_desktop.json)、[history](../../backend/internal/service/testdata/cloud_codex_0_164_history.json)、[migrated](../../backend/internal/service/testdata/cloud_codex_0_164_migrated_history_items.json)、[compaction](../../backend/internal/service/testdata/cloud_codex_0_164_compaction.json)、[additional tools](../../backend/internal/service/testdata/cloud_codex_additional_tools.json)、[catalog history](../../backend/internal/service/testdata/cloud_codex_catalog_history.json) | 从对应现有测试加载；JSON 不是完整多轮网络记录 |
| F3 BPS 工具与图片 | [function_code](../../backend/internal/service/basispoints/function_code_transport_test.go)、[function_cmd](../../backend/internal/service/basispoints/function_cmd_transport_test.go)、[tool images](../../backend/internal/service/basispoints/tool_images_test.go)、[HTTP flow](../../backend/internal/service/openai_excel_bps_images_e2e_test.go) 的 `TestExcelBPSImagesHTTPFlow` | HTTP flow 使用 httptest，属于 S；不能按 e2e 文件名标 P |
| F4 owner/存储 | [RPC fake](../../backend/internal/service/openai_cloud_transport_test.go) 的 `newCloudRPCFakeConn`；[routing fake](../../backend/internal/service/openai_cloud_routing_store_test.go) 的 `newCloudLiveRoutingStore`；[repository fixtures](../../backend/internal/repository/fixtures_integration_test.go) | 复用可控故障与种子对象；名称含 Live 不改变假 store 事实 |

需特别标记：

1. [actor/store 测试](../../backend/internal/service/openai_cloud_store_integration_test.go) tag 是 `unit`，`cloudIntegrationStore` 默认 miniredis。设置 `SUB2API_CLOUD_TEST_REDIS` 才用真实 Redis，且会对 **DB 15 执行 FlushDB**；只可接专用可丢弃实例，上游仍是假 wire。
2. [Cloud Redis 集成](../../backend/internal/repository/gateway_cache_cloud_integration_test.go) 的 `newCloudRealRedisStores` 创建两个 Redis client；[scheduler recovery](../../backend/internal/service/scheduler_recovery_test.go) 的 `TestRecoveryProbeTwoGatewayInstancesAndTurnCancellation` 使用内存 double。二者不证明两个独立网关进程。
3. [Cloud live](../../backend/internal/service/openai_cloud_live_integration_test.go) 的 `cloudLiveHarness` 是真实上游+假 routing store。缺 `SUB2API_CLOUD_LIVE_AUTH_FILE` 会 Skip；[通用 e2e helper](../../backend/internal/integration/e2e_helpers_test.go) 还有 `E2E_MOCK`，tag 不是证据等级。
4. [repository harness](../../backend/internal/repository/integration_harness_test.go) 的 `TestMain` 非 CI 且无 Docker 会 skip 并退出 0。R 命令用 `CI=1` 禁止该静默跳过，但仍核对命中测试/Skip；并发 suite 内也存在显式跳过项。

## 6. 最小验证命令菜单（本轮未运行）

已静态核对 [backend/Makefile](../../backend/Makefile)：`test-unit`/`test-integration`/`test-e2e` 使用 `unit`/`integration`/`e2e`，`test-e2e-local` 是别名；[根 Makefile](../../Makefile) 使用 `pnpm --dir frontend exec vitest run`；[package.json](../../frontend/package.json) 具有 `test:run`、`typecheck`、`lint:check`，pnpm 为 `9.15.9`；[go.mod](../../backend/go.mod) 指定 Go `1.27.3`。未检查本机已安装依赖、编译或运行成功。

后端命令在仓库 `backend/`，前端命令在仓库根。按受影响 AT 选取并可进一步收窄，不整表跑；同输入证据不按品牌/阶段重复。并发行为改变才给相关选择器加 `-race`。遵守 [最小充分验证](../CHANGE_DELIVERY.md#最小充分验证2026-10-23) 和 [产物收尾](../CHANGE_DELIVERY.md#本机编译与部署数据收尾)，隔离临时目录/日志、复用合规缓存，不新建平台。Go `-run` 缩小执行，但仍编译整个所选包。

下列 V 编号仅为本文索引，不是新增 Make target；选择器引用现有测试/前缀。目标缺口需要 WP 实现后在原测试上扩展，当前不提供虚构新包命令。

```sh
# V01 U：转换起点；不是所有外部入口
go test -tags=unit ./internal/pkg/apicompat -run '^Test(AnthropicToResponses_ToolUse|ToolInputOnContentBlockStart_SurvivesClientToolRestore|ResponsesToChatCompletionsRequest_CustomToolBecomesFunctionTool)$' -count=1 -v
# V02 U/S：当前 Cloud parser、协议及 usage
go test -tags=unit ./internal/service -run '^TestCloud(ParserPlan|EarlyProjection|CapturedCodexClientEnvelopes|ImagePayloads|ImageEnvelope|Protocol|Usage)' -count=1 -v
# V03 S：Cloud 工具轮、发送/交付/取消
go test -tags=unit ./internal/service -run '^TestCloud(HTTPTools|ResponsesWebSocketToolRoundTrip|CanceledDelivery|HTTPFlushFailure|CancelledPreparation|CancellationAfterDelivered|AcceptedTurnFailure|ExplicitPreInferenceRejection|PreInferenceFailure|RunNotStarted)' -count=1 -v
# V04 U/S：BPS 图片、403、取消；按实际影响收窄
go test -tags=unit ./internal/service -run '^TestExcelBPS(ImagesHTTPFlow|NativeToolScreenshots|Attachment429|FileOwnershipFailure|ImagesTransportError|ImagesFallbackSharesNativeAttemptBudget|403Recovery|UnknownContinuation|ClientCancellation|ChildCancellation|FlushFailure)' -count=1 -v
# V05 U/S：仅选择本次所改 Code/Cmd/Image
go test -tags=unit ./internal/service/basispoints -run '^Test(FunctionCode|FunctionCmd|ToolImages)' -count=1 -v
# V06 U/S：预算、重放、committed 与截断终态
go test -tags=unit ./internal/service -run '^Test(TurnRecovery(ActualTargetsShareBudget|PreparationAndSelectionAreIndependent|RepairDoesNotResetOwnerLimit|HeaderDeadlineAndCommittedStream|RejectedGrantUsesCurrentVersion|NativeHostedToolDoesNotRepeatUnknownExecution)|OpenAIWSTransportRecoveryBoundaries|HandleStreamingResponse_TruncatedFinalEventIsNotDispatched)$' -count=1 -v
# V07 S：每轮准入/价格、槽后复核、非业务写出
go test -tags=unit ./internal/handler -run '^Test(OpenAIResponsesWebSocket_(KeyMovedToAnotherGroupRejectsNextTurn|UngroupedKeyContinuesAndRevokes|GroupRateChangeAppliesToNextTurn)|OpenAIWSTurnBillingAPIKeysKeepPreviousTurnUntilItIsRecorded|AcquireResponsesAccountSlotProfitRecheck|OpenAIForwardMayFailoverOnlyAfterNonSemanticWrite)$' -count=1 -v
# V08 U/S：供应商和模式的发送资格
go test -tags=unit ./internal/service -run '^Test(VendorAuthorizationDispatch|DispatchRecheck|VendorHTTPDispatch|VendorOpenAISharedDispatch)' -count=1 -v
# V09 U/S：会话/owner；第二条为 miniredis store
go test -tags=unit ./internal/service -run '^Test(IsolateOpenAISessionID|SecurityContinuationJSONAndTenantPool|OpenAIWSStateStore_(ConnectionCleanup|HTTPResponseOwner)|CloudRecovery)' -count=1 -v
go test -tags=unit ./internal/repository -run '^TestCloudStore(ReferenceOwnership|LeaseFences|ImmutableBilling|SettlementRootCollision)' -count=1 -v
# V10 U/S：租约丢失、释放/续期竞争
go test -tags=unit ./internal/service -run '^Test(OpenAIWSIngressLease|AcquireOpenAIWSIngressLease|RecoveryLeaseLoss)' -count=1 -v
# V11 U/S：Unknown、下界、冻结、豁免/恢复；非真实 PG
go test -tags=unit ./internal/service -run '^Test(CloudBilling|CloudManagedBilling|CloudFinalize|CloudHostedBilling|CloudHostedUsage|CloudConfirmedEarlyEnd|CloudUnconfirmedEnd|CloudClientCancellation|UsagePricing|SuperAdminBilling|SuperAdminPricing|UsageBillingRecoveryFactory)' -count=1 -v
go test -tags=unit ./internal/repository -run '^TestCloudSettlementArchivePreservesRawFactsAndDetectsConflict$' -count=1 -v
```

```sh
# V13 U：模型观察重置；冻结价格复用 V11
go test -tags=unit ./internal/service -run '^TestUpstreamResponseModelObservationAttemptReset$' -count=1 -v
# V14 U/S：RBAC、step-up 和管理流；SQL 范围另见 V14-R
go test -tags=unit ./internal/server/middleware -run '^Test(AdminAuthorization|IdleManagementStreamClosesWhenPermissionRevoked|AdminSettingsRevisionDoesNotGrantFieldPermissions|EnforceStepUp)' -count=1 -v
# V16 U/S：worker、审计和插件；并非未来全局 Drain
go test -tags=unit ./internal/service -run '^Test(UsageRecordWorkerPool_SubmitAfterStop|UsageRecordSharedExecutionBudget|CloudBillingFailureLogClassAndRateLimit|PluginRequestSentErrorDoesNotFailOver)$' -count=1 -v
go test -tags=unit ./internal/securityaudit -run '^TestWorkerPanicLeaseLossAndLifecycleAreContained$' -count=1 -v
# V17 U/S：可信 IP、DNS/重定向/代理与图片下载
go test -tags=unit ./internal/pkg/ip ./internal/util/urlvalidator -run '^Test(Get(Security|Trusted)?ClientIP|PublicTransport)' -count=1 -v
go test -tags=unit ./internal/service -run '^TestBackfillOpenAIImagesB64JSON_RejectsPrivateHosts$' -count=1 -v
# V18 U/S：客户端与丢租约关闭；归属复用 V09
go test -tags=unit ./internal/pkg/openai -run 'Client' -count=1 -v
go test -tags=unit ./internal/service -run '^TestOpenAIGatewayService_ProxyResponsesWebSocketFromClient_LeaseLossSendsRetryClose$' -count=1 -v
# V19 U/S：Grok 音色/并发、实时帧与计费
go test -tags=unit ./internal/service ./internal/handler -run '^Test(GrokVoiceAndRealtimeEnforceUserConcurrencyBeforeUpstream|SecurityCustomVoice|ProxyGrokRealtimeConn|GrokRealtimeBillingResult)' -count=1 -v
# V20 U/S：结构化策略容量信号、续期与 Fable 独立阈值（不发起真实上游探测）
go test -tags=unit ./internal/service -run '^Test(PolicyCapacity.*|FableSchedulingThresholdIndependentScopeAndOverride|RateLimitServiceFableThresholdPreservesConcurrentUpstream429)$' -count=1 -v
go test -tags=unit ./internal/handler/dto -run '^TestPolicyCapacityFullAndLiteProjection$' -count=1 -v
# 前端在 frontend 目录按现有共享依赖/任务缓存规则执行
# pnpm exec vitest run src/components/admin/account/__tests__/PolicyCapacityBadge.spec.ts src/views/admin/__tests__/AccountsView.lite.spec.ts
# V21 U/S：作业结算/队列/视频归属；SQL 变更再选 V21-R
go test -tags=unit ./internal/service -run '^TestBatchImage(PublicService_StatusItemsAndCancel|SettlementService_(IdempotentAfterBillingCrash|UsesSubmittedPricingSnapshot)|BillingRecoveryService_)' -count=1 -v
go test -tags=unit ./internal/repository -run '^TestBatchImageQueue_(DuplicateEnqueueReturnsAlreadyQueued|JobLockReleaseOnlyDeletesMatchingToken|RecoverStaleActiveMovesStaleJobsToReady)$' -count=1 -v
go test -tags=unit ./internal/handler -run '^TestGrokMediaVideoLookupOwnerIsolation$' -count=1 -v
# V22 U/S：局部 epoch/outbox；全域 revision 待实现
go test -tags=unit ./internal/service -run '^Test(SchedulerGroupLifecycleEpochPreventsABA|SchedulerSnapshotServicePollOutboxDoesNotCleanupOnHandleFailure)$' -count=1 -v
# V23-checksum U：现有校验；真实迁移见 V23
go test -tags=unit ./internal/repository -run '^TestIsMigrationChecksumCompatible$' -count=1 -v
# V24 U：品牌渠道；UI 在下块
go test -tags=unit ./internal/service -run '^Test(SourceBuildUsesBrandUpdateChannel|UpdateReleaseChannelIsolation)$' -count=1 -v
# V25：现有 lint 入口的定向形式；新模块须先扩展 depguard 后按实际包验证
golangci-lint run ./internal/pkg/apicompat/...
```

```sh
# 仓库根执行。V15 组件 S，仅选择受影响文件
pnpm --dir frontend exec vitest run src/stores/__tests__/auth.permissions.spec.ts src/views/admin/__tests__/SettingsView.spec.ts src/components/account/__tests__/CreateAccountModal.spec.ts src/features/vendor/__tests__/portalGuard.spec.ts
# V24-ui 组件 S，不代替三品牌实际浏览器显示
pnpm --dir frontend exec vitest run src/utils/__tests__/branding.spec.ts src/i18n/__tests__/localeKeyCompleteness.spec.ts src/router/__tests__/title.spec.ts
```

**R 环境：** 下列命令会启动 repository Testcontainers harness 的真实 PG/Redis 并应用测试迁移，仅供后续实施。源码默认镜像为 `postgres:18.1-alpine3.23`、`redis:8.4-alpine`；已有覆盖项 `SUB2API_TEST_POSTGRES_IMAGE`。即使只选 Redis 测试，该 TestMain 也初始化 PG；`CI=1` 禁止无 Docker 静默 skip，不触发远端 CI。执行后按既有政策实测容器/产物收尾。

```sh
# V09-R：真实 Redis 双 client，不是 M
CI=1 go test -tags=integration ./internal/repository -run '^TestCloudStoreRealRedis' -count=1 -v
# V10-R：真实存在的 suite 子项；核对确实执行，不能空跑
CI=1 go test -tags=integration ./internal/repository -run '^TestConcurrencyCacheSuite$/^TestOpenAIWSIngressAPIKeySlot_HardLimitRefreshAndRelease$' -count=1 -v
# V12：幂等、冻结、崩溃恢复及豁免
CI=1 go test -tags=integration ./internal/repository -run '^Test(UsageBillingRepositoryApply_|UsageBillingRecoveryCrashAndDuplicate|UsageBillingRecoveryFreezesRepairedPrice|SuperAdminKeyBillingIsDurableWithoutBalanceDebit)' -count=1 -v
# V14-R：SQL scope、原子拒绝及已准入管理请求
CI=1 go test -tags=integration ./internal/repository -run '^TestRBAC(AuditScopeIsAppliedBeforePagination|BatchDeleteRejectsProtectedSelectionWithoutPartialWrites|PolicyCASDoesNotInvalidateAdmittedRequests)$' -count=1 -v
# V21-R：已有 job 持久化起点，不覆盖整套后台生命周期
CI=1 go test -tags=integration ./internal/repository -run '^TestBatchImageRepository_CreateJobAndDuplicates$' -count=1 -v
# V22-R：已有 CAS/outbox
CI=1 go test -tags=integration ./internal/repository -run '^Test(CloudAuthorizationPostgresSettingsAndRevisionCAS|SchedulerSnapshotOutboxReplay)$' -count=1 -v
# V23：现有迁移/旧数据保护；未来新 schema 未实现
CI=1 go test -tags=integration ./internal/repository -run '^Test(MigrationsRunner_(ConcurrentInstancesSerializeOnSessionLock|IsIdempotent_AndSchemaIsUpToDate)|CloudMigrationPreservesHistoricalSIWCUsageAndSettlement)$' -count=1 -v
```

可选 actor+真实 Lua：操作者将 `SUB2API_CLOUD_TEST_REDIS` 设为专用可清空 DB 15 的 `host:port` 后，执行 `go test -tags=unit ./internal/service -run '^TestCloudActorPersistsEveryTerminalStateInTheRealStore$' -count=1 -v`。不设置则为 miniredis；上游一直是假 wire，不能标 P。

**V26 P 环境：** `newCloudLiveHarness` 读取 `SUB2API_CLOUD_LIVE_AUTH_FILE` / `SUB2API_CLOUD_LIVE_MODEL`。后续真实调用授权内，由操作者明确设置测试授权文件和模型、核对源码规定的文件格式；本轮未读取凭据。缺 auth file 会 Skip，未显式指定 model 会用源码默认值，不应当作已选择本次验收模型。

```sh
# backend/；仅已授权且上述两变量已明确设置后使用；本轮未运行/验证编译
go test -tags=unit,cloudlive ./internal/service -run '^TestCloudLiveParserCompleteClientContract$' -count=1 -v -timeout=10m
```

工具图片/迁移历史受影响时，将选择器换为现有 `^TestCloudLiveAdapterToolPNGAcrossManagers$` 或 [migration live](../../backend/internal/service/openai_cloud_migration_live_test.go) 的 `^TestCloudLiveMigratedHistoryQueuedMessages$`。假 store 限制不变，不默认全跑。Native/BPS 的真实调用另按 [既有 BPS 验证资料](../ENGINEERING.md#req-bps) 和实际客户端取证，本轮不猜测 live 命令或环境变量。

**ce58 命令：** 新实现/测试已纳入当前资料基线；以下仅静态核对文件、符号和tag，未编译或执行，运行时仍须检查测试实际命中。

```sh
# V27 U/S：strict 与原生 aliases；在包含ce58实现的backend/运行
go test -tags=unit ./internal/service ./internal/config -run '^Test(ResponsesStrict|LoadResponsesStrictCompatibility|ResponsesNativeFieldsWinAliases|ResponsesNativeAliasOnlyAndRelaySchema)' -count=1 -v
# V28 S：并发、FIFO/取消/fork、每轮账务与作用域；实际并发修改时加定向 -race
go test -tags=unit ./internal/service ./internal/handler -run '^Test(OpenAIWSMultiplex|OpenAIResponsesWebSocket(MultiplexIndependentBilling|CrossStreamFork)|ResolveOpenAIWSExecution)' -count=1 -v
# V29 U：metadata 交集、scope、撤回
go test -tags=unit ./internal/service -run '^TestExperimentalTools(IntersectionAndScope|PersistPartialMetadataAndLiveWithdrawal)$' -count=1 -v
```

## 7. 跨进程补证与未确定事项

M 的最小步骤适用于 AT-09/12/13/16/21/28，复用现有服务/查询/日志，不建立故障注入平台：

1. 固定两个独立 PID/instance id、源码/合同/revision、专用 PG/Redis；共享可计数上游替身。A 建 turn、B 转交工具，核对原 owner/thread/turn/root 与发送数。
2. A 等容量时在权威端撤销，延迟/丢弃通知再释放槽；记录撤销提交、复核、实际发送时间。复核越接近发送越好，但不声称它与外部网络全局原子。
3. 终止 A/切断其 Redis，B 按真实模式合同 claim，再恢复旧 A；旧命令/输出/状态被 fence。必要迟到 usage 按 CT-07 幂等/归档处理，不因 owner 失效丢费，也不另建补差。
4. PG 新 revision 提交后中断通知；Drain/Stop/重启；核对持久化事实恢复、无新准入、自己的 lease 回收，实测耗时/资源。不用同进程两个 manager 代替这些步骤。

C/P/O 按 AT-32..34 采集版本、完整脱敏报文、指定账号/部署和限制。公共日志不默认保存 token、完整 prompt/工具内容，内容审计遵守原权限和留存要求。

| 未确定项 | 主决策/负责方 | 当前限制 |
| --- | --- | --- |
| 真实客户端版本/完整报文、模式恢复证据 | OPEN-01；WP-00/11/12/25 | `not_collected`；不凭 fixture 名称宣称真实兼容 |
| owner/租约/滚动部署与旧数据读者 | OPEN-02/04；WP-05/06/10/20/25 | M 为 `not_run`；不承诺活 socket 接管、旧二进制可读或数据回退，不预造生产 SQL |
| 新 root 与历史幂等键映射的具体持久化方案 | CT-07；WP-01/06，集成者统一合同 | 现有 lower_bound 规则已定；新格式仍需实际迁移论证，不新增独立补差路径 |
| 新 SendPermit、三轴、全域 revision、host/前端领域完整接口 | CT-01..11；对应 WP | 设计待实现；只在真实包/测试出现后补执行证据 |
| 性能基线/阈值、三品牌最终候选树/页面 | OPEN-03；WP-00/21/25/26 | `not_collected`；不承诺提速或上线；共享输入不重复全套验证 |
| 第三方来源许可与官方能力冲突 | OPEN-05/06；实际 WP、WP-26 | 未核实不复制；保留现行行为，不自行改变政策 |
| 当前机器依赖/Docker及命令编译可用性 | 后续执行者 | 本轮只做静态符号/tag/链接检查；Skip/零命中不是验收 |

## 8. 文档交接边界

本轮核对 REQ/CT/WP/AT、相对链接与源码符号、命令选择器/tag、证据状态和 Markdown；文档检查不证明业务通过。资料包实际状态和交付范围见READINESS；业务/测试代码增删、新增业务接口/运行配置/执行路径均为0，现行AGENTS与交付政策保持。

## 9. 统一项目与品牌配置验收

以下为REQ-19/BR-01至12的目标验收，不表示当前模板或网站已完成中性化。全部运行状态not_run；真实部署/客户端证据not_collected。当前三品牌回归用于迁移期，目标同角色统一制品由AT-40证明，不把源码分叉作为验收目标。

| ID | 输入与动作 | 必须观察的结果及反例 | 现有起点与缺口 |
| --- | --- | --- | --- |
| AT-38 | 空白安装、未配置品牌，查看首页/登录/标题/favicon/帮助及随包文档 | 默认sub2api和中性本地资产；无原作者广告、赞助墙、商业导流与相关请求；LICENSE/NOTICE和必要来源仍保留。不能以隐藏组件代替移除专属路径。 | [brand defaults](../../backend/internal/service/brand.go)、[frontend defaults](../../frontend/src/config/brand.ts)、当前README及最终制品清单；需要完整清理证据，当前常量并未中性化。 |
| AT-39 | 通过现有设置修改site_name/site_logo/api_base_url，刷新、清空Logo、切换语言并访问独立门户 | 对应消费者使用同一有效配置，无重新构建、另一品牌首屏闪现或跨部署缓存污染；金额/权限/历史不随显示值变化。 | [app store](../../frontend/src/stores/app.ts)、[branding tests](../../frontend/src/utils/__tests__/branding.spec.ts)、[public settings tests](../../backend/internal/service/setting_service_public_test.go)；现有测试不自动覆盖全部消费者。 |
| AT-40 | 两个独立部署profile使用同角色同版本镜像内容，设置不同名称/Logo/API展示链接及部署项目/容器/镜像引用 | 无品牌代码条件或重编译；配置渲染、升级/回滚信息、功能权限一致且数据各自隔离；功能角色不同可保留不同制品。 | [Compose](../../deploy/docker-compose.yml)、[brand release tests](../../deploy/tests/test_brand_release.py)；实现时更新相应旧品牌假设，实际制品/配置渲染证据单独记录。 |
| AT-41 | 存量部署分别有显式设置和依赖编译fallback；随后修改部署项目/容器名并升级 | 显式值优先，旧有效fallback有一次性映射；挂载仍指向原数据，账号/余额/账务键和必要会话合同不变；不能连接空卷或重复记账。 | [设置更新测试](../../backend/internal/service/setting_service_update_test.go)、[MIGRATION](MIGRATION.md)；需要隔离的真实存储/挂载验证，不能用重建数据库冒充迁移。 |
| AT-42 | 向Logo/名称/链接提交恶意值，修改公开API展示地址为另一测试域名，读取public settings | 复用既有URL/转义/归属保护；管理登录态不发往展示地址，provider/更新源不变，不泄露部署凭据，不执行宿主命令。合法图片/链接不能因新规范被无依据拒绝。 | [branding helper](../../frontend/src/utils/branding.ts)、[管理API client](../../frontend/src/api/client.ts)、[public DTO检查](../../backend/internal/handler/dto/public_settings_injection_schema_test.go)；补跨域/权限负例。 |
| AT-43 | 在隔离副本演练网站改名、部署改名和技术项目更名，扫描实际制品与调用点 | 可定位集中元数据与必要外部合同；展示改名不触及持久/鉴权身份；旧品牌/广告专属实现退出，许可/来源保留；不以全局替换代替迁移。 | [BRANDING](BRANDING.md)、[RETIREMENT](RETIREMENT.md)及实际代码引用；不在生产环境执行更名实验。 |

本轮不运行这些场景，不新增测试平台。实现后按影响复用现有设置、UI、发布与部署检查，静态渲染、真实存储、实际镜像及线上证据分别报告。

## 9. 前端架构增量验收（FRONTEND_ARCHITECTURE 1.0）

AT-44至55关联FR-01至12、REQ-10/11/13/18/19与CT-08/11；均未执行。C在本节指真实浏览器/客户端对实际运行前后端的交互，不能用组件mock代替。R只在实际修改持久化/权限/事务的风险需要时执行；当前只读已有测试作为起点，不声称新场景已经覆盖。

| AT / 风险 | 输入与反例 | 可观察结果 | 现有起点 | 证据 |
| --- | --- | --- | --- | --- |
| AT-44 FR-01/02/12：依赖与唯一owner | 从每个迁入域导出页面/面板；反例为shared导入feature、跨域深导入、barrel提前加载全部页面。 | 依赖方向和显式出口可审查；公共type没有重复业务实体；新增边界lint仅对已迁入范围生效。 | [现有ESLint配置；新规则尚未实施](../../frontend/.eslintrc.cjs) | U/S，not_run/not_collected |
| AT-45 FR-04：请求竞态与scope | A账号或供应商scope慢请求在切到B后返回；过滤翻页和卸载时也复现；正常token轮换作正例。 | 旧结果不写入新scope，loading复位、订阅停止、无误报toast；正常轮换不清空同一逻辑session。 | [useTableLoader现有行为测试](../../frontend/src/composables/__tests__/useTableLoader.spec.ts) | U/S+C，not_run/not_collected |
| AT-46 FR-05：单一刷新与退出 | 并发401、多tab一次性refresh竞争、刷新中登出再登录、取消后重试。 | 单一刷新机制保留有效peer结果，旧请求不以新身份重放；刷新失败不覆盖新登录。 | [tokenRefresh及client现有测试](../../frontend/src/api/__tests__/tokenRefresh.spec.ts) | U/S+C，not_run/not_collected |
| AT-47 FR-03/08：组合保存与冲突 | Group两个领域字段一起保存并制造其中一个校验失败；两人编辑不同Settings section、stale revision及无权字段。 | 当前原子用例全成或全败；并发冲突保留草稿、不覆盖他人更新；服务端字段过滤仍权威。 | [Settings当前测试；revision目标需WP-20补真实服务端证据](../../frontend/src/views/admin/__tests__/SettingsView.spec.ts) | U/S+R+C，not_run/not_collected |
| AT-48 FR-07：角色与供应商 | 普通管理员、超管、一级/二级供应商及aggregate/children/readOnly切换；stale policy、越权对象与step-up取消。 | UI路由/字段一致；服务器分别拒绝越权；vendor_id不能从旧query覆盖新scope，取消不重放。 | [vendor scope、portalGuard及client现有测试](../../frontend/src/features/vendor/__tests__/scope.spec.ts) | U/S+R+C，not_run/not_collected |
| AT-49 FR-05：支付与异步状态 | 支付回调参数声称成功但服务器未确认；提交超时、取消轮询、重复点击；后台作业仍在运行。 | 不以URL确付款，不自动再次扣款/提交；UI取消只结束等待，任务状态仍来自服务端。 | [paymentFlow、paymentWechatResume和useBatchImageAccess现有测试](../../frontend/src/components/payment/__tests__/paymentFlow.spec.ts) | U/S+C，not_run/not_collected |
| AT-50 FR-06：网关事实投影 | 同平台不同Native/BPS/Cloud与HTTP/WS组合、目录缺项、工具未知、confirmed lower_bound及未知账务。 | 六维不合并成平台bool；未知不等于unsupported或零，lower_bound不冒充完整Final，TPS口径保持。 | [latencyHealth现有TPS口径；新投影需CT-08/11实际返回](../../frontend/src/utils/latencyHealth.ts) | U/S+C，not_run/not_collected |
| AT-51 FR-11：切片与历史入口 | 新页面接入后枚举旧URL、OAuth/payment callback、root出口、locale、缓存与持久化键读者。 | 有效外部合同有具名映射，旧页面/平行API/store完全退出；无legacy/v2开关和空wrapper；不清历史数据冒充删除。 | [router guards与实际路由清单](../../frontend/src/router/__tests__/guards.spec.ts) | U/S+C，not_run/not_collected |
| AT-52 FR-09：品牌与地址隔离 | 同角色同版本制品加载两种配置，首屏无网络、配置更新、恶意logo/doc URL及不同api_base_url。 | 首屏中性或正确品牌；没有旧品牌闪现；公开API展示变化不改变带认证管理请求目标，许可证内容保留。 | [branding既有测试；AT-38至43补制品/配置证据](../../frontend/src/utils/__tests__/branding.spec.ts) | U/S+C，not_run/not_collected |
| AT-53 FR-10：UI和文案 | 键盘打开/关闭对话框、错误定位、窄屏表格、全部现有语言切换、导航/标题同步。 | 焦点/label/错误可达；状态不只靠颜色，菜单与route来自同一贡献；无缺键或拼接破句。 | [title与既有check:i18n；浏览器人工交互记录](../../frontend/src/router/__tests__/title.spec.ts) | U/S+C，not_run/not_collected |
| AT-54 FR-04/10：启动和资源 | 重复挂载、退出账号、切portal、页面隐藏再返回；检查懒加载、计时器/请求/订阅和两个功能角色产物。 | 没有重复轮询或泄漏；只加载所需页面；同条件记录资源/请求与现状比较，不凭文件行数宣称提速。 | [现有App生命周期；主站/vendor构建仅在相关变化时运行](../../frontend/src/App.vue) | U/S+C，not_run/not_collected |
| AT-55 FR-03/12：完整切片协作 | 从登录到Key/资源保存或供应商只读查询走完整链；各领域并行交付后由单一integrator接线并删除旧消费者。 | 路由到DTO/服务器/状态/页面完整，跨域保存无半成；实际diff不越界，未接入模块不能accepted。 | [精确写集和实际应用/接口证据；不是以任务JSON代替测试](work-items.json) | U/S+R+C，not_run/not_collected |
