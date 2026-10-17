# SIWC 手动授权与账号协议选择

## 入口与边界

普通、管理员自有 OpenAI OAuth 父账号的编辑弹窗新增「使用 ChatGPT 登录（SIWC）」面板。API Key、setup-token、供应商账号、Excel 主授权、PAT/AgentIdentity 和影子子账号不提供入口；服务端仍须独立校验身份、权限和账号归属。

授权保存与协议选择分离：授权完成后不会自动切换，用户须明确选择 SIWC 并保存账号。切换协议不清除 Codex 或 BPS 配置。SIWC 身份是由其 ID token 验证的独立身份，不能据此声称属于主 Codex grant 的同一工作区。授权状态不代表真实模型或额度测试通过。

## HTTP 与 WebSocket 传输

选择 SIWC 决定使用哪份授权，不等于强制一种传输方式。两条路径都使用独立的 SIWC grant；不改用 Codex 或 BPS 的凭证。无需新增 SIWC 专属 WS 开关，继续使用现有全局和账号 WS 设置。

| 客户端接入与有效设置 | SIWC 上游路径 |
| --- | --- |
| HTTP 请求 | 公共 Responses HTTP API，流式响应使用 SSE；每次发送完整上下文。 |
| 客户端 WebSocket；全局 WS、OAuth WS、Responses WS v2 均开启，账号有效模式为 `ctx_pool` 或 `passthrough`，且未强制 HTTP | 使用 SIWC 认证的持久上游 WebSocket；符合下述续接条件时发送父响应 ID 与新增输入。 |
| 客户端 WebSocket；全局或账号强制 HTTP，相关 WS 能力关闭，或账号有效模式为 `off` / `http_bridge` | 保留客户端 WS 到公共 Responses HTTP/SSE 的桥接，每轮发送完整上下文。 |

持久连接只服务当前已认证的客户端会话，不跨客户端共享会话上下文。只有同一条认证后的上游连接、上轮已成功完成、设置一致且完整历史前缀匹配时，才使用该连接已确认的 `previous_response_id` 加新增输入；客户端提供的父响应 ID 本身不足以证明可以增量续接。

首次请求，以及设置、历史前缀、连接或 access token 改变后的请求，都在本轮首次发送前准备完整上下文，不继续使用旧连接的增量锚点。若授权身份或账号归属变化，或无法可靠重建完整历史，则拒绝续接并要求重新提供完整上下文，不将旧会话搬到其他身份下。

向上游写入本轮生成请求后，不因断线或错误自动重发，也不切换 HTTP 重放已发送或已接受的生成。HTTP 桥接是配置决定的路径，不是生成中途的重试策略。这些条件减少可安全续接时的重复上下文传输；不保证具体首字耗时，仍需真实账号和网络环境验证。

## 管理接口契约

使用现有 apiClient 的响应解包方式；路径为 /api/v1/admin/accounts/:id/siwc：

- GET：bound/enabled/registered/client_id/identity_verified/direct_access/refresh_required/subject/email/expires_at 等非秘密状态。已保存、身份已验证、已获 direct scope 且不需重新登录时才允许选择 SIWC。identity-only 与可使用套餐明确分开显示。
- POST /authorize：auth_url/session_id。仅接受官方授权 URL 与当前固定回环回调地址。
- POST /callback：session_id/callback_url。拒绝不匹配 state、重复关键字段、错误回环地址、错误 returning client ID；后端再次进行权威校验。拒绝授权、会话失效、一次性代码失败后显示错误，清除已提交回调材料，重新读取注册信息并由用户重新发起登录。
- DELETE：bound/remote_revocation_confirmed/message。断开立即移除授权；页面重新读取状态并提示尚未确认的远端撤销。若用户要改回原协议，须明确保存账号的协议选择。注册信息仍保留以便复用 issued client ID。

未新增自动登录 Worker、保存密码/2FA、轮询任务、供应商 SIWC 权限选项。页面不会显示 token、PKCE verifier 或上游原始认证响应。

