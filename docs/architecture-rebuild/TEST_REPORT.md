# sub2api重构测试报告

报告状态：开发与验收准备阶段。完整业务测试尚未开始，不能据此认定整个重构已完成。

本报告按用户要求持续更新，记录“本机Docker部署→电脑工具按用例操作→报告缺陷→自行修复→更新镜像→复测”的每一轮结果。用例定义不在这里复制：系统用例见[TEST_CASES](TEST_CASES.md)，前端见[唯一入口](../../frontend/tests/acceptance/README.md)。任务状态仍只由work-items.json维护。

## 本轮输入与环境

| 项目 | 已核实状态 |
| --- | --- |
| 分支/临时根 | codex/sub2api-rebuild；673c53c13不是最终验收基线，未提交源码须单独计算摘要 |
| 已部署UI对应历史源 | 1948文件，d4d7bfae71c4ccbca8d36302cf9d709c1bd06a1544e8ff95579d6315be7809f5；不是当前前端冻结源 |
| 最近前端交接 | 1963文件，748782a3af429e29765bb6fd90a1c66385f2191623521ee5119ac4e195438ff4；owner已恢复开发，本快照不再代表当前工作区 |
| UI完整构建上下文 | 90c927992b545676e5080c936125a3c474598493999877f2615c3e282c10e539 |
| 主站UI镜像 | sha256:67ee43c31f2850c8a45cadbb47a60e56fbda2be609d1f7412c29ae7783f8843e |
| 供应商UI镜像 | sha256:ef077d26578c7a417eb7c4e7ae86bda86094efb795ad27bfa4ad2f20e203348f |
| 当前API | 仍为旧阶段162a697a6711源码镜像，不能代表最新后端；完整验收前必须更新并核对来源 |
| 主站/供应商 | http://127.0.0.1:32181 / https://127.0.0.1:32182，仅本机隔离环境 |
| 浏览器 | Chrome 156.0.8078.48，独立未登录“sub2api 本机验收 20261026”配置；不得混用其他个人会话 |
| 数据 | 隔离合成角色、账户、代理、草稿、套餐和页面；凭据仅保留在既有0600私有文件 |
| 源冻结 | 前后端均仍在开发；旧冻结证据按其输入保留，尚未达到完整业务验收开始条件 |

## 证据与覆盖状态

| 范围 | 当前状态 | 证据边界 |
| --- | --- | --- |
| 752项前端定义 | not_run | 92页面、17redirect、2alias、52设置面板为覆盖输入，不是通过数 |
| 79项后端/系统场景 | 完整执行not_run | 既有组件回执只能按有效输入复用，不能直接折算为场景全部通过 |
| 13项本机API子集 | 最终镜像执行not_run | 先前角色fixture准备不等于该套脚本已对最终镜像通过 |
| UI镜像与来源 | 旧阶段镜像已部署、与其历史来源匹配 | 当前源码未部署；health和版本JSON不等于业务页面实际操作 |
| 浏览器TLS前置 | 正例与错误主机名负例已验证 | 专用浏览器配置可信，macOS系统信任未修改，无证书忽略或sandbox关闭参数 |
| 开发期定向回归 | 原记录按输入复用 | 例如实际Native Finalizer的280项U/S和4项PG串联；不代替全部协议/平台/页面 |

精简来源回执位于.local/rebuild-integration/ui-build/receipt.json、ui-runtime.json、browser-tls-verification.json，以及各开发owner的既有验证回执。最终业务结果应使用前端现有result.schema.json和根ACCEPTANCE_RECORD模板保存，包含实际步骤和证据引用；不得只手填passed。

## 缺陷记录

2026-10-26新增前端复核：FR-01为vendor guard factory未接实际router，MAIN已核对`frontend/src/app/router/index.ts`与`app/compositions/vendor-portal/guard.ts`；此前实际路由仍内联旧规则，缺少factory中await后的session复核，现由前端owner修复并交回归证据。FR-04已撤回：再次逐项核对原冻结输入后，platform、profile和children等角色原值本就正确；此前采信的“通用vendor角色过宽”报告没有事实依据，不记为已修复缺陷。拒绝跳转不能当作目标页面正向通过，真实导航仍按原正确角色矩阵验收。

| 编号 | 范围/发现 | 处理与复验 | 状态 |
| --- | --- | --- | --- |
| ENV-001 | 本地TLS边缘只读根目录下，NGINX默认fastcgi等临时目录不可创建 | 所有临时写路径进入既有tmpfs；进程healthy、HTTPS来源核验通过 | 环境配置修复已验证 |
| ENV-002 | 合成iframe原拟由主站提供，受到现有X-Frame-Options DENY阻挡 | 使用独立32183合成fixture，仅允许32181作为frame ancestor；主站防护不变 | 配置/HTTP验证通过，实际业务iframe用例待执行 |
| DEV-001 | Native账务终结失败可能被Retry=true吞掉并继续生成 | 已修复循环错误传播，已有实际准备/发送次数回归；最终输入下仍须关联回归 | 开发回归通过，完整验收待执行 |
| DEV-002 | WS测试夹具遗漏真实host.TurnAdmission，首帧前multiplexer退出，34条旧批次EOF失败 | 夹具补真实admission owner及严格journal，单个首轮race已通过；不得把夹具故障当生产Bind授权失败 | 首轮验证通过，完整新批次待执行 |
| REG-001 | 四个维护会话的已发布修复没有持续逐项对照到重构，确认BPS/Cloud/Native及CPR请求体保护遗漏 | 详细行为、owner及修复落点见MAINTENANCE_FIX_AUDIT；已扩展7条既有场景；只记本分支实测结果 | 修复/验证进行中 |
| QA-PRE-001 | 电脑进入/home时模型区失败；已确认model_plaza_enabled=false，目录API按合同404，常驻区仍请求该API | 原“整区常驻”意图与query权限条件需前端owner统一；不得简单开权限或伪报价。详见FRONTEND_HANDOFF；最终镜像复测 | 开发期联调缺口，未计为完整页面通过 |
| GW-03/04 | Native service两个失败：前者最终查实为夹具错误形态不匹配，后者为client/upstream transport硬等导致发送前拒绝 | GW03改正确触发后通过，不扩大生产修复条件；GW04登记同operation的真实sealed HTTP身份复核，补丁已接，相关验证见WS owner回执 | 已查明并处理；历史失败仍保留，完整合同未全部验收 |
| GW-19/20/21 | 扩审发现public/wire模型授权混用、真实Key仓储漏canonical grants、原始wire采集点太晚 | 见GATEWAY_REVIEW；分别以实际HTTP入口、真实PG及真实router/单读捕获证明，修改真实consumer | 已接修复，组合验收继续 |

