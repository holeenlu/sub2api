# 12 · 供应商门户、资源运营、归档与结算

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`feat(vendor): preserve supplier portal operations and settlement`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 供应商使用独立一级、二级角色：超管管理全部供应商，一级管理直属二级身份、合同及自有资源，二级仅管理自有资源。平台/分组范围、资源上限和供给默认值由后端实时鉴权，站点消费者的原分组/订阅资格不因供给身份放宽。
2. 独立供应商子域名/构建复用原登录、MFA、Passkey、密码及会话，保留 Origin 与反代白名单、未配置/关闭门户提示及无公开注册的边界。开户填写邮箱、密码、用户名和角色级别，用户与供应商原子创建；默认1000账号、1000代理、并发15、优先级1，已有值保留。
3. Claude/OpenAI OAuth/Token 账号进入原账号池；名称、备注、原生授权、有效分组和私有代理由供应商按策略管理。身份占用、并发/优先级/统计倍率、暂停、授权刷新、探测、模型同步与原调度检查保持。
4. 账号删除为归档：保留数据库行、账号池列表、凭据、授权/分组/代理关联和历史，用归档标记停止供给、拒绝后续管理写入。不得把归档转换为清空凭据或硬删除，也不得因解除暂停复活归档账号。
5. 授权导入导出要求显式确认、step-up 和自有范围；混合平台逐行映射有效分组/出口，预览不泄露秘密。代理修改不重置账号授权；连接池按出口版本失效，OAuth 引用及备用代理引用受保护。
6. 私有代理支持标准/快捷/文件导入、重复和错误反馈、分页筛选、脱敏详情、连接/质量检测、部分成功和取消；引用保护由服务端执行。
7. 用量报表保留供应商/账号/模型/日期维度、客户与上游成本口径、Token/缓存、固定窗口、水位及统计快照、独立模型分页和全筛选 CSV。归档账号历史仍可查，不以当前列表裁掉历史归属。
8. 平台与一级、一级与二级分别按独立合同结算，同一供给用量在每条付款边最多结算一次。冻结预览及收款档案，保留确认、付款登记、冲正、调整及用量占用去重；Decimal 精度与合同版本冲突规则不变。结算单价按CNY/统计USD表达，版本服务器递增；无可结算用量时不保存空结算。
9. 运营界面复用原组件、图标与风格：账号右侧详情抽屉、安全出口复制、焦点恢复和归档只读；代理单行直接测试、有限并发批量、全筛选/选中范围、停止与迟到响应隔离。深色、窄屏、四语及头像个人中心保留；供应商侧栏移除操作审计、账号安全，个人中心复用原认证设置，后台审计继续保留。
10. 账号、代理、用量、结算审计按明确供给归属和字段权限查询；历史创建来源与当前归属不混淆，未知事实不伪造。集成测试和原型数据均不代表生产账号或资金操作。
11. 超管账号池和代理页默认展示全部供应商的完整工作台，一级二级资源页默认全部直属二级，不要求先选供应商。表格固定显示归属名称和ID，详情与用量按实际归属查询；一级查看下级始终只读，后端拒绝跨树、凭证和管理写操作。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. 沿用 users、accounts、proxies、groups 与原 usage_logs，共用PostgreSQL和Redis及原调度器。迁移273/274保留；276补角色层级及存量身份转换，277补双边结算和用量归属快照，与Ent生成物及服务事务共同交付。
2. vendor 服务、repository 和 authz 在原账号/代理操作入口补范围检查、归档拒绝及出口版本机制；不另建账号系统、计费网关或消费资格。
3. 独立门户复用原 Vue 路由、API 客户端和安全组件；feature/vendor 局部工作台处理页面态，后端返回正式数据。交互原型只是设计与验收辅助。
4. 原历史重建中的两处归档SQL mock修正保留。后续V3复用同一工作台、代理表单与服务范围规则，实现两级权限、树额度、双边结算及直属资源聚合，清理旧精简列表。
5. 最新方案与实际验证以 [统一平台方案](../../vendor-console/UNIFIED-PLATFORM-PLAN.md)、[实施验收](../../vendor-console/UNIFIED-PLATFORM-ACCEPTANCE.md) 及 [API路由](../../vendor-console/API-ROUTES.md) 为准。早期原型和实施记录作为历史保留。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。后续V3增量及新增入口数量在实施验收中分别记录。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

历史重建与V3分别记录验证范围。V3已执行供应商后端五包真实本地PostgreSQL的-race测试、前端回归、类型检查、两种生产构建及真实本地Go/Redis浏览器联调。真实 DNS/TLS、提供方 OAuth、代理外网、生产结算和迁移属于独立验收。

