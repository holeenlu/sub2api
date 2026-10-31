# 前端验收隔离 fixture 准备说明

> 状态：准备方案，所有 fixture 初始均为 **not_prepared**，页面/操作初始均为 **not_run**。本文不表示数据已创建、浏览器已执行或外部集成已通过。只有开发完成、main 确认 source freeze 并冻结实际镜像后，才按本说明准备运行环境和执行完整用例。
>
> 唯一写入成果为本文。不建立新的 fixtures.json/fixture registry；用例、路由清单、执行顺序及结果格式分别由同目录的 cases.json、route-inventory.json、EXECUTION.md、README.md、result-template.json 管理。

## 1. 来源、证据等级与执行门槛

- 分类键复用 [静态页面计划](../../../.local/frontend-page-acceptance/routes.json) 的 `fixtureCatalog`，共 24 类；入口约束复用其 `globalGuards`。该文件是静态来源快照，实际执行必须与 main 冻结的 route-inventory.json 和当前源码核对，不能把快照中的 planned/static 状态解释为通过。
- 角色与字段权限以 [identity/auth](../../src/features/identity/model/auth.ts)、[router](../../src/app/router/index.ts)、[supplier scope](../../src/features/supply/scope.ts) 和对应服务端权限检查为准。数据库中的主体、关系、授权和服务端响应才是权限证据；不改 Pinia、localStorage 权限字段或全局 provider 来“登录”某个角色。
- 所有账号、历史记录、资源、文件均为本次隔离环境的 synthetic 数据。管理 JWT、生成 API Key、上游凭据、pending token、支付 capability 各由其实际 owner 产生、保存和使用，不能相互替代。
- 每个准备项须登记：fixture 分类键、本文角色别名、实际对象 ID、所属 actor/scope、run 标记、准备方法、只读核验结果、清理方式、源码/镜像摘要。秘密只登记安全引用。沿用 main 的执行记录，不新增另一套权威状态表。
- 数据可以由现有本地 UI/API 创建；无法由正常接口构造的历史/错误状态，只能由后端 owner 按当前 schema 在专用测试数据集装载，并标记 synthetic。若尚无经过核对的装载方法，相关用例记录 **blocked_by_fixture_preparation**，不能临时编造端点、篡改前端状态或把单元测试 mock 当真实后端。
- 准备成功不等于业务测试成功。分别记录：静态/类型、单元组件、真实本地 API+浏览器、受控替身、获准外部 sandbox。替身层只能证明其覆盖的本地合同。

### 1.1 开跑前检查单

| 门槛 | 执行人必须核实的事实 | 未满足时 |
| --- | --- | --- |
| 源码冻结 | main 给出的前后端 source hash、实际 UI/API 镜像 ID、入口和构建角色匹配；不是只看 Git SHA | not_run，等待冻结 |
| 运行隔离 | 本次专用 project、数据库、Redis、对象存储/bucket、文件目录、端口及浏览器 profile 已核对所有权 | blocked_by_fixture_preparation |
| 外部出口 | 上游生成/探测、支付、邮件、对象存储等外连默认拒绝；获准项目逐项开放并记录 | 保留关闭，不以空配置猜测可用 |
| 时间 | 记录后端 UTC 的 T0、浏览器时区和偏差；不改宿主机全局时钟 | 无受控时钟时，精确时序项单独 blocked；其余用 T0 相对时间准备 |
| 合规基线 | SA-H 的已确认状态来自明确标记的合成历史；ADM-PENDING 保持未确认 | 不点击确认绕过 |
| 秘密保护 | 凭据存储目录仅本地 owner 可读；截图/日志/HAR/导出证据在落盘前屏蔽秘密 | 停止采证并修复配置 |
| 外部前置 | HTTPS 信任、OAuth 租户/回调、sandbox、SMTP、WebAuthn 各有实际核验结果 | 对应项 blocked，不能计 pass/N/A |

## 2. 隔离运行环境与准备顺序

### 2.1 三种用途不能混用

| 用途 | 资源要求 | 允许的生命周期 |
| --- | --- | --- |
| 主验收 project | 独立 DB/Redis、主站和门户 origin、synthetic 用户/账本/文件；保留至 main 完成所有回归 | 只清理本次 run 拥有的对象；不得清空现有测试库 |
| 空白安装 project | **另建一次性 project**；未初始化的全新 DB/Redis/卷/目录/端口 | 专用于 setup-local；不能对主验收 project 清表来获得 needs_setup=true |
| 备份恢复 target | 单独且可丢弃的恢复 DB/卷；只能装入 synthetic 备份 | 不覆盖主验收库、开发库或其他会话的库 |

1. main 提供实际隔离部署配置后，先只读检查其 resolved 配置。project 名不同不保证隔离：检查显式 volume name、external volume、host bind、container name、外部 DB/Redis 连接以及 bucket 前缀是否仍共享。
2. 用本次随机 run 标记命名可控资源；核验 DB 实例/库名、Redis 实例或独立命名空间、对象存储和下载目录均属于本次 run。不能仅凭容器名推断可删除。
3. 记录实际 MAIN_ORIGIN、VENDOR_ORIGIN、API_ORIGIN 和各角色镜像。它们是运行时填写的非秘密值，本文不提供“已可用”的域名、证书、端口或商户配置。
4. 主站和供应商门户使用实际 HTTPS 信任链；不得使用忽略证书错误、关闭 origin 校验或关闭浏览器安全机制替代。门户未满足精确 origin+开启条件时，走 unavailable 负例，正例 blocked。
5. 关闭真实上游任务、自动探测、定时监控、真实邮件投递和支付任务，并在网络层验证默认出口拒绝；dummy 凭据不是禁止外连的保证。
6. 创建运行时私密凭据目录，随后通过现有正常登录/创建接口建立角色和对象。登录、refresh、API Key、MFA、pending OAuth、支付 capability 必须由实际本地后端生成。
7. 先准备角色和合规基线，再准备组/目录/用户授权，再准备资源和供应商关系，最后准备订单、历史用量、任务、审计及设置快照。所有关联 ID 来自前一步实际响应。
8. 用各角色各自的新浏览器 profile 正常登录，读回实际身份、权限和数据 owner。主站与门户、A/B 用户不能共享登录存储或互相复制 JWT。
9. 每类同时准备“有数据”和“已确认成功的空数据”。另外单独制造 401/403/409/423/428/5xx、超时、断网等状态；失败/权限拒绝/未加载不能当空列表。
10. 做只读预检后才能把该 fixture 标为 prepared；执行证据依然是 not_run。使用秘密的登录/创建动作不录制原始请求体或明文响应。

