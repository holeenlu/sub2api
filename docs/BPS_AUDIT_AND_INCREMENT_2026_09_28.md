# ranxi2001 Excel/BPS 完整性审计与增量适配

## 范围与来源

- 本地公共基线：da2d7d52edfb7f94171e87afc971ae5069554bed。
- 本次功能适配固定来源：9ee2041d3d8ab1235cb53725077e7c70ef5cefda（ranxi2001/production）。没有整体合并 fork 分支。
- 收尾再次以 git ls-remote 核对 production：fd31aa36367975f3f05f23262d4a2414091216a4。其相对固定源的三个非合并提交逐项审查后排除，见下表；BPS 协议本体没有追加变化。
- 归属公共。公共本地提交通过普通 merge 传播至 KDAN、TapModels，保留品牌内容和繁体生成规则。本轮不推送、不发版、不接触生产数据库，不改变版本号。
- 用户明确确认：保留本地主库资格、逐次 RPM、TPM 有界重试和图片容量设置；自动 BPS 仅恢复必要规则、历史、探针和账号入口；旧质量计划继续停用。

| 源提交 | 作者 | 本次处理 |
| --- | --- | --- |
| b570f412ca776edabec6ae6115608fbbd6258e6f | danvilig | 图片策略，经 cherry-pick -x 保留作者并适配本地发送保护 |
| 85c220bc693ac6e49a0d3ef6956f2d5f477c8a4d、b20518a32b289ce2266d5d25aee53fd4439546f7 | AI8888-SHOP | 403 自动恢复/可配周期已在本地；补齐审计、源测试及发送前保护，不重复导入 |
| bf7829995d830b9f698d714d5342748a7f0238b5、b3e494dbdb90db789f7c06a1453b642892badbe2 | 小莫小莫1 | 两次请求状态探针、按次数/用量开 BPS、连续正常恢复；只适配 enable_bps 分支 |
| 211202d4a8abfe7d1b8d51f5edc8cda19ecdf50f、9ec3c0422792dc87ae1b58b440e92cb794a29ac0 | ranxi2001 | 所有权快照、规则冲突防护、BPS 与历史退役规则的索引兼容、账号编辑入口 |
| ee093dcb5e71d02efcb76f4b02439a42e8500676 | ranxi2001 | 优先调度及 Teams 成本回收目标；不替换本地资格、分池、连续性与并发保护 |
| af400b6deb3912241c1e7284b9e54ce830ec53f0 | even | 凭证运营 V2、加密配置、持久化重新登录任务与独立 Worker |
| 5e87f6d0d30899a8fecb34fcab4241d4d31d64be、9a71d281cd1348525c044de67d48bc54fbb7730f | ranxi2001 | 凭证运营命名与后端修正；复用本地图标，不引入旧守护 |
| ce86fc17b9140ebc94c750b5902cb3c3d5c2d04e、bc0e5353cd4476357ff0c7abc26863fc7a060dd0 | ranxi2001 | 排除：源 2FA 导入弹窗关闭和默认登记运维；本地未恢复该导入子系统，不默认登记账号 |
| 2a1400acb21035001a884c071b42859a90594826 | ranxi2001 | 排除：旧外部凭证守护的错误账号恢复；不是本次 V2 / BPS 协议代码 |

没有恢复 sub4api 独立平台、Mihomo、780 定向铸造、请求采集、观察员权限、通用摘组/停调度、鹈鹕展示页、糖果判题或旧第三方凭证守护。普通定时连通性测试继续使用原行为。历史数据库迁移和已退役数据不删除、不改写。

## 已适配部分的审计

