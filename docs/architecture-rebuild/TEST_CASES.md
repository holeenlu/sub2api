# 独立重构首版完整测试用例

2026-10-25，按用户要求开始编写。本文组织最终执行顺序与证据；详细用例见同目录 test-cases.json。需求、契约、工作包状态分别继续以 REQUIREMENTS、CONTRACTS、work-items.json 为准。这里不另立业务规则或宣称父工作包通过。

## 执行时机与边界

- 开发期间仅执行受影响的定向回归。全部开发完成、前后端源码及生成输入冻结后，再执行整套测试和本机 Docker 实际页面验收。
- 前端会话维护[版本化前端用例](../../frontend/tests/acceptance/README.md)及其机器清单。根用例只链接这个入口，不复制页面、redirect、alias清单。
- 已有单测、替身测试、真实 PostgreSQL/Redis、多进程、浏览器、真实客户端和真实上游分别记证据，不互相替代。测试文件存在、退出 0、零命中或 Skip 均不代表通过。
- 本轮编写的用例初始均为 not_run。既有阶段回执只可在源码、依赖、工具链、夹具和环境仍匹配时复用；旧镜像不能证明当前工作区通过。
- 仅使用隔离本机测试数据。真实上游、真实支付、生产迁移和线上测试不在本轮执行授权中；不得接受真实法律承诺、关闭权限保护或绕过 TLS 来清除阻塞。
- 测试完成之前不 stage、commit、push；首版验收通过后才按现有交付规则统一整理独立根提交。

## 用例与执行入口

2026-10-26按用户点名的四个维护会话，已将[维护修复对照](MAINTENANCE_FIX_AUDIT.md)纳入同一用例目录，连同整个网关的扩展复核，已补充13条既有用例，编号与92项定义总数不变。固定main的历史通过记录不作为重构通过结果；当前全部定义仍为not_run。