### 2.2 动态本地凭据示例

以下代码仅供冻结后的执行人运行；本次文档准备没有运行它。它只生成本地账号资料，**不签发 token、不创建账号、不替管理员确认任何承诺**。目录路径可以登记；内容不得进入仓库、报告、截图或普通 CI artifact。若现有受控秘密存储可用，优先使用该存储。

```sh
# 先关闭命令回显；不得把密码拼到 shell 参数或 URL。
set +x
umask 077
python3 - <<'PY_LOCAL_ONLY'
import json, os, secrets, tempfile
from pathlib import Path

folder = Path(tempfile.mkdtemp(prefix='sub2api-acceptance-secrets-'))
os.chmod(folder, 0o700)
labels = ['SA-H', 'ADM-W', 'ADM-RO', 'ADM-DENIED', 'ADM-PENDING',
          'ADM-CONTENT-DENIED', 'U-A', 'U-B', 'U-DISABLED',
          'V-P1', 'V-C1', 'V-C2', 'V-P2', 'V-X', 'V-EMPTY', 'CAS-A', 'CAS-B']
accounts = {
    label: {
        'email': secrets.token_hex(12) + '@fixture.invalid',
        'password': secrets.token_urlsafe(36),
        'incorrect_password': secrets.token_urlsafe(36),
    }
    for label in labels
}
path = folder / 'credentials.json'
fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
with os.fdopen(fd, 'w') as out:
    json.dump(accounts, out)
print(folder)  # 只输出私密目录位置，不输出凭据内容。
PY_LOCAL_ONLY
```

- 上述别名不是登录名或真实账号；实际邮箱/密码每次动态生成。邮箱域只供隔离账号标识；发送邮件仍需已经核验的本地捕获器和允许后缀配置，不能尝试公网投递。
- POST 创建用户或正常注册时，从受控存储读取值，在进程内构造请求；禁用请求体/Authorization 日志。不把密码写在 curl 参数、浏览器自动填充导出或版本化配置里。
- 账号创建接口目前为 `POST /admin/users`；实际 API 前缀使用运行配置。只有具有相应权限的本地会话可调用；不得假定匿名可创建 admin/super_admin。初始账号通过一次性安装或已核验的本地 fixture 装载方法建立。
- TOTP seed、恢复码、passkey 私钥、OAuth client secret、支付签名秘密、API Key 与 access/refresh token 不使用示例固定值，按各自实际流程生成/登记安全引用。不能用本地随机字符串冒充第三方已经签发的有效 token/code。

## 3. 主体、权限与浏览器上下文矩阵

以下每一行是测试别名；真实 ID、登录资料和返回的 policy_version 在准备时获取。普通用户、admin、super_admin、vendor_level1、vendor_level2 使用服务端真实 role；父子关系与供应商身份使用供应商实际 API，不能把管理用户 create 的 role 枚举擅自扩为 vendor。

| 别名 | 服务端角色/关系 | 准备和读回要求 | 必须区分的边界 |
| --- | --- | --- | --- |
| ANON | 未登录、全新 profile | 无 access/refresh token、无旧 pending cookie | 默认 requiresAuth，NotFound 也受保护；不能与空数据混用 |
| SA-H | super_admin，合成历史已确认 | 后端读回当前合规版本对应的 synthetic 历史，确认绑定本次 synthetic ID | 不代表执行人/真实管理员接受条款；权限足够也不能绕过其他 feature/origin 门槛 |
| ADM-W | admin，指定页面及写权限 | 返回精确 admin_pages、permissions、admin_write_fields、policy_version | 有页面不等于可写；有 operation 权限不等于任意字段可写 |
| ADM-RO | admin，目标页可读 | 页面和必要读取权限齐全；写权限/字段白名单为空或仅指定允许字段 | 编辑入口不可提交；直接 API 仍由服务端拒绝；不能使用 admin 角色名称推断可写 |
| ADM-DENIED | admin，缺一个目标页 | 保留另一可进入页面，明确缺失目标页的 exact path | 地址栏、刷新、alias、深链接都测；不得因共享前缀而进入 |
| ADM-PENDING | admin，权限原本足够但未确认合规 | 合规未确认；选不被 bypass 的 admin API | 获得 423 ADMIN_COMPLIANCE_ACK_REQUIRED；不能与普通 403 混淆 |
| ADM-CONTENT-DENIED | admin，提示列表可读但提示内容不可读 | 配置 prompt_audit.read，缺少 prompt_audit.content.read；审计按 audit.read 另核验 | 行数/元数据允许不等于提示词、请求、凭据可见 |
| U-A / U-B | 两个独立 user | 各自 Key、订单、订阅、用量和会话；至少一个同名对象但 ID 不同 | 切账号、跨 tab、复制他人 ID/URL，不能回写或复用对方对象 |
| U-DISABLED | disabled 的 synthetic user | 另备正常用户，确认禁用由后端生效 | 登录拒绝、既有会话复核；不能靠 UI 隐藏模拟禁用 |
| CAS-A / CAS-B | 完整 SettingsWorkspace 的 super_admin 编辑者；或同一有效 super_admin 会话的两个编辑页 | 两个账号使用隔离 browser profile；同一会话则登录一次后打开两个编辑页，不在共享 storage 中重新登录覆盖对方 | 10 tabs 及 compare/mergeReviewed UI 仅 super_admin；共享真实 revision 基线并保留各自草稿，不能以普通 ADM-W 代替 |
| V-P1 | vendor_level1，父供应商 | 关联 V-C1/V-C2；自己另有账号/代理/账本 | own 与 children 分开；子库存只读不能被“父角色”扩大为可写 |
| V-C1 / V-C2 | vendor_level2，同父的两个子供应商 | 分别有资源、用量和结算记录 | 同级不能互读互写；子级不可打开父级 children/payables 路由 |
| V-P2 / V-X | 另一个父及其子供应商 | 完全独立的关系与对象 ID | V-P1 不可借 vendor_id 访问另一棵树 |
| V-EMPTY | 无资源/用量/结算的 vendor | 正常授权并成功读取空结果 | 空数据仍不是网络失败、缺 scope 或拒绝访问 |