## 账号备份与迁移

超级管理员继续使用账号页现有的导出、导入入口和敏感操作验证。新备份使用 `sub2api-data` version 2：主授权在 `credentials`，独立 Excel/BPS 授权在 `bps_credentials`，SIWC 注册及独立令牌在 `siwc_authorization`。未开启协议但已经保存的授权也会导出；已断开的 SIWC 只保留注册信息，不会恢复已删除的令牌。供应商导入仍不接受 SIWC。

SIWC 的本地绑定指纹由主账号的稳定身份元数据计算，不含数据库账号 ID，也不含会轮换的 Access Token 或 Refresh Token。账号必须具有工作区、用户 ID 或邮箱等稳定身份元数据。数据库仍通过 `account_id` 关联授权，读取与更新仍检查账号身份、权限及并发版本；SIWC 的 issuer/subject 是独立身份，不强制等同于 Codex 工作区身份。SIWC 尚未投入授权使用，本次不保留旧账号 ID 指纹的兼容读取或迁移代码。

导出从同一个数据库快照读取主凭据、独立授权和协议设置。导入验证注册结构、主账号绑定和 Excel 的用户/工作区关系，再使用目标站已有加密密钥保存；不导出系统加密密钥。保留 SIWC 上游 client/host 注册信息，重新生成本地会话代次，原站的连接和会话缓存不会迁入。恢复备份不会主动消费或轮换源站的 Refresh Token；它不构成一次在线授权有效性验证，过期或已撤销的令牌仍由正常刷新流程处理。

账号、独立授权、分组绑定及调度通知作为同一事务写入，任一步失败全部回滚。保留账号启停/可调度状态、并发、优先级、倍率、负载系数、到期设置及协议配置。分组按平台和完整名称匹配目标站已有配置，包含每组优先级和 BPS 403 目标组；缺少或重名的分组明确报错，不能按源站数字 ID 绑定，也不会自动建立计费政策不同的分组。

勾选包含代理时导出主、备用代理链及认证信息。备用代理使用文件内的 `backup_proxy_key` 关联，导入不依赖 JSON 的排列顺序；缺失、重名的旧名称引用及循环引用会明确失败。代理会单独创建或复用，导入结果分别报告代理和账号成功/失败数。

既有不带头部的备份及 version 1 的 Codex/Excel/BPS 文件继续可导入；SIWC 必须使用 version 2，未知版本被拒绝。多个文件合并时保留最高已支持版本。账号 JSON 不是完整数据库备份：历史用量、审计、临时限流/探针状态和 Spark 影子账号仍不在本次迁移范围。

## 官方回环登录与本地助手

官方要求先启动回调监听再打开浏览器。服务器上的网页无法监听管理员电脑的 127.0.0.1；因此新增可选标准库工具 tools/openai_siwc_authorize.py：

1. 在使用浏览器的电脑上，从项目目录运行 python3 tools/openai_siwc_authorize.py。
2. 在隐藏输入提示中粘贴本次完整授权 URL，不把链接放入命令行参数或日志。
3. 助手只绑定 127.0.0.1:1455，监听成功后才打开官方授权页面。端口占用时立即报错，不把浏览器交给未知监听器。
4. 只接受当前 state 对应的单次 /auth/callback，检查 client ID，错误 state 不结束等待；拒绝授权回调仍会回传给管理端以结束会话。
5. 助手只在本机终端打印回调地址。管理员粘贴至账号弹窗后，由管理端完成 token 交换。助手不交换 token、不写凭证文件、不向远端上传数据，HTTP 页面不回显授权码且禁止缓存，等待最长 30 分钟。

手动打开授权页仅作为已准备好本机回调监听时的备用方式；原来源粘贴浏览器失败页地址仍由管理端回调输入承载，但它并不等于已启动官方要求的回环监听。

参考：https://developers.openai.com/siwc/token-sharing-open-source/sign-in （2026-10-21 阅读）。