历史重建结果见 [VALIDATION.md](VALIDATION.md)，后续V3结果见上述实施验收。来源文档中的早期通过记录不能代替后续结果。

## 相对官方 v0.2.17 的累计差异

以下逐文件清单与计数为历史重建时快照，不含随后合入的V3增量；增量计数见实施验收。

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 19 | 9766 | 0 | 0 |
| generated | 71 | 29499 | 49 | 0 |
| source | 115 | 23198 | 123 | 0 |
| test | 60 | 10565 | 48 | 0 |
| tooling | 1 | 55 | 0 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 52 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | generated | 42 | 3 | 12 | [backend/ent/account.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/account.go) |
| M | generated | 26 | 0 | 12 | [backend/ent/account/account.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/account/account.go) |
| M | generated | 180 | 0 | 12 | [backend/ent/account/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/account/where.go) |
| M | generated | 241 | 0 | 12 | [backend/ent/account_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/account_create.go) |
| M | generated | 158 | 0 | 12 | [backend/ent/account_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/account_update.go) |
| A | generated | 164 | 0 | 12 | [backend/ent/accountsupplyidentityclaim.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim.go) |
| A | generated | 102 | 0 | 12 | [backend/ent/accountsupplyidentityclaim/accountsupplyidentityclaim.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim/accountsupplyidentityclaim.go) |
| A | generated | 400 | 0 | 12 | [backend/ent/accountsupplyidentityclaim/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim/where.go) |
| A | generated | 797 | 0 | 12 | [backend/ent/accountsupplyidentityclaim_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/accountsupplyidentityclaim_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/accountsupplyidentityclaim_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim_query.go) |
| A | generated | 442 | 0 | 12 | [backend/ent/accountsupplyidentityclaim_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/accountsupplyidentityclaim_update.go) |
| M | generated | 1037 | 42 | 12 | [backend/ent/client.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/client.go) |
| M | generated | 14 | 0 | 12 | [backend/ent/ent.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/ent.go) |
| M | generated | 84 | 0 | 12 | [backend/ent/hook/hook.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/hook/hook.go) |
| M | generated | 210 | 0 | 12 | [backend/ent/intercept/intercept.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/intercept/intercept.go) |
| M | generated | 21 | 0 | 12 | [backend/ent/predicate/predicate.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/predicate/predicate.go) |
| M | generated | 74 | 2 | 12 | [backend/ent/proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/proxy.go) |
| M | generated | 60 | 0 | 12 | [backend/ent/proxy/proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/proxy/proxy.go) |
| M | generated | 365 | 0 | 12 | [backend/ent/proxy/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/proxy/where.go) |
| M | generated | 486 | 0 | 12 | [backend/ent/proxy_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/proxy_create.go) |
| M | generated | 320 | 0 | 12 | [backend/ent/proxy_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/proxy_update.go) |
| M | source | 3 | 0 | 12 | [backend/ent/schema/account.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/account.go) |
| A | source | 23 | 0 | 12 | [backend/ent/schema/account_supply_identity_claim.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/account_supply_identity_claim.go) |
| M | source | 7 | 0 | 12 | [backend/ent/schema/proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/proxy.go) |
| M | source | 8 | 0 | 12 | [backend/ent/schema/usage_log.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/usage_log.go) |
| A | source | 27 | 0 | 12 | [backend/ent/schema/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor.go) |
| A | source | 21 | 0 | 12 | [backend/ent/schema/vendor_group.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor_group.go) |
| A | source | 56 | 0 | 12 | [backend/ent/schema/vendor_settlement.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor_settlement.go) |
| A | source | 38 | 0 | 12 | [backend/ent/schema/vendor_settlement_allocation.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor_settlement_allocation.go) |
| A | source | 37 | 0 | 12 | [backend/ent/schema/vendor_settlement_line.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor_settlement_line.go) |
| A | source | 42 | 0 | 12 | [backend/ent/schema/vendor_settlement_payment.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/schema/vendor_settlement_payment.go) |
| M | generated | 21 | 0 | 12 | [backend/ent/tx.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/tx.go) |
| M | generated | 58 | 2 | 12 | [backend/ent/usagelog.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/usagelog.go) |
| M | generated | 32 | 0 | 12 | [backend/ent/usagelog/usagelog.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/usagelog/usagelog.go) |
| M | generated | 295 | 0 | 12 | [backend/ent/usagelog/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/usagelog/where.go) |
| M | generated | 96 | 0 | 12 | [backend/ent/usagelog_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/usagelog_create.go) |
| M | generated | 24 | 0 | 12 | [backend/ent/usagelog_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/usagelog_update.go) |
| A | generated | 315 | 0 | 12 | [backend/ent/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor.go) |
| A | generated | 220 | 0 | 12 | [backend/ent/vendor/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor/vendor.go) |
| A | generated | 955 | 0 | 12 | [backend/ent/vendor/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor/where.go) |
| A | generated | 1732 | 0 | 12 | [backend/ent/vendor_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendor_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendor_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor_query.go) |
| A | generated | 967 | 0 | 12 | [backend/ent/vendor_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendor_update.go) |
| A | generated | 125 | 0 | 12 | [backend/ent/vendorgroup.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup.go) |
| A | generated | 68 | 0 | 12 | [backend/ent/vendorgroup/vendorgroup.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup/vendorgroup.go) |
| A | generated | 173 | 0 | 12 | [backend/ent/vendorgroup/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup/where.go) |
| A | generated | 618 | 0 | 12 | [backend/ent/vendorgroup_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendorgroup_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendorgroup_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup_query.go) |
| A | generated | 317 | 0 | 12 | [backend/ent/vendorgroup_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorgroup_update.go) |
| A | generated | 387 | 0 | 12 | [backend/ent/vendorsettlement.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement.go) |
| A | generated | 243 | 0 | 12 | [backend/ent/vendorsettlement/vendorsettlement.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement/vendorsettlement.go) |
| A | generated | 1440 | 0 | 12 | [backend/ent/vendorsettlement/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement/where.go) |
| A | generated | 2047 | 0 | 12 | [backend/ent/vendorsettlement_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendorsettlement_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendorsettlement_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement_query.go) |
| A | generated | 1242 | 0 | 12 | [backend/ent/vendorsettlement_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlement_update.go) |
| A | generated | 193 | 0 | 12 | [backend/ent/vendorsettlementallocation.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation.go) |
| A | generated | 109 | 0 | 12 | [backend/ent/vendorsettlementallocation/vendorsettlementallocation.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation/vendorsettlementallocation.go) |
| A | generated | 390 | 0 | 12 | [backend/ent/vendorsettlementallocation/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation/where.go) |
| A | generated | 942 | 0 | 12 | [backend/ent/vendorsettlementallocation_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendorsettlementallocation_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendorsettlementallocation_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation_query.go) |
| A | generated | 484 | 0 | 12 | [backend/ent/vendorsettlementallocation_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementallocation_update.go) |
| A | generated | 205 | 0 | 12 | [backend/ent/vendorsettlementline.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline.go) |
| A | generated | 119 | 0 | 12 | [backend/ent/vendorsettlementline/vendorsettlementline.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline/vendorsettlementline.go) |
| A | generated | 678 | 0 | 12 | [backend/ent/vendorsettlementline/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline/where.go) |
| A | generated | 1066 | 0 | 12 | [backend/ent/vendorsettlementline_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendorsettlementline_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendorsettlementline_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline_query.go) |
| A | generated | 613 | 0 | 12 | [backend/ent/vendorsettlementline_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementline_update.go) |
| A | generated | 230 | 0 | 12 | [backend/ent/vendorsettlementpayment.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment.go) |
| A | generated | 142 | 0 | 12 | [backend/ent/vendorsettlementpayment/vendorsettlementpayment.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment/vendorsettlementpayment.go) |
| A | generated | 795 | 0 | 12 | [backend/ent/vendorsettlementpayment/where.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment/where.go) |
| A | generated | 1171 | 0 | 12 | [backend/ent/vendorsettlementpayment_create.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment_create.go) |
| A | generated | 88 | 0 | 12 | [backend/ent/vendorsettlementpayment_delete.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment_delete.go) |
| A | generated | 564 | 0 | 12 | [backend/ent/vendorsettlementpayment_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment_query.go) |
| A | generated | 662 | 0 | 12 | [backend/ent/vendorsettlementpayment_update.go](https://github.com/holeenlu/sub2api/blob/main/backend/ent/vendorsettlementpayment_update.go) |
| A | source | 21 | 0 | 12 | [backend/internal/authz/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/vendor.go) |
| A | test | 30 | 0 | 12 | [backend/internal/authz/vendor_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/authz/vendor_test.go) |
| M | test | 16 | 0 | 12 | [backend/internal/handler/admin/account_data_handler_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/account_data_handler_test.go) |
| M | source | 21 | 7 | 12 | [backend/internal/handler/admin/proxy_handler.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/admin/proxy_handler.go) |
| M | source | 13 | 7 | 12 | [backend/internal/handler/openai_live.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/openai_live.go) |
| A | source | 232 | 0 | 12 | [backend/internal/handler/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor.go) |
| A | source | 279 | 0 | 12 | [backend/internal/handler/vendor_account.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_account.go) |
| A | test | 50 | 0 | 12 | [backend/internal/handler/vendor_account_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_account_test.go) |
| A | source | 271 | 0 | 12 | [backend/internal/handler/vendor_proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_proxy.go) |
| A | test | 258 | 0 | 12 | [backend/internal/handler/vendor_proxy_router_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_proxy_router_integration_test.go) |
| A | test | 62 | 0 | 12 | [backend/internal/handler/vendor_proxy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_proxy_test.go) |
| A | source | 189 | 0 | 12 | [backend/internal/handler/vendor_settlement.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_settlement.go) |
| A | source | 144 | 0 | 12 | [backend/internal/handler/vendor_usage.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_usage.go) |
| A | test | 236 | 0 | 12 | [backend/internal/handler/vendor_usage_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_usage_integration_test.go) |
| A | test | 92 | 0 | 12 | [backend/internal/handler/vendor_usage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/handler/vendor_usage_test.go) |
| M | source | 6 | 2 | 12 | [backend/internal/pkg/httpclient/pool.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/httpclient/pool.go) |
| A | source | 90 | 0 | 12 | [backend/internal/pkg/oauth/session_atomic.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/oauth/session_atomic.go) |
| A | test | 93 | 0 | 12 | [backend/internal/pkg/oauth/session_atomic_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/oauth/session_atomic_test.go) |
| A | source | 15 | 0 | 12 | [backend/internal/pkg/openai/session_atomic.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/openai/session_atomic.go) |
| A | test | 29 | 0 | 12 | [backend/internal/pkg/openai/session_atomic_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/openai/session_atomic_test.go) |
| M | source | 2 | 2 | 12 | [backend/internal/pkg/proxyurl/parse.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/proxyurl/parse.go) |
| M | source | 4 | 1 | 12 | [backend/internal/pkg/proxyutil/dialer.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/proxyutil/dialer.go) |
| A | source | 158 | 0 | 12 | [backend/internal/pkg/proxyutil/vendor_transport.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/proxyutil/vendor_transport.go) |
| A | test | 74 | 0 | 12 | [backend/internal/pkg/proxyutil/vendor_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/proxyutil/vendor_transport_test.go) |
| M | test | 113 | 35 | 12 | [backend/internal/repository/account_repo_codex_display_snapshot_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_codex_display_snapshot_test.go) |
| A | test | 99 | 0 | 07,12 | [backend/internal/repository/account_repo_excel_bps_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_excel_bps_test.go) |
| M | test | 3 | 0 | 12 | [backend/internal/repository/account_repo_model_availability_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_model_availability_test.go) |
| M | test | 4 | 0 | 12 | [backend/internal/repository/account_repo_ollama_cloud_usage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_ollama_cloud_usage_test.go) |
| M | test | 11 | 2 | 12 | [backend/internal/repository/account_repo_schedulable_projection_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_schedulable_projection_test.go) |
| A | source | 235 | 0 | 12 | [backend/internal/repository/account_repo_supply_identity.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_supply_identity.go) |
| A | test | 159 | 0 | 12 | [backend/internal/repository/account_repo_supply_identity_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_supply_identity_test.go) |
| M | test | 5 | 1 | 12 | [backend/internal/repository/account_repo_temp_unsched_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_temp_unsched_test.go) |
| M | test | 12 | 10 | 07,12 | [backend/internal/repository/account_repo_upstream_billing_probe_update_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_upstream_billing_probe_update_test.go) |
| A | source | 436 | 0 | 12 | [backend/internal/repository/account_repo_vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_vendor.go) |
| A | test | 396 | 0 | 12 | [backend/internal/repository/account_repo_vendor_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_vendor_integration_test.go) |
| A | test | 204 | 0 | 12 | [backend/internal/repository/account_repo_vendor_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/account_repo_vendor_test.go) |
| M | source | 75 | 21 | 12 | [backend/internal/repository/group_repo.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/group_repo.go) |
| M | source | 9 | 1 | 12 | [backend/internal/repository/proxy_latency_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_latency_cache.go) |
| A | test | 33 | 0 | 12 | [backend/internal/repository/proxy_vendor_cache_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_vendor_cache_test.go) |
| A | test | 340 | 0 | 12 | [backend/internal/repository/proxy_vendor_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_vendor_integration_test.go) |
| A | test | 28 | 0 | 12 | [backend/internal/repository/proxy_vendor_scope_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_vendor_scope_test.go) |
| A | test | 68 | 0 | 12 | [backend/internal/repository/proxy_vendor_transport_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/proxy_vendor_transport_test.go) |
| M | source | 11 | 0 | 12 | [backend/internal/repository/req_client_pool.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/req_client_pool.go) |
| A | source | 60 | 0 | 12 | [backend/internal/repository/usage_log_live_snapshot.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_live_snapshot.go) |
| A | test | 35 | 0 | 12 | [backend/internal/repository/usage_log_live_snapshot_unit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_live_snapshot_unit_test.go) |
| M | source | 44 | 4 | 12 | [backend/internal/repository/usage_log_repo_insert.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_repo_insert.go) |
| A | test | 72 | 0 | 12 | [backend/internal/repository/usage_log_vendor_snapshot_unit_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/usage_log_vendor_snapshot_unit_test.go) |
| A | source | 24 | 0 | 12 | [backend/internal/repository/user_repo_vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/user_repo_vendor.go) |
| A | source | 96 | 0 | 12 | [backend/internal/server/routes/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/server/routes/vendor.go) |
| M | source | 47 | 1 | 12 | [backend/internal/service/admin_proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/admin_proxy.go) |
| M | source | 1 | 1 | 12 | [backend/internal/service/gateway_systemone.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_systemone.go) |
| M | source | 1 | 0 | 12 | [backend/internal/service/gateway_usage_billing.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/gateway_usage_billing.go) |
| M | source | 4 | 0 | 12 | [backend/internal/service/oauth_refresh_api.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/oauth_refresh_api.go) |
| M | test | 9 | 0 | 12 | [backend/internal/service/oauth_refresh_api_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/oauth_refresh_api_test.go) |
| M | source | 2 | 2 | 12 | [backend/internal/service/openai_alpha_search.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_alpha_search.go) |
| M | source | 1 | 1 | 12 | [backend/internal/service/openai_embeddings.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_embeddings.go) |
| M | source | 1 | 1 | 12 | [backend/internal/service/openai_gateway_cc_pipeline.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_cc_pipeline.go) |
| M | source | 2 | 2 | 12 | [backend/internal/service/openai_gateway_count_tokens.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_count_tokens.go) |
| M | source | 4 | 2 | 12 | [backend/internal/service/openai_gateway_grok.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_grok.go) |
| M | source | 2 | 1 | 12 | [backend/internal/service/openai_gateway_grok_chat_bridge.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_gateway_grok_chat_bridge.go) |
| M | source | 1 | 1 | 12 | [backend/internal/service/openai_images.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_images.go) |
| M | source | 47 | 19 | 12 | [backend/internal/service/openai_live.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_live.go) |
| M | source | 20 | 7 | 12 | [backend/internal/service/openai_live_types.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_live_types.go) |
| M | source | 3 | 1 | 12 | [backend/internal/service/openai_plugin_account_directory.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/openai_plugin_account_directory.go) |
| M | source | 32 | 15 | 12 | [backend/internal/service/proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/proxy.go) |
| M | source | 3 | 3 | 12 | [backend/internal/service/proxy_fallback.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/proxy_fallback.go) |
| M | source | 17 | 15 | 12 | [backend/internal/service/proxy_latency_cache.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/proxy_latency_cache.go) |
| M | source | 102 | 3 | 12 | [backend/internal/service/scheduler_snapshot_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduler_snapshot_service.go) |
| A | test | 188 | 0 | 12 | [backend/internal/service/scheduler_snapshot_vendor_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/scheduler_snapshot_vendor_test.go) |
| M | source | 34 | 0 | 12 | [backend/internal/service/usage_log.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/usage_log.go) |
| A | source | 472 | 0 | 12 | [backend/internal/service/vendor.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor.go) |
| A | source | 770 | 0 | 12 | [backend/internal/service/vendor_account.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account.go) |
| A | source | 475 | 0 | 12 | [backend/internal/service/vendor_account_credentials.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_credentials.go) |
| A | test | 208 | 0 | 12 | [backend/internal/service/vendor_account_flows_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_flows_test.go) |
| A | source | 525 | 0 | 12 | [backend/internal/service/vendor_account_identity.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_identity.go) |
| A | test | 384 | 0 | 12 | [backend/internal/service/vendor_account_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_integration_test.go) |
| A | source | 115 | 0 | 12 | [backend/internal/service/vendor_account_network.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_network.go) |
| A | test | 172 | 0 | 12 | [backend/internal/service/vendor_account_oauth_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_oauth_test.go) |
| A | source | 360 | 0 | 12 | [backend/internal/service/vendor_account_operations.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_operations.go) |
| A | source | 149 | 0 | 12 | [backend/internal/service/vendor_account_overview.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_overview.go) |
| A | source | 240 | 0 | 12 | [backend/internal/service/vendor_account_stats.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_stats.go) |
| A | test | 388 | 0 | 12 | [backend/internal/service/vendor_account_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_test.go) |
| A | source | 626 | 0 | 12 | [backend/internal/service/vendor_account_transfer.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_account_transfer.go) |
| A | source | 178 | 0 | 12 | [backend/internal/service/vendor_admin.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_admin.go) |
| A | test | 197 | 0 | 12 | [backend/internal/service/vendor_admin_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_admin_integration_test.go) |
| A | source | 368 | 0 | 12 | [backend/internal/service/vendor_oauth.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_oauth.go) |
| A | test | 180 | 0 | 12 | [backend/internal/service/vendor_policy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_policy_test.go) |
| A | source | 975 | 0 | 12 | [backend/internal/service/vendor_proxy.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy.go) |
| A | source | 151 | 0 | 12 | [backend/internal/service/vendor_proxy_probe.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy_probe.go) |
| A | test | 75 | 0 | 12 | [backend/internal/service/vendor_proxy_references_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy_references_test.go) |
| A | test | 164 | 0 | 12 | [backend/internal/service/vendor_proxy_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy_test.go) |
| A | source | 357 | 0 | 12 | [backend/internal/service/vendor_proxy_transfer.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy_transfer.go) |
| A | test | 23 | 0 | 12 | [backend/internal/service/vendor_proxy_ws_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_proxy_ws_test.go) |
| A | source | 458 | 0 | 12 | [backend/internal/service/vendor_settlement.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement.go) |
| A | source | 411 | 0 | 12 | [backend/internal/service/vendor_settlement_actions.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_actions.go) |
| A | source | 86 | 0 | 12 | [backend/internal/service/vendor_settlement_contract.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_contract.go) |
| A | test | 512 | 0 | 12 | [backend/internal/service/vendor_settlement_integration_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_integration_test.go) |
| A | source | 341 | 0 | 12 | [backend/internal/service/vendor_settlement_read.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_read.go) |
| A | source | 372 | 0 | 12 | [backend/internal/service/vendor_settlement_store.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_store.go) |
| A | test | 189 | 0 | 12 | [backend/internal/service/vendor_settlement_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_settlement_test.go) |
| A | source | 410 | 0 | 12 | [backend/internal/service/vendor_usage.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_usage.go) |
| A | source | 179 | 0 | 12 | [backend/internal/service/vendor_usage_export.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_usage_export.go) |
| A | source | 320 | 0 | 12 | [backend/internal/service/vendor_usage_query.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_usage_query.go) |
| A | test | 474 | 0 | 12 | [backend/internal/service/vendor_usage_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/vendor_usage_test.go) |
| A | source | 66 | 0 | 12 | [backend/migrations/273_vendor_supply.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/273_vendor_supply.sql) |
| A | source | 95 | 0 | 12 | [backend/migrations/274_vendor_settlements.sql](https://github.com/holeenlu/sub2api/blob/main/backend/migrations/274_vendor_settlements.sql) |
| A | tooling | 55 | 0 | 12 | [deploy/examples/vendor-portal.conf](https://github.com/holeenlu/sub2api/blob/main/deploy/examples/vendor-portal.conf) |
| A | documentation | 102 | 0 | 12 | [docs/vendor-console/API-ROUTES.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/API-ROUTES.md) |
| A | documentation | 439 | 0 | 12 | [docs/vendor-console/DESIGN.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/DESIGN.md) |
| A | documentation | 158 | 0 | 12 | [docs/vendor-console/HIERARCHY-DESIGN.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/HIERARCHY-DESIGN.md) |
| A | documentation | 207 | 0 | 12 | [docs/vendor-console/IMPLEMENTATION-STATUS.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/IMPLEMENTATION-STATUS.md) |
| A | documentation | 263 | 0 | 12 | [docs/vendor-console/IMPLEMENTATION.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/IMPLEMENTATION.md) |
| A | documentation | 88 | 0 | 12 | [docs/vendor-console/README.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/README.md) |
| A | documentation | 54 | 0 | 12 | [docs/vendor-console/REQUIREMENTS-REVIEW.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/REQUIREMENTS-REVIEW.md) |
| A | documentation | 119 | 0 | 12 | [docs/vendor-console/RESEARCH.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/RESEARCH.md) |
| A | documentation | 184 | 0 | 12 | [docs/vendor-console/UI-ACCEPTANCE.md](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/UI-ACCEPTANCE.md) |
| A | documentation | 32 | 0 | 12 | [docs/vendor-console/generate-prototype-locales.cjs](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/generate-prototype-locales.cjs) |
| A | documentation | 185 | 0 | 12 | [docs/vendor-console/prototype-admin.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype-admin.js) |
| A | documentation | 131 | 0 | 12 | [docs/vendor-console/prototype-hierarchy-model.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype-hierarchy-model.js) |
| A | documentation | 723 | 0 | 12 | [docs/vendor-console/prototype-hierarchy.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype-hierarchy.js) |
| A | documentation | 146 | 0 | 12 | [docs/vendor-console/prototype-proxies.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype-proxies.js) |
| A | documentation | 4671 | 0 | 12 | [docs/vendor-console/prototype-translations.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype-translations.js) |
| A | documentation | 1615 | 0 | 12 | [docs/vendor-console/prototype.css](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype.css) |
| A | documentation | 10 | 0 | 12 | [docs/vendor-console/prototype.html](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype.html) |
| A | documentation | 516 | 0 | 12 | [docs/vendor-console/prototype.js](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/prototype.js) |
| A | test | 149 | 0 | 12 | [docs/vendor-console/tests/hierarchy-model.test.cjs](https://github.com/holeenlu/sub2api/blob/main/docs/vendor-console/tests/hierarchy-model.test.cjs) |
| A | documentation | 123 | 0 | 12 | [frontend/VENDOR_API_REQUIREMENTS.md](https://github.com/holeenlu/sub2api/blob/main/frontend/VENDOR_API_REQUIREMENTS.md) |
| M | source | 1 | 1 | 12 | [frontend/src/api/url.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/api/url.ts) |
| M | source | 3 | 2 | 12 | [frontend/src/components/common/GroupSelector.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/GroupSelector.vue) |
| M | source | 1 | 0 | 12 | [frontend/src/components/icons/Icon.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/icons/Icon.vue) |
| A | test | 37 | 0 | 12 | [frontend/src/features/vendor/__tests__/AccountColumnChooser.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/AccountColumnChooser.spec.ts) |
| A | test | 339 | 0 | 12 | [frontend/src/features/vendor/__tests__/AccountForm.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/AccountForm.spec.ts) |
| A | test | 934 | 0 | 12 | [frontend/src/features/vendor/__tests__/AccountWorkbench.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/AccountWorkbench.spec.ts) |
| A | test | 31 | 0 | 12 | [frontend/src/features/vendor/__tests__/ActionMenu.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/ActionMenu.spec.ts) |
| A | test | 79 | 0 | 12 | [frontend/src/features/vendor/__tests__/DisplayModeSwitcher.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/DisplayModeSwitcher.spec.ts) |
| A | test | 104 | 0 | 12 | [frontend/src/features/vendor/__tests__/IdentityReconciliation.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/IdentityReconciliation.spec.ts) |
| A | test | 155 | 0 | 12 | [frontend/src/features/vendor/__tests__/ProxyForm.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/ProxyForm.spec.ts) |
| A | test | 504 | 0 | 12 | [frontend/src/features/vendor/__tests__/ProxyWorkbench.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/ProxyWorkbench.spec.ts) |
| A | test | 282 | 0 | 12 | [frontend/src/features/vendor/__tests__/UsageAccountPicker.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/UsageAccountPicker.spec.ts) |
| A | test | 72 | 0 | 12 | [frontend/src/features/vendor/__tests__/VendorAccountSettings.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/VendorAccountSettings.spec.ts) |
| A | test | 58 | 0 | 12 | [frontend/src/features/vendor/__tests__/VendorAudit.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/VendorAudit.spec.ts) |
| A | test | 50 | 0 | 12 | [frontend/src/features/vendor/__tests__/VendorEditor.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/VendorEditor.spec.ts) |
| A | test | 178 | 0 | 12 | [frontend/src/features/vendor/__tests__/VendorsView.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/VendorsView.spec.ts) |
| A | test | 445 | 0 | 12 | [frontend/src/features/vendor/__tests__/importPrivacy.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/importPrivacy.spec.ts) |
| A | test | 145 | 0 | 12 | [frontend/src/features/vendor/__tests__/portalGuard.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/portalGuard.spec.ts) |
| A | test | 35 | 0 | 12 | [frontend/src/features/vendor/__tests__/renderedTemplates.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/renderedTemplates.spec.ts) |
| A | test | 399 | 0 | 12 | [frontend/src/features/vendor/__tests__/scope.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/scope.spec.ts) |
| A | test | 351 | 0 | 12 | [frontend/src/features/vendor/__tests__/settlements.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/settlements.spec.ts) |
| A | test | 533 | 0 | 12 | [frontend/src/features/vendor/__tests__/usageScope.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/__tests__/usageScope.spec.ts) |
| A | source | 284 | 0 | 12 | [frontend/src/features/vendor/api.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/api.ts) |
| A | source | 124 | 0 | 12 | [frontend/src/features/vendor/components/AccountColumnChooser.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/AccountColumnChooser.vue) |
| A | source | 127 | 0 | 12 | [frontend/src/features/vendor/components/AccountCreatedRangePicker.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/AccountCreatedRangePicker.vue) |
| A | source | 494 | 0 | 12 | [frontend/src/features/vendor/components/AccountForm.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/AccountForm.vue) |
| A | source | 592 | 0 | 12 | [frontend/src/features/vendor/components/AccountImport.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/AccountImport.vue) |
| A | source | 1530 | 0 | 12 | [frontend/src/features/vendor/components/AccountWorkbench.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/AccountWorkbench.vue) |
| A | source | 120 | 0 | 12 | [frontend/src/features/vendor/components/ActionMenu.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/ActionMenu.vue) |
| A | source | 64 | 0 | 12 | [frontend/src/features/vendor/components/DisplayModeSwitcher.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/DisplayModeSwitcher.vue) |
| A | source | 122 | 0 | 12 | [frontend/src/features/vendor/components/GroupSupply.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/GroupSupply.vue) |
| A | source | 155 | 0 | 12 | [frontend/src/features/vendor/components/IdentityReconciliation.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/IdentityReconciliation.vue) |
| A | source | 224 | 0 | 12 | [frontend/src/features/vendor/components/ProxyForm.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/ProxyForm.vue) |
| A | source | 197 | 0 | 12 | [frontend/src/features/vendor/components/ProxyImport.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/ProxyImport.vue) |
| A | source | 1134 | 0 | 12 | [frontend/src/features/vendor/components/ProxyWorkbench.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/ProxyWorkbench.vue) |
| A | source | 25 | 0 | 12 | [frontend/src/features/vendor/components/RequestState.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/RequestState.vue) |
| A | source | 991 | 0 | 12 | [frontend/src/features/vendor/components/SettlementWorkbench.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/SettlementWorkbench.vue) |
| A | source | 204 | 0 | 12 | [frontend/src/features/vendor/components/SupplierInventory.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/SupplierInventory.vue) |
| A | source | 215 | 0 | 12 | [frontend/src/features/vendor/components/UsageAccountPicker.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/UsageAccountPicker.vue) |
| A | source | 129 | 0 | 12 | [frontend/src/features/vendor/components/UsageMetrics.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/UsageMetrics.vue) |
| A | source | 306 | 0 | 12 | [frontend/src/features/vendor/components/UsageTable.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/UsageTable.vue) |
| A | source | 46 | 0 | 12 | [frontend/src/features/vendor/components/UsageTrend.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/UsageTrend.vue) |
| A | source | 696 | 0 | 12 | [frontend/src/features/vendor/components/UsageWorkbench.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/UsageWorkbench.vue) |
| A | source | 129 | 0 | 12 | [frontend/src/features/vendor/components/VendorAccountSettings.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/VendorAccountSettings.vue) |
| A | source | 118 | 0 | 12 | [frontend/src/features/vendor/components/VendorAudit.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/VendorAudit.vue) |
| A | source | 331 | 0 | 12 | [frontend/src/features/vendor/components/VendorEditor.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/VendorEditor.vue) |
| A | source | 295 | 0 | 12 | [frontend/src/features/vendor/components/VendorLayout.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/VendorLayout.vue) |
| A | source | 324 | 0 | 12 | [frontend/src/features/vendor/components/VendorManagement.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/components/VendorManagement.vue) |
| A | source | 242 | 0 | 12 | [frontend/src/features/vendor/helpers.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/helpers.ts) |
| A | source | 24 | 0 | 12 | [frontend/src/features/vendor/store.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/store.ts) |
| A | source | 379 | 0 | 12 | [frontend/src/features/vendor/types.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/types.ts) |
| A | source | 48 | 0 | 12 | [frontend/src/features/vendor/useVendorPage.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/features/vendor/useVendorPage.ts) |
| A | source | 560 | 0 | 12 | [frontend/src/i18n/locales/en/vendor.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/en/vendor.ts) |
| A | source | 553 | 0 | 12 | [frontend/src/i18n/locales/ja/vendor.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/ja/vendor.ts) |
| A | generated | 538 | 0 | 12 | [frontend/src/i18n/locales/zh-TW/vendor.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh-TW/vendor.ts) |
| A | source | 536 | 0 | 12 | [frontend/src/i18n/locales/zh/vendor.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/i18n/locales/zh/vendor.ts) |
| A | source | 299 | 0 | 12 | [frontend/src/views/admin/VendorsView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/admin/VendorsView.vue) |
| A | source | 32 | 0 | 12 | [frontend/src/views/vendor/VendorPortalView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/vendor/VendorPortalView.vue) |
| A | source | 24 | 0 | 12 | [frontend/src/views/vendor/VendorUnavailableView.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/views/vendor/VendorUnavailableView.vue) |