### 3.1 精确权限装配

1. 从 `GET /admin/roles/admin/permissions` 的 catalogue 和该页 uiPermissionsObserved 提取真实 permission 名。**当前 admin 使用共享角色策略**，不是可任意新建并逐人绑定的自定义角色系统。通过现有编辑接口，用读取的 `expected_version`、实际 permissions 和测试变更原因切换策略；检查 affected_admin_count 和返回版本。
2. ADM-W、ADM-RO、ADM-DENIED、ADM-CONTENT-DENIED 是**不同策略快照/串行阶段**的代表账号，不承诺能在同一后端同时具有不同全局 admin 策略。W→RO→DENIED→CONTENT 阶段分别读回身份及版本；若必须同时比较不同策略，使用各自独立的隔离 project。不能手工插入不受支持的 per-user permissions/admin_pages，不能伪造 profile。
3. 例：资源页当前读取 `accounts.authorize`、`accounts.manage`、`billing.rates.update`、`billing.cost.read`、`accounts.diagnostics`；代理页涉及 `proxies.manage` 和 `accounts.read`。按用例逐个缺失/撤销，不授予通配“全部”以消除失败。提示列表与内容使用 `prompt_audit.read` / `prompt_audit.content.read`；审计列表/详情当前共用 `audit.read`，不能虚构独立的 audit.content 权限。
4. admin_pages 以规范化后的 exact path 匹配。分别准备 `/admin/accounts` 可进入和该 path 缺失。相似前缀/父子页负例只使用冻结 inventory 中实际存在、且共享策略确实能产生的权限组合；不可构造时交 main 核对，不手工植入伪 admin_pages，也不以父路径代表所有子页面。
5. admin_write_fields 按真实 schema/field 白名单建立：至少一个允许字段、一个保留但不可编辑字段和一个敏感字段。写请求只含用户明确可编辑字段；不可为了通过测试把整包 payload 丢弃。
6. 读取权限、写权限、字段权限、内容权限、step-up、合规、站点 feature flag 分别变更，一次只切一个条件。撤销从后端发起，等待/触发已有身份刷新并记录新 policy_version，不能手改客户端缓存。
7. 共享策略/全局 feature 配置的变更由 main 串行排期，避免改变另一个正在执行的案例权限。普通 admin 权限/DelegatedSettings CAS 测试必须在对应阶段完成身份读回；只有撤销用例才有意在 await 期间切换策略。
8. 同一用户再登录使用真正的新 session；同 session 令牌轮换、同用户新 session、U-A→U-B、token/profile 交错更新分别准备。冻结后用已有受控响应延迟能力覆盖迟到读取/step-up/提交；不得通过伪造 JWT payload 授权。

### 3.2 合规历史和未确认负例

- **SA-H 正例**：由后端 owner 只在专用 synthetic 数据集中装载一条明确来源的历史 acknowledgement，使用当前服务端版本，绑定 SA-H 的实际 ID；通过现有合规读取接口确认可读。此准备是测试历史记录，不是当前操作者替真人作出承诺。
- 用于普通权限、设置 CAS 等非合规流程的其他 synthetic admin，也各自准备同类已确认历史并绑定自己的实际 ID，避免 423 遮蔽目标读写/403/409 分支；不得复制 SA-H 的整条 acknowledgement。只有 ADM-PENDING 等专门负例保持未确认。
- **ADM-PENDING 负例**：保留没有当前确认的管理员；后端在非 compliance bypass 路径返回 423 和实际版本/文档元数据。验证弹层/导航限制，同时确认原写入未发生。
- 再备旧版本确认、其他 synthetic admin 自己 ID 对应存储项的确认、缺记录三种状态；不能把 SA-H 的确认复制到真人账号或用全局状态替代 per-user 归属。
- 本地人工确认流程另列用例，必须由明确授权且实际操作的本人执行并记录操作者。缺少此条件时为 **blocked_by_manual_confirmation**；自动化不得点击“接受”、提交确认 API 或伪造实际签署动作来解除门槛。

### 3.3 供应商 scope 固定矩阵

| 场景 | 实际资源 scope/查询 | 写入要求 |
| --- | --- | --- |
| 供应商 own | 当前供应商身份；不沿用上次选择的 vendor_id | 仅按实际 capability/permission；不是所有 supplier 都可编辑全部字段 |
| 父级 children 库存 | `children=true, readOnly=true, admin=false`；查询 `scope=children`，不带 vendorId | 必须只读；关联父身份不构成子资源写授权 |
| 平台 aggregate | `admin=true, readOnly=true, vendorId=0`；保留 `vendor_id=0` 哨兵 | 只读，不把 0 当成缺失值后默认到某个供应商 |
| 平台选中供应商 | 实际正整数 vendorId，由服务器核验权限 | 切换选择后重新捕获 scope；不继承 aggregate 的写能力，也不推断所有子节点可写 |
| 失效/不合法 scope | 缺 supplier ID、非正整数、children 混用 admin/vendorId | 停止提交，不能补默认 owner |

门户要求精确配置的 origin 且 vendor_console_enabled=true。super_admin 仅进入 platform/profile；vendor_level1 才有 children、child-accounts、child-proxies、child-usage、payables；vendor_level2 留在 own 页。普通 user/admin、错误 origin、关闭或未确认配置分别验 unavailable。safePortalRedirect 只用实际允许的站内路径；第三方 URL/协议相对地址不能得到管理 JWT。


