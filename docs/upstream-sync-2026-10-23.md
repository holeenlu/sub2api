# 四源同步与冲突决定（2026-10-23）

状态：来源及冲突决定已完成，代码已实施；实际验证和发布映射存于本轮交付目录。隔离 worktree 手动三方合并；未运行旧 checkpoint 发布器。
本地基点 06b51d712f29a58ebf02244a80dadea7c8f27673，旧官方 dba770d56a0ee0ae5bad1b3dc5e11614d323163c。

| 来源 | 固定 SHA | 本轮范围 |
|---|---|---|
| Wei-Shaw/sub2api main | 5c185c5c5d4f5e409e2139806e0964ae944e03e8 | 完整增量，5 commits 含 merge |
| ranxi2001/sub2api production | c5f9b0ec5f573b60e699b4e94b31bc442d456ea8 | ffb547127..tip，6 non-merge commits |
| QuantumNous/new-api main | 33c61a24234aa20cb10e8a644304322c476361de | 7fb6e57a..tip，2 commits；既有 G01–G11 与 TTL 补缺证据沿用 |
| zyycn/codex-proxy-rs 默认 main | 6b4b8faaf41c03f7da345bb9679bc0d6d204760a | bea002ae..tip，7 commits；既有 A–F、zstd/SSE 比较不重演 |

## 官方冲突
- README 三语言：新增官方赞助广告与已确定品牌去广告规则冲突，保留品牌 README；不引入其专属未引用图片。保留官方署名。
- VERSION：上游版本快照变更，不属于本次人工发布，保留现有版本。
- DOMPurify：本地锁定的实际库已是 3.4.17，但最低声明/override 仍是 3.4.16；保留本地其他依赖与锁文件，提升这两处约束到 3.4.17，不恢复退役文档依赖。来源官方828626804 / ranxi d8e1ae06 / CPR7da79b82只实施一次。
- Codex 身份：采用官方成对 TUI→CLI 归一，保留版本及 OS/终端规则、账号身份隔离和显式配置入口。避免仅改 originator 导致 UA 不配对。更新本地剩余测试/文案；不改存储值。
- 空流恢复：采用官方空字符串 delta/done 与空 part 不提交输出的判断；实际非空或未知结构仍维持屏障。capacity Retry-After接入既有 SameAccountRetryDelay，并受原整轮预算约束，不添加重试层。保留本地 TTFT 口径、usage与 hosted-tool 未知执行保护。

## 选择性来源
- ranxi 64ee65fb5：本地 IsUpstreamContentRefusal 已保护完整结构化拒绝，但 CheckErrorPolicy 早退入口和 JSON+SSE 尾部及精确 WS 握手403存在缺口。融合到现有账号错误入口，复用现有拒绝分类器，不复制一套策略码规则；精确握手失败作为请求级故障，不消耗凭据403次数。保持当前请求恢复合同与真实凭据错误处理。
- ranxi20bc244d5/f34a88730：采用 Images 非流式成功响应显式 Content-Type，覆盖 Gin只在头缺失时写入造成的策略失效；验证真实 header filter 启用/禁用行为。
- ranxi b4c4e5ba8：源测试时间跨秒不稳，不涉及本地新增生产行为，检查本地相关夹具后不照搬独立代码。
- ranxi7079c7efd：用户确认本轮暂不引入第三方生图模型白名单，需独立权限/计费设计；不是等效覆盖。
- New API30748c655：本地两级访问日志已仅记录 URL.Path，无 RawQuery，不存在该查询凭据漏洞；进一步发现 Logger 的 path 覆盖了 RequestLogger 对 /api/bps-images/ 的令牌脱敏，复用 requestLogPath修复。验证查询不写日志且原URI仍可由handler读取。
- New API33c61a242：React表单 undefined 不清旧值；本地 Vue KeysView editKey每次完整赋值 expiration_date及其他字段，不使用来源表单库；来源 model metadata/redemption drawer不是本地产品，不导入平行UI。
- CPR8fc8c622：本地模型404仅按结构化模型信号处理，现有 model_not_found_error与runtime-block测试保护无整账号停用；无来源 CloudflarePathBlocked空body路径，已覆盖风险而非算法相同。
- CPR6b4b8faa：上游保留轮次ID，本地已有按账号重映射及测试，改变会触及隔离/恢复合同；用户确认本轮保留现有隔离，待真实客户端证据，不推进此项为已采用。
- CPRf195de38：OpenAI Live/WebRTC业务头/呼叫归属属于本地未启用入口；现有Responses caller-intent已具单一安全投影；不新增Live产品。
- CPR07bdd425：地理上下文改写保留历史日期，当前没有对应自动地理改写链；不引入新外部查询/正文改写。
- CPR39b35d46/d48982eb：来源React管理UI/i18n/布局；本地Vue四语言与响应式已有，保持现有产品，不导入Rust/React面板。

## 配置、验证与边界
无需新增配置，部署后修复自动生效；已有管理设置→网关设置→Codex User-Agent/版本配置继续有效，旧TUI出站配对归一为CLI，不修改数据库。图片响应头沿用 security.response_headers.enabled。
定向 pkg/openai、service身份/流边界/恢复/403/图片与handler恢复测试，middleware日志测试；多语生成与类型检查按实际输入执行。无并发机制或schema变更，不启动DB，不跑无关全仓/构建。
本轮不发版、不部署、不执行迁移。发布及最终验证状态以实际回执和交付映射补充，不把观察SHA当整仓审查水位。

## 验证修正
来源测试中“内容拒绝仍换号、其他平台处罚账号”的断言与本地合同冲突，修正断言以保护本地共享拒绝分类及终止/不重放行为；生产保护未放宽。未核查的历史产品不推进已审查水位。

来源作者：官方 c591412c4 / apple050620312；ranxi2001 <ranxi169@163.com>（64ee65fb5、20bc244d5）；New API Yi Cai / Seefs，CPR Zyy仅作行为对照。未复制后两者实现代码。
