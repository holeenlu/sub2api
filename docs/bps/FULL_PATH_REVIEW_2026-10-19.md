# BPS 当前版本全链路复核（2026-10-19）

## 范围与基线

本轮补齐此前 OpenAI 原生复核没有重新完整覆盖的 BPS 专项：独立 Excel 授权、路由/权限、HTTP JSON/SSE、客户端 WS 桥接、Messages/Chat、工具和图片转换、会话状态、缓存及费用归集。以既有 REQUIREMENTS、DUAL_PATH_REVIEW、VALIDATION 和 Responses 恢复契约为准，不恢复 Worker、Mihomo、Cookie/票据或其他退役能力。

- 冻结主工作区当时全部已跟踪/未跟踪代码，包含前轮原生修复以及 #31791、#31847；隔离快照 2e5cfa5fe42623917b3064ef36d16f2a35adbe6c，主工作区 HEAD 1118a5363911bcc84dd07968397be148f150b739。快照仅供本机审计，没有推送。
- 归属：三品牌共享网关；未修改品牌、模型/供应商配置、单价或历史账单。
- KDAN 只读取证：0.2.17.2，revision 77924f19e83630f3afafb7f40fe0af792d0c3a3a，2026-10-19 09:24:09（Asia/Shanghai）启动。线上不是本轮修改后的代码。

## 四项确认缺陷与修复

### BPS-01：凭据身份更换后复用旧缓存作用域

原 scope 只有本地 account ID、API key ID 和会话/线程。同一账号行换成另一个上游 user/workspace 后，工具目录、原生工具回放、图片检查点及 prompt_cache_key 仍可落入旧作用域。

修复复用已有 codexAccountIdentityNamespace，加入不可逆凭据身份摘要；不使用会轮转的 access token。回归证明：同用户正常 AT 刷新保持缓存键和目录，替换上游用户/工作区改变键且不继承旧目录。账户/租户/线程隔离和匿名请求的既有规则继续生效。

文件：backend/internal/service/openai_excel_bps.go；测试 TestExcelBPSCredentialIdentityScopesCatalogAndPromptCache。

升级影响：新命名空间可能使既有会话经历一次缓存冷启动；旧工具回放/目录按原 TTL 淘汰，不跨身份搬迁。仅发送工具结果、依赖旧缓存的客户端可能需要重送完整工具定义和调用历史。未增加清库或生产迁移动作。

### BPS-02：显式 compact 改写损坏大整数

/v1/responses/compact 用 json.Unmarshal 解到 map[string]any，再编码追加 compaction_trigger；工具输出里的 9007199254740993 被改成 9007199254740992。普通 Responses/Chat/Messages 对照保持正确，缺陷位于 compact 的额外改写。

改用项目已有 decodeJSONUseNumber，保留原整数及未知 JSON 值。测试覆盖四种入口，compact 使用完整工具调用/输出历史，检查实际发往 BPS 的正文，而不是仅测试辅助函数。

文件：backend/internal/service/openai_excel_bps.go；测试 TestExcelBPSRequestNumbersRemainExactAcrossIngress。

### BPS-03：工具纠错已计量，但断流恢复先判断零用量

转换器会暂扣尚未验证的工具响应；首次生成已报 10/2 token、纠错又报 7/3 后发生 io.ErrClosedPipe 时，外层 result 仍为零。原逻辑先判断可否换号/重试，直到 defer 才读取转换器已合并的 17/5 快照，因而错误允许再生成；WS 还可能直接关闭而没有对应失败终态。

修复把已有快照读取提为同一局部闭包：join 转换器后、决定重试前即读取；最终 defer 继续复用它。已观测的纠错成本不被当成零消耗，正常错误终态与账单得到保留；compact 用量仍由外层 defer 仅追加一次。没有新重试器或计费链。

原 EOF/取消矩阵增加 closed-pipe，并覆盖 JSON/SSE/WS；验证 17/5、仅原生成+一次纠错两次上游调用、不再产生可重放 failover。真实 handler 的已有中断计费/下一轮测试一并纳入。