## 4. fixtureCatalog 对照与装载验收

下表完整保留来源中的 24 个分类键。最小数量不是容量要求；先记录页面真实可选页大小 P，准备 0、1、P、P+1 条的列表阶段，最后一页仅 1 条。若实际支持虚拟列表阈值 R，则另备 R-1、R、R+1；页面无法请求该规模时不伪造超大 pageSize，由组件层证据和页面实际规模分别记录。凡写入都会改变状态的 fixture，先记录基线及清理方式再执行。

| fixture key | 可执行准备动作和最小数据 | prepared 的只读核验 / 限制 |
| --- | --- | --- |
| `local-runtime` | 按第 2 节核验独立 project、origin、DB/Redis、时间、站点名称/Logo/doc_url 和默认出口；记录 actual 镜像 | 正常 API/页面连到本次本地实例；不能使用生产回包或覆盖别人的数据 |
| `session-roles` | 创建第 3 节主体；admin 权限按共享策略阶段切换；每个测试上下文正常登录 | 身份 API 返回真实 role、ID、策略版本及允许范围；账号别名不能代替服务端结果 |
| `admin-compliance` | 后端 owner 装载 SA-H 的当前版本 synthetic 历史；ADM-PENDING 保持没有当前确认 | SA-H 的 `GET /admin/compliance` 显示 required=false，acknowledgement.admin_user_id 对应实际 synthetic ID；待确认 actor 对非 bypass 请求得到 423 |
| `auth-local` | 正常用户、禁用用户、动态错误密码；本地支持时按实际流程建立 MFA；新 session、refresh 失败及跨 tab 场景 | 每个 token 都来自实际登录；错误密码和禁用账号不可得到成功会话；不能手签测试 JWT 冒充鉴权通过 |
| `registration-mail` | 未注册/已注册的动态 synthetic 邮箱、允许/不允许后缀、有效/过期邀请与促销数据；只接已核验本地邮件捕获器 | 验证码由实际隔离服务生成并从受控捕获器读取；过期/重放分支不沿用有效会话；无捕获器时邮件步骤 blocked |
| `pending-oauth` | 使用实际本地后端支持的 pending 流程/经审阅的装载方式准备 create/bind/adoption/MFA/过期/取消状态 | pending cookie/token 仅安全引用；不得使用生产 provider code；外部授权往返独立 blocked，不能由 pending 页面替身证明 |
| `profile-security` | 本地资料、已绑定/未绑定身份、可更新与保留字段、TOTP 和 passkey 列表；建立 step-up 成功/取消/迟到条件 | 密码、绑定和凭据仅作用于本次 synthetic 用户；无受信任 HTTPS/authenticator 时仅不支持/禁用分支可准备 |
| `user-directory` | 本人/同级/上级管理者/普通用户；空目录、多页目录、历史已删除用户；exclusive/public group、group_rates、配额/余额历史 | 身份/Key/订阅/属性关系有实际 ID；保留至少一个可恢复的 SA-H，不用删除最后 super_admin 来清理 |
| `catalog-groups` | 至少两个实际支持平台和两个组、不同倍率；已知模型与 synthetic 未知 modelId；另有空/禁用/加载失败目录 | capability unknown 与 false 分开；需登录、公开和 embedded 的入口分别核对，不能把未知模型补成已支持 |
| `keys` | U-A/U-B 各创建本地 disposable Key；active/disabled/expired、空列表、组/模型限制和 v1/v2 导入样本 | Key 明文只在本地秘密存储；证据使用 Key ID/安全引用；管理 JWT 不作为生成 Key，peer Key 可见性以真实授权为准 |
| `pricing-groups` | 真实 schema 下的启用/禁用渠道、组、倍率、RPM、复合路由；非法输入/冲突对照；ADM-RO 阶段 | 保存与读回匹配实际字段/作用域；不靠前端开关扩大 group/model 授权；不触发真实计费请求 |
| `subscriptions` | 后端历史装载或实际本地管理接口建立 active/expired/revoked；有/无可分配组；standard/simple 独立配置阶段 | subscription_enabled=true/false/unknown 分别准备；延期/重置/撤销/恢复用不同 disposable 对象，不能覆盖唯一原始基线 |
| `payment-orders` | 本地 synthetic 订单及匹配账务历史；U-A/U-B 各自订单；采用真实 OrderStatus：PENDING、PAID、RECHARGING、COMPLETED、EXPIRED、CANCELLED、FAILED 和退款系列状态 | 订单归属、币种、金额、费用、赠送、退款及恢复 capability 相互一致；历史显示可测，真实扣款/webhook/清算不得从 fixture 推断 |
| `payment-plans` | 有效/禁用 group 对应的在售/下架套餐、同名、缺字段、非法价格；payment_enabled 三态 | 不存在 sandbox 商户/签名凭据时保持真实 provider 关闭；只创建本地套餐/配置展示数据 |
| `usage-observations` | 由后端 owner 装载 synthetic 用量/错误：unknown、lower_bound、确切 0、非零、空集合；raw duration 与 first-token 不同；跨日期/组/Key/用户、已删除主体 | 原统计口径和可观测性标记保留；不把未知补 0，不把 raw duration 改为扣 first-token 后时间；导出只含 synthetic 数据 |
| `operations` | 当前/过期/缺失监控快照、审计事件、告警规则、诊断错误；仅 synthetic 提示词/请求；ops 开/关阶段、内容权限受限阶段 | 列表、内容、诊断、写规则权限分开；失败/未采样/过期不算健康或空；禁用真实监控请求，不能由 UI 补造 operation/billing 事实 |
| `resource-inventory` | 按真实 DTO 准备 Native/BPS/Cloud 平台账号、凭据模式、代理、v1/v2 导入及不支持/损坏样本；资源默认禁调度，自动刷新/探测关闭 | 管理 Account 与供应商 method/authorization_modes/authorizations 分别准备；不强转 DTO；本地代理无公网出口；真实授权刷新/探测独立 blocked |
| `jobs` | 装载已有本地图片和 synthetic batch：queued/running/indexing/processing_results/settling/completed/failed/cancelled/output_deleted；另备未知字符串状态、父子任务、部分失败、空结果 | 采用真实 DTO 状态，不把目录概述中的 pending/canceled 当成写入枚举；未知状态不推断成功；只用本地生成权限 Key，不启动真实上游 |
| `site-content` | 设置站点名称/Logo、协议、公告草稿/发布/已读、Markdown；设置 fixture-terms 文档 ID 与 fixture-markdown/fixture-embed 菜单；仅本地 iframe 目标 | 配置及资源文件都能从本地读回；空/危险链接作为拒绝/转义负例，不真的访问外网；供应商业务不是赞助文案，不能当清理目标 |
| `settings-revision` | CAS-A/B 使用 super_admin 上下文验证完整工作区；普通 admin 的 DelegatedSettings CAS 单列，见第 5.1 节；分别准备真实 revision、dirty 字段及敏感保留/清除意图 | 64 字符 revision 只能来自真实响应；409/428/未知结果均保留草稿语义，不生成假 revision，不改 client revision 后自动重放 |
| `vendor-scope` | 第 3.3 节供应商树；每个节点自己的账号/代理/用量/应收应付/结算；aggregate 与选中节点、空节点 | 自己、children、同级、另一棵树、aggregate 的 ID 集及金额维度可区分；收支和结算不是用户余额；读写依据 actual scope/capability |
| `setup-local` | 第 2.1 节另建一次性未初始化 project；由其 setup 流程动态创建初始 synthetic 管理员 | 先观察 needs_setup=true，安装后观察 false；不得清空主验收库，不能靠前端假状态测试安装 |
| `plugin-local` | 后端 owner 审阅的无外网/无额外副作用 synthetic .s2plugin 包；合法/损坏/不支持版本、安装/未安装、开/关/灰度与 UI session | 记录包 hash 和来源；不得导入未知可执行插件；加载与 step-up 状态均来自实际本地 owner；未知安装结果先核对，不重复安装 |
| `backup-local` | 仅 synthetic 小备份、分片和失败样本；独立恢复 target；本地无公网出口对象存储（若已实际配置） | 下载校验、计划、连接失败、恢复状态分别核验；恢复不触及主验收/开发库；没有可确认的 restore target 或对象存储则 blocked |

