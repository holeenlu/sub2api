# 账号上游资料与信息布局提案

日期：2026-10-28。基于本工作树 NewAPI 的实际消费者分析，不代表所有上游接口当前都能成功返回，也未调用真实上游补抓用户资料。用户随后选定精简悬浮卡并明确双排内容；正式组件实施与验证见文末。

## 数据来源与可用性

| 平台 / 入口 | 授权或刷新时现有实现取得 | 后续查询 / 探测 | 限制 |
| --- | --- | --- | --- |
| OpenAI 原生 OAuth | access / refresh / id token、访问凭据有效期、client ID、email、ChatGPT user/account ID、organization ID、plan_type | enrichTokenInfo尽力查询accounts/check及个人subscription.active_until，取得套餐有效期；隐私设置结果；独立quota查询取得5h/7d及附加窗口、额度、重置券 | Excel client不走同一enrichment；个人套餐与workspace有效期必须属于同一订阅；失败/缺字段不可推成Free或永久有效。 |
| OpenAI Excel/BPS | 独立授权凭据及其返回的身份字段 | 授权scope/状态、独立BPS能力与恢复状态由既有合同投影 | 不能把原生登录成功当BPS已授权，或把BPS-only凭据标成可原生调用。 |
| Anthropic OAuth | TokenInfo当前投影access / refresh token、访问凭据有效期、scope、org_uuid、account_uuid、email_address | 供应商完整OAuth可查询profile：organization_type、rate_limit_tier、billing_type；后续usage/响应观测取得窗口用量/限流 | 管理员基础exchange不保证执行同一profile补全；仅推理setup token不能被强制要求user:profile；现有profile类型未投影套餐有效期。 |
| Anthropic setup-token / 手工导入 | 视凭据入口而定，身份、scope、refresh或有效期可能缺失 | 仅在真实权限允许时读取额外资料 | 缺刷新凭据不等于授权异常；手工导入的声明不能冒称已被上游核验。 |
| Antigravity OAuth | access / refresh、有效期、email、project_id（部分账号缺失）、plan_type | LoadCodeAssist的currentTier/paidTier、模型配额；独立隐私设置并复核 | Project或套餐补全失败可能保留基础授权；缺值不能写零或Free。 |
| Gemini OAuth | access / refresh、有效期、scope、project_id、oauth_type、canonical tier_id及有消费者的extra | 各OAuth类型的项目/配额查询 | code_assist与ai_studio不可混为同一订阅；本token DTO不提供通用邮箱/套餐到期日。 |
| Grok OAuth | access / refresh / id token、有效期、scope、email、sub、team_id、subscription_tier、entitlement_status，取决于返回/声明 | 独立billing/usage快照和实际响应头；既有受时效约束的Heavy识别 | SuperGrokPro声明存在歧义；不能仅凭它无条件显示Heavy。 |
| API Key / AWS / 服务账号 | API Key入口、供应端地址或服务身份等显式配置及凭据存在性 | 已实现且授权允许的models、usage、billing probe | 不保证个人邮箱、消费订阅套餐或到期日；不能根据Key格式猜测。 |

主要源码：

- `backend/internal/service/openai_oauth_service.go`：OpenAITokenInfo、enrichTokenInfo、Excel client例外。
- `backend/internal/pkg/openai/oauth.go`：IDTokenClaims.GetUserInfo身份与套餐声明。
- `backend/internal/service/openai_privacy_service.go`：多workspace归属与subscriptions.active_until；will_renew和subscription ID虽被解码，当前仅用于诊断、不持久化展示。
- `backend/internal/service/openai_quota_service.go`：独立用量、credits、reset credit及时间戳。
- `backend/internal/service/oauth_service.go`：Claude基础TokenInfo与exchange投影。
- `backend/internal/service/vendor_account_credentials.go`：Claude可选profile和推理scope边界。
- `backend/internal/service/antigravity_oauth_service.go`、`gemini_oauth_service.go`、`grok_oauth_service.go`：其他平台实际token DTO。
- `frontend/src/features/resources/model/planType.ts`：OpenAI pro/prolite/promax显示为Pro 200/100/500；Claude rate tier映射Max 5x/20x。
- `frontend/src/app/compositions/resource-workspace/model/accountDisplay.ts`：套餐、母账号回填、Grok快照优先级。
- `frontend/src/app/compositions/resource-workspace/components/AccountInfoCell.vue`：现有信息卡消费者。

## 展示边界