以上属于已有环境准备/开发发现，不是从尚未执行的752项业务用例中“测试通过”。后续每个新缺陷必须记录case_id/variant_id、角色/作用域、源/镜像、复现步骤、预期/实际、证据、根因、修复文件和复测记录。

## 修复与复测轮次

| 轮次 | 输入/镜像 | 失败/阻塞 | 修复 | 复测与影响回归 | 结论 |
| --- | --- | --- | --- | --- | --- |
| 准备阶段 | 见上方，后端未冻结 | 完整业务用例未运行 | 已完成的准备修复见缺陷表 | 仅现有定向/环境证据 | 不作整体验收结论 |
| 维护修复定向回归 | 固定main11ad37987对照当前源码；无新API镜像 | REG-001的请求体/zstd解码保护 | 沿main行为补route共享预算、取消与解码器窗口/资源约束，保留重构Native封定链 | Go1.27.3真实httputil包-race，150测试及子项通过，0失败/跳过；源码输入前后匹配，临时目录已移除。回执.local/rebuild-wave4/cpr-body-maintenance-check.json | 仅此组件通过，入口/transport与业务整体验收待执行 |
| 核心图片事实回归 | 当前model/execution与modfiles前后匹配 | 新链遗漏独立image measurement的消费端 | clone/decoder/core/NativeEvidence共同补口 | Go1.27.3、-race，139测试及子项通过，0失败/跳过；.local/rebuild-wave4/native-image-core-check.json | 核心组件通过，不代替全部媒体验收 |
| 首轮维护组合回归 | 后端全部Go输入前后不变，无overlay；实际pkg | GW-03/04；新接入Ops回归的catalog类型及路由旧Stop签名编译失败，handler9未执行 | 两处测试消费接口已修；WS owner定位两个实际service失败 | CPR transport/固定TTL32通过；ingress仅middleware1通过，其余未编译；Native service77通过/2失败；handler9零命中且编译失败。回执.local/rebuild-wave4/maintenance-integration-check.json | 批次失败，不能宣称全绿或复用32证明其他组；受影响复测待执行 |
| 入口及handler复测 | 分开使用实际包、原shared lock与SDK；不复制production或overlay | 前批编译、真实撤权分类和新增测试的协议/Partial预期偏差 | 修复source类型/Stop接口；同一root识别typed principal撤回；取消后显式发全历史、过时control按真实mux合同拒绝，Partial仅耐久pending | ingress/Ops82通过且输入稳定；handler最终39通过，只有一个未执行review测试文件并发漂移，非全树freeze；回执cpr-ingress-ops-recheck1.json、native-handler9-recheck3.json | 相应回归通过，测试预期修正与业务修复分开；不升级全站结论 |
| 网关scope与映射复测 | 实际fresh authority/原builder/money journal端口 | principal与资源原因混淆、simple过时cycle、正常结束旧permit仍活 | typed provenance；非计费映射也校验raw；原Dispatcher退休gate | authority35通过（未执行WS测试文件漂移）；adapter13通过/输入稳定；finish/permit36通过/输入稳定；回执native-authority-origin-recheck1.json、native-simple-mapping-recheck.json、native-finish-dispatch-fence.json | 受影响组件通过，不能相加为唯一全覆盖数 |
| scheduler/BPS应用扩审 | 当前实际service包-race，记录Go输入稳定 | 新的local probe归因与c5af模式范围；BPS repair/completion以及WS续接 | 原共享分类早于probe结束；BPS保typed cause/修复身份/有界completion确认 | scheduler94通过；BPS187通过/5失败，全部失败落在新WS continuation consumer，0skip；回执scheduler-local-origin-and-main-caps.json、bps-gateway-review-application.json | BPS组合失败，WS接线由owner继续修；187不冒充完整WP11通过 |
| public模型和真实grant回归 | 三个HTTP实际handler与独立真实PG harness | 映射后授权错位；GetByID漏实体edge | 两项先有失败回执后改真实函数；模型测试隔离独立OAuth别名默认值 | handler7通过；PG3通过，0fail/skip，记录输入稳定；回执native-public-model-repro2.json/recheck2.json、native-real-key-scope-repro.json/recheck.json | 只证明对应scope/撤回；PG key测试不当作新intent/原账务兼容验证 |

完整执行开始后，每轮追加实际记录。旧轮次保留，修复后影响到的旧结果标明失效和替换关系，不混合不同源码/镜像制造全绿。

2026-10-26后续组合阶段：gateway-next-consumers-unit为I2公共字段接线未齐而零执行；补字段后gateway-next-consumers-recheck的service又受supplier新测试调用旧签名阻挡，handler48项通过/1项旧log-only期望失败。两处均已原位修正。gateway-service-consumers-recheck实际286项通过/4条失败（3叶子+父汇总），失败仅是MAIN图片映射夹具没有价卡却预期Settled；生产保持正确PendingPricing，已修正测试为耐久待定价并保持原始/hosted/canonical断言。后续image-scope-handler-recheck选择器只匹配handler1项通过，未匹配的service不能算通过。image-scope-and-intent-unit又遇正在写入的冻结结算器类型/字段编译缺口，只有billing4项执行通过；失败记录保留，正在收齐共享合同后分别复验。不能把不同阶段的通过数相加冒充完整通过。

## 2026-10-26 最新回执复核与未覆盖项

本次为回执、源码hash和实际运行镜像复核，没有重新执行应用测试。最近组合回执`.local/rebuild-wave4/ec-contracts-stable.json`：Go1.27.3、unit、race，121.693秒，退出码1；265个测试/子测试pass事件、1个fail事件、0个skip。原日志有268个run事件，其中2个Anthropic工具适配测试已pause而未取得pass/fail终态，不能计为通过。来源记录前后一致，本次再次重算记录内全部后端文件亦无漂移。