## 5. 关键 fixture 的可执行准备配方

### 5.1 两个编辑者的设置 CAS

完整 SettingsWorkspace 的 10 tabs 仅供 super_admin。普通 admin 进入 DelegatedSettings，复用同一事务和冲突审阅控件，但只显示获授权可读字段、只合并可写字段；两个角色视图分别验收。

1. CAS-A/B 选择一种明确的 super_admin 上下文：两个 synthetic super_admin 分别在隔离 browser profile 正常登录；或同一有效 super_admin 会话登录一次后打开两个独立编辑页。后一种方式不得在共享 storage 中再次登录覆盖对方会话。用于本组案例的 synthetic 主体应具备各自当前合规历史。两个编辑页在任何保存前读取同一真实 revision，要求初始相等且符合实际 64 字符格式，不使用固定重复字符伪装。
2. 在 A 修改一个实际可写的无秘密字段；B 在旧基线上修改另一个字段，并保留一个未改敏感字段。B 的草稿至少包含一个业务允许的 `0`、`false`、`null` 或空字符串值，不用 truthy 判断决定是否发送。
3. A 保存后读回新 revision。B 用旧 revision 保存，保留实际请求和 409 SETTINGS_REVISION_CONFLICT 的脱敏证据；B 的草稿不得被成功 toast 或新 revision 自动覆盖。
4. 在上述 super_admin 完整工作区，通过现有 compare/mergeReviewed UI 让执行人明确选择保留字段，再在新 revision 下提交一次。`sentFields` 指当前合同中明确发送的字段集合：实际 transaction 请求是 `revision + prepare(dirty fields)`，**不自行新增名为 sentFields 的 JSON 属性**。
5. 再备缺失/失效 revision 的 428 分支、提交后断连的 uncertain 分支、切账号/权限撤销后的迟到 GET/PUT；比较和读回不能自动重放保存。
6. 恢复配置也要重新 GET 最新 revision，按允许字段做 CAS；发生冲突时停下核对，不用旧全量快照覆盖其他执行者变更。

**普通 admin 的 DelegatedSettings CAS 另列验证**：在共享策略 W/RO/DENIED 串行阶段，使用该页实际允许的 section、字段和正常 admin 会话，核对真实 revision、允许字段提交，以及冲突/失败后的草稿保留。该页复用 SettingsConflictReview；必须按普通 admin 实际可读/可写范围执行比较、显式合并及再保存，确认没有超管字段泄漏。通用/协议两份草稿同时编辑后分区保存，并在PUT发出后继续编辑；不得丢失另一分区和提交后的新草稿。保存后延迟旧公共GET、失败新公共GET，验证代际及已保存/刷新失败分离；不切换身份或借完整工作区绕过授权。本组证据与 super_admin 的 10 tabs/冲突工作区证据分别记录。

### 5.2 unknown 幂等及恢复归属

必须有已核验的本地响应延迟/断连控制点；没有时相关时序项 blocked，不能把立即返回 mock 当完整链路通过。