文件：backend/internal/service/openai_excel_bps.go；扩展 TestExcelBPSRepairInterruptionPreservesMeteredUsage。

### BPS-04：HTTP 多笔实际消耗共用账单 ID

真实中间件会给请求设置 ClientRequestID。BPS 在账号 A compact 成功、随后生成被限流，按既有安全条件切到账号 B 后，两笔记录都通过 resolveUsageBillingRequestID 变成同一个 client:* ID。去重会吞掉后一次消耗或产生指纹冲突；旧 handler 测试未带中间件上下文，因而没有暴露。

修复复用原生 WS 已有的按实际请求/turn ID 结算规则，将 BPS HTTP 也纳入；有观测用量但无上游 ID 的 BPS 路径已有内部账单 ID，继续使用，不伪造 response.id。原生 OpenAI HTTP 的请求级幂等语义保持。没有调整价格、补扣历史记录或创建新账本。

回归在真实 handler 矩阵注入 ClientRequestID：Responses JSON/SSE、Messages JSON/SSE、Chat JSON/SSE、WS 均验证第一笔 7/3、第二笔 10/4、两个不同账单 ID、无重复记录；同时检查 native WS、native HTTP 的原有语义。该 handler 夹具是 Simple Mode，用于证明归集和身份，不冒充真实扣款；真实数据库去重/扣款由单独集成测试验证。

文件：backend/internal/service/openai_gateway_usage.go；扩展 openai_gateway_record_usage_test.go 与 openai_excel_bps_failover_test.go。

影响：完整计入多次真实消耗后，账面金额可能高于此前漏记值；这不是涨价，也不代表上游成本增加。只有在原恢复契约允许的情况下才继续切换账号；不放开不透明上下文迁移或已交付内容重放。

## 授权、协议、缓存、数据与计费矩阵

| 层面 | 复核与实际验收 |
| --- | --- |
| Excel OAuth | 固定 profile/client/redirect、state/PKCE；独立 grant 加密存储与原生凭据分离；用户/工作区匹配；管理员导入与供应商已验证身份分开。现有 Excel/vendor 专项和全量回归覆盖。 |
| 刷新和撤销 | 本地/分布式刷新锁、40 秒刷新与保存窗口、CAS 防迟到 401/旧刷新覆盖新授权、失败保留凭据、不拿 Codex AT 代用。新增真实 PostgreSQL 16 并发 CAS、撤销后迟到刷新与主身份替换拒绝。 |
| 403 恢复 | 真实 PostgreSQL 验证跨实例单次 claim、恢复和 outbox 原子性、探测期间配置/凭据/状态变化拒绝、进程重启后间隔保持。修正旧夹具：直接种入存量账号，不拿伪 OAuth token 绕过现已加强的创建准入；生产校验未放宽。 |
| 资格与路由 | 全局/账号/映射后模型、Excel-only 不落原生、403 模型权限与通用403分离、429 冷却与原生额度分离、兼容入口及 WS 后续轮次资格。 |
| HTTP/WS 协议 | 一条已确认生命周期、完整终态、取消身份、首输出/尝试预算、关闭/join、真实 response.id 与账单 ID 分开；没有从无 ID 错误捏造续接锚点。 |
| 工具与数据 | catalog/namespace/custom/function/code/cmd；整批验证与有界纠错；原操作不被纠错替换；结构化输出、精确数值、密文/完整上下文、工具回放和图片附件。保留 #31791/#31847 及历史错误修复。 |
| 本地/共享缓存 | 账号+Key+线程+稳定凭据作用域；显式 tools:[] 清空、追加目录 CAS、回放签名、TTL/LRU/容量、状态失败的拒绝语义。新增两个真实 Redis 客户端的 Lua/CAS 竞争、隔离与到期失效；原 miniredis 的边界覆盖继续保留。 |
| 图片/compact | 请求内附件去重、不复活已移除的跨请求 file-ID 缓存；授权/账号/代理贯穿上传、compact、纠错与生成；历史/新增字节边界、检查点及累计用量。补跑此前超时的大图竞态范围。 |
| 计量 | progressive 为快照，terminal 为最终口径；生成/compact/repair 真实消耗累计一次；cache creation 转普通输入只影响 BPS 结算/下游口径，保留内部观测值；未凭本地美元费用推算官方周额度。 |
| 持久结算 | 单独真实 PostgreSQL/Redis 验证余额、订阅、账号配额、NUMERIC 精度、durable event、去重、崩溃恢复；不以 mock/编译成功代替数据库运行。 |

