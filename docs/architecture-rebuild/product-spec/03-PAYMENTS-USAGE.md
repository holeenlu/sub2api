# 03 支付、订单、用量与仪表盘功能规格

c2维护35路径的应用和前端开发证据已交接，当前e5源码已冻结；详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。当前catalog为51面板/753业务定义，均not_run；开发测试不替代实际业务验收。

## 功能索引

| 功能编号 | 功能名 | 实际入口 | 角色 |
| --- | --- | --- | --- |
| FS-PAY-001 | 充值金额、支付方式与报价 | /purchase 充值页签 | 已登录用户，支付功能开启 |
| FS-PAY-002 | 套餐购买、续费选择与确认 | /purchase 订阅页签、续费选择弹窗 | 已登录用户，支付及订阅功能允许 |
| FS-PAY-003 | 创建订单、渠道交接与原订单恢复 | /purchase 支付流程 | 当前订单所属用户 |
| FS-PAY-004 | 二维码、支付等待与取消 | /purchase 支付面板、/payment/qrcode | 当前订单所属用户 |
| FS-PAY-005 | 微信支付授权回调 | /auth/wechat/payment/callback | 公开入口，持有效恢复上下文者 |
| FS-PAY-006 | Stripe 独立支付页 | /payment/stripe | 公开路由；实际付款须通过本人订单校验 |
| FS-PAY-007 | Airwallex 独立支付页 | /payment/airwallex | 有效公开恢复凭据或本人订单会话 |
| FS-PAY-008 | Stripe 父子窗口交接 | /payment/stripe-popup | 可信同源 opener 与本人订单会话 |
| FS-PAY-009 | 支付结果查询 | /payment/result | 有效公开订单凭据或当前登录订单所有者 |
| FS-PAY-010 | 我的订单、取消与退款申请 | /orders、取消/退款弹窗 | 当前用户，支付功能开启 |
| FS-PAY-011 | 管理订单查询、详情与审计 | /admin/orders | 获该页面访问权的管理员 |
| FS-PAY-012 | 管理取消、重试、退款和退款查询 | /admin/orders、退款弹窗 | 分别具备 orders.manage / orders.refund |
| FS-PAY-013 | 交易套餐列表、上下架与删除 | /admin/orders/plans | 获页面访问权及相应套餐权限的管理员 |
| FS-PAY-014 | 交易套餐创建和编辑 | 套餐编辑弹窗 | plans.manage；创建另需 billing.rates.update |
| FS-PAY-015 | 支付 Provider 配置编辑 | /admin/settings → 支付 Provider 面板 | super_admin，独立命令仍由后端鉴权 |
| FS-PAY-016 | Provider 开关、类型、删除和排序 | Provider 卡片、删除确认 | super_admin |
| FS-PAY-017 | 支付通用规则配置 | /admin/settings → PaymentSettingsPanel | 设置工作区允许的字段编辑者 |
| FS-PAY-018 | 充值赠金/折扣阶梯配置 | /admin/settings → RechargeBonusSettingsPanel | 设置工作区允许的字段编辑者 |
| FS-PAY-019 | 低余额通知默认配置 | /admin/settings → BalanceNotificationSettingsPanel | 设置工作区允许的字段编辑者 |
| FS-PAY-020 | 个人与管理余额历史 | 应用顶栏余额历史、用户目录/管理用量的历史弹窗 | 本人，或获目标用户读取权限的管理员 |
| FS-USAGE-001 | 用户请求记录、统计和筛选 | /usage | 当前登录用户 |
| FS-USAGE-002 | 管理请求记录、统计和筛选 | /admin/usage | 获页面访问权的管理员 |
| FS-USAGE-003 | 用户/Key 排行与分布下钻 | 用户/管理用量页、仪表盘图表 | 各页面当前授权范围 |
| FS-USAGE-004 | 错误记录和详情 | /usage、/admin/usage 错误页签 | 本人或获管理读取权者 |
| FS-USAGE-005 | 用量、费用、延迟和图片明细口径 | 用量列表、明细浮层、图表和导出 | 可读取对应记录及费用字段者 |
| FS-USAGE-006 | 用户 CSV 与管理 XLSX 导出 | 用量记录页导出按钮 | 本人；管理导出另需 usage.export |
| FS-USAGE-007 | 用量清理任务与取消请求 | /admin/usage → 清理弹窗 | super_admin |
| FS-USAGE-008 | 匿名 Key 用量查询与展示 | /key-usage | 持有效 API Key 者，无需管理登录 |
| FS-DASH-001 | 用户仪表盘 | /dashboard | 当前登录用户 |
| FS-DASH-002 | 管理仪表盘 | /admin/dashboard | 获该页面访问权的管理员 |
| FS-DASH-003 | 订单收入仪表盘 | /admin/orders/dashboard | 获该页面访问权的管理员，支付功能开启 |

## 范围、承接与共同合同