- 失败为`TestCloudCatalogPreflightPreservesPendingOwnerAndAllowsRetry`。小目录重试在`openai_cloud_session.go`的`startTurn`写入`Telemetry.UpstreamStartedAt`时panic。测试helper没有初始化Telemetry，而正常新请求入口有初始化；目前是可定位的夹具生命周期缺口，不能据此直接断言正常生产请求也会panic。未修复/复测，不关闭失败。
- 该service进程提前退出，后续Cloud交接/终态/compaction测试，以及VendorAccountProjection、AccountAuthorizationModes、OAuth401和BPS池恢复测试没有取得本批执行证据。admin/repository的独立投影测试通过不能代替这些service消费者。
- 部分NativeIntent单测及execution 429、共享工具schema、图片URL/handler等在本批通过；此处不把与旧回执重叠的计数累加为总覆盖率。
- WS429/HTTP bridge凭据恢复的中断补丁尚无对应完整定向回执；最新I2夹具变更未在真实PG下复验；SSR/embed消费者未在本批执行。不能以unit或编译代替这些验证。
- Native runtime接入尝试已按精确文件备份撤回，`.local/rebuild-wave4/native-runtime-incomplete.json`保存校验。生产仍是先prepare后Record，发送前意图与原worker hook未接通；组件结果不能关闭GW-07。
- 本次读取的main/origin-main为`ce3ecf6a1c9c6bac9d4614db174d1fef0599942c`，ec→ce新增Cloud生命周期及前端行为尚需适配和本分支验证。维护分支的测试数不转记为重构通过。

本机Docker现场检查仍为旧API `rebuild-api-162a697a6711`、主站与供应商 `rebuild-90c927992b54`。752项前端用例目录为`authored_not_executed`，最新前端交接也明确Docker/实际页面/完整752项未执行。owner在新一轮六项缺口修复和维护同步中，旧manifest与本次读取时的87个已有文件不一致；这是开发中的合法变化，不作为其他会话错误，也不据旧manifest构建最终镜像。

本次完整规划落地结论见[READINESS](READINESS.md)。它是状态审计，不是业务验收通过。

## 最终判定

2026-10-26组合复测：`.local/rebuild-wave4/gateway-reviewed-consumers-combined.json`，Go1.27.3 -race实际service包203测试及子项通过、0fail/skip，217.961秒。包含BPS重连/HTTP↔WS/恢复工具目录与failover、R5三个WS模式的raw/canonical计量、父账号状态/期限、Cloud实时终态及价格视图/热更新。期间只新增account_parent_projection_integration_test.go与openai_cloud_pricing_pin_test.go，二者都不在本次执行selector；生产及已执行测试源未漂移，不能将这份证据说成全工作树冻结。

2026-10-26真实PG复测：`.local/rebuild-wave4/native-shadow-logonly-postgres.json`，CI=1、integration -race、3项通过、0fail/skip，41.500秒、输入稳定。实际repository验证影子父状态/组成员/删除投影、原Native公开adapter正常并发结算只扣一次，以及simple/log_only两次终结仅1usage_log/1dedup且余额/Key额度窗口不变。临时测试容器由原harness退出，长期UI环境不重建；不是生产数据或真实上游证明。此前adapter定向22项通过，只有未执行的PG测试文件发生变化。

Native intent I1的独立owner证据已收到：73项分包单元与8项真实PG测试及子项分别通过，覆盖299/300迁移形状、既有money兼容、技术行领取隔离、NotSent关闭与保留；详见`.local/rebuild-billing-owner/native-intent-i1-consumer-unit.json`和`native-intent-i1-postgres.json`。这不是MAIN before-send/Finalizer/恢复worker已接通，I2/I3仍在实施。

2026-10-26实时观测与账务恢复分离定向复验：prepareOpenAIUsage原来会重置403计数和累计缓存指标；现已移至live完成边界，Native重复finalize只执行一次，Cloud补账不重放。实际service包Go1.27.3 -race运行22测试及子项通过，0fail/skip，57.688秒，输入前后完全一致；包含真实Cloud worker的ledger失败重试和Native journal失败。回执`.local/rebuild-wave4/native-live-observation-boundary.json`，临时目录已删除。此证据不代表新价格快照/intent接线完成，也不是新Docker镜像验收。

当前结论：未达到首版完整验收条件。

完成条件：同一有效来源下全部必需本机用例及变体有结果，已发现的可修复产品缺陷关闭并完成复测/影响回归；failed、blocked、not_run逐项可追溯，未执行项不得算passed。真实OAuth、支付、上游等缺少条件或必须由人完成的动作保持明确阻塞，不为清空报告而关闭保护、伪造数据或改为不适用。

最终报告应同时给出用例覆盖统计、各轮缺陷及修复闭环、剩余条件、实际来源和镜像、数据恢复及资源实测。只有达到用户规定的共同交付条件后，才整理一次独立根提交并推送；本报告不授予发版、生产部署或生产迁移。

## 2026-10-27 BPS规则调整的独立复核（进行中）

范围：同请求BPS不转Native、分类停用/恢复、429作用域与持久期限，以及停用后独立Native WS执行。当前不是完整统一桥接或整站验收结论。主源为共享codex/sub2api-rebuild未提交工作树；所有原维护分支和Git索引未修改。

- 已确认并修复Native发送前pricingFacts遗漏、handler请求级pricing绑定、ctx_pool/passthrough真实producer投影；同轮重试固定原价格/模型/身份，缺canonical不生成随机ID。handler后续真实消费者仍在复测。
- 定向service回归初次运行有59个pass事件、7个fail事件（含父测试），进一步定位到Native journal确认后仍走writeOpenAIAuthorizationUsage的重复日志旁路；已删除该重复写入并保持原防重复断言。
- 后续稳定源码组合回归native-http-ws-and-oauth-review：119个pass事件、3个fail事件（2个根场景：HTTP桥接次轮、同轮重试），0skip，退出1，运行97.403秒，前后源码/SQL/embed输入哈希相同。真实OAuth HTTP边界的拒绝分类与非授权故障负例已运行通过；整组仍失败，不能记为整体passed。
- PostgreSQL组bps-state-postgres-independent未进入业务测试，因旧usage_billing_event_native_adapter_integration_test.go的回执类型不能编译，0命中；正在迁移同一测试到技术/货币回执及真实PG intent保存，不跳过它。
- 独立新增PG冷却并发只延长、72小时期限、scope隔离与事务回滚/发布回滚测试，尚未运行；它们只计划R级证据，不冒称两个真实网关进程的M级验收。
- 本轮机器用例目录检查5项通过。此前编译失败和共享锁等待退出均单独保留，不计作业务测试通过。

证据：.local/bps-independent-review-20261027/；早期编译/锁等待回执为.local/rebuild-wave4/bps-review-native-fixtures*.json。脚本复用原定向runner，私有副本补齐backend全部版本化源输入（含SQL/embed）哈希；不修改项目验证平台。当前API Docker仍是旧检查点，尚未重建本轮镜像，不以旧容器响应替代当前源码证明。最终待源码稳定、定向race/真实存储通过后更新受影响镜像，并在本节补充最终结果。