1. **身份与订阅**：邮箱、平台/账号类型、套餐、同一订阅的有效期、工作区/组织短ID。套餐名称来自已记录字段的显示映射，不能把“Pro 200”当作真实支付账单。
2. **授权凭据**：原生/BPS等实际可用授权入口、访问凭据到期、refresh存在性；不展示access/refresh/id token、cookie、session key或完整凭据对象。存在性不等于下一次续期一定成功。
3. **用量观测**：实际已有窗口、额度及重置时间，显示快照来源/采集时间；仅在对应读权限存在时消费已有查询，不因hover触发真实上游请求。没有快照显示“尚未查询”，过期显示“历史快照”。
4. **本地策略**：Compact模式与实际探测结果分别表达，并发上限、优先级、分组和代理等归本地配置。不能将本地并发10显示为上游额度。
5. 套餐有效期、访问token有效期、本地账号expires_at必须分别命名；不能互相回填。个人订阅与组织席位权益不能混用。
6. 无字段时显示“未取得”/“不适用”，不显示0、Free、永久或“正常”作为默认。不可用原因只有服务端确有证据时才标“权限不足/查询失败”。
7. 当前很多元数据没有独立采集时间/来源字段，account.updated_at不能冒充上游刷新时间。布局若显示来源或更新时间，必须消费真实已有事实；缺失标“未记录”，另行按选定方案提出最小DTO变更。
8. 套餐详情继承当前页面/字段读权限；母账号回填须明确“来自母账号”，不能伪装成影子凭据独立取得的身份。敏感身份ID默认缩略，不新增批量导出或权限。

## 待用户确认的三个布局

| 方案 | 行内摘要 | 详细内容 | 适用 |
| --- | --- | --- | --- |
| 精简悬浮卡 | 名称、ID、套餐 | 轻量卡：邮箱、平台、套餐有效期、授权入口及Compact摘要；其余保留现有入口 | 最小改动、高密度列表；不适合同时放全部诊断。 |
| 分组信息卡（建议） | 名称、ID、套餐 | 身份与订阅 / 授权凭据 / 本地策略三段，缺值和归属清晰；用量保留当前用量列及已有详情入口 | 兼顾信息完整性和列表密度；点击可固定，hover/focus可查看。 |
| 详情侧栏 | 名称、ID、套餐，点击信息按钮 | 上游资料 / 用量快照 / 本地策略三个页签，适合长字段及诊断；手机改为可关闭抽屉 | 信息最多，不遮挡相邻行，但交互较重。 |

交互预览为对话可切换的三方案，OpenAI/Claude及用量数字均为明确示例，未访问真实账户。该阶段未修改AccountInfoCell正式布局；随后按下述用户确定需求实施。

## 同轮运维节点修正

默认最多3个新鲜上报的本项目API实例，少于3个不以过期记录补位；全部列表保留历史节点并标记最后已知状态。无在线节点时显示明确空态，仍可打开历史列表。footer分别显示在线总数、历史数和当前展示数。

节点是共享监控registry内上报的API进程/容器实例，不是物理机清单、代理服务器或上游账号。`ops.node_id/node_name`可显式配置；未配置ID回退容器hostname，因此重建可能产生历史ID。当前时效界限30秒、registry保留24小时；不修改这些后端合同，不删除历史记录，不擅自修改部署身份。

线上截图对应TapModels维护部署，本次代码只作用于NewAPI重构工作树和本机隔离Docker，不代表已部署截图中的线上站点。


## 用户确定的双排与精简悬浮卡（2026-10-28）

- 第一排保留名称、ID、平台图标和可点击固定的信息入口；第二排固定三个摘要：套餐类型、支付/订阅方式、访问凭据有效期，不折出第三排。长付款名称省略，完整内容在可复制的悬浮卡和无障碍标签中。
- 悬浮卡按附件内容平铺：名称和ID、邮箱、平台/类型、套餐名称及原始rate tier、支付方式、实际授权方式、访问凭据有效期。仍有明确用途的套餐有效期、隐私、Compact及扩展详情保留，日期分别命名。
- 管理员只读取credentials.expires_at；供应商只读取authorization_expires_at。绝不以本地account.expires_at或subscription_expires_at代替访问凭据到期。时间戳秒/毫秒、带时区RFC3339统一转换为浏览器时区；缺失或不可信格式显示未取得。
- Stripe/Apple/Google subscription等明确付款枚举本地化；未知付款值原样可读。subscription_type=claude_max等套餐等级不能冒充支付方式。没有数据时不编造“oa授权1”或其他授权编号。
- 交互继续复用HelpTooltip：名称可直接复制，悬停/键盘可查看，信息按钮可固定，卡片内文本可选择复制；不增加真实上游请求或凭据读取权限。
