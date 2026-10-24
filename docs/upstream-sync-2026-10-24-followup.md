# 五源后续同步：实施前对照（2026-10-24）

固定来源及本地三品牌基点见 state.json；官方基线5c185c5→37067e8e，选择性来源按上轮主题证据比较，不声称整仓历史已审。

## 官方受限管理员密钥
官方37067e8e新增随机、仅哈希存储、按操作/分组/平台/账号类型匹配的机器密钥，JWT管理生命周期，明确路由目录及脱敏响应；本地只有全权机器Key和超管/普通管理员策略、创建人归属。上游单一admin角色不能直接映射为本地普通管理员；上游c.Next也不能绕过本地管理授权。
推荐融合：完整整合官方资源范围功能，生命周期及创建人仅super_admin JWT；普通管理员归属/策略保持单一现有authz owner。受限Key通过原生adminaccess资源规则，并进入本地管理路由/审计、主体及流式重新验证，不获超管会话专属能力，不提供普通管理员创建全站机器Key。保留原全权Key兼容以及现有会话/最后超管/转账边界；优惠码已明确退役，不恢复被修改的promo处理器/入口/权限文案。
配置/升级：系统设置→安全→管理员访问密钥；新表仅按官方迁移文件升级时创建，本轮不执行生产迁移；默认不创建或启用任何密钥。需补四语言与前端权限呈现，验证角色、精确路由、scope隔离、事务重验、脱敏、生命周期和旧Key回归。

## 选择性候选初筛
- New API 1dafb19a：Chat→Gemini新增顶层imageConfig→generationConfig.imageConfig；比较本地唯一转换入口，若存在同形缺口直接补齐，保持已有extra_body优先级。0034b249将Ark/Vidu等异步任务默认改72h，来源任务产品/退款合同不同，不照搬默认。axios本地声明1.20.0，核实lock；其余React/Bun依赖不适用本地Vue/pnpm。
- ranxi：自动重登录仍属明确退役；TypeSafe分类与邮件豁免新增送审和授权语义，作为独立候选保留，不默认改变当前用户内容/蒸馏范围。计费journal/原子额度/缓存ACK/精度按本地现有usage_billing_events和原子账本实际比较，不重复引入来源整套新表及平台额度双投影。已存在持久投递和原子平台配额不等于其所有边界等效，独立缺口另记或修复。
- CPR：读取5cfc6dee异常WS清理、ef5b4d7d reasoning内容注册、6648198d拷贝及完成观测，比较本地socket/重放/终态；插件适配器、组合中间件和独立预算缓冲属于来源框架，不新增并行网关。表格排序持久化独立UI产品暂不纳入；Rust依赖无本地调用。
- vm2api：本轮3c45c685→8f3b6bab集中303身份与完整提示词、入口billing标签及发版。当前已验证Linux指纹和保存blocks不因版本号改变；无整套平台新采样及工具/缓存验收，不替换。授权沿用用户已确认适配、分发和商用范围，LICENSE/NOTICE保留。

待决定项仅实际改变明确需求时提出；机械合并与既有退役规则直接执行。后续填实际证据、采用/覆盖/排除/未审及验证。

## 已核对的选择性结果与新增资金决定
- New API固定e835b1bf共8项提交：1dafb19a在本地ForwardAsChatCompletions经Responses/Anthropic中间层时丢imageConfig，已通过真实服务→替身请求的4K断言复现；沿既有Gemini转换补字段。额外body内非空image_config优先、空值不覆盖顶层。axios锁定与实装均1.20.0；其他React/Bun依赖与当前栈不适用。异步72h是Ark/Vidu任务合同，不更改本地自己的批次/实时生成时限。
- CPR e033e219→a36137ba：31974bc2是来源TraceState编码上限，本地Ops有自己的有界诊断/脱敏与持久化，不引入第二套trace schema；ef5b4d7d是Rust canonical decoder的重复part注册，本地原生wire保留、apicompat分别按output_index/summary_index累计与终态差量恢复（responses_to_chatcompletions.go:reasoningByPart），没有该重复注册器。5cfc6dee异常结束停止pump，本地openAIWSConn.abort→CloseNow及读失败清理已具有对应强制释放机制；6648198d Rust owned UTF8/mem::take无Go所有权等价移植，当前已直接转发准备字节，新增阶段观测暂不加；31de0bd8在独立client budget存储外新增持久缓冲，本地已有同步持久事件/原子账本，不替换资金协议。8983eb4c/16aa0809/2e7a39e1面向managed-adapter插件的会话亲和/账号选择/组合中间件，本地没有该插件链，不新增；2eed9b20表格排序持久化、abae4a63来源技能、cd605b85 UI包、7beebb23 Rust依赖和a36137ba发版不直接导入。保留既有turn ID隔离决定。
- vm2api3c45c685→8f3b6bab：完整阅读身份/模板/协议与worker头白名单delta；8d1d3b73和3c122e64保留caller entrypoint、生成时sdk-cli标签和轮次索引是来源VM identity builder合同，不能用其常量替代本地已验证的Linux CCH与账号指纹。5d9001c1扩展CLI头仅适用于其捕获样本，本地认证/租户头白名单不放宽。303静态system/UA/sdk-cli payload与full模板均按前轮待整套采样与本地工具缓存验收继续暂缓，发布oneshot不采用。未声称303与本地等效或性能更好。
- ranxi7c6851ca→cc9e57a9：autoreauth系列继续排除；TypeSafe 5bee9bbf/3ac7a971分类开关、snapshot、f0a04007独立邮件豁免/通知属于新增审核数据与绕过合同，暂未采用，不推进为已吸收。账务3ec171de及7ba7ef94的持久投递/SQL原子额度本地已有usage_billing_events、usage_billing_repo和usage_billing_platform_quota实现，缓存失效失败不ACK；不引入其双投影及legacy flusher桥。a232951e揭示依赖缺失ACK缺口，本地keys=nil或repo=nil会跳过鉴权失效并返回nil，已用回归复现并在原usageBillingCacheInvalidator补齐。
- **用户明确批准本轮批量资金隔离**：263798a8/de83a746/305持有凭证相关方案纳入，独立回归先证实本地释放指纹冲突被当成功。按现有业务路径增加version2单次量化/持久hold凭证及事务捕获-释放互斥；保留精确legacy兼容，不修正历史资金。旧超八位金额只读核对原10位指纹回执，超出原指纹可表达精度的金额须拒绝，不宣称哈希能证明未保存的精度。部署须停止新接单、排空旧请求/批次worker/财务写入后统一升级；不运行此操作。本地普通额度按当前SQL账本owner，ranxi delayed reset cutoffs及报价双投影的完整兼容改造未纳入，不把它们写成等效或已修。

## 验证进展
受限Key路由/SQL新回归已使用本任务隔离PostgreSQL18执行；初次测试定位并修正本地供应商约束接入、旧支付配置已退役路径以及本地JSON请求校验fixture差异。前端12项设置面板+4项四语言键测试通过，vue-tsc通过。Gemini与失效依赖、批量释放指纹的修复前回归均已真实失败，短证据保留regression-before.txt/batch-regression-before.txt。最后稳定候选以validation回执为准。