### 独立复核后续修复与实际存储复测

- Native handler两个真实失败已修复：正常HTTP桥接清理先解绑capacity取消关联再cancel；同operation的安全EOF恢复仍遵循PROTOCOLS §4.1，不能借取消BPS回退而删除Native已允许的有界恢复。扩大组中handler新旧消费者通过；服务夹具并发race及每轮intent遗漏另行修复。
- postgres-state-and-ledger-final3.json：真实PostgreSQL，integration/race，25个测试及子测试pass事件、0fail/skip，35.634秒。含冷却72小时/并发仅延长/作用域隔离/事务和outbox回滚、自动测试成功不清除其他BPS模型冷却、系统停用claim/CAS恢复，以及Native实际public billing adapter的intent/技术与货币回执。仅两个service测试文件漂移，go list依赖审计确认不属于本组2236个编译/embed输入；该证据可按实际依赖复用。
- native-fixture-race-followup.json：56pass/4fail事件；并发journal、账号变化和晚完成释放场景通过，剩余失败仅reasoning passthrough模型/allowlist夹具。修正后native-reasoning-priced-passthrough.json有10pass、0fail/skip，59.003秒；期间Cloud/BPS生产文件并发变化，不能记作全树稳定组合通过。
- Cloud维护版已取消的8/16/128会话及待结算数量准入仍残留在重构，现按原合同清理，保留正常队列/字节/owner fencing/Stop-Drain-Join和账务保护。该切片尚未执行验证，不标完成。

下一组从一致的临时源码快照执行，复用共享SDK和依赖缓存；原始回执保留。临时快照仅用于避免并发编辑污染编译输入，结束即清理；最终仍需将已验快照与实际工作树差异对照，不把旧快照等同当前源码或镜像。


### Cloud额外准入清理的冻结源码验证

cloud-admission-native-frozen-unit：一致临时源快照6654文件/84,412,146字节，复用共享依赖，Go1.27.3 unit/race，128.249秒，153个pass事件、0fail/skip。包含超过旧actor/holds阈值、Stop/Drain和账务保护、Redis替身索引/队列/fencing、Cloud直接Responses事件输出、Native pricing与多lane/root夹具。执行后工作树仅DTO测试与BPS续接文件变化；快照结果有效，不能自动等同随后变化的工作树。临时源及产物已移除。

cloud-admission-real-redis：integration/race、CI=1，实际Docker Redis的跨客户端超过旧阈值准入及重复session单owner竞争2项通过，0fail/skip，59.406秒。来源前后一致，临时源及产物已移除；未连接真实上游，不代表两个网关进程或全站验收。全部7个原有联调容器仍保留，无新增测试容器残留。

新发现的BPS隐式全模型兼容残留已进入独立修复/测试组，不能沿用上述Cloud结果宣称该新变更通过。当前尚未重建本轮API/Web镜像。


### 严格勾选规则首轮与夹具纠正

bps-explicit-selection-and-bridge-review：冻结快照6655文件，Go1.27.3 unit/race，213.069秒，102pass/8fail事件（6个叶子失败，含2个父汇总）、0skip。Cloud取消/flush/错误分类6个根场景通过；新规则HTTP JSON/SSE及Excel-only拒绝场景通过。handler包编译失败，本组Native intent绑定测试没有执行，不能复用旧组结果充作本组通过；正在定位编译消费错误。整组仍失败。

失败均定位到新增测试边界注入：4个WS双授权case直接调用桥接但没有Native authority/operation/容量绑定；2个Native目录case错误地仅替换httpUpstream，而OAuth目录使用独立HTTP client，真实目录端点收到合成测试令牌后返回401。这不是业务上游验证；没有使用真实账号token。现仅修正测试：WS复用真实Native授权、root、pricing与capacity夹具；目录绑定已有本机httptest URL并断言BPS调用为零。生产授权检查未放宽。修正后的专项复测待完成。


### 严格勾选与目录独立复核闭环

bps-explicit-selection-recheck：实际service unit/race，120pass、0fail/skip，81.079秒，冻结源与运行后源一致；覆盖新选择规则、实际Native WS授权/预算/账务绑定、BPS目录与恢复probe相关测试。两个新目录来源分支经独立只读审查另发现clientVersion丢失和BPS失败阻断未选Native模型，已修复。

bps-manifest-independent-review-fixes：23pass、0fail/skip，89.159秒，来源前后一致。公共Codex入口传递显式clientVersion；缺失列表不请求BPS；BPS access失败时仅提供真实Native目录中的未选模型；selected-only与Excel-only仍返回错误；失败不关闭账号BPS。独立复核确认两条缺陷修复，未运行测试的审查不计入上述pass数。没有把两组重复测试相加为唯一覆盖数。

Native handler扩大组native-handler-intent-and-technical-records：32pass/9fail事件，6个Native intent根场景及两类metered技术记录场景通过，但旧usage日志/partial/拒绝前一轮/usage-limit failover仍有失败，已交独立worker定位真实消费者。不会用新fixture通过掩盖整组失败，也不会把partial强行改为零账或虚构canonical ID。


主集成继续复核负例质量：原严格选择测试的4个Excel-only WS子项仅检查error与零发送，不能排除因缺Native夹具上下文而假阳性。现改从实际WS准入入口执行，并要求明确命中Excel-only未选模型的mode拒绝；不修改生产拒绝逻辑。上轮120pass保留原始记录，这4项加强后的证明须待下一组重新执行，不能自动继承。


### 统一事件输出与Native实际消费者复测

- direct-events-context-and-native-metadata：60pass、0fail/skip，84.711秒，来源前后一致。包括BPS直接事件/缺终态/写入失败、加强后的Excel-only实际WS准入拒绝、Cloud权限上下文保真、Native准备白名单及WS403/首帧来源分类。
- bps-direct-output-existing-consumers：92pass、0fail/skip，89.944秒，来源一致；覆盖既有BPS兼容入口、HTTP/WS取消、flush失败、续接发布/重连、错误帧与HTTP心跳。
- 在这些行为接通后，已移除旧excelBPSResponseWriter的SSE解析器并迁移为共用responsesEventWriter；Write只保留有实际消费者的预流JSON错误体收集。共享context-window/compact错误也直接交事件输出，Cloud错误归因两个消费者一起迁移。shared-response-event-writer-retirement有145pass、0fail/skip，79.421秒；随后新增幂等keepalive Stop及defer Join导致4个文件变化，不冒称整树冻结。该新增生命周期用例在下一组通过。
- native-sse-repair-and-handler-metadata：78pass/3fail事件（含父汇总），119.054秒，源稳定。此前Native UserAgent/推理等级、partial技术记录、额度拒绝及failover旧失败均通过；新HTTP-200 SSE合法修复、严格保护和预算/取消通过。剩余2个叶子失败是修复后撤权/容量租约失效被bridge误包装成通用502并提前写帧，现增加typed Native admission错误的原样返回，待定向复测；没有把保护断言改成接受502。