## KDAN 数据核验

只读 SQL 固定交易为 READ ONLY、限制查询时间；不输出 access/refresh token、私有 grant 密文、会话正文或工具参数。没有创建真实授权、强制刷新、付费探针、部署或生产迁移。

冻结窗口 2026-10-19 09:24:09 至 14:46:59.414941（Asia/Shanghai）：

- BPS usage 共 6,384 行；全部有 billing dedup 和 applied event；重复账单身份、负 token、超过八位金额精度的记录均为 0。
- 其中 65 行是 free/零用量；这只能表示网关没有计入用量，不证明上游绝对免费。
- 启用 BPS 的 12 个账号均使用独立 Excel grant，cache_creation_as_input 都为 true。日志中 cache_write=0 是当前结算政策下的值，不能当作上游没有写缓存的证明。

| 已结算 BPS 分组 | 行数 | 计费输入中 cache-read 占比 |
| --- | ---: | ---: |
| Responses HTTP/SSE | 5,001 | 92.2272% |
| 客户端 WS | 1,046 | 95.2712% |
| Chat SSE | 257 | 97.3332% |
| Responses HTTP JSON | 15 | 53.3244% |

比例为 cache_read / (ordinary_input + cache_read + cache_write) 的 token 加权结果，不是请求命中率、物理 KV cache 命中率、单次费用变化或周配额权重。

另取同一批近期 BPS 用户的两次一致性快照，14:39:37.365714 至 14:57:49.899562：共新增 2,217 行，其中 BPS 350 行、其他协议 1,867 行；BPS 日志费用 233.1474360000，全部余额结算费用 363.0740884400，实际余额下降 363.07408844，充值变化 0，差额 0。因存在混合流量，不把这个结果标为 BPS 独立扣款对账。

截至第二快照，6,619 行 BPS 用量均没有可用的独立 upstream_request_id 列值；5,541 行的账单身份为 client:*。运行时日志级别仍为 warn，未保留 gateway.cache_usage/cache_route/cache_prefix_transform 原始观测；本次读取的应用日志也未找到 billing fingerprint conflict 字样。不能用“没有冲突日志”否定本地已复现的 ID 合并缺陷，也无法据这些旧数据补出每次 compact/repair 的上游原始成本。

## 验收证据

所有下列命令均使用 -count=1，实际执行后退出码为 0。数量按顶层测试统计，各套范围有交集，不能相加冒充独立用例数。

| 验证范围 | 结果 | 边界与证据 |
| --- | --- | --- |
| 后端全量 unit | 13,127 通过，99 跳过；376.71 秒 | 61 个包通过。跳过不是通过，主要受外部服务/专项数据库前置条件限制；本轮相关 BPS 实库项目另行补跑。 |
| BPS/Excel service + handler race | 239 通过，1 跳过；350.62 秒 | 15 分钟超时，GOMEMLIMIT=2GiB；不排除大图测试。唯一跳过的供应商 BPS-on-Claude 导入测试已在下列实库组通过。 |
| BPS 转换协议 + transportdiag race | 252 通过，零跳过；41.35 秒 | 真实 Go race detector，无 race 报告。 |
| repository BPS/Excel/UsageBilling 实库 race | 37 通过，零跳过；29.84 秒 | Testcontainers 实际启动 PostgreSQL 18.1 与 Redis 8.4；涵盖 CAS、撤销、403 恢复、共享缓存和账本。 |
| 供应商 OpenAI/BPS 授权实库 race | 13 通过，零跳过；12.27 秒 | 全选现有授权 integration 文件中的 13 个测试；真实 PostgreSQL，锁夹具仍为 miniredis。上游身份校验为替身，不等同真实 IdP 换票。临时数据库容器已清理。 |
| 三项协议/缓存缺陷定向 race | 3 通过，零跳过 | 含凭据轮换、四入口精确数值及 JSON/SSE/WS 纠错中断矩阵。 |
| 计费身份定向 race | 7 通过，零跳过；52.96 秒 | 含真实 handler ClientRequestID、compact 后安全 failover、原生 HTTP/WS 幂等语义。 |
| 合回主工作区后定向回归 | 10 通过，零跳过；29.94 秒 | 主工作区直接运行，合回文件在运行前后没有漂移。 |
| 静态与构建 | go vet 和 go build 均通过 | vet 覆盖 service、handler、repository、basispoints；build ./...。 |

