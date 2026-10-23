# 四源增量审查（2026-10-23，第二轮）

本地基点441f231efa55e5978b9dd6e0d83f88d2c5b57b14；保留前轮交付后新增协议链路修复。共享能力，目标main/TapModels/tokensavy。
固定来源：Wei-Shaw/main 5c185c5c5d4f5e409e2139806e0964ae944e03e8（无增量）；ranxi/production 5e9bbe994c907de1c569827e94796473f531097f（从c5f9b0ec）；New API/main 33c61a24234aa20cb10e8a644304322c476361de（无增量）；CPR默认main 605354c8611dcf88fcc52a1f25d52c669b0f6169（从6b4b8faa）。只读复用既有源码镜像，不改其工作区。

## 采用与融合（实施前决定）
- 鉴权缓存：ranxi836d89c41及Redis订阅事件适配。现本地重连仅更新connected，旧L1可漏过断线期间失效消息。采用本地epoch包装及自动resubscribe通知，不Clear并发缓存；补齐读取开始时捕获epoch，防旧读取回填新代际。保留TTL、容量、独立负缓存及现有失效渠道，不添加表/Worker。当前请求与持久失效outbox合同不改。
- HTTP续接归属：ranxifa71b879e。当前两个整数Redis键分两次读写、本机归属优先；新共享适配器原子发布v1和旧整数键，读取校验三者一致，Redis不可用/不一致拒绝，不用旧本机缓存绕过。用户已明确选择本轮纳入。旧键仍为当前版本契约，用于滚动升级；全部实例升级且旧最大续接TTL过后可移除旧格式读取/双写，不新增永久开关。无Redis的现有进程内实现保留；非生产测试适配器明确提供同一接口，不保留两次读写的运行期后备链。
- 快照隔离：ranxi341abde05/2a333f9ae。官方解码母本已存在；本地原生/扩展调用会写Credentials/Extra，浅拷贝只保护顶层字段。采用JSON map/切片及分组列表独立副本、克隆模型映射，保留内容签名更新检测。保留revision/TTL/last_used与失败回源规则。增加分配是正确性代价，不宣称吞吐改进；不重复实现调度器。
- Cloudflare1010：46a0519eb。本地handle403豁免发生在自定义规则/团队状态处理后，补到CheckErrorPolicy、HandleUpstreamError及OpenAI fastpath早期；复用唯一分类器。真实凭据403不放宽。
- Anthropic安全拒绝：370975f61。保留本地共享读泵/取消计量/终态合同，在既有共享拒绝分类与compat延迟错误裁决处阻止前输出拒绝变成换号重放；不引入来源第二个流框架。不按任意message文本做宽泛拒绝识别。
- DeepSeek重复图片：c2612d17f/413bc2ae1中删除image/source多余载荷，沿用现有image_url输出契约。新增legacy url配置未发现本地调用方/存量契约，暂不引入开关或平行适配路径；不改变file_id-only项。
- 表格卸载：d581332fa。现useTableLoader取消请求但无法阻止迟到debounce再次发起、忽略AbortSignal的请求回写旧结果。增加原作用域disposed守卫，继续使用已有loader，不新增监听/接口。

## 已覆盖与排除
- ranxi官方cherry-picks：19个原SHA属于固定官方祖先；另结合实际函数核对：模型映射无排序签名、模型限流COW、Vertex配额、维度SQL、按需Antigravity判断、Gemini门禁/usage/schema、账号筛选、Codex queued/metadata/空流等已有本地实现。不能仅凭祖先认定等效；已有测试/最新协议链路修复继续保留。Gemini存量开关保留先前用户决定，不照搬源默认/迁移。
- 图片出口79a85c21e/144c263f2/ac7f27c8b：本地allowedStoredImageContentType按真实字节白名单；PublicTransport对直接、HTTP/SOCKS代理均解析校验并固定目标IP，保留原Host/SNI、拒绝TLS绕行与HTTPS降级；来源仅做本地DNS检查不能替代本地绑定远程目标保护，保留更完整既有单一路径。
- Anthropic/Gemini取消与异常结束：保留本地共享有界读泵、usage排空及终态判定，已发布protocol-chain-hardening；本轮仅补明确安全拒绝缺口，不恢复重复流实现。
- ranxi多节点部署拓扑：runtime角色、scheduler readiness、节点/boot路由、BPS图片owner origin、Caddy/HTTPS入口及完整应用CI是独立部署产品；本轮不导入。抽取其中与现有凭据失效和HTTP归属直接相关修复，不能称其他多节点能力已覆盖。相关纯测试、780打票身份测试及fork平台测试不导入；打票仍退役。
- CPR60e6bc01只升级Rust chacha20/ctutils/wnaf；本地Go/Vue无这些Cargo调用链，不照搬依赖。bfea3e1d新增CC Switch导入选择弹窗，属来源产品UI，不替换本地已有客户端配置/下载入口；605354c8为来源发版，不提高本地版本。
- New API无新提交，复用前轮网关范围G01–G11/TTL补缺/查询日志与表单审查。本轮不把未审历史标成已完成；前轮第三方生图白名单、轮次ID调整的暂缓仍有效。

验证按实际风险：缓存/订阅/归属/快照定向race与仓储协议测试，服务层403/拒绝/图片回归，前端loader测试和类型检查；需要的真实Redis验证限本任务，未改变SQL/schema不启动数据库。无发版/部署/迁移。

## 交付前远端漂移
交付锁内发现三品牌新增已发布账号池/认证资料及优惠码移除，main eae450da64c6abfabdb3ab0889ef6232cae9142b。与本任务25个改动路径无交集，原官方父基线不变，当前CHANGE_DELIVERY无变化。采用初始441f231作为显式基点三方整合，逐一确认本任务blob/mode未变，并保留全部远端新树。最终回归覆盖组合后的源码、ORM与前端类型；不重新审查四源或重复无关发布测试。

适配修正：来源AccountGroup.AllowedModels为fork专属字段，本地组关系不存在该字段；仅复制现有GroupIDs/AccountGroups/Groups容器，关联对象指针保持只读。首轮编译因此失败，已移除不适用字段，未扩展本地账号schema。

鉴权并发适配进一步核查：singleflight按代际隔离，重连后的请求不能加入重连前仍在运行的查询；L2/数据库读取捕获原代际，旧回填不绑定新代际。独立回归覆盖此边界。快照对比仍比较全部可序列化字段；新私有map指针必然不同，不能要求物理指针相等。

## 验证和实际落地
受影响服务/仓储、Redis连接丢失与共享归属并发、定向race、HTTP/WS与handler续接边界、表格loader和前端类型检查按实际回执执行。未运行全仓/真实上游/浏览器/生产部署或数据库迁移。首轮来源字段及可空测试上下文问题已修复；快照回归比较完整可序列化值和独立对象，忽略本就应不同的私有指针身份。最终状态见本轮verification-receipt.json和publication.json。