Native原有UserAgent与可信IPAddress仅作为用量日志标量保留；准备快照仍不包含账号/Key对象、原始请求头、cookie、token或正文。新增可选字段使用omitempty保留旧缺失值的序列化形状；真实PG持久化/幂等用例已扩展，尚待重新运行。

## 2026-10-27 后端收尾：真实入口与条件更新

本节接续已有证据，未提交、推送或调用真实上游。工作树仍为独立重构根快照上的本地改动；前端由其owner处理。新增修复的整体验收尚在执行，不能以本节某一组通过关闭全部WP-18。

- 已应用`control-integration.patch`的完整编辑、Extra、bulk和供应商事务接点，新增安全恢复DTO；旧草稿开启系统停用状态须携带当前disabled_at条件。供应商旧`bps_all_models`明确拒绝，不再删除模型选择列表。
- `closeout-handler-matrix`首次停在编译：SSE扫描器工厂少了调用括号，0业务命中。
- `closeout-handler-matrix-v2`：41pass / 8fail事件（含父汇总），0skip。WS夹具遗漏应用层TurnAdmission导致升级后EOF；另一个真实失败为BPS长Retry-After被通用七天对客规则丢弃。已补真实生命周期owner并保留准入/定价保护；BPS头部复用原绝对截止解析器，其他模式保留原边界。
- `closeout-handler-matrix-v3`：52pass / 0fail / 0skip，62.984秒，unit/race。包含JSON、SSE、WS、Chat两种返回形态、Messages两种返回形态的32个核心叶子用例，以及限流取消/准入与Retry-After邻近回归。HTTP上游和存储为明确替身，真实认证、handler、service、协议及定价代码实际执行；不是真实上游或PG金额事务证明。冻结源运行期间其他独立切片仍在编辑，因此不是整个当前工作树冻结通过。

上述首轮之后继续执行控制面真实PG并发/CAS、供应商list/detail和更新、OAuth凭据应用冲突、WS拒绝后重新准入、Cloud严格整数参数与大小边界及Native bridge实际消费者；最终结果见下表和部署记录。修复与回执保存在`.local/unified-closeout-20261027/`和`.local/bps-independent-review-20261027/closeout-*`。前端753项用例没有因后端通过而转为通过。

### 本轮受影响验证结果

| 回执 | 实际结果与证据边界 |
| --- | --- |
| closeout-control-postgres | integration/race、CI=1，31pass/0fail/0skip，63.125秒，源输入无漂移。真实PG覆盖full/Extra/bulk stale-enable回滚、正确条件重开、显式false取消旧probe和真实行锁竞争；不是外部BPS证明。 |
| closeout-vendor-postgres | 1pass/0fail/0skip，85.639秒。专用loopback PostgreSQL库、独立测试schema；真实供应商list/detail、安全恢复投影、旧草稿冲突及metadata/outbox回滚、条件开启/再次关闭。临时容器及其卷已删除，回执为vendor-postgres-container.json。 |
| closeout-control-oauth-unit | 整个命令exit1，原因是service包新增Cloud测试读取不存在的RPC字段，不能标整组通过。实际已执行的admin包28pass、dto包1pass、repository包14pass，均0fail/skip；这三包对应输入经哈希复核未变化。Cloud测试修正为实际wire结构后，缺失的service验证由下组补齐。 |
| closeout-control-cloud-final | unit/race，98pass/0fail/0skip，104.646秒。系统停用条件/DTO、严格整数字节投影、完整publication大小及envelope边界、缓存重放、root用量和retention。没有修改生产用量解析器来容忍不完整usage；fixture补齐实际协议要求的cachedInputTokens。 |
| closeout-cross-instance-storage | integration/race、CI=1，14pass/0fail/0skip，80.952秒。真实Redis多个客户端的工具命令/事件/账务claim、owner过期/竞争、请求关闭与近限结果，以及PG恢复claim与原子状态发布。是共享存储跨客户端证明，不冒充两个生产网关和真实上游的验收。 |
| closeout-ws-readmission | unit/race，49pass/0fail/0skip，64.749秒。原32个BPS核心叶子用例，以及ctx_pool/http_bridge拒绝后Native/BPS四种新请求组合、重新授权/模型校验、独立root/slots/账务及零旧模型派发。 |
| closeout-service-combined | 325pass/29fail事件，0skip，127.806秒。扩大组发现旧Native测试未经过真实owner、错误历史/计时fixture和旧EOF重放预期；保留强断言并迁入实际授权/许可/账务链。默认计价本来优先客户端模型，旧上游模型oracle已改为默认/requested/channel-mapped三策略，未改变生产计价规则。 |
| closeout-gateway-final | unit/race，436pass/0fail/0skip，159.974秒，冻结源与运行后源一致。覆盖此前扩大组全部失败及Cloud HTTP JSON/SSE/WS工具轮、取消/续接/错误归因、原生/BPS捕获报文矩阵、链式模型映射及取消、严格整数/大小、Native bridge发送状态/未知用量/不重放/上游端点。不是全仓测试或全站验收。 |
| closeout-passthrough-final | unit/race，12pass/0fail/0skip，154.429秒，源输入无漂移。实际handler覆盖拒绝后新请求重做授权、模型映射、root/slots及撤权拒绝；含既有auth-only映射边界。修复此前17pass/7fail组暴露的passthrough出站账号映射遗漏。 |
| closeout-mode-plan-complete | unit/race，37pass/0fail/0skip，117.365秒，源输入无漂移。新增真实Native passthrough次轮channel alias选中BPS的消费者回归，并复测ctx_pool/http_bridge/passthrough拒绝后重新准入、统一计划与Cloud迟到压缩用量。最终4428个后端输入与已部署API构建输入逐文件相同。 |

各组pass数包含父测试与子测试事件，存在重叠，不相加作为唯一用例数量。原`closeout-passthrough-readmission`的17pass/7fail记录保留；错误映射及随后的channel alias/BPS选路遗漏已经修复并由最后两组复测。436组之后仅passthrough生产文件及对应测试变化，最后37组覆盖其影响；不是重跑整组436，也不以编译成功替代后续行为验证。精确输入比较见`.local/unified-closeout-20261027/validation-build-alignment.json`。