2026-10-27按[CT-02 BPS路由修订](CONTRACTS.md#bps-routing-20261027)扩展TC-EXEC-02、TC-SESSION-04：当前BPS执行在能力/授权/限流失败及候选耗尽后不调用Native；403仍按条件持久化关闭开关，后续独立新请求可以走Native，旧续接/工具回复不自动迁移。覆盖HTTP JSON/SSE与WS、自动停用开关两种取值、Excel-only及并发恢复条件。编号和总数不变，新增断言仍为not_run；本次文档变更不代表旧回退代码已删除。

同日按[故障分类与恢复合同](CONTRACTS.md#bps-failure-recovery-20261027)进一步扩展TC-EXEC-02（请求/模型/账号/池失败分域及429期限）、TC-ACCESS-04（有限刷新、确定失效、依赖故障与重新授权）、TC-CONTROL-04（真实存储/多实例领取、停用/恢复CAS及状态传播）、TC-HOST-06（探测退避、完整成功、取消与Stop）。仍沿原92项编号和证据级别；单进程替身不替代多实例/真实PG证明，旧403通过记录不覆盖新增授权/限流分支。

| 入口 | 内容与适用范围 |
| --- | --- |
| [test-cases.json](test-cases.json) | 79项后端/系统场景及13项本机API场景；稳定ID、前置、步骤、断言、WP/AT映射、既有测试锚点和所需证据层级 |
| [VALIDATION](VALIDATION.md) | 原有AT到REQ/CT映射、协议夹具、已有命令及U/S/R/M/C/P/O边界 |
| [本机API用例](../../tools/tests/rebuild_api_acceptance.py) | 直接请求实际Docker API的13个unittest入口；不是页面自动化或生成链路验收 |
| [脚本安全检查](../../tools/tests/test_rebuild_api_acceptance.py) / [目录检查](../../tools/tests/test_rebuild_test_catalog.py) | 离线验证误连保护、凭据文件、镜像绑定、重定向拒绝、用例/源码引用；不读取本机fixture、不启动Docker |
| [前端用例唯一入口](../../frontend/tests/acceptance/README.md) / [前端交接](FRONTEND_HANDOFF.md) | 页面、redirect、alias、跨角色/异常/响应式等用例和实际进度，由前端会话维护 |
| [证据模板](templates/ACCEPTANCE_RECORD.md) | 实际命令、输入、退出/命中/Skip、断言和清理；不把用例JSON变成第二套任务状态库 |

existing_test_files表示可复用起点。existing_partial不保证既有测试覆盖用例的全部步骤；manual_or_gap必须由对应WP补齐脚本或实际人工操作。源文件存在不代表用例已实现。13项API用例为ready_not_run，代码已编写但尚未对最终制品执行。

P0优先验证权限、账务、身份隔离和不可重复副作用；P1验证其他完整功能、部署、维护与体验风险。优先级仅用于排定执行顺序，不授权跳过任一必需用例。每条用例完成后记录对应实际数据与恢复结果，不以同组另一用例通过代替。

## 用例分组

| 前缀 | 数量 | 主要核验 |
| --- | --- | --- |
| TC-PROTO | 8 | 各协议入口、SSE分片、Cloud完整envelope、BPS工具图片、strict与metadata |
| TC-EXEC | 9 | 唯一执行/预算、一次性许可、NotSent、首输出、连接deadline、逐跳redirect、密封重放 |
| TC-ACCESS | 8 | 排队撤权、组范围、OAuth/PAT/Excel分类、容量租约、模型/价格身份 |
| TC-SESSION | 7 | 跨主体续接、WS多流逐轮准入、排队取消、FIFO/fork、双进程fencing |
| TC-BILL | 9 | 事务幂等、失败恢复、unknown/zero/lower_bound、迟到usage、价格、豁免、历史键 |
| TC-CONTROL | 6 | RBAC、批量原子拒绝、revision/CAS、通知丢失、step-up、合规负例 |
| TC-HOST | 7 | 构造、Start/Drain/Join、关闭顺序、inline任务、SMTP/定时器、容器重启 |
| TC-MEDIA | 5 | batch、文件归属、语音并发/音色、双向音频、降智与Fable阈值 |
| TC-SEC | 5 | 可信IP、出口保护、公开设置、日志秘密、恶意品牌输入 |
| TC-DEPLOY | 7 | 独立构建、空卷安装、三角色制品、HTTPS、双品牌、更名保卷、依赖故障 |
| TC-CLEAN | 4 | 旧路径退出、依赖方向、许可证与广告、下载包和语言生成物 |
| TC-FINAL | 4 | 真实客户端、待另授权的真实上游、性能/资源、整体证据与一次交付 |
| TC-API | 13 | 匿名/无效令牌、六角色、合规/RBAC、供应商范围/对象隔离/step-up及公开设置 |

前端页面数以其最终冻结的路由/用例目录为准。当前已核实的92项页面验收、17 redirect、2 alias仅是输入快照，路由变化由前端owner重新核对，根目录不复制第二份页面用例。

## 测试数据与环境

| 夹具 | 前置与使用方式 | 证据限制 |
| --- | --- | --- |
| F-CORE | 既有Go测试、httptest/RPC double、可计数上游；完整报文、工具/图片第二轮、失败/取消 | mock和miniredis归S，文件名不升级证据 |
| F-STORE | 现有Testcontainers，专用PG/Redis及测试迁移；CI=1防无Docker静默成功 | 两个client不是两个进程；Cloud actor DB15 FlushDB只能指向专用可丢弃Redis |
| F-ROLES | .local/rebuild-integration/role-fixtures.json六个合成角色及environment.json；秘密文件0600、仅运行时读取 | 不使用bootstrap人类账号、不改注册策略、不调用compliance/accept；缺fixture则blocked |
| F-RESOURCE | 已暂停/不可调度的admin/L1/L2账号与代理、empty vendor、无效上游凭据/保留域名 | 不probe、refresh、授权真实上游，不产生真实生成 |
| F-CONTENT | draft公告、非上架合成套餐、md/iframe页面；可变操作保存前值并finally恢复 | 取消UI等待不等于取消job，套餐显示不证明支付 |
| F-FRESH | 新Compose project/端口/空卷用于setup、双进程故障或迁移试验 | 不清持续联调卷、不打断并发页面验收、不复制生产数据、不全机prune |
| F-TLS | vendor浏览器origin严格匹配配置的HTTPS及受信证书/隔离测试信任 | HTTP API携带Origin仅验证服务器分支，不能证明浏览器TLS通过 |

重建镜像后先用实际容器/镜像和构建回执核实输入，再更新既有environment与角色fixture的非秘密provenance字段；不能为通过脚本只改SHA文字。当前运行旧镜像不证明最新源码。fixture不保存验收结果；实际结果用既有证据模板记录。

## 完整执行顺序

1. **冻结输入**：各owner完成计划范围，确认前后端源码、lockfile、生成文件、Wire/Ent、工具链、测试数据及合同版本；未提交内容也纳入摘要。HEAD不能唯一标识dirty工作区。
2. **静态及离线回归**：目录/脚本、既有根工具、后端unit/必要race/lint、前端lint/typecheck/完整Vitest、locale与下载包。零命中、Skip、导入失败不算passed。保持共享验证锁，复用仍匹配的阶段证据。
3. **真实存储**：repository integration及账务/RBAC/revision/lease风险场景；记录实际PG/Redis版本。再用两个独立网关进程完成M级场景，不能用同进程fake替代。
4. **构建并更新本机Docker**：从同一冻结源构建受影响API/main/vendor镜像，记录镜像ID、项目、端口、卷和source fingerprint。health之后执行本文件API子集及控制面CRUD/CAS/隔离场景。
5. **前端逐项实际操作**：沿前端唯一目录完成创建、编辑、取消、错误、刷新、权限等动作，核对实际服务器持久化。外链/iframe/redirect/alias和两个门户按定义覆盖；只截图空页面不算验收。TLS前置缺失则对应项blocked。
6. **安装、配置与恢复**：专用空卷setup向导；同制品多配置、改名保卷、重启、依赖失效、owner切换、资源回收和性能。与持续联调数据隔离。
7. **最终核对**：修复失败并按影响重测；根用例、前端目录、必需AT无漏项。外部条件如实标记，未满足首版要求不宣称全部完成。全部要求完成后才按现有规则统一整理一次独立根提交/推送。

## 最终执行命令

下列是执行菜单，不是本轮已执行清单。Go命令在backend目录，其余在仓库根。重用现有repo_lock、合规共享缓存及任务GOTMPDIR，不新增锁/CI平台/依赖。命令输出用既有任务临时目录记录，保留精简非敏感证据后按CHANGE_DELIVERY清理。

~~~sh
# 现在可运行：不会访问Docker或本机测试账号
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/tests -p 'test_rebuild_*.py' -v

# 最终冻结：根既有工具及生成物
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s tools/tests -v
node --test tools/zh-tw/gen-locale.test.mjs
node tools/zh-tw/gen-locale.mjs --locales-only --check
node tools/zh-tw/audit-locale.mjs
node tools/zh-tw/audit-ui.mjs

# 最终冻结：前端现有脚本（或make test-frontend）
pnpm --dir frontend run lint:check
pnpm --dir frontend run typecheck
pnpm --dir frontend run test:run

# backend/：Go1.27.3、GOTOOLCHAIN=auto，并持有现有共享验证锁
go test -tags=unit -p=2 -count=1 -json ./...
go test -tags=unit -race -p=2 -count=1 -json ./internal/gateway/... ./internal/providers/... ./internal/access/... ./internal/billing/... ./internal/host/...
# service/handler等并发路径按VALIDATION及实际修改另加race；纯核心不能代替全链
CI=1 go test -tags=integration -p=2 -count=1 -json ./internal/repository/...
~~~

lint与生产server编译使用现有入口及实际SDK，不升级依赖来迁就工具。根make test-frontend改为调用完整test:run，删除失效的旧目录选择器及无人引用的critical目标。旧test-e2e/e2e-local可能依赖真实Key或Skip；E2E_MOCK只能证明替身，不能作为自动全通过入口。真实上游按TC-FINAL-02另行授权后执行。

### 本机API子集

下列两个变量必须来自同一次已核对的冻结构建回执。脚本校验Compose project、container ID、运行状态、镜像和127.0.0.1端口，关闭HTTP环境代理且拒绝重定向；登录令牌仅在内存，失败不输出正文或密码。仅执行读取、合成登录和必须拒绝的敏感导出。

~~~sh
PYTHONDONTWRITEBYTECODE=1 python3 tools/tests/rebuild_api_acceptance.py \
  --fixtures .local/rebuild-integration/role-fixtures.json \
  --environment .local/rebuild-integration/environment.json \
  --expected-image-id "${REBUILD_ACCEPTANCE_IMAGE_ID:?set verified frozen image ID}" \
  --expected-source-sha256 "${REBUILD_ACCEPTANCE_SOURCE_SHA256:?set verified frozen source fingerprint}"
~~~

退出0表示13项子集确实执行、无Skip；退出1为执行/断言失败；退出2为前置阻塞。脚本不创建fixture、不更新容器、不写通过回执。它不代替UI、网关生成、真实事务和全量验收；其余自动化缺口仍由对应WP补齐。

## 结果与重测

用户已明确要求：全部开发完成后，将前后端部署到本机Docker，使用指定“电脑”工具（Computer Use/cua_repl）按本目录和前端唯一目录实际操作，生成[测试报告](TEST_REPORT.md)，再依据缺陷自行修复、更新受影响镜像、复测及影响回归，持续到本轮可执行的全部问题关闭。无需逐项再次等待“继续修复”指令；仍保留真实外部环境和必须由人完成事项的明确边界。

每轮顺序为：固定输入和镜像→电脑实际操作→记录预期/实际及缺陷→代码修复和必要定向检查→更新受影响Docker角色→原失败用例复测→关联用例回归→更新报告。前端修改须由其owner接手并给新来源摘要；不能沿用修改前的freeze或通过数。核心权限、账务、重复提交等失败应先完成修复再扩大业务测试。

每项结果关联TC及AT/WP，在ACCEPTANCE_RECORD记录：预期/实际、命中/Skip、命令、source/依赖/fixture/镜像、角色/协议/模式/传输、脱敏日志/截图/请求、数据恢复及资源测量。修复后只复用输入不变的证据；影响完整链路时重测关联链路。failed、blocked、not_run分别统计，不减少分母制造通过率。

前端版本化目录已经落地；首版验收前仍需冻结最终路由、开发中的生产链路以及M级操作脚本。本轮目录/脚本检查通过不代表这些开发已完成。真实上游、供应商TLS和性能阈值等未具备条件保留为明确缺口。