1. A 对本次 synthetic 账号/组发起复制；记录 actor ID、源 ID、操作 key 的安全引用/摘要及初始对象数。正常返回、明确拒绝、请求未发送、提交结果未知分别记录。
2. 制造“服务端已提交、客户端未收到确认”的分支；只读核查后端 durable operation/对象数，不把前端超时解释为失败回滚。待显式重试时仍使用原 actor + source + key。
3. 在未知期间刷新页面、重新登录同一用户、更新权限，再由实际可操作身份尝试恢复。不得清空 sessionStorage pending key 或换一个新 key 来让用例“通过”。
4. 切换至 B 验证不能复用 A 的恢复操作。共享 session 的两个 tab 通过正常退出/登录或 refresh 延迟产生身份切换，不直接伪造 auth_user/JWT。明确 bearer/profile 不一致时停止提交；profile 追上后不能产生同一真实 actor 的第二个未知 intent。
5. 并发两个 K1 请求，第一个成功后显式开始 K2，再延迟旧 K1 的确认；K1 只能清除自身已确认状态，不能误删 K2。key 摘要仅用于关联，不替代实际服务端防重。
6. 旧无 owner 的历史 pending key 若需要验证，只导入经过审阅的本地合成浏览器历史快照；不修改身份/权限字段。它必须保持待核对，不自动认领到当前账号或删除后重放。
7. 权限拒绝、step-up 取消、关闭页面或本地等待取消不能被记录为服务端回滚/取消成功。清理前必须先核对未确认操作；无法核对时保留其恢复证据与对象，登记 blocked。

### 5.3 资源编辑、选择变化和供应商边界

- 先准备同平台、跨平台、同代理、不同代理、已有凭据、未设置凭据、脱敏凭据、允许/禁止授权、只读供应商各组数据。组 ID 保持接口中的字符串过滤值；空字符串、null、ungrouped、__unset__ 不合并成任意布尔筛选。
- 选择 A 资源后开始授权或提交；在首次 await、step-up、409 mixed-channel 确认等待时分别切身份、代理、组、选择 ID 或权限。确认必须对应最初捕获的 IDs/payload/writer/permission scope，不能 fresh capture 到新选择后继续写。
- allowAuthorize=false 时，隐藏的默认 credentials/model_mapping 不应发出；明确可编辑的名称、备注或其他被允许字段仍可更新。已有秘密字段的遮罩不等于新凭据，空值清除仅在实际字段合同及权限允许时发送。
- 管理账号和供应商账号分别使用其真实 DTO。供应商的 method、authorization_modes、authorizations、proxy_id=null 和实际 scope 不从管理 Account 结构补齐。
- 父级 children 和平台 aggregate 的只读状态使用真实 API 验证；直接构造另一个 vendor_id、同级 ID 或跨父树 ID 不能得到读写授权。撤销后重验、迟到数据丢弃及退出释放都要准备可区分的数据快照。
- Native/BPS/Cloud 只建立本地元数据和禁调度资源；需要授权交换、token 刷新、诊断探测、模型列表外拉或真实推理的正例，缺少明确获准的测试上游/预算/网络条件时 blocked。不能把假凭据发给真实 provider 试探。

### 5.4 图像批任务与取消

- 按 [真实 batch DTO](../../src/features/jobs/api/batchImage.ts) 保留 model、task_name、parent_batch_id、provider、image_size、response_mime_type、aspect_ratio、items.custom_id/prompt/output_count/reference_images 和 metadata；仅使用本地小图片及受控 file_uri。不能为测试方便缩减成只有 status/id。
- 准备 success_count=0 与实际成功数、fail_count=0 与部分失败、actual_cost=null 与确切 0、hold_amount/estimated_cost、submitted_at/settled_at=null 以及有/无下载和删除时间。账务与任务状态由后端 synthetic fixture 提供，UI 不生成假结算事实。
- cancel API、取消本地等待、暂停轮询/订阅、退出页面是不同动作。只有服务端实际返回并读回 cancelled 才能断言作业取消；本地 abort 或拒绝迟到响应不能替代它。
- 准备 A/B Key 权限、同一 actor 的新 session、父子任务、分页 has_more、空 output、输出已删除、本地文件损坏及下载失败。换身份/Key/scope/卸载后不能继续显示旧 owner 的结果。
- 无获准本地任务执行替身时，仅准备已有任务/图片和提交前校验，提交到真实上游的完整执行保持 blocked。已有图片、synthetic completed 状态不证明真实生成成功。

### 5.5 支付和恢复 capability

- 退款状态采用真实枚举：REFUND_REQUESTED、REFUNDING、REFUND_PENDING、PARTIALLY_REFUNDED、REFUNDED、REFUND_FAILED；不得把目录概述的小写别名直接当 API 状态。
- 默认关闭真实支付 provider。没有实际获准的 sandbox 商户、签名/回调配置时，不填写猜测的“测试密钥”，不使用真实卡号/钱包、不生成真实扣款；订单历史读取和前置校验可单独测试。
- 数据须区分付款币种/pay_amount、账户记账 amount、fee_rate、bonus_amount、refund_amount 与订单/退款状态；订阅订单和余额订单分开。零金额是否合法按具体端点核验，不以“都可以 0”作统一规则。
- 待执行的 sandbox 正例从本地 checkout 建立本人的实际订单及 matching recovery snapshot。Stripe 绑定本人的订单 session；Airwallex 的 snapshot.paymentType 必须为 airwallex 且具备实际 intentId。clientSecret/resumeToken/openid/单次 code 仅安全引用，不出现在 URL 截图或测试报告中。
- 准备同 owner 正确 capability、另一 owner、过期/错误/已消耗 capability、缺失 recovery snapshot、订单 ID 与 snapshot 不匹配、重复 callback、未知 mutation。不得仅依靠 URL order_id 重建支付 intent，不能从别人订单恢复页获得本人 session。
- 公开 resolve/verify 路径使用现有匿名策略；检查没有管理 JWT、Cookie、Basic auth 或 XSRF 凭据泄漏。前端支付页成功、synthetic PAID/COMPLETED、受控 webhook 替身都不能替代真实 sandbox 支付/退款回执。

## 6. 全领域边界值矩阵

准备时只向实际允许的字段写入该值；不被 schema 接受的值作为校验负例，不能强行装载成“合法业务状态”。所有日期、金额和 ID 的含义以各自 DTO/服务端为准。