| 链路 | 主要代码 | 核对结论 |
| --- | --- | --- |
| OAuth 路由/模型范围 | service/openai_excel_bps.go、account_excel_bps_tools.go、gateway/manifest 相关测试 | 保留 OAuth BPS、模型白名单/全部模型、PAT/Agent 排除和退役独立平台拒绝；不把独立平台重新挂回路由 |
| 原生协议/工具往返 | service/basispoints/{request,response,route,tool*,transport*}.go | 与固定源核对；正文、工具历史和嵌套 cmd/code 参数、混合批次已验证操作及修复上限保留。本地 hosted tool_choice 放宽能力优于源的部分不回退 |
| 上下文/加密/能力声明 | openai_excel_bps_encrypted.go、history_messages、models_manifest | 账号/模型范围下的加密历史忽略和有界恢复、multi-agent 能力限制继续生效；已开始下发的流不重放 |
| 图片 | attachments、image_relay、openai_excel_bps_attachments.go | 原生附件和 HTTPS 中转、工具截图、忽略图片、逐请求数量/字节限制、缓存隔离保留；容量占用与纯文本请求分离 |
| 发送/资格/RPM | openai_turn_admission.go、openai_rpm*、openai_excel_bps_transport.go | 图片压缩、续发和恢复探针都沿用真实发送前主库资格检查与原子 RPM；绑定或凭据改变不向旧出口继续发送 |
| 429/TPM/鉴权 | openai_excel_bps_start_retry.go、ratelimit、auto_disable | TPM 短等待有界、同账号同出口；401/403 按既有分类处理；模型权限类 403 不触发账号全局自动处置 |
| 计费/取消 | openai_excel_bps_usage.go、billing/completion/cancellation 测试 | 压缩与续发的已消费用量合并；压缩后资格/RPM失败转为可计费终止结果，避免 failover 丢失用量；取消与流读取释放保留 |
| 403 自动恢复 | openai_excel_bps_recovery.go、repository/account_repo_excel_bps_recovery* | 修复源/本地均缺少的探针发送前资格及 RPM；复核 BPS 规则/凭据/代理快照；多实例原子认领和条件恢复测试通过 |
| 运维/迁移 | scheduled_test*、account_quality*、priority_scheduling*、account_token_guard_v2*、openai_oauth_reauth* | 新能力按下述最小依赖适配；生成 Wire 与源测试同步。数据库在独立容器验证，不代表生产迁移演练 |

审计依据是实际调用链、代码差异和本机测试；未把同名提交、祖先关系或上游测试报告当作功能已同步的证据。basispoints 核心与源的剩余差异主要是本地 hosted tool_choice 兼容、数量错误说明和 NOTICE 排列，均保留本地行为。

## 新增功能与配置入口

### 图片数量策略

管理侧栏 → 系统设置 → 功能设置 → Excel / BPS 图片（/admin/settings）：

- excel_bps_image_limit_policy：off（默认）、warn、auto_compact。
- excel_bps_image_warning_remaining：默认 8；excel_bps_image_compact_reserve：默认 3。
- 已有 excel_bps_image_max_images 和容量/字节设置保持可编辑。off 维持原限制；warn 提醒并为压缩预留空间；auto_compact 对图片过多的历史执行压缩并在同账号/出口续发，产生额外上游请求与用量。
- 缓存按认证 API key/账号等作用域隔离，策略变化形成独立作用域。不是无限图片能力，也不绕过单张/总字节和并发容量限制。

### 自动 BPS

管理侧栏 → 账号管理（/admin/accounts）→ 新增/编辑 OpenAI OAuth 母账号 → 自动 BPS；管理侧栏 → 自动 BPS（/admin/account-quality）调整模型、Cron、策略并查看记录。

- 默认关闭，账号入口默认每 30 分钟探测 gpt-6-astra；Cron 使用服务器时区。需按实际支持模型调整。
- scheduled_test_plans.pelican_config 中 question_kind=state_probe、quality.action=enable_bps；quality.bps 配置连续异常次数、5h/7d 用量阈值、任一/同时满足、启用后的模型和 BPS 选项；auto_restore、pass_threshold、hold_on_usage 控制关闭。
- API：/api/v1/admin/account-quality-plans、account-quality-results、scheduled-test-plans；只接受自动 BPS 规则。旧计划仍停用，不能通过本次接口恢复通用质量处置。
- 探针使用账号现有代理，两次极短普通 Codex 请求，仅依据 x-codex-turn-state 是否续接回新票判断。这是启发式，不能证明实际模型或能力。默认不发送，只有显式规则启用后运行；会消耗上游请求额度/RPM。
- 网络错误、限流、不完整响应一律不修改账号，也不因已有用量阈值开启 BPS（本地修复了源的这个边界）。探针中途账号编辑、暂停或规则改动使结果失效。
- 仅修改规则拥有的 BPS extra 键，不改变通用分组/调度。已存在的 BPS 403 专项转组选项可显式配置。手工开启的 BPS 不接管，手工修改选项后不强制覆盖，403 自动关闭后不擅自重开。
- 暂停规则不会关闭已经启用的 BPS。数据库租约避免重复执行；每个计划有数量上限，另外按 7 天保留并以每分钟最多 1000 行批量清理自动 BPS 结果。

### 优先调度