### 已验证快照的本机联合部署与实际操作

后端由已验证的未提交源码快照`3a0638612e059de48e7f4fcb665ba1b66175e58c61e16a669e26698ac61ed2ea`独立构建，41.767秒；镜像`sub2api:rebuild-api-3a0638612e05`，ID为`sha256:de9404189f9712c17ff572250b6425443ed3077885ce70206510fb382e4a6304`。构建和部署回执分别为`.local/rebuild-integration/api-unified-20261027/receipt.json`、`deploy-receipt.json`。HEAD仍为独立根快照673c53c13，不能用HEAD代替本次未提交源码摘要。

前端owner交接`frontend/.local/frontend-final-ready-20261027/HANDOFF.md`及2018文件manifest，SHA为`9f7b1da1938a4e376d227ff027ccf1a4b78f3bbd2fab98cd723aeec8e33073e4`；其单次全量Vitest为6401/6401通过、0失败/跳过，双角色构建及五项静态检查通过。主集成逐文件匹配后构建双UI，完整Docker上下文SHA为`4563951d97961fea0847850f9bdc27fed17505445dfbbceae17fe94353c9f8dd`。输入集合不同，因此两个摘要不应相等；对应关系见`.local/unified-closeout-20261027/ui-final-build.json`和`ui-runtime.json`。主站image ID为`sha256:2d217ec7aeec925dd9c81d69ee8526b9545a5db069cda72f63fb4b2c868276e9`，供应商为`sha256:8513972fb6b7127659a87cd448ddd35d433943616ac8dee364bbe39d1ce23923`。

只更新原隔离Compose项目的api、web、vendor-portal，PG/Redis/TLS容器ID与测试卷保持，部署前导出1,014,674字节数据库回滚备份并限制为0600。API `http://127.0.0.1:32180/health`、主站`http://127.0.0.1:32181/login`、供应商`https://127.0.0.1:32182/login`均200；HTTPS使用既有任务CA验证，未绕过证书检查或改变系统信任。

- `rebuild_api_acceptance.py`对上述精确API image/source运行13项，13通过，1.791秒。包括认证/角色、管理scope、供应商own/foreign对象、敏感导出step-up拒绝及公开设置脱敏；没有生成、付款、外部OAuth或真实上游请求。命令与结果见`api-acceptance.json`和`api-acceptance.log`。
- 主集成使用电脑工具的独立本机IAB页面实际输入合成错误凭据，观察登录被拒绝；刷新后表单清空，再访问`/admin/accounts`，确认重定向至`/login?redirect=/admin/accounts`。临时标签已关闭，未操作生产标签。截图为`.local/unified-closeout-20261027/login-rejection.jpg`，完整观察索引为`browser-smoke.json`。
- 该操作仅证明登录拒绝/刷新及未登录路由保护，不满足整个页面用例的角色、语言和全部断言，不把`FE-PAGE-LOGIN-ENTRY`或753项整体标为passed。中文页面显示英文凭据错误已交前端owner处理，最新快照之后的修复待重新验证及构建。

### 当前仍未完成的边界

部署时共享后端已有39个相对已建快照的并发变化路径，完整清单保存在部署回执；未收取、覆盖或宣称这些后续改动已验证。桥接会话仍在处理跨流续接、缺失模式索引及Cloud跨实例预算/归属转交；须取得其新稳定交付后单独审查受影响输入。前端owner也已恢复修改，9f7b1da快照仍是当前镜像来源，不代表此刻工作树冻结。

753个前端用例、92个页面及51个面板的完整电脑操作验收仍由前端owner执行；本轮13项API和登录冒烟不能替代它。真实上游/外部OAuth/支付不在本机替身验证中。完整WP-18、WP-21和WP-25不标accepted，没有暂存、提交或推送。

本轮已完成的Cloud retention稳定交付审核及受影响复测闭环，已删除仅用于重复轮询该交付的定时跟进；这不表示新一轮并发开发或整个重构完成。只清理本任务临时快照/编译产物、专用测试PG资源、成功运行的冗长日志及两个未部署的中间UI镜像；保留精简输入/结果、失败诊断、数据库回滚备份和持续联调资源。精确保留容量、删除结果及共享缓存超限而未擅自清理的说明见`.local/unified-closeout-20261027/resource-closeout.json`。


## 2026-10-27 继续收尾：实际Docker控制面与生命周期缺口

新增`tools/tests/rebuild_bps_control_acceptance.py`，复用现有loopback/固定镜像/0600fixture校验与HTTP客户端，独立写集仅限每次新建的停止调度合成账号。管理员full/bulk、供应商PATCH/list/detail、条件重开、旧草稿409、显式false、退休all-models输入、OAuth身份拒绝与原Extra事务等10项实际HTTP/PG验证通过。精确API来源仍为3a063861，回执`.local/remaining-closeout-20261027/bps-docker-control-v3.json`。首两轮失败分别来自测试把reason当code、遗漏OAuth必填type、未反映既有full编辑关闭选项合同，以及供应商输入实际422和合成凭据身份拒绝；生产保护未放宽。真实签名OAuth替换仍非本机合成证明。

在同镜像启动第二个实际Docker API进程，连接原隔离PG/Redis，追加管理员/供应商相同停用条件并发提交与跨实例读取。13/13通过（10基础+3双实例），两次条件写均仅一个200、一个409，失败者metadata回滚。回执`bps-two-gateway-control.json`、`replica-runtime.json`；专用13个账号、用户/供应商、第二容器和临时私有环境已验证移除，原API未变。此M级子集不代表真实BPS恢复probe或Cloud跨实例会话完成。

前端新交接`frontend/.local/frontend-runtime-acceptance-20261027/final-v2/HANDOFF.md`：2026文件、66f4ada428b498fec87e7bf67d072e8dc629f3740221a87641940b895882e5d1，6482/6482单次全量回归、六项静态及双build通过。MAIN逐文件/交接校验后构建并更新主站与供应商；Docker上下文1bae0522affeb33825ba0d3d742f444ceeea876c02fcf587e92665ba2c157d2d，两条实际build metadata一致。PG/Redis/API/TLS容器ID不变。详见`ui-66f4-build.json`、`ui-66f4-deploy.json`；部署首次vendor元数据读取尚未就绪的JSON错误保留，后续同容器读取匹配，未绕过TLS或再重建。完整页面验证由owner在新镜像继续，不能用本次构建代替753用例。