- 原始比较基线为6f48d6ab6fcc29763af84410690530aeaa957403，维护输入固定c2a10f2；当前前端e5已冻结。完整6196测试/双build真实输入63fe，按唯一测试类型补丁和生产等价证明复用于e5；五项static直接绑定e5。旧f4fe、5c3005及5085记录保留原hash，详见[最终证据](REVIEW-EVIDENCE.md#final-e5-evidence)。开发验证不等于业务验收。
- 路由取自 [payments/routes](../../../frontend/src/features/payments/routes.ts)、[usage/routes](../../../frontend/src/features/usage/routes.ts)、[应用组合路由](../../../frontend/src/app/router/index.ts)及 [route-inventory](../../../frontend/tests/acceptance/route-inventory.json)。`/purchase` 是购买入口；微信旧返回路径 `/payment` 在回调解析中规范为 `/purchase`，不据此虚构一条现存独立页面。
- 普通管理员须先满足实际 `admin_pages` 页面授权；按钮权限、字段白名单、合规/step-up 和后端对象归属继续生效。公开路由仅允许打开页面，不自动授予订单读取或付款权限。具体 guard、角色及个人通知设置见[第1章](01-PUBLIC-IDENTITY-KEYS.md)。
- 套餐关联组、订阅政策、用户订阅分配及模型定价归[第2章](02-RESOURCES-ACCESS-CATALOG.md)；本章拥有交易侧套餐展示/售卖与付款。供应商门户的账单、双边结算与角色操作归[第4章](04-OPERATIONS-SUPPLY-SYSTEM.md)，仅引用本章 FS-USAGE-005 的共享展示口径，不套用普通用户订单流程。
- 本章 HTTP 路径除特别标明外均为共享管理客户端的相对路径；不据此修改部署 base URL。匿名订单读取及 `/v1/usage` 使用现有 shared/http 的匿名策略；支付 SDK 不接收管理 JWT。
- 查询/命令固定当前身份、目标、必要筛选及请求代次；换身份、换目标或卸载取消等待并拒绝晚到结果。Abort 不代表服务端撤销，未知写结果不能当失败后盲目重发。见 [payments 请求归属](../../../frontend/src/features/payments/queries/usePaymentRequest.ts)、[usage 请求归属](../../../frontend/src/features/usage/queries/useUsageRequest.ts)。
- 下列 case ID 来自 [cases.json](../../../frontend/tests/acceptance/cases.json)。753是当前定义数量，不是通过数量；工作包和执行结果仅归 [work-items](../work-items.json)、[TEST_REPORT](../TEST_REPORT.md)。本章不新增验收状态、后端接口、发票功能或真实第三方交易成功记录。

### 权威状态和金额边界

| 对象 | 已存在的枚举/单位 | 必须保持的解释 |
| --- | --- | --- |
| PaymentOrder.status | PENDING、PAID、RECHARGING、COMPLETED、EXPIRED、CANCELLED、FAILED、REFUND_REQUESTED、REFUNDING、REFUND_PENDING、PARTIALLY_REFUNDED、REFUNDED、REFUND_FAILED | UI 将 PAID/RECHARGING/COMPLETED 展示为已支付路径，不表示三者履约阶段相同；再次进入支付 SDK 只接受权威 PENDING。未知字符串不得推断已付款 |
| PaymentOrder.order_type | balance、subscription | 充值和购买订阅分别履约；订单创建成功不等于余额到账/订阅生效 |
| 支付金额 | amount、pay_amount、currency、fee_rate、bonus_amount | 到账基数与网关实付分开；多币种统计按 currency 分组，不直接相加；页面报价是预览，最终以服务端订单为准 |
| billing_status | unknown、pending、pending_pricing、settled、free、lower_bound、unmetered | 未知、等待定价、已结算、免费与确认下界不能合并为“零费用” |
| UsageCleanupTask.status | 页面识别 pending、running、succeeded、failed、canceled；DTO 保持开放 string | 未识别状态原样展示；取消请求与后台取消完成不是同一事实 |

来源：[支付 DTO](../../../frontend/src/features/payments/api/types.ts)、[服务端订单枚举](../../../backend/internal/payment/types.go)、[用量 DTO](../../../frontend/src/features/usage/api/types.ts)。付款成功证据须来自权威订单读取，URL、SDK 返回和 popup 关闭均不能单独充当证明。

## 支付、订单与配置

### FS-PAY-001 充值金额、方式与报价

- **入口/前置**：登录进入 `/purchase`；支付开关允许且 `balance_disabled=false` 才提供充值页签；无可用方式、配置未读出与金额不适用分别呈现。
- **输入/校验**：快捷金额默认 10/20/50/100/200/500/1000/2000/5000，按可见渠道范围过滤；自定义框接受至多两位小数，空值/非正数不形成可提交金额。方式限额 `single_min/single_max` 的 0 表示不限制。
- **流程/结果**：展示渠道名称、可用状态、币种、充值倍率、赠金或折后实付、手续费及到账 USD；当前方式不满足额度时按现有规则选另一个可容纳金额的方式。帮助 Markdown 经清洗后显示，帮助图片可预览关闭。
- **边界**：折扣以输入金额命中阶梯，额度适配使用折后基数；提交仍传输入金额。充值手续费保留现有向上取分规则；没有渠道可承接时显示金额错误，不以预览值自行记账。
- **权威数据**：`GET /payment/checkout-info` 的 methods、plans、倍率/汇率/手续费/帮助和优惠字段；配置变化后重新读取，未知订单创建按 FS-PAY-003。
- **依据/既有 case**：[金额输入](../../../frontend/src/features/payments/components/AmountInput.vue)、[报价](../../../frontend/src/features/payments/model/useCheckoutQuote.ts)、[页面](../../../frontend/src/features/payments/pages/CheckoutPage.vue)；`FE-PAGE-PURCHASE-ACTION-1`、`FE-BILL-001`、`FE-BILL-002`。

### FS-PAY-002 套餐购买与续费确认

- **入口/前置**：购买页订阅页签受订阅开关控制；显示服务端可售套餐、当前订阅、关联组及有效期/额度/倍率等已有字段，空列表显示不可用。
- **输入/校验**：选定 plan ID 与支付方式；`tab=subscription&group=ID` 只定位组，单个匹配套餐可直接选中，多个匹配套餐打开续费选择弹窗；关闭选择或取消确认不创建订单。
- **流程/结果**：套餐卡进入确认视图，列出有效期、渠道实付/手续费；续费选择只从该组现有套餐中挑选，不在前端直接延长订阅。
- **金额边界**：正的 `subscription_usd_to_cny_rate` 仅用于默认 CNY 渠道换算；未配置时维持原 price 直付。渠道额度使用含手续费的套餐实付；币种显示标签不自行改变服务端计价。
- **权威数据**：`checkout-info.plans` 与注入的当前有效订阅投影；发起 `order_type=subscription, plan_id` 的订单，付款后的权益仍重新读取订阅域。
- **依据/既有 case**：[套餐卡](../../../frontend/src/features/payments/components/SubscriptionPlanCard.vue)、[购买页](../../../frontend/src/features/payments/pages/CheckoutPage.vue)、[报价](../../../frontend/src/features/payments/model/useCheckoutQuote.ts)；`FE-PAGE-PURCHASE-ACTION-1`、`FE-BILL-002`。

### FS-PAY-003 下单、渠道交接和恢复

- **入口/前置**：充值或套餐确认通过当前校验，且该会话没有进行中或未知的提交；捕获金额、支付方式、订单类型、plan ID 及移动端/微信环境。
- **流程/结果**：`POST /payment/orders` 返回后，按真实 result_type/字段选择二维码、支付 URL、Stripe popup/独立页、Airwallex 或微信 JSAPI/OAuth；保留服务端 order ID、out_trade_no、client secret、resume token、期限等原恢复格式。
- **恢复**：刷新时读取原恢复快照，核对时间/结构及目标；私有购买页先查询订单并确认 user_id 属于当前用户再展示。微信授权恢复先移除消费过的 query；签名恢复 token 可按现有合同以 amount=0 发起恢复，而非信任 URL 金额。
- **异常/取消**：未知创建结果显示处理中并提供订单入口，禁止重复下单；配置拒绝只有现有微信商户拒绝条件允许一次桌面 QR fallback，网络/网关/JSAPI 模糊失败不能借 fallback 再下单。
- **权威数据**：[paymentAPI](../../../frontend/src/features/payments/api/payment.ts)；浏览器快照不是订单事实，客户端取消桥接等待不等于撤销已创建订单；成功后仅刷新相关余额/订阅投影。
- **依据/既有 case**：[流程决策和快照](../../../frontend/src/features/payments/model/paymentFlow.ts)、[下单命令](../../../frontend/src/features/payments/commands/useCheckoutOrder.ts)、[重试边界](../../../frontend/src/features/payments/model/paymentRetryPolicy.ts)；`FE-PAGE-PURCHASE-ACTION-2`、`FE-PAGE-PURCHASE-ACTION-3`、`FE-PAY-001`。

### FS-PAY-004 二维码与支付等待面板

- **入口/前置**：购买页内支付阶段或 `/payment/qrcode?order_id=…`；独立页先读取指定订单并核对 ID、user_id。qr/pay_url/payment_type/到期参数只供呈现，不能绕过订单归属。
- **操作**：显示支付金额/币种、订单号、二维码与倒计时；可重开支付窗口。移动支付宝按既有深链打开 App，超时才显示 QR fallback；可重开支付宝及保存本次二维码 PNG。
- **正常结果**：每 3 秒单次轮询 `GET /payment/orders/{id}`；对支付宝/微信 PENDING，至少相隔 15 秒、最多 6 次调用 `POST /payment/orders/verify` 核实；收到服务端已支付状态再发出成功事件。
- **取消/异常**：取消走 `POST /payment/orders/{id}/cancel`；未知结果锁住重复取消，继续读取订单。迟到取消不能覆盖已确认付款；页面离开停止轮询/深链计时。倒计时结束是本地到期展示，不单独证明第三方未扣款。
- **返回**：完成/关闭返回购买页或带原 order ID/resume token 的结果页；公开回跳仍由 FS-PAY-009 查证。
- **依据/既有 case**：[二维码页](../../../frontend/src/features/payments/pages/PaymentQRCodePage.vue)、[支付面板](../../../frontend/src/features/payments/components/PaymentStatusPanel.vue)、[轮询](../../../frontend/src/features/payments/queries/useOrderMonitor.ts)；`FE-PAGE-PAYMENT-QRCODE-ACTION-1`、`FE-PAGE-PAYMENT-QRCODE-ACTION-2`、`FE-PAY-001`。

### FS-PAY-005 微信支付回调

- **入口/输入**：公开 `/auth/wechat/payment/callback`；读取 fragment 优先、query 后备的 error/描述、wechat_resume_token 或旧 openid/state/scope/payment_type/amount/order_type/plan_id。
- **正常流程**：显示处理中；将合法恢复字段交给购买页，以 `wechat_resume=1` 标记恢复；签名 token 优先，旧 openid 仅保留现有兼容恢复分支。
- **校验/失败**：缺 token 且缺 openid、或携带错误时显示错误和返回购买按钮；redirect 仅接受现有本地路径规则，空/绝对外部 URL/双斜线回退 `/purchase`，旧 `/payment` 转为购买入口。
- **边界**：此回调不写“已付款”、不读取管理 JWT 当公开凭据、不自行授予订阅；实际恢复和订单事实由下单/查询 API 校验。
- **依据/既有 case**：[回调页](../../../frontend/src/features/payments/pages/WechatPaymentCallbackPage.vue)、[恢复解析](../../../frontend/src/features/payments/model/paymentWechatResume.ts)；`FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-ACTION-1`、`FE-PAGE-AUTH-WECHAT-PAYMENT-CALLBACK-ACTION-2`、`FE-PAY-001`。

### FS-PAY-006 Stripe 独立页

- **入口/前置**：`/payment/stripe` 虽为公开路由，当前流程使用本人会话验证恢复快照中的订单；不单独采信 URL 的 client_secret。需要匹配的本地快照和服务端 publishable key。
- **流程**：权威 PENDING 才加载 Stripe；method=alipay 走支付宝确认，wechat_pay 使用原移动/桌面 client 选项，其余呈现 Payment Element；就绪后才允许手动确认。
- **结果**：SDK 成功或未决结果转为服务端订单轮询；PAID/RECHARGING/COMPLETED 走已支付结果；取消、失败、过期、退款及未知非 PENDING 状态转只读结果页，不再次进入 SDK。
- **异常/取消**：输入/card 等明确拒绝允许原界面修正；网络/api_error 等未知结果仅查询，不盲重发 confirm。换订单/会话、卸载销毁 Element、取消读取及关闭计时器；返回 URL 不构成成功证据。
- **依据/数据/case**：[Stripe 命令](../../../frontend/src/features/payments/commands/useStripeCheckout.ts)、[恢复校验](../../../frontend/src/features/payments/queries/useCheckoutRecovery.ts)、[配置 API](../../../frontend/src/features/payments/api/payment.ts)；`FE-PAGE-PAYMENT-STRIPE-ACTION-1`、`FE-PAGE-PAYMENT-STRIPE-ACTION-2`、`FE-PAY-001`。

### FS-PAY-007 Airwallex 独立页

- **入口/前置**：`/payment/airwallex` 保留 user 壳但允许匿名入口；本地恢复快照须含匹配订单、clientSecret、intentId、paymentType=airwallex。
- **鉴别范围**：有签名 resume token 时以匿名公共订单 wrapper 核对 out_trade_no；无公开凭据则查本人订单。公开凭据有效性和登录身份分别管理。
- **流程/结果**：只有权威 PENDING 初始化既有 SDK，并按 snapshot 的 prod/demo 环境、currency/country_code 与语言进入 checkout；其他状态进入结果页。successUrl 携带原订单标识而不直接确认支付。
- **异常/取消**：缺参数、过期/不匹配快照或 SDK 初始化失败显示错误；在 import/init 等等待中离开或改变私有 scope 后，不再发起 checkout/跳转。真实跳转及第三方收款尚须 sandbox 验证。
- **依据/数据/case**：[页面流程](../../../frontend/src/features/payments/commands/useAirwallexCheckout.ts)、[恢复读取](../../../frontend/src/features/payments/queries/useCheckoutRecovery.ts)；`FE-PAGE-PAYMENT-AIRWALLEX-ACTION-1`、`FE-PAGE-PAYMENT-AIRWALLEX-ACTION-2`、`FE-PAY-002`。

### FS-PAY-008 Stripe popup

- **入口/前置**：`/payment/stripe-popup?order_id=…&method=…`；窗口发送 READY，仅接收同源且 source 为真实 opener 的一次 INIT，要求 clientSecret/publishableKey 非空。
- **流程/结果**：读取订单并核对当前 user_id、目标 ID；仅 PENDING 可调用对应支付宝/微信确认；金额按订单币种显示。完成仍须轮询读取，成功后按现有延时关闭窗口。
- **异常/取消**：INIT 超时、缺字段、不可信 origin/source、重复消息均不产生付款；不接受 URL/消息单独确认成功。未知 SDK 结果保持读取，不能重新初始化付款；换 scope/订单或关闭清除监听和计时器。
- **依据/数据/case**：[popup owner](../../../frontend/src/features/payments/commands/useStripePopup.ts)、[本人订单 API](../../../frontend/src/features/payments/api/payment.ts)；`FE-PAGE-PAYMENT-STRIPE-POPUP-ACTION-1`、`FE-PAGE-PAYMENT-STRIPE-POPUP-ACTION-2`、`FE-PAY-001`。

### FS-PAY-009 支付结果页

- **入口/输入**：`/payment/result` 接受 resume_token、order_id 及现有 legacy out_trade_no/trade_status 回跳参数；按签名公开读取、合法本人读取和既有旧订单号查询分支解析。
- **正常结果**：显示权威订单号/状态；只有响应确含金额/币种/支付方式时才显示相应信息，不把公共精简响应补成完整 PaymentOrder。完整本人余额订单 COMPLETED 后可刷新用户余额。
- **未知/失败**：URL 的 trade_status、金额等只能作为回跳信息，不能确认成功；有查询依据的待确认/无结果分支每 2 秒继续读取，最多再查 15 次，不重新下单。
- **scope/返回**：公开结果不随无关登录变动变成私有数据；私有投影在换会话时失效。可返回购买/我的订单，目标页面再次执行其登录和开关检查。
- **权威 API/case**：[结果查询](../../../frontend/src/features/payments/queries/useOrderResult.ts)、[结果页](../../../frontend/src/features/payments/pages/PaymentResultPage.vue)、[公开 wrapper](../../../frontend/src/features/payments/api/payment.ts)：`POST /payment/public/orders/resolve`、`/verify`；`FE-PAGE-PAYMENT-RESULT-ACTION-1`、`FE-PAGE-PAYMENT-RESULT-ACTION-2`、`FE-PAY-001`。

### FS-PAY-010 我的订单与退款申请

- **入口/前置**：`/orders` 读取本人订单；可按状态过滤、分页/改变页大小、刷新、返回购买。表格显示订单号、类型、到账/实付金额、币种、状态及创建时间。
- **取消**：PENDING 行打开确认；取消对话框不发命令，确认后调用 `POST /payment/orders/{id}/cancel`，成功重读列表。
- **申请退款**：显示、打开及提交统一要求 order_type=balance、COMPLETED、provider_instance_id 存在且在 `GET /payment/orders/refund-eligible-providers` 返回集合内；理由 trim 后非空才提交 `POST /payment/orders/{id}/refund-request`。
- **服务端边界**：申请仅接受本人 balance 订单、允许用户退款的 Provider，且当前余额足够；前端以同一 refundEligibility 过滤订阅订单，后端仍是类型、归属及余额权威。申请成功只进入 REFUND_REQUESTED，不代表已退款。
- **异常/取消**：明确拒绝保留可修正输入；未知取消/退款结果阻止重复命令；换身份清空目标、草稿和列表，不将上一身份响应显示到新列表。
- **依据/既有 case**：[列表和资格](../../../frontend/src/features/payments/queries/useUserOrders.ts)、[动作](../../../frontend/src/features/payments/commands/useUserOrderActions.ts)、[服务端申请边界](../../../backend/internal/service/payment_refund.go)；`FE-PAGE-ORDERS-ACTION-1`、`FE-PAGE-ORDERS-ACTION-2`、`FE-PAY-001`。

### FS-PAY-011 管理订单列表和详情

- **入口/前置**：`/admin/orders`，页面访问授权与写权限分开；关键词防抖搜索，状态、支付方式、balance/subscription 类型过滤，分页、页大小和手动刷新。
- **详情**：按目标 ID 查询，展示服务端订单及 auditLogs/audit_logs 中的 action、detail、operator、created_at；金额/退款字段不从相邻行或前端推算补齐。
- **正常结果**：列表请求 `GET /admin/payment/orders`；详情 `GET /admin/payment/orders/{id}`。打开另一订单或关闭弹窗使旧详情失效。
- **失败/取消**：列表失败通知；详情失败保留当前选中行快照，不伪造新的审计结果；换会话清空列表、页码、详情及防抖请求。
- **依据/既有 case**：[页面](../../../frontend/src/features/payments/pages/AdminOrdersPage.vue)、[列表/详情 owner](../../../frontend/src/features/payments/queries/useAdminOrders.ts)、[API](../../../frontend/src/features/payments/api/adminPayment.ts)；`FE-PAGE-ADMIN-ORDERS-ACTION-1`、`FE-PAGE-ADMIN-ORDERS-MANUAL-COMPLIANCE`。

### FS-PAY-012 管理订单写操作与退款弹窗

- **权限/入口**：orders.manage 可取消 PENDING、重试 FAILED 的充值履约；orders.refund 可进入申请审批/退款失败重试/已完成退款弹窗，REFUND_PENDING 行只查询结果。现有管理取消/充值重试为行按钮直接调用，不虚构额外确认弹窗。
- **输入校验**：退款 amount 为到账金额口径，必须 >0 且不超过弹窗 maxRefundable；reason 必填，deduct_balance 默认为 true，force 默认为 false。服务端 require_force 时必须用户显式勾选；关闭弹窗不发送退款。
- **金额和状态**：REFUND_REQUESTED/REFUND_PENDING 中 refund_amount 是申请/待确认额，只有 PARTIALLY_REFUNDED/REFUNDED 才作为已退金额扣除。服务端按原 amount/pay_amount 和币种换算网关退款，不由浏览器决定到账。
- **正常 API**：`POST /admin/payment/orders/{id}/cancel|retry|refund|refund/query`；refund 返回 success、warning、require_force 等真实结果。待确认退款只查询；异常为未知时冻结该订单再次写入，允许既有查询收敛。
- **状态资格**：[refundEligibility](../../../frontend/src/features/payments/model/refundEligibility.ts) 统一显示、打开与提交，仅允许 COMPLETED/REFUND_REQUESTED/REFUND_FAILED。REFUND_PENDING 仅查询，PARTIALLY_REFUNDED 即使 force 也不发起新退款；首次对可退款订单输入部分金额仍支持。实际到账需独立 sandbox 验证。
- **依据/既有 case**：[退款表单](../../../frontend/src/features/payments/components/AdminRefundDialog.vue)、[动作 owner](../../../frontend/src/features/payments/commands/useAdminOrderActions.ts)、[服务端准备退款](../../../backend/internal/service/payment_refund.go)；`FE-PAGE-ADMIN-ORDERS-ACTION-2`、`FE-PAY-002`。

### FS-PAY-013 交易套餐列表、上下架与删除

- **入口/前置**：`/admin/orders/plans`；展示交易套餐、组信息、价格/展示币种、有效期、排序及售卖状态。该页没有用户订阅 tab；分配/撤销等订阅管理在 `/admin/subscriptions`，见第2章，不由套餐售卖动作替代。
- **操作权限**：plans.manage 允许编辑/删除；创建还要求 billing.rates.update；上下架同时要求 update 字段集合包含 for_sale（超管不受该白名单缩减）。
- **流程/API**：`GET /admin/payment/plans`；上下架 `PUT /admin/payment/plans/{id}` 仅传 for_sale，确认成功再更新对应卡片；删除先确认，`DELETE` 成功后重读，不乐观移除。
- **异常/取消**：取消删除不写；失败显示支付域错误；未知结果锁住该目标再次写入，角色/字段权限变化关闭失效编辑器，换会话不接纳旧结果。
- **依据/既有 case**：[套餐页](../../../frontend/src/features/payments/pages/AdminPaymentPlansPage.vue)、[动作权限](../../../frontend/src/features/payments/commands/usePaymentPlanActions.ts)；`FE-PAGE-ADMIN-ORDERS-PLANS-ACTION-2`、`FE-PAGE-ADMIN-ORDERS-PLANS-MANUAL-COMPLIANCE`。

### FS-PAY-014 交易套餐编辑

- **字段**：name、group_id、description、price、original_price、currency、validity_days、validity_unit、sort_order、for_sale、逐行 features；组选择仅列 subscription 类型组，组摘要为只读政策信息。
- **校验**：组必选，price 必须有限且 >0，有效期数量必须有限且 ≥1；有效期单位保留 days/weeks/months。名称等 HTML 必填由表单约束；currency trim 后大写，features 去除空行后按原换行字符串格式提交。
- **流程**：新建 `POST /admin/payment/plans`，编辑 `PUT /admin/payment/plans/{id}`；只发送当前 create/update 允许字段；可读支付配置时展示 CNY 换算和手续费预览，不新增定价规则。
- **异常/取消**：关闭/换 plan/换权限使旧提交结果失效；明确失败保留当前草稿，未知保存阻止本次编辑器重发；服务端保存成功才关闭并刷新列表。
- **依据/既有 case**：[编辑器](../../../frontend/src/features/payments/components/PaymentPlanEditDialog.vue)、[校验命令](../../../frontend/src/features/payments/commands/usePaymentPlanEditor.ts)、[payload](../../../frontend/src/features/payments/model/paymentPlanDraft.ts)；`FE-PAGE-ADMIN-ORDERS-PLANS-ACTION-1`、`FE-BILL-001`。

### FS-PAY-015 Provider 编辑、密钥与回调配置

- **入口/前置**：支付设置 Provider 列表可创建/编辑 easypay、alipay、wxpay、stripe、airwallex；创建选项来自已启用类型，编辑不任意切换 provider_key。
- **字段/校验**：名称 trim 后非空；配置键及 required/optional/sensitive/clearable 属性以 PROVIDER_CONFIG_FIELDS 为准。新建要求所有非 optional 配置，编辑敏感字段留空保持服务器旧 secret；明确 clearable 键的空串才表达清空。
- **专属配置**：既有商户/应用 ID、签名/证书/密钥、API base、币种/国家、退款开关和 payment_mode；EasyPay 自定义方法要求本地 type、upstreamType，按既有字符集校验，禁止重复及保留 alipay/wxpay 前缀。
- **回调/限额**：notify/return 各自基地址加固定路径，空基地址用当前 origin；Stripe/Airwallex webhook 地址按提示在渠道后台配置。单次最小/最大和每日限额只序列化正值，全部空继承全局；不捏造“配置成功即 webhook 已联通”。
- **正常/失败**：保存走独立 create/update Provider API；成功重新读取排序后的列表，错误 metadata.key/keys 用支付配置标签本地化，失败保留草稿；scope 变化清除选择及凭据草稿。
- **依据/既有 case**：[配置合同](../../../frontend/src/features/payments/model/providerConfig.ts)、[配置弹窗](../../../frontend/src/features/payments/components/PaymentProviderDialog.vue)、[独立 API](../../../frontend/src/features/payments/api/adminPayment.ts)；`FE-PROVIDER-CREATE`、`FE-PROVIDER-EDIT`。

### FS-PAY-016 Provider 独立操作和排序

- **入口/操作**：列表刷新、开关 enabled/refund_enabled/allow_user_refund、supported_types 切换、编辑、删除确认和拖动排序；这些操作不等待设置工作区“全局保存”。
- **规则**：启用的支付宝/微信与 EasyPay 对外可见方式按既有冲突检查互斥；关闭 refund_enabled 同时发送 allow_user_refund=false。服务端待处理订单/权限/配置拒绝仍保留，不强行修改状态。
- **API/结果**：`GET/POST /admin/payment/providers`、`PUT/DELETE /admin/payment/providers/{id}`；删除取消无请求；排序对每个目标 PUT sort_order，等待所有结果再刷新实际顺序，不承诺多请求原子回滚。
- **生命周期**：只允许当前 scope 的单一在途命令；未知结果阻止重发但保留只读刷新，重读列表不能证明原写入未发生；换 scope/卸载取消等待并拒绝晚到通知、弹窗关闭或列表覆盖。
- **依据/既有 case**：[Provider 面板](../../../frontend/src/features/payments/settings/ProvidersSettingsPanel.vue)、[设置 ports](../../../frontend/src/features/payments/settings/ports.ts)、[支付错误 owner](../../../frontend/src/features/payments/model/paymentError.ts)；`FE-PROVIDER-TOGGLE`、`FE-PROVIDER-DELETE`、`FE-PROVIDER-ORDER`。聚合字段另对应 `FE-SETTING-PROVIDERSSETTINGSPANEL-FIELDS`、`FE-SETTING-PROVIDERSSETTINGSPANEL-WRITE`。

### FS-PAY-017 支付通用规则

- **字段范围**：payment_enabled、启用渠道类型、商品名称前/后缀、最小/最大/每日充值额、到账倍率、订阅 USD→CNY 汇率、手续费、订单超时、最大待付款数、负载策略、取消限频窗口/单位/次数、支付宝二维码/移动深链开关、帮助文字和图片 URL。
- **校验/解释**：页面限额输入保留 0 语义；倍率输入最小 0.01，手续费限制 0–100，订单超时/最大待付数至少 1；汇率 0 是未配置。仅声明当前页面约束，服务端仍最终验证。
- **正常流程**：领域面板产出已编辑字段；主设置工作区以原 revision 和实际 sentFields 一次 `PUT /admin/settings`，字段权限/冲突处理见[设置合同附录](SETTINGS_AND_CONTRACTS.md)。
- **异常/取消**：切页或撤销草稿不执行 Provider 命令；409 保留差异供审阅、428 重读合法 revision；未知保存不能自动重放。帮助展示链接不能替换管理 API base。
- **依据/既有 case**：[面板](../../../frontend/src/features/payments/settings/PaymentSettingsPanel.vue)、[字段 owner](../../../frontend/src/features/payments/settings/paymentModel.ts)；`FE-SETTING-PAYMENTSETTINGSPANEL-FIELDS`、`FE-SETTING-PAYMENTSETTINGSPANEL-WRITE`。

### FS-PAY-018 充值优惠阶梯

- **输入/校验**：bonus/discount 模式、min_amount 与 bonus_percent、Markdown 活动文案；编辑器限制最多 20 档，并提示非负、至多两位小数、重复阈值等错误；百分比有效范围为 0–1000，discount 的业务规则另要求 <100。编辑器提示与最终提交处理不是同一层。
- **流程/结果**：新增、编辑、删除阶梯并预览区间；不完整空行按既有 sanitize 规则处理，提交保留原 min_amount/bonus_percent 数据格式。命中规则、到账倍率和舍入复用同一 rechargeBonus 模型。
- **边界**：赠金和折扣含义不同，不能将折扣百分比当追加到账；活动文案不改变实际扣款。保存仍属于设置原子 PUT，不由面板另发付款命令。
- **异常/取消**：当前提交清洗会丢弃非法或不完整行，相同阈值保留首项并按阈值排序；不能承诺“发现任意非法行就整次阻止提交”。是否拒绝剩余配置仍由服务端合同决定。失败保留草稿，revision/未知结果按 FS-PAY-017 处理。
- **依据/既有 case**：[阶梯编辑](../../../frontend/src/features/payments/settings/RechargeBonusTierEditor.vue)、[计算/校验](../../../frontend/src/features/payments/model/rechargeBonus.ts)、[提交投影](../../../frontend/src/features/payments/settings/preparePatch.ts)；`FE-SETTING-RECHARGEBONUSSETTINGSPANEL-FIELDS`、`FE-SETTING-RECHARGEBONUSSETTINGSPANEL-WRITE`、`FE-BILL-001`。

### FS-PAY-019 低余额通知默认值

- **入口/字段**：设置面板仅维护 balance_low_notify_enabled、balance_low_notify_threshold、balance_low_notify_recharge_url；阈值为现有金额字段，充值 URL 为通知引导。
- **正常流程**：编辑允许字段后交同一设置聚合保存；不开辟独立邮件发送/试付款流程。个人通知邮箱、个人偏好及资料保存归第1章 FS-ID-016。
- **异常/取消**：未保存草稿不改变服务器默认值；授权失败、revision 冲突和未知写结果沿设置工作区处理。配置成功不证明邮件已投递。
- **依据/数据/case**：[通知面板](../../../frontend/src/features/payments/settings/BalanceNotificationSettingsPanel.vue)、[字段投影](../../../frontend/src/features/payments/settings/balanceNotificationModel.ts)；`GET/PUT /admin/settings`；`FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-FIELDS`、`FE-SETTING-BALANCENOTIFICATIONSETTINGSPANEL-WRITE`。

### FS-PAY-020 余额与历史读取

- **入口/输入**：个人从应用顶栏余额历史菜单打开；当前弹窗固定查询 category=balance、direction/source 为空，每页 15 条，提供分页和失败重试。API 的类别/方向/来源参数不等于该弹窗存在相应筛选控件；弹窗显示账户摘要、历史发生时间、带正负号 value、notes 和 total_recharged。
- **数据边界**：`GET /user/balance-history` 的历史 ID 是 string；管理 `GET /admin/users/{id}/balance-history` 是另一种数字 ID/兑换记录投影，保留 type、分页和 nullable used_by/used_at/group_id 等，不强制合并 DTO。
- **管理协作**：用户目录和管理用量的历史面板由 app 装配；余额调整 set/add/subtract、notes 和显式 0 的交互归第1章 FS-ID-021。`POST /admin/users/{id}/balance` 可仅返回 id，不能补造余额或完整 AdminUser。
- **失败/取消**：profile 与账本分别捕获身份/目标；关闭、换账户或 scope 后不接纳旧响应。缺失余额/历史不是已知零，失败不删除真实历史。
- **依据/既有 case**：[个人弹窗](../../../frontend/src/features/payments/components/BalanceHistoryModal.vue)、[查询 owner](../../../frontend/src/features/payments/queries/useBalanceHistory.ts)、[个人 API](../../../frontend/src/features/payments/api/balanceHistory.ts)、[管理 API](../../../frontend/src/features/payments/userBalances.ts)；管理入口 `FE-PAGE-ADMIN-USERS-ACTION-3`、数值 `FE-BILL-001`。目录暂无“个人顶栏余额历史”独立操作 case，不把仪表盘进入用例冒充该弹窗验收。

## 用量、导出与清理

### FS-USAGE-001 用户用量记录

- **入口/前置**：`/usage` 仅读取本人；初始日期来自既有本地日期范围规则，允许修改范围、日/小时粒度、刷新和重置。
- **筛选/操作**：API Key、模型、组、sync/stream/WS/Live 请求类型、独立 native_compaction_v2、余额/订阅计费类型、token/请求/图片/video 计费模式；用户筛选没有 Cyber 选项。支持服务端排序、游标分页、页大小、列显隐及可见 IP 信息。
- **结果**：上部统计/趋势与模型、组、端点分布；下部“请求记录 / Key 排行 / 错误请求”页签。筛选变更从第1页重新查询，未知总数按 total_exact 保留近似语义，不能拼接旧筛选游标。
- **权威数据**：`GET /usage`、`/usage/stats`、`/usage/dashboard/snapshot-v2`、`/usage/dashboard/models`；Key 下拉仅投影 id/name，不为筛选加载 Key 明文。
- **异常/取消**：读取失败有错误路径，不用空记录证明未发生请求；换 scope 清空私有结果、取消旧加载，旧响应不能影响新列表/统计。
- **依据/既有 case**：[页面](../../../frontend/src/features/usage/pages/UserUsagePage.vue)、[列表 owner](../../../frontend/src/features/usage/queries/useUsageList.ts)、[API](../../../frontend/src/features/usage/api/usage.ts)；`FE-PAGE-USAGE-ACTION-1`、`FE-PAGE-USAGE-ACTION-3`。

### FS-USAGE-002 管理用量记录

- **入口/权限**：`/admin/usage`；用户、Key、模型、组、请求/计费条件及上游模型不一致筛选；上游账号筛选/身份仅 super_admin，账号费用字段另受 billing.cost.read 控制，导出另受 usage.export 控制。
- **历史定位**：支持仪表盘携 user_id/start_date/end_date 深链；按 ID 查询用户时保留 include_deleted=true，不把已删除用户历史变为查无记录。账号搜索保留 1/20，组候选保留 1/1000 的现有读取合同。
- **流程/结果**：请求、错误、用户排行、Key 排行四页签；模型分布可区分 requested/upstream/mapping，入口/上游端点与路径分别呈现；列显隐、排序/分页、重置、刷新和用户余额历史入口按实际字段权限工作。
- **权威数据**：`GET /admin/usage`、`/admin/usage/stats`，目录搜索 `/admin/usage/search-users`、`/search-api-keys`；图表消费 usage 的 adminDashboard API，不把 account_id 缺失补成可访问账号。
- **异常/取消**：权限/scope 变化不泄漏旧账号、费用、错误详情或展开行；晚到请求不覆盖新筛选。导出/清理分别见 FS-USAGE-006/007。
- **依据/既有 case**：[页面](../../../frontend/src/features/usage/pages/AdminUsagePage.vue)、[筛选](../../../frontend/src/features/usage/components/UsageFilters.vue)、[app 窄服务](../../../frontend/src/app/compositions/usageServices.ts)；`FE-PAGE-ADMIN-USAGE-ACTION-1`、`FE-PAGE-ADMIN-USAGE-ACTION-2`、`FE-PAGE-ADMIN-USAGE-ACTION-3`。

### FS-USAGE-003 排行、分布与下钻

- **入口/输入**：用量页的用户/Key 排行、模型/组/端点分布图；选择范围、现有筛选、排行数量和可见列，按页面支持的统计字段排序。
- **用户范围**：本人 Key 排行使用 `/usage/dashboard/api-key-breakdown` 并隐藏所属用户列；管理排行使用 `/admin/dashboard/user-breakdown`、`/api-key-breakdown`。Key 名缺失保留 ID，已删除 Key 标识不可被当成可编辑对象。
- **联动结果**：点击排行行带对应 user_id 或 api_key_id 回到用量记录；展开模型/组/端点时按选中维度读取用户明细，再次点击收起；关闭/变筛选取消旧展开查询。
- **边界**：用户排行接口不支持的 billing_mode 不硬塞入请求；页面懒加载/重复切页不另建全局排行缓存。缺少费用权限时不从其它统计反推受限成本。
- **依据/数据/case**：[排行骨架](../../../frontend/src/features/usage/components/BreakdownRanking.vue)、[展开 owner](../../../frontend/src/features/usage/queries/useDistributionBreakdown.ts)、[API](../../../frontend/src/features/usage/adminDashboard.ts)；`FE-PAGE-USAGE-ACTION-2`、`FE-PAGE-ADMIN-USAGE-ACTION-2`、`FE-PAGE-ADMIN-DASHBOARD-ACTION-2`。

### FS-USAGE-004 错误记录与详情

- **用户入口**：错误页签可按本人 Key、模型片段、错误分类、HTTP 状态码及日期筛选、排序分页；点击记录弹窗读取时间、模型、入口、状态、分类、平台、上游状态、message 和服务器实际返回的 error_body。
- **管理入口**：在管理用量共享筛选上增加 error_phase/error_category/status_code；管理错误表及详情由 operations 的公开面板装配，重放/运维控制不属于本章，见第4章。
- **权威 API**：本人 `GET /usage/errors`、`/usage/errors/{id}`；管理通过 UsageServices.getAdminErrors → operations 错误查询，不另造 usage 管理错误端点。
- **异常/取消**：缺详情 ID 不查询；更换错误目标、关闭弹窗、换 scope 清空旧详情并取消等待；没有错误 body 不编造响应，失败状态区别于空内容。
- **依据/既有 case**：[用户错误 owner](../../../frontend/src/features/usage/queries/useUserUsageErrors.ts)、[详情弹窗](../../../frontend/src/features/usage/components/UserErrorDetailModal.vue)、[管理适配](../../../frontend/src/features/usage/queries/useAdminUsageErrors.ts)；`FE-PAGE-USAGE-ACTION-1`、`FE-PAGE-USAGE-ACTION-2`、`FE-PAGE-ADMIN-USAGE-ACTION-2`。

### FS-USAGE-005 用量明细和指标口径

- **列表/浮层操作**：查看 token、缓存、费用、时延及图片详情；复制请求/上游请求 ID；按当前权限显示请求模型、映射/上游响应模型、推理强度、计费类型、User-Agent、IP/地理信息。读取/复制失败不得冒报成功。
- **金额/未知**：缺失或非有限 token 显示未知，观察到的 0 保留；任何分项未知时不生成“完整总 token”。unknown/pending/pending_pricing/unmetered 费用不当作已知数，lower_bound 的输入/输出/总量/缓存/图片 token 保留 ≥ 标记，且不依赖费用是否展示；billing_status=unknown 不抹去已实测 token。free 与已知零不等于未知。
- **TPS/TTFT**：文本 TPS=output_tokens×1000/原始 duration_ms，保留一位；图片、非支持请求类型、非正/无效分母或输出显示 —。不减 first_token_ms，不用统一阶段 duration 替代 TPS 分母；首 token 展示仍直接取原 first_token_ms。
- **时延/缓存**：latency_metrics version=1 才解释 first_event/content/text/duration；未带 metrics 使用 legacy，未知版本不伪装 legacy。缓存命中率 read/(input+read+creation)，缺分项/负数/无效分母为未知；缓存 TTL 细项属于写入总数，不重复累计。
- **图片/费用**：图片 count、输入/输出尺寸、来源与 breakdown、图片 token/cost 按真实字段；requested/upstream/response model 不合并。账号费用遵循已有 account_stats_cost 或 total_cost 基数及账号倍率投影，浏览器不成为扣费权威。
- **依据/既有 case**：[列表和明细](../../../frontend/src/features/usage/components/UsageTable.vue)、[数值](../../../frontend/src/features/usage/model/usageNumbers.ts)、[指标](../../../frontend/src/features/usage/model/metrics.ts)、[TPS](../../../frontend/src/features/usage/model/latencyHealth.ts)、[图片口径](../../../frontend/src/features/usage/model/imageUsage.ts)；`FE-BILL-001`、`FE-PAGE-USAGE-ACTION-3`、`FE-PAGE-ADMIN-USAGE-ACTION-3`。

### FS-USAGE-006 导出

- **入口/前置**：用户请求记录导出 CSV；管理有 usage.export 才导出 XLSX，account 列仅超管、账号倍率/费用列另需 billing.cost.read。捕获当前过滤/权限，不导出错误页签或偷偷扩大为全租户。
- **数据/格式**：`GET /usage/export` 或 `/admin/usage/export` 返回 rows/complete/error NDJSON 帧；只有 complete.rows 与累计条数一致且流结束才下载。用户文件保留 BOM、字段顺序和 CSV 转义，管理工作表为 Usage；名称保留 usage_开始日期_to_结束日期。
- **内容边界**：时间、模型/推理、入口、token、缓存、费用、计费状态及原/统一时延字段按 exportRows 的既有列序；用户费用 8 位、管理费用 6 位格式，未知费用/时延保持空值。CSV 公式前缀及空白前缀执行既有防公式注入转义。
- **异常/取消**：EOF 无完成帧、错误帧、非法格式/行数、完成帧后数据均失败；保留 256 MiB 总量和 8 MiB 批次边界。管理进度可取消，权限/scope 变化或卸载释放流读取，不下载半文件，不宣称服务端导出任务已撤销。
- **依据/既有 case**：[导出命令](../../../frontend/src/features/usage/queries/useUsageExport.ts)、[流协议](../../../frontend/src/features/usage/api/usageExport.ts)、[列定义](../../../frontend/src/features/usage/model/exportRows.ts)；`FE-PAGE-USAGE-ACTION-2`、`FE-PAGE-ADMIN-USAGE-ACTION-2`、`FE-BILL-001`。

### FS-USAGE-007 清理用量任务

- **入口/权限**：管理用量页仅 super_admin 显示清理按钮；弹窗复制当前过滤，开始/结束日期必填。实际 payload 支持 user_id/api_key_id/account_id/group_id/model/request_type/兼容 stream/billing_type/timezone，不把页面其它筛选默认为删除条件。
- **正常流程**：确认后 `POST /admin/usage/cleanup-tasks`；最近任务通过 `GET` 按每页 5 项显示，支持翻页、手动刷新和打开期间每 10 秒轮询；展示服务器 status、deleted_rows、范围、错误及时间。
- **取消**：pending/running 任务提供取消确认，`POST /admin/usage/cleanup-tasks/{id}/cancel`；关闭确认不发命令。后台最终状态由后续读取决定，页面关闭仅停止轮询/等待。
- **未知/失败**：明确拒绝允许修正；未知创建/取消和提交中关弹窗标记结果未知，重开同一组件不能盲重发。换 scope 清除旧任务/目标，迟到响应不能发成功通知或覆盖新列表。
- **依据/既有 case**：[清理弹窗](../../../frontend/src/features/usage/components/UsageCleanupDialog.vue)、[API/payload](../../../frontend/src/features/usage/api/adminUsage.ts)。关联入口 `FE-PAGE-ADMIN-USAGE-ENTRY`；现有 cases.json **无清理创建/取消专项 case ID**。现有[组件回归](../../../frontend/src/features/usage/components/__tests__/UsageCleanupDialog.spec.ts)仅为开发证据，不能替代待补的真实清理操作验收。

### FS-USAGE-008 匿名 Key 查询

- **入口/输入**：公开 `/key-usage`，输入非空 API Key，可切换明文可见性、Enter/按钮查询；范围 today/7d/30d/custom，自定义开始/结束日期，独立每日数据窗口 7/30/90 天，主题明暗切换。
- **授权/数据源**：`GET /v1/usage` 使用显式 x-api-key、匿名 sessionPolicy 和本地时区参数；不带管理 JWT/cookie，不触发管理登录刷新/退出；Key 不放 URL、导航元数据或持久化偏好。
- **正常展示**：quota_limited 展示 quota、5h/1d/7d 限额/重置、剩余和到期；unrestricted 按响应展示订阅窗口或余额。统计含今日/累计请求、输入/输出/缓存/总 token、费用、RPM/TPM、平均时长、模型与日明细。
- **异常/取消**：空 Key 提示输入；错误 Key/读取失败显示错误，不显示上一 Key 数据；更换 Key、日期/窗口或离开页面取消旧查询。缺少统计数值保持未知；主题偏好不持久化秘密。
- **依据/既有 case**：[页面](../../../frontend/src/features/usage/pages/PublicKeyUsagePage.vue)、[查询](../../../frontend/src/features/usage/queries/usePublicKeyUsage.ts)、[匿名 API](../../../frontend/src/features/usage/api/publicKeyUsage.ts)、[呈现](../../../frontend/src/features/usage/model/usePublicKeyUsagePresentation.ts)；`FE-PAGE-KEY-USAGE-ACTION-1`、`FE-PAGE-KEY-USAGE-ACTION-2`、`FE-BILL-001`。

## 仪表盘

### FS-DASH-001 用户仪表盘

- **入口/前置**：`/dashboard` 登录后进入；先刷新本人资料再接纳统计；余额来源身份域，Key/用量/平台配额来源各自权威读取，不额外创建仪表盘余额缓存。
- **展示/操作**：余额（非简易模式）、总/有效 Key、今日/累计请求、标准/实际费用、token/缓存命中、RPM/TPM、平均耗时；按平台折叠展示用量和实际配置的额度，limit=0 表示禁用，null/缺失不伪造正额度。
- **图表/导航**：默认最近 7 个本地日期，日期范围/日小时粒度与刷新；模型分布和近期 5 条记录；快捷入口仅导航 /keys、/usage 和获批图能力时的 /batch-image，不直接执行创建 Key 或批图。
- **权威 API**：`GET /usage/dashboard/stats|trend|models`、`/user/platform-quotas`；近期读取原 `/usage` 日期查询再截取 5 条。日期图表改变与近期列表的刷新按各 owner 保持独立。
- **失败/取消**：失败不生成零统计；同 scope 可保留已知旧快照，换身份必须清空，迟到 profile 不触发新身份的旧链路。批图资格缺失不显示其快捷动作。
- **依据/既有 case**：[组合页](../../../frontend/src/app/compositions/dashboard/pages/UserDashboardPage.vue)、[查询](../../../frontend/src/features/usage/queries/useUserDashboardData.ts)、[统计卡](../../../frontend/src/app/compositions/dashboard/components/UserDashboardStats.vue)；`FE-PAGE-DASHBOARD-ACTION-1`、`FE-PAGE-DASHBOARD-ACTION-2`、`FE-BILL-001`。

### FS-DASH-002 管理仪表盘

- **入口/前置**：`/admin/dashboard` 须获得页面授权；显示总/有效 Key、账号健康数量、请求、新用户/用户数、今日/累计 token、标准/用户/账号费用、缓存命中、RPM/TPM、平均耗时和活跃用户等实际统计字段。
- **操作/结果**：日期范围、日/小时粒度、手动刷新；模型分布/用户支出排行、token 趋势、用户用量趋势，可在 tokens/actual_cost 间切换。默认使用既有最近 24 小时的本地日期边界；不将日期粒度解释成精确秒区间。
- **下钻/导航**：点击用户支出排行携 user_id/start_date/end_date 到 /admin/usage，返回保留正常路由行为；快捷入口为分组定价和获权限的批图工作台，目标页再执行自身鉴权。
- **权威 API**：`GET /admin/dashboard/snapshot-v2`、`/users-trend`、`/users-ranking`；快照、趋势、排名分别持有请求归属。API 模块另有 stats/realtime/groups 等公开读方法，不意味着此页同时展示全部方法的 UI。
- **异常/取消**：排名读取失败可识别，金额/总量不补零；切身份清空三个查询投影并取消旧等待，不让旧排名跳到新人的用量。
- **依据/既有 case**：[页面](../../../frontend/src/app/compositions/dashboard/pages/AdminDashboardPage.vue)、[查询](../../../frontend/src/features/usage/queries/useAdminDashboardData.ts)、[完整公共 API](../../../frontend/src/features/usage/adminDashboard.ts)；`FE-PAGE-ADMIN-DASHBOARD-ACTION-1`、`FE-PAGE-ADMIN-DASHBOARD-ACTION-2`、`FE-BILL-001`。

### FS-DASH-003 订单收入仪表盘

- **入口/前置**：`/admin/orders/dashboard`，页面/支付开关通过后默认查询 30 日，可切 7/30/90 日及手动刷新。
- **展示**：累计/今日收入、今日订单数、平均金额、每日收入曲线、支付方式分布和支付用户排名；amount/avg/top users 以 currency 字典分组，按币种显示原精度，不将 CNY/USD 等混为一个总数。
- **正常数据**：`GET /admin/payment/dashboard?days=…` 的服务端聚合；切范围只更新对应读取，不启动退款/履约。
- **异常/取消**：失败通知且不生成“零收入”；换管理员 scope 清空结果并取消旧查询，老范围响应不得覆盖新范围。
- **依据/既有 case**：[页面](../../../frontend/src/features/payments/pages/AdminPaymentDashboardPage.vue)、[query](../../../frontend/src/features/payments/queries/usePaymentDashboard.ts)、[金额卡](../../../frontend/src/features/payments/components/OrderStatsCards.vue)；`FE-PAGE-ADMIN-ORDERS-DASHBOARD-ACTION-1`、`FE-PAGE-ADMIN-ORDERS-DASHBOARD-ACTION-2`、`FE-BILL-001`。

## 重构前后承接与验收缺口

下表记录有效功能的归属迁移；原始差异复核以 [FRONTEND_REVIEW](../FRONTEND_REVIEW.md) 为总入口，历史路径不作为当前运行入口或兼容 wrapper。当前公开出口继续保留原 DTO/端点和已有历史数据读取，不创建第二套运行实现。

| 重构前路径/职责 | 当前 owner/公开出口 | 本章保持的行为 |
| --- | --- | --- |
| api/payment.ts、api/admin/payment.ts、stores/payment.ts | [payments/index](../../../frontend/src/features/payments/index.ts) 的 paymentAPI/adminPaymentAPI；领域 queries/commands | 用户/管理/公开订单分别鉴权；单一流程 owner，取消/未知结果不自动重发 |
| views/user/PaymentView.vue、PurchaseSubscriptionView.vue，全部 Payment/Stripe/Airwallex 页面 | [payments/pages](../../../frontend/src/features/payments/pages/CheckoutPage.vue) 和 payments/routes | 充值/套餐、恢复/回跳、二维码及 SDK 原 URL；不把整页复制为第二套支付 |
| views/admin/orders/*、components/admin/payment/* | payments 页面、退款/收入组件与 [panels](../../../frontend/src/features/payments/panels.ts) | 订单权限、退款金额/force、套餐字段权限和多币种统计 |
| SettingsView 内支付、Provider、充值优惠、余额提醒 | [payments/settings](../../../frontend/src/features/payments/settings.ts) 与 app 设置装配 | 聚合 revision PUT 和独立 Provider 命令分别执行，排序部分失败不假称回滚 |
| api/usage.ts、admin/usage.ts、usageExport.ts、utils/usageMetrics.ts | [usage/index](../../../frontend/src/features/usage/index.ts)、领域 API/model/queries | 本人/管理查询、原导出格式、raw-duration TPS、未知值与下界 |
| views/KeyUsageView.vue、用户/管理 UsageView | usage/pages 和 [usage/panels](../../../frontend/src/features/usage/panels.ts) | API Key 匿名通道、筛选/详情/清理的请求归属 |
| views/user/DashboardView.vue、views/admin/DashboardView.vue、api/admin/dashboard.ts | app/compositions/dashboard + usage/adminDashboard | 跨域组合与只读数据 owner 分离，完整公共 dashboard API 仍有唯一归属 |
| api/user.ts 历史/用量读取、api/admin/users.ts 的余额方法 | payments/api/balanceHistory、payments/userBalances、usage/api/userRead | 历史余额未删除；个人与管理投影、ID/分页/空值不混用 |

- 两项退款入口差异已按服务端合同修复：用户仅 balance 订单，管理不重退 PARTIALLY_REFUNDED；单测/交互回归与真实退款到账分别留证。
- 现有用例目录缺少清理创建/取消及个人顶栏余额历史的独立操作 ID；本章只关联真实已有入口/数值用例和源码，不把相近标题当作完整覆盖。补充用例与排期交现有验收 owner，不另造状态表。
- 真实第三方付款、微信授权恢复、Stripe/Airwallex 返回与 webhook、退款到账分别需要获准 sandbox 和对应回执：`FE-PAY-002` 及各支付页面的既有 EXTERNAL-PAYMENT 用例。前端结果页、组件测试、冻结哈希及已定义用例数量均不证明这些步骤成功。
- 本文仅完成路径、字段、调用和 case ID 的事实核对；未执行应用测试、构建、Docker、真实交易或全站页面验收。整体进度继续以根 work-items/TEST_REPORT 的实际记录为准。