| 维度 | 必备输入 | 核验重点 |
| --- | --- | --- |
| 不存在 / nullable | 未发送字段、显式 null、空字符串、空数组、缺对象 | 未修改不发送；null 清除与空字符串含义按字段核对；缺失观测不补 0 |
| 零 / false | 数值 0、字符串 "0"、false、true | Boolean false 不能丢失；数字字段不接受布尔替代；vendor_id=0 的 aggregate 哨兵不被默认逻辑吞掉 |
| 金额 / 比例 | 业务允许的 0、最小正值、分数、上限邻界、精度边界、非法负数；账本合法冲正单独准备 | currency/单位/手续费/赠送/退款各自核对；负数不一概判非法，也不能把格式化/舍入后的显示当账务事实 |
| 观测事实 | unknown、lower_bound、exact zero、nonzero、stale、empty-success | 真实 observability 原义、覆盖程度和时间戳可见；空图不证明全部为零；操作/账务来源可追踪 |
| 日期 / 时区 | T0 前/等于/后、跨日/月、闰日、UTC 与配置时区；秒/毫秒、合法 epoch 0、null、非法日期 | 仅在字段合同允许时解释 0 为 epoch 或“无过期”；不得统一用 falsy 替代；TOTP/TTL/到期判断与后端时钟一致 |
| 标识 / 分页 | 两主体同名不同 ID、字符串 group ID、空筛选、第一页/最后页/空页、实际页大小边界 | 按稳定 ID 操作；保留跨页选择的原 owner；筛选变化后不能用旧 selection 重放写入 |
| 文本 / URL / 文件 | Unicode、空白、最大长度邻界；站内安全路径、协议相对地址、危险协议、URL 编码边界；空文件/损坏文件/不支持版本 | 危险 URL 仅作为阻止导航/转义断言；不实际访问外网；导入/下载/iframe 不转发管理凭据 |
| 响应 / 生命周期 | 400/401/403/409/423/428/5xx、成功但空数据、超时、断网、乱序、重复响应 | 保留原错误用于取消分类；未确认不自动重放；错误/取消不宣称服务端完成或回滚 |
| scope / 权限 | own/children/sibling/other-parent/aggregate/selected/empty；页面缺失/字段只读/内容拒绝/撤销 | UI 限制与服务端独立验证；任何 await 后不可借新 actor、新代理、新选择执行旧 intent |

跨 tab 专用测试使用一个可丢弃 profile 内的同源两个 tab，通过正常登录流程切换；A/B 独立数据基线仍分别使用隔离 profile。不能因为跨 tab 测试而共享不同 actor 的秘密文件或越过 origin。

## 7. 全局守卫与配置阶段

这些是测试输入阶段，不是新增设置名或后端环境变量；使用实际已支持的配置方式，记录每阶段配置/镜像输入，之后按 CAS/owner 规则恢复。

| 阶段 | 要准备的差异 | 必须记录 |
| --- | --- | --- |
| 主站 / 门户 | 主站 origin、精确门户 origin、相似但不相等 origin、门户关闭、public settings 加载失败 | 真实 origin+端口+scheme；vendor build 标记不能绕过精确 origin 和开启条件 |
| 默认认证 | ANON、有效登录、过期/撤销身份；显式 public 页面与未声明 requiresAuth 的路由 | NotFound 默认受保护；不能因 URL 未识别就归类匿名可访问 |
| admin 页 | W/RO/DENIED/CONTENT 策略阶段；缺 policy_version、随后加载或撤销 | exact admin_pages、实际 permissions/admin_write_fields、每次策略版本；不是客户端自造列表 |
| 功能开关 | ops、risk control、payment、subscription 等各自 true/false/unknown；standard/simple 分离 | 显式 false 与未加载/unknown 不合并；不能把缺配置写成默认放行 |
| backend mode | 正常模式与实际 backend-mode 配置阶段；pending 有/无/过期 | 路由前缀、精确 callback、pending-only 分别验证，不修改 guard 以增加通路 |

当前来源中 backend-mode allowedPrefixes 为 `/login`、`/key-usage`、`/setup`、`/payment/result`、`/payment/airwallex`、`/legal`；实现使用 startsWith。精确 callback 为 `/auth/callback`、`/auth/linuxdo/callback`、`/auth/dingtalk/callback`、`/auth/dingtalk/email-completion`、`/auth/oidc/callback`、`/auth/wechat/callback`、`/auth/wechat/payment/callback`；`/register` 和 `/email-verify` 为 pending-only。alias `/auth/oauth/callback` **不在这个精确列表中**，不能静默当成 canonical callback。冻结时复核实际注册和 guard；路径近似、alias 与重定向出现差异时按用例记录，不改白名单掩盖。

## 8. 外部条件与 blocked 判定

| 条件 | 准备人必须出示 | 缺失时仍可准备 / 不能声称 |
| --- | --- | --- |
| HTTPS 信任 | 实际证书链、SAN/origin 匹配、浏览器信任检查；本机解析和端口核验 | 可准备错误 origin/unavailable；门户安全正例 blocked。不得跳过证书错误 |
| 外部 OAuth | 获准测试租户、真实注册 callback、受控账号、实际单次授权 code/state 流程 | 可准备本地 pending/错误分支；外部往返 blocked_by_external_dependency，不用 dummy code 冒充成功 |
| 支付 sandbox | 获准 sandbox 商户、测试 SDK/session、签名与 webhook 路由、受控退款能力及费用边界 | 可准备本地订单历史；网关支付/退款/回执 blocked。配置值未知就保持未知 |
| SMTP / 邮件 | 本地捕获器的真实监听/收件证据，或另行获准的隔离 SMTP | 无捕获器时验证码/邮件链路 blocked；本地捕获不能证明外部邮件投递 |
| WebAuthn | 受信任 HTTPS、实际 RP/origin 条件、获准 authenticator 或明确标为虚拟设备的环境 | 可准备 unsupported/取消分支；虚拟设备证据不宣称真实设备通过；缺硬件为 blocked_by_external_dependency |
| 真实上游 / 外网 | 明确获准凭据的安全引用、目标/网络、费用/配额预算及执行边界 | 可准备禁用资源、本地任务/快照；推理、真实刷新、探测、GeoJS/外网下载等保持 blocked |
| CAPTCHA / 对象存储 / 插件依赖 | 各自实际隔离配置和允许出口、包来源或存储所有权 | 本地替身只证明本地合同；不可推断真实集成已可用 |