独立生命周期审查发现余额变更后的detached Redis失效、支付完成后的detached通知，两项修复已写入。余额在成功原子变更后、再次读取用户之前登记同步有界失效，保留提交结果和原错误；支付通知复用maintenanceTaskOwner，位于支付扫描join之后、订阅/存储退出之前，稳定复制通知事实。真实Wire生成成功且应用go.mod/sum不变。首次Go组合因活动Cloud接线编译失败0命中；第二次有35个测试通过事件，但runner220秒进程超时且未取得package终态，不记整组passed。最终有界复测仍待回执。

其他平台独立审计确认WP13–17/19仍有真实未迁移消费者；TypeSafe SystemOne单次provider/执行/计价已开始实现，原持久journal接管、唯一恢复分派、实际消费者测试及旧链删除仍在进行。原路径在接管完成前保持服务；不得以新目录、接口或能力检测标记完整迁移。证据在`.local/remaining-closeout-20261027/`，所有代码继续本地未提交。

最终生命周期复测`remaining-lifecycle-payment-balance-final`：Go1.27.3 unit/race，36pass/0fail/0skip，138.467秒、exit0。实际payment通知SMTP/投递持久化阻塞、关闭重试保留owner、不重放支付、余额写后读取失败/请求取消/失效超时及根HTTP排空均有消费者结果。八个本切片源码/Wire/测试哈希仍等于冻结输入；期间五个BPS生产/迁移路径变化，不将该快照外推为当前整树通过。回执`lifecycle-result-summary.json`保存逐项终态；旧超时和编译失败保留。新API镜像待活动桥接/账务接线稳定后联合更新，不把当前3a运行镜像称为已含本修复。

## 2026-10-27 SystemOne 完整本地切片与二次复核

本节覆盖此前 SystemOne 接线中的未完成/错误实现；旧报告中“只实现接口”或“临时仓储直接 Apply”不再代表当前代码。实际链路为 `GatewayHandler.SystemOne → ExecuteSystemOne → RecordSystemOneUsage → FinalizeNativeUsageIntent → Ledger.FinalizeNativeIntent`，复用原 technical journal、canonical binding、dedup 和唯一 usage recovery worker。SystemOne 的 Gateway 计量映射与 OpenAI 映射显式区分，Protocol 绑定发送前 scope；可选新字段省略零值以保持历史 JSON 指纹。未知数量不补零，缺钱款 ID 不生成随机 ID，已确认 Final 不被迟到 unknown/partial 擦除。旧 ForwardSystemOne、handler 内 failover 分支、异步 recordSystemOneUsage 和 capability fallback 已删除，有效旧断言已迁移到真实新消费者。

首次 PostgreSQL race 测试发现浅拷贝 JSON 解码复用调用方嵌套指针，已修为解码至全新对象。最终六个顶层 PG 场景、十个测试/子测试事件在 race 下通过：并发重放只扣一次、真实 worker 重启恢复、未知用量零扣款、伪造 scope/跨协议拒绝、403 后新 attempt 保持单根、缺失 canonical 后合法迟到绑定；还覆盖已结算后的迟到未知观察。初次失败摘要与最终结果见 `.local/systemone-closeout-20261027/race-first-failure.txt`、`test-evidence.json`。service 真实计价器与仓储测试分层；PG worker 的 quote fixture 不冒充真实上游。

最终冻结后端 597456181362b3642814e6ec0e1253cc3d3fbdd311df1cf388f9f32c89c7f07a，受影响六包定向验证 344 个测试/子测试事件通过，零失败/跳过，取得所有包终态；不将重复事件累加为唯一覆盖。根 Dockerfile 用固定基础镜像构建通过（58.496 秒），生成 `sub2api:rebuild-api-597456181362`，image `sha256:73fe0c5bf78884643d8882fe6a910947558a55b77c858ee248a6de6aab64240a`。本次未使用 backend/Dockerfile 或宿主机二进制覆盖作为最终制品。构建、输入、验证回执在 `.local/systemone-closeout-20261027/receipt.json`。

已替换原隔离项目的 API，端口仍为 32180；主站、vendor、TLS、PG、Redis 容器 ID 全部未变。备份 1,340,255 字节仅含本机测试数据，0600 保存。相同镜像上基础 API 13/13、BPS 控制面 10/10 均通过，见 `deploy-receipt.json`。另以真实 HTTP `/v1/systemone` 经实际调度/准入/适配器访问隔离 mock 容器，再核对实际 PG：已知 10 输入/2 输出仅一笔账单，100 余额减为 99.999972；无 usage 的成功响应只留 `pending_usage`，不新增金额或用量行。临时用户、组、Key、账号及 mock 容器已清理；结果 `systemone-http-result.json`。这证明本地全入口接线，不证明真实 TypeSafe 上游可用。

同时修复两项真实缺陷：OpsMetricsCollector 的 SQL 参数推导冲突（PG 作用域回归通过）；非冻结混合图片输出缺分组/渠道价卡时误走默认按张价格（保留目录 token 价格，专用 Images/显式 image 或 per-request 价卡不变）。新镜像无新的 metrics 参数类型错误。电脑工具实际使用合成管理员打开 `/admin/ops`、刷新、查看全部服务器，确认 a07092a5e920 正常上报；截图/AX 为 `ops-browser.png`、`ops-browser-ax.txt`。旧离线节点仍如实显示，未清历史数据。此页面子集不代表全部页面验收。

“分析统一协议桥接方案”新稳定交付的两项 Cloud sampling 归档原因映射已独立核对实际消费者、仓储允许值、SQL 约束及文件 SHA；交付文件与本次冻结镜像一致。补充消费者 3 个事件、真实 PG/Redis 归档 4 个事件均通过，精确字节保留、归档失败不出队、已结算金额不变。见 `cloud-archive-independent-review.json`；没有重复连接生产或真实上游。整个统一模式矩阵、其他 provider 迁移及全站页面仍不得据此标为通过。

此前无 `-tags unit` 的 service 测试未包含依赖的 tagged fixture，不能把那些 undefined helper 诊断一概归因于并发会话；当前已按仓库标签执行并取得明确终态。未 stage/commit/push，完整父工作包仍依 work-items.json 的剩余范围处理。

### 同轮追加：历史协议消费者、前端节点排序与输入漂移

`public-mode-consumers` 原正则在稳定源码取得17个顶层测试/68个通过事件、0失败/跳过，历史15个失败项逐项找到PASS。其全部4489个backend文件与597456构建manifest对应项一致，聚合SHA不同仅因输入集合不同；证据在 `.local/responses-completion-20261027/public-consumers-current-20261027T181806/`。