管理侧栏 → 优先调度（/admin/priority-scheduling）；setting 键 priority_scheduling_v1。默认总开关关闭。

按对应模型的完整探测轮次、TTFT P90、实时负载、实际收入与理论成本调整自由选择的 OpenAI 文本候选。同一协议/订阅分池内保留手动优先级与连续请求绑定，不降低账号资格、最终并发或 RPM 标准。配置/信号缓存失败或数据不足时按代码的保守回退路径处理。

Teams 子策略的源默认成本为 50 CNY、窗口 4 小时、换算因子 1；这只是可编辑配置，不是当地价格事实或回本保证。总开关打开前必须按经营口径校对。可设置显式窗口、到期倒推或首次使用锚点；不会自动滚动采购周期。详见 priority-scheduling.md。

### 凭证运营

管理侧栏 → 凭证运营（/admin/token-guard-v2）。显式添加现有 OAuth 母账号；不自动登记历史账号，自动重登默认关闭。

后台模型列表巡检，异常阈值后可按管理员选项排队；独立 Worker 实施重新登录。API 要配置 TOTP_ENCRYPTION_KEY；API/Worker 配置相同 OPENAI_REAUTH_WORKER_TOKEN（至少 32 字符）。Worker 另需 SUB2API_BASE_URL 与自行审核固定的外部协议目录 CODEX_PROTOCOL_ROOT/TURB_ROOT、TOSUB2_ROOT。仓库不自动安装这些外部协议，也不启动 Worker。私有邮箱端点必须显式可信主机白名单。

仅支持账号代理或选择现有静态代理。密码、TOTP、邮箱 API URL 加密保存，管理接口不返回明文；Worker 令牌可领取秘密，必须只交给受信任进程。重登写回同时检查任务所有者、状态、原凭据以及任务创建后账号版本；管理员中途暂停账号不能被旧任务重新开启。详情见 account-token-guard-v2.md。

## Bug 修复与兼容性

- 403 恢复探针补上逐次 RPM 与主库资格；无需配置，部署后自动生效。
- 图片压缩成功后续发失败时保留计费结果；无需配置，图片压缩策略启用后适用。
- 自动 BPS 遇到不可判定结果或中途编辑时不改账号；新增规则默认关闭。
- 重登任务不能覆盖后来的账号暂停/编辑；默认不自动重新登录。
- 修复旧批量 BPS 设置测试遗漏的恢复周期/开关；补充自定义恢复周期回归。
- 中英文和生成的 zh-TW 完整；新增运维模块的日文采用显式英文回退。未启用后端繁体转换。

## 数据库与上线须知

新增完整文件名：254_openai_oauth_reauth.sql、254_quality_bps_coexist.sql、255_account_token_guard_v2.sql、256_openai_oauth_reauth_proxy_override.sql、257_openai_oauth_reauth_proxy_source.sql。已有相同编号其他文件保持原样。自动 BPS 复用已存在且未删除的 scheduled_test/quality 表；共存索引允许新 BPS 规则和退役规则行并存，绝不启用旧计划。

本次仅在本机独立 PostgreSQL/Redis 中运行迁移。生产升级仍需按实际数据库、数据规模和备份制度另行安排。回退二进制不会删除新表或加密秘密；凭证任务停用时需单独停止 Worker 并处理排队任务。没有 Docker 镜像构建、生产迁移或 release 操作。

## 本机验证

- Go 全量 unit 已通过；收尾安全修正另跑受影响服务与数据库测试，并执行最终全量回归。
- PostgreSQL/Redis：全新迁移、自动 BPS 生命周期/快照/暂停/8 路并发认领、403恢复认领与条件恢复、V2租约/任务去重/凭据 CAS、优先调度统计通过；额外验证管理员暂停不能被重新登录覆盖。
- 定向 race 与 basispoints 核心全包 race 通过；Worker 24 项离线/本地假邮箱测试通过。
- 公共前端全量 372 个文件、2968 个用例通过；类型检查、生产构建、ESLint、zh-TW 生成检查通过。
- ent 重新生成没有差异；Wire 生成结果与暂存代码一致；本次增量 golangci-lint 为 0 问题。手动发版触发器的 2 项契约测试通过。
- 品牌分支采用普通 merge，并分别复核品牌差异和后端/前端；具体结果随本地交付报告记录。
- 没有真实上游账号或真实 BPS/Codex/密码/TOTP 登录验收。启发式探测的现实准确率、外部协议兼容性及自动压缩的真实上游效果不能由模拟测试保证。