此前未完成的 TestExcelBPSImageByteCompactionFlow 本轮已在完整 BPS race 范围实际通过，单项耗时 189.58 秒；没有通过降低断言、跳过测试或移除 race 来规避原超时。

主要命令（backend 目录，Go 1.27.2；真实数据库仅使用本机临时实例）：

```text
go test -p 4 -tags unit -count=1 -json -timeout=15m ./...
go test -p 1 -tags unit -race -count=1 -json -timeout=15m -run 'BPS|Excel' ./internal/service ./internal/handler
go test -p 2 -race -count=1 -json -timeout=5m ./internal/service/basispoints ./internal/util/transportdiag
CI=true go test -tags integration -count=1 -race -json -timeout=10m -run 'Test.*(Excel|BPS|UsageBilling)' ./internal/repository
go vet -tags unit ./internal/service ./internal/handler ./internal/repository ./internal/service/basispoints
go build -p 4 ./...
```

完整定向命令、退出码、耗时及跳过项列在本机 validation-summary.json，原始测试输出独立保留。

修复前失败复现与修复后通过日志均保留。首次真实数据库运行暴露了旧 403 fixture 不再满足创建准入，以及新增测试误用 Redis EXPIRE 的秒精度；分别修正为存量账号夹具和 PEXPIRE 后重跑。没有降低业务准入或删除失败断言。数据库测试设置 CI=true，Docker 不可用会明确失败，不允许静默零测试退出冒充通过。

本轮代码由主代理实施并二次复核：join 与用量采集顺序、重复采集不重复累加、compact 外层仅加一次、BPS HTTP/WS 账单身份、原生 HTTP 幂等、凭据刷新稳定性、日志不含凭据、真实存储并发与作用域均检查。未声称独立人工审计。

## 交付与仍需线上验证的边界

- 本轮相对冻结工作区：业务代码 2 个文件 +26/-11；测试 7 个文件 +235/-30。文档、指纹清单与精确 .gitignore 例外另计，不把此前其他任务的修改纳入本轮统计。

- 新增对外接口、配置、数据库字段/迁移、上游执行路径、独立计费链路均为 0。复用现有 JSON 解码、凭据命名空间、状态缓存、已 join 的 usage 快照及 WS 账单规则；仅提取一个局部快照闭包，移除 defer 内重复逻辑。
- 仅更新本轮改动在 integration-state.json 中已有的本地指纹，不推进来源 SHA，不将其他任务的改动冒充本轮修复。
- 9 个代码/测试文件已逐文件比对冻结基线后合回主工作区，最终内容与受测快照一致；合回时另外 96 个修改/未跟踪文件未变，主工作区 HEAD 与暂存区未动。文档和指纹清单单独核对后合回。
- 上述审核阶段未提交、推送、发版、部署或修改生产日志/授权设置。后续用户明确授权将本轮修复归入既有功能提交并推送；该代码交付仍不包含发版、部署或生产修改。
- 代码与本地实库验收可完成；真实 IdP 授权刷新、上游每次调用成本/周额度和修复后的线上效果仍需对应真实数据及另行部署后的观察。现有历史日志缺失这些信息，不通过强制刷新生产 grant 或重放用户付费请求来补造证据。

本机证据目录：.release/upstream-sync/2026-10-19-bps-full-review-ytpvtkjl/。包括冻结清单、修复前后日志、只读 SQL、脱敏聚合、实库测试和最终文件哈希；原始生产记录不提交到 Git 仓库。