另外两份历史报告 `mode-consumer-regression`、`mode-review-v3` 的未覆盖项已按实际断言复核。三份旧fixture修复：区分模型权限403与账号故障503并断言模型冷却；WS补实际host逐轮准入和释放；独立生成使用独立response ID且断言只记对应账务；显式tools空列表清除工具目录，同时保留已完成的历史call/result。MAIN已审阅精确三文件增量patch，未放宽BPS→Native回退、账务次数或tool历史约束。10个顶层测试的144个事件在分组运行中通过，**repair运行与后续Cloud改动交叠，不计稳定全源码验收**。精确patch、逐项旧失败对照和限制在 `historical-consumers-20261027T182131/closure.json`；不能累加为144项新独立功能覆盖。

构建后出现并行Cloud生产改动和上述fixture改动，已登记 `.local/systemone-closeout-20261027/post-build-drift.json` 及work-items实际路径；它们不在597456镜像内，不继承其运行通过结果。本轮自行追加的变更仅是删除未使用的backend构建入口和修正BPS注释；根构建与应用行为未变，deploy静态7项通过。

前端owner随后交付并自行部署7c67dd244109…节点排序检查点，MAIN逐文件核对2068文件无差异，并核对main镜像2aa674fe3a48…、vendor镜像3c11eefba989…均健康。MAIN再次使用电脑工具在 `/admin/ops` 点击刷新，确认a07092a5e920正常上报并位于默认预览首项；旧节点仍过期。新截图与AX在 `ops-node-priority-verified.png`、`ops-node-priority-verified-ax.txt`，复核回执 `frontend-ops-independent.json`。本节覆盖前一节“web/vendor仍为66f4ada”的时间性描述；该先前观察仍是当时部署事实。完整753用例、供应商品牌正例及统一模型管理后端接线仍未完成。

资源收尾已删除本任务的临时Darwin/Linux二进制、旧注入镜像及可压缩的完整日志，文件合计372,868,037字节；保留精简证据、当前持续联调角色、0600测试DB备份和必要输入manifest。共享Go缓存实测约16.5GiB，归属混合且仍有并行任务，不擅自全局清理；保留原因与精确清理对象见resource-closeout.json。


## 2026-10-27 重新初始化人工验收环境

按用户明确要求，MAIN已备份并移除旧 `sub2api-rebuild-ui-20261025-4e2074d6` 的六个容器、三个命名卷及项目网络，另移除带 `codex.task=sub2api-rebuild-contract-check` 标签的合同测试PG及其匿名卷。已核对归属且无容器引用的13个旧重构镜像标签一并清理；共享基础镜像、来源未确认的fixture镜像、其他项目网络及共享编译缓存未动。备份仅保留在0600本地文件，不进入新数据库。

新项目为 `sub2api-rebuild-acceptance-20261027`；主站 http://127.0.0.1:32181 ，API http://127.0.0.1:32180 ，供应商 https://127.0.0.1:32182 。后端从当前冻结来源 `4cb8cac3be34dd4cb40f530865a4705398afe051c1b78dcee6933a08220f79ce` 根Dockerfile构建，image `sha256:6698fe2f6dab9f2b089633b054f7d81370d62cc187edbf4f71f71cf1a9d9e24d`。构建前344项定向测试/子测试通过，构建期间输入未变；此组不覆盖所有新增Cloud逻辑。前端2068文件逐个校验，继续复用owner最新 `7c67dd244109...` 的两角色镜像，实际重新创建容器；未修改frontend源码。

六个新容器healthy；10项部署检查通过，含主站反代登录与身份、主站/门户健康、构建来源、门户测试CA验证。数据库仅有1名初始超级管理员，上游账号/API密钥/用量/供应商均为0。电脑实际验证登录后停在部署与运营确认弹窗，由用户亲自确认；未代为接受协议。未执行真实上游调用。该环境供人工验收，不能标记全功能或整个重构验收通过。

权威运行记录、启动配置、私有登录资料和备份位置：`.local/manual-acceptance-20261027/environment.json`、`smoke-report.json`、`LOGIN.md`、`old-environment-backup/`。旧 `.local/rebuild-integration/environment.json` 已显式标为superseded；旧role fixture仅是历史证据，不得拿它运行新环境测试。用户人工验收期间保持这套运行镜像固定，后续替换需重新记录来源并与人工验收协调；新数据库不沿用此前的合成多角色及确认记录。


## 2026-10-28 CPR增量整合与同源复核

用户指定会话最新深挖研究已按当前重构实际消费者复核；采用/不采用理由和版本矩阵见 [CPR_INTEGRATION](CPR_INTEGRATION.md)。落地范围：已接纳请求计划一致性及发送边界资源读取；目录scope/generation和三态证据；Redis普通用户/账号FIFO ticket、取消及共享等待期限；Ops连接预算、统计缺口与诊断完整度。未引入另一套执行器、目录、插件Runtime或账本。

扩大验证另修真实桥接：Messages/Chat向原Native意图传入原始/上游/计价模型；模型事实读取实际持有正文而非不可信GetBody；WS复用绑定目标、代理和凭据owner代际；取消采集不继续SQL查询。显式group_id=0仍拒绝，权限未放宽；旧夹具已按当前合同修正。

冻结来源 04b36ed10bd01e6cd0ed2802ed6ed480b4341692584d13b02cf963035693a636，六包2061个测试/子测试通过，定向race294通过，真实Redis13通过事件，均0失败/0跳过。真实PostgreSQL取消和连接池复用计入unit/race，覆盖重叠不相加。Root Dockerfile构建56.901秒，候选 sub2api:cpr-review-04b36ed10bd0，image sha256:617cf758a569c5f7015772a04a263e7b2a1449cddfd37bd80e7e82b118e56599。隔离容器内health/public settings/login/auth me四项200；没有真实上游生成，未接受运营协议，不代表全站页面或整个重构验收。

回执 .local/cpr-integration-20261028/verification.json 与 docker-smoke.json；命令、输入、失败修复记录、压缩日志同目录。临时API/网络/Redis/PG及测试卷清理，候选镜像与精简验证/恢复资料保留，见 resources.json。共享Go缓存超限且有其他任务，未不安全全机清除。

人工验收项目 sub2api-rebuild-acceptance-20261027 未替换，仍由 .local/manual-acceptance-20261027/environment.json 指向。新增诊断字段已交接frontend owner，MAIN未修改frontend，无Git暂存/提交/推送。