外部 blocked 需记录具体缺项、owner、受影响分类和用例 ID、可继续的本地范围、解除条件；不得用“暂时正常”、未经审阅的 N/A、假端口或假 secret 代替。

## 9. 准备回执、无秘密证据与清理

### 9.1 每类 prepared 的最低回执

- fixture key 与使用它的 cases.json 用例 ID；实际 actor ID/role、策略阶段和版本、scope、对象 ID、run 标记。别名只用于说明，不当真实主键。
- 实际前后端 source hash、镜像 ID、配置阶段、origin、浏览器/profile、语言、viewport、UTC T0/时区、准备者和时间。
- 正常接口或已审阅后端装载方法；实际读取验证的结果；预期数据与空/拒绝/未知状态的区分；文件/包只记录允许公开的名称、大小、hash。
- 秘密仅记录本地安全引用，不存明文、Authorization、Cookie、完整回调/支付 URL、clientSecret、resumeToken、授权 code、TOTP seed、恢复码或真实个人数据。请求相关性只用非秘密 ID/摘要。
- 生命周期控制点的位置与能力：延迟哪个请求、是否已提交、如何只读核对；不能仅凭“请求被 abort”判定后台作业或账务已取消。
- main 统一填写真实执行状态；本文不创建第二份结果表，不把 prepared 改写为 accepted。

### 9.2 清理顺序

1. 停止本次 profile 的轮询、订阅、下载和自动操作；从实际后端核对未确认写入、作业和付款状态。**先核对 unknown，再决定清理**；不能删 pending key 后重试来完成清理。
2. 取消/撤销仅通过实际支持的接口执行，读回后确认。无法证明取消的后台任务/订单保持登记，标出 owner 和阻塞原因；本地不再等待不等于服务端取消。
3. 按依赖关系删除/撤销本次 disposable 对象：任务输出/临时文件、Key、订阅关联、资源/代理/组引用、供应商子关系、测试用户等。每一步根据 actual ID 和 run 所有权确认，拒绝按相似名称批量清空；历史账本/审计不强行物理删除。
4. 将共享 admin policy、设置和 feature flag 恢复到本次记录的基线语义，使用最新 expected_version/revision。另一执行者已有变化时先核对，不整包覆盖、不重置成默认全权限。
5. 主验收 project 留存到 main 完成所有依赖用例；只清理明确归属本 run 的数据。备份恢复 target、空白安装 project 可以单独丢弃，但先验证其 DB/卷/bind/bucket 没有与其他 project 共享。
6. 严禁对主验收/现有开发库执行 TRUNCATE、DROP DATABASE、Redis FLUSHALL/FLUSHDB 或宽泛 Docker prune。只有确认一次性 target 的全部资源归本次 run 所有，才能按 main 的现有执行/收尾流程删除对应资源；本文不提供通用破坏性命令。
7. 撤销本次 disposable 密钥和会话，退出并删除本次浏览器 profile 中的存储。共享可信证书、SDK/cache、他人 profile 和仍用于验收的运行环境不删除。
8. 按最小保留原则处理秘密文件、原始 trace/HAR、备份和下载。证据先完成必要脱敏；不能把原始秘密文件复制进报告后再指望后续清理。删除仅限本次明确创建的路径，不按整个 TMPDIR/目录时间批量删除。
9. 记录删除与保留的对象 ID/路径类别、原因、字节数、owner 和复查条件；保留 unknown 恢复证据直到消歧。准备失败、blocked 和 not_run 也有清理/保留记录，不能写成全部通过。

### 9.3 无秘密审查清单

- [ ] 本文和案例/结果只含别名、字段名、安全引用与动态生成表达式，没有真实登录资料、固定测试密码、明文 Key/JWT、private key 或第三方商户秘密。
- [ ] 未从生产 DB、浏览器 profile、生产日志/备份或真实用户导出数据。
- [ ] synthetic 邮箱、密码及错误密码在本机动态产生；其他 token/challenge/capability 由实际 owner 正常签发，不在文档中生成可用配置。
- [ ] 浏览器截图避开一次性明文 Key、二维码、授权参数和回调 URL；HAR/trace/console/body 日志在持久化之前过滤敏感字段。
- [ ] 合规历史、账本、用量、作业记录都标为 synthetic；不表示真人签署、真实支付、真实上游生成或真实投递。
- [ ] 准备阶段未运行完整浏览器用例；完整运行必须等项目完成和 source freeze，再由 main 按 EXECUTION.md 执行。

## 10. 本说明核对的具体源码

- 角色、exact page 和字段权限：[identity/auth](../../src/features/identity/model/auth.ts)、[角色策略 API](../../src/features/identity/api/roles.ts)、[共享 admin policy](../../../backend/internal/service/admin_role_policy.go)、[管理 API 权限表](../../../backend/internal/authz/routes.go)。
- 合规 per-user 存储/版本及 423：[合规 API](../../src/features/identity/api/compliance.ts)、[服务](../../../backend/internal/service/admin_compliance.go)、[中间件](../../../backend/internal/server/middleware/admin_compliance.go)。合成历史按这些真实字段装载，不通过自动提交 accept API 来替代真人操作。
- 门户入口和 scope：[router](../../src/app/router/index.ts)、[supplier scope](../../src/features/supply/scope.ts)、[supplier DTO](../../src/features/supply/types.ts)。
- 原 actor/source 的复制恢复：[accounts duplicate](../../src/features/resources/api/accounts.ts)、[groups duplicate](../../src/features/access-policy/api/groups.ts)、[session snapshot](../../src/shared/session/storage.ts)。
- CAS 和真实提交字段：[settings transaction](../../src/app/compositions/settings/transaction.ts)。
- 任务与付款状态：[batch API/DTO](../../src/features/jobs/api/batchImage.ts)、[payment DTO](../../src/features/payments/api/types.ts)。

这些引用用于准备时核验合同，不改变源码 owner，也不替代冻结后的服务端读回和实际浏览器验收。
