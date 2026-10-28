# Sub2API 详细编码规范

规范版本：1.3。日期：2026-10-25。适用：本次重构及其后续新增、实际修改的 Go / Vue / TypeScript / 数据访问与测试代码，三品牌共用。业务重构尚未开始，本轮只制定规范及接入开发资料，不修改运行代码、lint/编译配置或依赖。

本文件是语言级编码、文件组织及审查标准的唯一来源。业务行为以[需求](REQUIREMENTS.md)、模块边界和生命周期以[契约](CONTRACTS.md)为准，顶层布局见[架构](ARCHITECTURE.md)，授权与交付仍遵循[AGENTS](../../AGENTS.md)和[交付规范](../CHANGE_DELIVERY.md)。规范不替代实际客户端、协议和历史数据合同。

## 1. 规则等级与执行方式

未标为“建议”的规则均用于新增和本次实际修改代码的审查。既有代码不能仅因为不符合新风格就全仓重写；迁移切片内失效实现的删除仍按RETIREMENT执行。

检查标记：**F** 格式化，**G** Go静态检查，**T** TypeScript类型检查，**E** ESLint，**U** 定向单元/组件/替身测试，**I** 必要的真实依赖/多实例/上游验证，**R** 代码审查，**C** 真实浏览器交互。标记表示验证方式，**不表示工具已经完整实现该规则，也不表示本轮运行通过**。

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-01 | 不以编码规范改变产品权限、收费、协议、存储或重试行为；冲突回到对应REQ/CT和现行需求规则处理。 | R/U |
| CS-02 | 每条业务规则只有一个权威实现；类型、状态、配置解释和恢复决策不得在不同层各写一份。 | R/U |
| CS-03 | 开始前确认工作包、基线、合同版本及允许写集；不把其他任务未完成代码混入本次变更。 | R |
| CS-04 | 不做无关全仓改名/格式化/升级，不以新增工具或抽象层替代当前owner可完成的修改；必要例外写清原因和退出条件。 | R |

普通工程选择在已授权范围内直接处理，不新增固定批准流程。例外记录包含规则ID、具体位置、必要性、影响和移除条件；不能用例外豁免明确的安全、账务或用户需求。

## 2. 项目结构与依赖

### 2.1 后端包内组织

顶层模块按ARCHITECTURE归属。以下是按需选择的文件职责示例，不要求创建全部文件或空目录：

~~~text
backend/internal/gateway/execution/
  engine.go          对外执行入口与主编排
  operation.go       本模块拥有的操作状态与转移
  attempt.go         单次尝试、预算和恢复动作
  ports.go           本模块消费的必要窄接口
  errors.go          本模块独有且需要跨文件使用的错误
  engine_test.go     行为测试；与目录既有build tag一致
  testdata/          有来源、可脱敏的协议或状态夹具
~~~

Go包名用简短小写单词，文件用snake_case.go；测试用_test.go。外部测试包的_test后缀沿Go惯例。不创建通用的manager/service/base/common目录来吸收无法归类的职责。

### 2.2 前端目录与领域归属

详细目录和共享边界以[FRONTEND_ARCHITECTURE](FRONTEND_ARCHITECTURE.md)为唯一来源：app装配、features拥有领域API/草稿/查询、shared提供无业务依赖的技术机制。小领域按需使用单文件；不预建空目录，不重复维护旧views/contracts与新pages/api两份布局。

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-05 | 核心model/protocol/execution不导入Gin、Ent、Redis客户端或具体provider；具体设施经端口注入。新包禁止反向依赖旧service万能实体；确需过渡转换只放集成边界并登记退出条件，不得流入核心或长期保留。 | G部分/R |
| CS-06 | 接口通常由消费者定义；跨模块真正共享的值对象归model/合同owner。不为每个struct生成对应interface，不使用I前缀或空泛BaseService接口。 | R |
| CS-07 | 类型和方法名称表达责任与行为：业务ID保持明确含义，Do/Handle/Manager等泛名仅在已有明确上下文中使用。同一方法不能直接实现多个领域的具体规则；CT定义的主编排可以依次调用各owner端口，但不复制其规则，也不为形式拆分制造空转发层。 | R |
| CS-08 | 按FRONTEND_ARCHITECTURE保持app→features/shared、features→shared；跨域运行组合在app，禁止feature相互导入实现或借event bus绕过；最小跨域只读类型须登记owner。公开出口显式，路由贡献保持懒加载。 | T部分/R |
| CS-09 | 只有实际共享且语义一致的代码才下沉；不能为未来可能复用建立工具库。新公共helper须说明调用方、唯一owner和为什么原模块不能承载。 | R/U |
| CS-10 | 拆分依据是责任/状态所有者/协议差异，不能按任意行数或机械文件切割。若一个函数直接实现解析、选号、重试、写响应和扣费等多领域规则，应按CT拆分；调用各端口的合法编排不受此禁令。文件变小但职责不变不算完成。 | R |

跨feature路径、包导入和新公开导出在审查时显式核对。当前depguard只覆盖部分旧service/handler规则，新包依赖检查在WP-01/对应切片落地；本文件不声称已经自动禁止所有逆向依赖。

## 3. Go 编码规则

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-11 | 使用gofmt；新标识符遵守Go大小写与initialism惯例，如APIKeyID、HTTPClient、UserID。不要为了命名统一破坏既有外部字段或无关公共符号。 | F/R |
| CS-12 | 导出API说明职责、前置条件、返回值、nil/零值和副作用；复杂内部逻辑解释原因与不变量，避免复述代码。注释随行为变化更新。 | R |
| CS-13 | 稳定业务边界使用明确类型，不用any/map[string]any传递整个业务上下文；动态协议数据可保留RawMessage或受控结构，不能伪造强类型承诺。 | G部分/R/U |
| CS-14 | 明确nil、空集合、缺失、显式null与零值差异；状态枚举的零值必须声明是否有效，未初始化不能自动变成安全NotSent或零费用。共享slice/map的可变性与复制边界必须明确。 | R/U |
| CS-15 | 错误身份使用errors.Is/As或既有类型化错误，跨层补充必要上下文并用%w保留原因；多错误合并不能改变已提交的主业务结果，辅助收尾错误单独归因；禁止以错误文本字符串匹配替代稳定分类。上游原始文本解析只限其协议适配边界。 | G部分/R/U |
| CS-16 | 错误由有处置责任的边界记录一次；底层返回、边界分类并记录。不能各层log后再return形成重复告警，也不能将需要恢复的错误吞掉。 | R/U |
| CS-17 | 不用返回空值、false或零金额掩盖依赖失败；not-found与不可用区别表达。注意带类型nil的interface，调用方不能仅靠interface!=nil推断资源可用。 | G部分/R/U |
| CS-18 | context作为需要取消/期限的方法首参并向下传播；不向下游偷偷换成Background，不把业务对象塞进context。host生命周期等明确拥有的context可存储，但要写清创建/取消/等待责任。 | G部分/R/U |
| CS-19 | 每个派生context、stream、lease、timer、body和订阅都有明确释放者；所有成功、错误、panic边界及取消路径检查资源栈。需要独立账务收尾时使用CT规定的有界上下文，不无限脱离请求。 | G部分/R/U/I |
| CS-20 | 每个goroutine都有停止信号、错误去向和等待结束的owner；不得请求结束后无人接管地运行。构造器/循环中的隐藏异步工作必须迁入显式生命周期。 | R/U/I |
| CS-21 | mutex保护的字段和锁顺序写清；锁内只做必要状态操作，外部回调、网络、数据库和阻塞发送原则上移到锁外。若无法移出，说明必要性与有界阻塞证据。atomic不代替多字段不变量。 | G部分/R/U/I |
| CS-22 | channel的关闭者只有明确owner；发送方不能假设接收方永远存在。缓冲、队列、等待和溢出策略有界且可取消；不用default空转或无界起goroutine规避背压。 | R/U/I |
| CS-23 | wire不可变输入、出站副本与共享缓存分开；池化buffer归还前不能被其他goroutine继续引用。仅在有测量依据的热点使用Pool/零拷贝，并证明所有权。 | R/U/I |
| CS-24 | 网络请求沿用现有受控client、代理、TLS、出口与timeout机制；响应body与连接释放按stream合同执行。自动重试不得隐藏新的业务生成或绕过attempt登记。 | R/U/I |
| CS-25 | 持续时间和绝对时间分开；运行期限使用同一权威预算，重试不重置。Retry-After、时钟偏差、等待累计和抖动按既有规则实现；测试需可控制时间。 | R/U |
| CS-26 | 构造函数只构造并校验依赖，返回初始化错误；不偷偷启动后台任务或修改全局环境。依赖显式注入，生命周期由host装配；禁止以service locator隐藏依赖。 | R/U |
| CS-27 | 可预期输入/依赖失败返回错误，不能panic；recover只在明确进程/请求/worker边界保护并保留诊断，不能把panic改成成功或绕过资源收尾。 | G部分/R/U |
| CS-28 | 默认值、开关、限制和单位有唯一配置owner；优先复用已有设置，不在handler/provider/frontend重复解释。新增常量也须说明业务/安全含义，不为每个helper加配置项。 | R/U |

### 3.1 错误与上下文写法

以下仅说明局部写法，类型、预算和返回签名需以实际CT为准；不是新接口实现或已编译测试结果。

~~~go
// 推荐：保留原因，交由上层按错误类别处理。
value, err := store.Load(ctx, id)
if err != nil {
	return Value{}, fmt.Errorf("load resource state: %w", err)
}

// 推荐：预算来自拥有者，不在helper里重新定义一套总期限。
stageCtx, cancel := context.WithTimeout(ctx, stageBudget)
defer cancel()
result, err := dependency.Call(stageCtx)
~~~

不要以“发生错误就返回空集合继续选号”“context取消就把用量记零”“Close失败统一忽略”作为通用模板。可忽略的错误必须有语义依据；当前errcheck的blank赋值检查关闭，不代表忽略错误已被批准。

### 3.2 状态和职责命名

Operation、Connection、Stream、Session、ProviderTurn、Attempt、BillingRoot分别使用CT定义；不得统一叫requestID再靠注释猜含义。状态转移集中在拥有者方法中，调用方不能直接改多个共享bool形成隐式状态。

接口方法尽量表达单一责任，如Prepare/Open/Next、Acquire/Release、RecordUsage/Settle。调用方是否取得资源所有权、重复调用是否幂等、失败后哪些副作用可能发生，需要写入方法合同。

## 4. Vue 与 TypeScript 编码规则

新TS文件采用两空格、单引号、不写无必要分号；Vue属性使用双引号。现有文件保持其既有格式，不能为少量业务改动制造整文件格式差异。当前没有统一Prettier强制配置，本规范不新增格式工具依赖。

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-29 | 新组件默认Composition API与script setup lang=ts；组件PascalCase.vue，composable用useXxx.ts，普通模块用明确camelCase文件名。组件名应体现领域职责；不批量重命名有效旧路由。 | E部分/R |
| CS-30 | 输入边界用unknown并收窄；不得新增any、双重as断言、非空断言或ts-ignore来掩盖类型问题。确有第三方动态边界时局部隔离并注明依据；类型断言不能替代运行校验。 | T部分/E部分/R/U |
| CS-31 | 服务器DTO、领域值和展示状态仅在语义有差异时分开；转换集中且有测试。用判别联合表达互斥状态，避免loading/error/success多bool不一致；缺失/null/0按API合同保留。 | T/R/U |
| CS-32 | 常规API调用复用现有共享HTTP客户端及会话刷新/错误处理，不在页面新建axios实例或私有token刷新器。流式/上传等必要特殊client有明确owner和理由。 | R/U |
| CS-33 | 异步请求绑定发起时的会话/参数/版本；取消旧请求或识别过期响应，防止旧结果覆盖新账号或新筛选。组件销毁、登出和换用户时取消或失效在途任务。 | R/U |
| CS-34 | props只读；变更通过具名emits或领域action，不直接修改父组件对象。props/emits/slots公开合同类型明确，事件名表达意图，不把完整内部store作为组件API。 | T/E部分/R/U |
| CS-35 | 纯派生值用computed，watch只承载明确副作用；避免watch间相互回写形成循环。需要deep/immediate时说明数据规模和触发理由，不靠watch同步重复事实。 | R/U |
| CS-36 | composable默认持有当前调用实例状态；副作用有取消和onScopeDispose等清理责任。不在模块顶层无意共享用户状态，不靠导入触发请求/订阅。 | T部分/R/U |
| CS-37 | 局部交互留在组件/composable；只有跨页面共享且生命周期清楚的状态进入store。持久化有版本、作用域和退出策略，不扩大现有凭据/敏感数据持久化范围。 | R/U |
| CS-38 | 路由guard和按钮权限是体验层，服务端仍权威鉴权；不能前端过滤代替供应商/对象归属检查。共享router/项目身份/API出口按单写者集成。 | R/U/I |
| CS-39 | 明确idle/loading/empty/success/error及局部提交状态；防重复提交但不把禁用按钮当幂等。列表使用稳定业务key，过滤放computed/上层结构，不依赖同节点v-if/v-for的隐含优先级。 | E部分/R/U |
| CS-40 | 面向用户文本使用i18n与网站设置，时间/金额/比例/用量沿统一展示工具；技术项目名统一sub2api，镜像/容器等部署信息不在应用写死。前端不重新实现价格/平台规则，不硬编码品牌名、Logo、API展示域名或推广链接；按BRANDING复用现有字段。 | T部分/R/U |
| CS-41 | 不可信HTML/Markdown/URL经过现有安全边界；v-html、外链、iframe、下载和文件预览必须核对来源/清理/权限，不因为是模型输出就信任。 | R/U/I |
| CS-42 | 复用现有设计组件、Tailwind和样式约定；样式作用域明确，避免全局选择器污染。交互具备label、键盘、焦点、错误提示和状态可辨识性，不仅靠颜色。 | E部分/R/U |

### 4.1 类型与异步边界示例

~~~ts
// 互斥状态显式表达；E使用经过公共错误映射的类型，不新增后端DTO。
type RequestState<T, E> =
  | { status: 'idle' }
  | { status: 'loading'; requestId: number }
  | { status: 'success'; data: T }
  | { status: 'error'; error: E }
~~~

每个Promise必须被await、返回给拥有者，或交给有明确错误处理和取消责任的宿主。单独写void request()并不等于已处理失败。当前ESLint没有启用完整Promise生命周期检查，这项需要人工及行为测试。

TypeScript类型只在编译期存在，不能使用response.data as DTO代替对不可信数据的必要验证。不要把所有DTO再包装成一个万能Result类型；沿用既有API错误合同。

### 4.2 feature迁移约束

将root api.ts或旧页面逻辑迁入feature时，同次删除被替代的端点包装、缓存、状态和locale。网络基础设施继续唯一；不得在feature中叠加第二套刷新、重试、权限或计价逻辑。共享基础组件不解释OAuth模式和供应商商务政策。

## 5. 数据 接口与持久化规则

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-43 | 金额、token、倍率、时区、时间戳和duration明确单位及精度；沿用已验证的账务数值类型。不得因风格改全链精度或临时引入decimal库；显示舍入不进入计算链。 | T部分/R/U/I |
| CS-44 | 需要保真时保留原始JSON及大整数语义，不经float64或JS不安全整数再编码。缺失与显式null、别名与原生字段优先级按现有协议；不能新增通用清洗器偷偷删字段。 | R/U |
| CS-45 | 事务由领域用例定义，repository执行；完整列出锁顺序、原子更新及失败恢复。不能把网络调用放入长事务，不能先commit一半账务再以普通重试修补另一半。 | R/U/I |
| CS-46 | SQL参数化；动态列/排序采用明确允许项，不拼接用户值。查询限定范围、排序和分页，避免无界扫描及N+1；新增索引由实际查询与规模依据驱动。 | G部分/R/U/I |
| CS-47 | 缓存有权威来源、scope、版本、容量/TTL和失效合同；缓存命中不自动代表授权有效，缓存丢失不伪造账务事实。禁止不同租户/Key共享不明确的缓存键。 | R/U/I |
| CS-48 | 锁/lease释放校验自己的token或epoch，重复释放安全，不误删后来持有者。续期与停止的关系明确，TTL过期不被解释成上游必然停止。 | R/U/I |
| CS-49 | 幂等键、指纹、累计/增量usage、unknown和lower_bound沿CT-07；实际费用只走唯一账务用例，不通过零值、状态码或日志补造结算。 | R/U/I |
| CS-50 | 公开API字段、枚举、路径、错误和分页保留有效合同；新增字段定义可选性、默认和授权。内部revision不自动要求旧客户端补字段；对外变化同步DTO、客户端和兼容退出条件。 | T部分/R/U/I |
| CS-51 | 历史SQL迁移不可改写；结构变化须说明必要性、旧读者、回填幂等和回滚边界。生成的Ent/Wire按实际需求生成并审查，不手改生成文件掩盖类型冲突；编译产物、web/dist、SDK及其他标记generated的输出同样从真实源与已有生成器更新。 | R/U/I |

查询示例只表达参数绑定方式，不授权新增表或改变现有repository：

~~~go
var accountID int64
err := db.QueryRowContext(ctx,
	"SELECT id FROM accounts WHERE vendor_id = $1 AND id = $2",
	vendorID, requestedID,
).Scan(&accountID)
if err != nil {
	return fmt.Errorf("load account reference: %w", err)
}
~~~

查询返回sql.ErrNoRows时仍由调用方按既有错误分类处理；示例保留错误身份，不把查不到记录当作授权成功。

事务提交后如何发事件、通知和失效缓存由现有outbox/owner合同决定。不能因为规范提到outbox就给所有场景新建消息表。

## 6. 安全 日志与可观测性

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-52 | 用户/Key/供应商/资源归属来自已验证身份；每个异步交接和发送边界按CT复核。输入中的user_id、session_id、role或对象存在性都不能代替授权。 | R/U/I |
| CS-53 | 使用现有结构化logger和稳定字段；不记录token、代理密码、完整提示词、工具结果、图片或密文原文。确需内容审计使用既有受限通道和保留规则，不走普通日志。 | G部分/R/U |
| CS-54 | URL、代理、文件/图片下载、重定向和目标地址沿现有出口保护；不可通过新HTTP client、redirect或helper绕开。CLI/进程调用使用明确参数边界，不拼接未验证shell文本。 | G部分/R/U/I |
| CS-55 | 指标标签限制基数，不把request/session/user ID或正文作为常规label；关联ID放受控日志/trace。业务、上游、权限、依赖和客户端取消分别归因，观测丢失不改账务结果。 | R/U/I |
| CS-56 | 复用已安装、锁定依赖；新增依赖说明必要性、体积、信任和适用许可。保持现有来源/授权声明，不因改语言或移文件删除NOTICE，也不重新索取已明确覆盖的授权。锁文件随必要依赖变更通过现有包管理器更新，不手改校验摘要或夹带无关升级。广告/赞助内容清理不包括LICENSE/NOTICE及必要来源署名。 | R |

当前gosec有明确排除项，不能据“lint通过”宣称秘密或所有注入风险已被扫描。审查代码、运行合同和工具能力必须分别说明。

## 7. 测试与验证代码规范

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-57 | Go测试与被测包同目录并沿现有unit/integration/e2e标签；TS沿现有__tests__/xxx.spec.ts等约定。优先扩展现有测试，不为命名统一搬动无关测试。 | G部分/R |
| CS-58 | 测试名表达行为和条件；相似输入用表驱动，独立风险有独立断言。断言可观察结果、身份、副作用和状态，不只断言调用一次或状态200。 | R/U |
| CS-59 | 时间、随机、网络和全局状态尽量受控；测试不依赖执行顺序、个人凭据或真实用户数据。环境修改必须恢复，并发测试不共享可变全局夹具。 | G部分/R/U |
| CS-60 | fake/mock只实现其被消费合同，不能通过省略错误、延迟、取消或重复事件隐藏风险。fixture有来源、版本、脱敏和覆盖说明，合成样本不得冒充真实客户端报文。 | R/U/I |
| CS-61 | 并发测试用同步点控制交错并有超时；不以任意sleep猜完成。检查取消、晚到响应、重复释放、lease失效及goroutine退出；实际并发变更补定向race。 | R/U/I |
| CS-62 | 前端测试通过输入、交互、DOM、请求和公开状态验证行为；避免只测内部方法或大快照。覆盖过期响应、卸载、换用户、错误和权限展示；后端权限另验。 | T部分/R/U |
| CS-63 | 单测、替身、真实DB/Redis、多实例、真实客户端/上游和线上证据分别记录。Skip、零测试命中、仅编译或文件存在都不能填passed；未执行标not_run/not_collected。 | R/U/I |
| CS-64 | 性能改动先说明瓶颈与同条件比较；基准固定输入、环境和计量口径，报告分布/分配与限制。不得以代码行减少、一个微基准或覆盖率数字宣称整体提速。 | R/U/I |

测试预算按独立风险选择，不制定全仓统一覆盖率门槛、每次全量build或每个品牌重复相同验证。真实数据/数据库改变需要相应真实依赖证据，不能用mock替代。

类型负例确需使用@ts-expect-error时，须局部说明预期诊断，并有实际类型检查发现失效指令；不能在业务代码改用它绕过CS-30。

当前frontend/tsconfig.json排除了__tests__、spec和test文件；pnpm typecheck不能被宣称覆盖这些测试的静态类型。测试类型风险涉及的检查在实际工作包中明确补齐，Vitest转译通过本身也不等于完整类型检查。

## 8. 代码审查 例外与收尾

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-65 | 审查记录列出实际适用的CS编号、差异与证据；不要求每个小任务机械填写全部规则。例外必须局部、有理由和退出条件，不能使用无解释的nolint/ts-ignore/全局关闭检查。 | R |
| CS-66 | 共享契约、配置schema、Wire、全局router/API出口和品牌资源由单一owner集成。worker不得以新增同名DTO、平行v2或兼容wrapper绕开合同冲突。 | R/U |
| CS-67 | 功能替换同时处理旧入口、字段、helper、配置、UI、文案、依赖与测试；保留项有实际调用方/数据合同及退出条件。代码清理与历史数据处置分别核实。 | R/U/I |
| CS-68 | 按实际风险选择下述现有检查，记录命令、输入、环境与结果；不因规范文字变化运行无关业务测试，不为落实规范新建验证平台或自动发布流程。 | R |
| CS-69 | 规范变更更新版本与影响，指明哪些消费者/工作包要调整；业务合同变化仍回到CT/REQ，不把编码偏好变成未授权产品变更。 | R |

### 审查清单

- 该变化解决哪个REQ/CT/WP，适用哪些CS规则？有没有无关变更或隐式新增层？
- 依赖方向、公开类型与状态所有者是否清楚？是否新增第二份规则？
- 错误、取消、资源和并发是否闭合？发送、交付和账务事实是否混淆？
- 权限、敏感信息、数据精度、事务与历史读取是否保留？
- 前端是否有重复事实、请求竞态、无owner副作用或品牌硬编码？
- 测试能否覆盖独立风险，证据等级是否真实？未验证项及必要例外是否明确？
- 旧路径是否同次退出？生成差异、业务/测试增删和新接口/配置/执行路径是否如实报告？

这些问题按任务实际影响使用；不相关项不应触发额外构建、数据库或上线流程。交接沿用[HANDOFF](templates/HANDOFF.md)，不再维护第二套完成状态。

品牌与更名规则详见[BRANDING](BRANDING.md)：项目身份、部署身份、网站品牌分域；禁止对用户内容、协议body、持久化键和版权文本做全局字符串替换。

## 9. 现有自动检查的真实覆盖

检查基线：abaab4811bb6e2daa44708bc8e731c3c857f9cb1。本节记录读到的配置，不代表本轮执行了检查。

| 工具/配置 | 已存在的能力 | 尚不能据此声称 |
| --- | --- | --- |
| [Go配置](../../backend/.golangci.yml) | gofmt；depguard、errcheck、gosec、govet、ineffassign、staticcheck、unused；部分旧service/handler禁止导入repository/Redis等 | 新架构所有依赖已自动约束；全部错误已检查；所有秘密/注入已扫描 |
| Go命名/注释 | gofmt格式已配置；staticcheck相关ST1000/ST1003/ST1020/ST1021/ST1022当前关闭 | 导出注释、initialism和全部文档规则已自动强制 |
| errcheck | 部分忽略函数已有明确列表；check-type-assertions=true，check-blank=false | 所有下划线忽略和所有Close错误均已被拒绝 |
| [TypeScript](../../frontend/tsconfig.json) | strict、noUnusedLocals、noUnusedParameters、noFallthroughCasesInSwitch；生产源文件范围明确 | 测试文件全部类型安全；运行时DTO已校验；异步生命周期无竞态 |
| [ESLint](../../frontend/.eslintrc.cjs) | eslint recommended、Vue3 essential、TypeScript recommended；已有项目例外 | any、ts-ignore、多词组件名、Promise生命周期、feature依赖都已自动拒绝 |
| [前端脚本](../../frontend/package.json) | lint:check、typecheck、test:run、check:i18n及既有build入口 | lint默认只读：lint脚本实际带--fix；build只是格式检查 |
| [忽略范围](../../frontend/.eslintignore) | 依赖、生成文件和构建输出排除 | 被排除输出可代替源代码质量证据 |

现有命名、any/ts-comment、feature边界和新核心依赖的缺口先由规则审查与相应行为测试承担。WP-01负责新核心边界与合同，WP-28负责迁入边界的必要类型/lint补齐，WP-29至35负责领域证据，WP-21负责最终收敛，WP-24核对旧规则退出；只有实际实施这些任务后才更新“已自动覆盖”状态。本次不修改这些工具配置，也不追溯性开启全仓新门禁。

## 10. 按影响选择的检查命令

下面是后续编码时的命令示例，不是本轮执行结果，也不是每次全部运行的套餐。先确认SDK、依赖和文件存在；使用当前仓库固定工具链，不自动安装/升级。

~~~sh
# Go：在backend目录，只列出有格式差异的实际修改文件。
gofmt -l internal/gateway/execution/engine.go
# 上行目标包尚待开发；文件未存在时不能拿它作为已验证命令。
# 现有包的定向测试示例；按实际影响替换为已有包和匹配的测试。
go test -tags=unit ./internal/pkg/sse
# 在依赖规则或静态风险确有变化时，用已有工具检查实际包。
golangci-lint run ./internal/pkg/sse/...

# 前端：在frontend目录；不要把带--fix的pnpm lint当作只读验证。
pnpm exec eslint src/api/client.ts
pnpm typecheck
pnpm exec vitest run src/api/__tests__/client.spec.ts
# 涉及多语言键时按实际变化使用现有检查。
pnpm check:i18n
~~~

普通gofmt不等同于golangci配置中的额外重写规则；涉及该配置的检查需使用相匹配的已有工具版本。对未安装工具如实记录环境缺口，不借规范创建一份依赖缓存或升级依赖。

是否添加-race、真实依赖测试、前端build/供应商门户build，以实际风险和[VALIDATION](VALIDATION.md)、[交付规范](../CHANGE_DELIVERY.md)为准。测试输入和命令一致时复用有效证据；文档本身只检查内容、链接、规则引用、格式和必要示例语义。

## 11. 代表性审查样例

| 变更 | 应用规则 | 应得到的结论 |
| --- | --- | --- |
| 新增普通provider | CS-05/06/24/49/66 | 在adapter端实现单次尝试；核心不增加平台分支，不自建重试/账务；核对CT-03消费者 |
| 账号列表增加筛选 | CS-31/32/33/39/40/62 | 复用client，旧请求不能覆盖新筛选，empty/error/loading明确；只验相关组件/API，不构建整个后端 |
| 将BPS上传拆入prepare | CS-13/19/23/44/66 | 输入指纹与上传后的sealed wire分开，Open不再改正文，清理责任可查；不改变owner总预算 |
| 取消后处理Cloud用量 | CS-15/18/19/49/63 | 保留已确认lower_bound与unknown差异，使用有界账务恢复，不用context取消推断免费 |
| 供应商费用字段调整 | CS-43/45/49/50/51 | 先核对单位、旧数据/DTO和双边结算；需要真实持久化验证时不能仅跑fake |
| 给已有代码加一条文档说明 | CS-04/68 | 仅文档差异/链接/示例检查，不启动业务测试、Docker或全量构建 |

规范和示例均不授予上线、生产数据库操作或未明确需求的代码开发范围。当前交付是规范文本与协作接入；实际自动化覆盖、代码迁移及运行验收分别记录。

## 12. 语言级细则（1.3增量）

本节将前述职责规则落到可直接审查的语法和文件写法。沿用CS-01至69，新增CS-70至100；规则是本项目选择，不代表所有检查器已开启。目录、领域和品牌的唯一来源不变。

### 12.1 Go

| ID | 写法与边界 | 检查 |
| --- | --- | --- |
| CS-70 | import按标准库、第三方、本项目分组；普通包不使用点导入。空白导入只在确有注册要求的装配/驱动入口并说明原因；别名解决真实冲突，不给所有导入重命名。 | F/R |
| CS-71 | 业务身份、金额单位、计数与时间不能只靠变量名区别；在易混用边界使用有明确意义的命名类型。别名仅用于保留同一类型身份的明确需要，不能假装类型隔离；不批量改变现有wire字段或数据库类型。 | G部分/R/U |
| CS-72 | 接收者短名且同类型一致；需要修改状态、持有锁或共享资源的类型使用指针语义。带mutex/atomic/连接所有权的值不得在使用后复制；小型不可变值可按值传递，不机械全指针。 | G部分/R/U |
| CS-73 | 配置/实体/结果用具名字段构造；公开构造函数验证必要依赖。可选依赖和默认值显式记录，不以nil代表多种业务模式；不将可变全局变量或init中的环境读取当依赖注入。 | G部分/R/U |
| CS-74 | slice/map零值、空集合和未设置按合同处理；跨goroutine或缓存边界交接前明确谁可以修改底层内容。浅复制不当深复制；输出顺序、摘要和幂等输入不能依赖map遍历顺序。 | R/U |
| CS-75 | JSON字段名/omitempty/指针按外部合同选择。普通指针字段不能单独表达缺省、null和显式清空三种状态；PATCH等需要区分时复用现有presence表达，在边界判定后进入类型化命令。敏感字段不因omitempty缺失就认为不会序列化。 | R/U |
| CS-76 | 端口实现方可用编译期接口断言核对方法集；返回interface前检查typed nil。接口留给真实替换边界，泛型只提炼稳定类型算法，不用反射/泛型注册器抹平provider和账务生命周期。 | G部分/R |
| CS-77 | switch显式覆盖领域状态；default按合同返回错误或未知，不能作为隐式成功。向枚举添加值时检查所有消费者、持久化与观测标签，不仅改定义；不以数值排序隐含状态迁移。 | G部分/R/U |
| CS-78 | goroutine启动处可看见输入捕获、取消与join责任；循环中的共享对象、slice和指针仍需审查。按go.mod的实际语言版本理解闭包/计时器语义，不机械复制旧版本修复片段；并发数量来自已有受控预算。 | G部分/R/U |
| CS-79 | 作用域内及时关闭body/rows/file；长循环不要累积直到函数返回的defer。Close/Rollback/恢复失败按CS-15/19保留主要错误与必要次要事实；不把一个函数的裸命名返回值和defer写入作为隐藏业务状态机。 | G部分/R/U |
| CS-80 | 测试命名表达条件与结果，表驱动case使用具名字段；t.Parallel只用于无共享可变环境/fixture的用例，t.Cleanup归还自己创建的资源。子测试失败应能定位输入、状态与副作用，不依赖前一case预热。 | R/U |

### 12.2 TypeScript

| ID | 写法与边界 | 检查 |
| --- | --- | --- |
| CS-81 | 默认const，确实需要重新赋值才用let，不新增var。普通变量/函数camelCase，类型/类PascalCase；布尔命名体现肯定条件，避免isNotDisabled等叠加否定和无解释缩写。 | E部分/T/R |
| CS-82 | 纯类型依赖用import type；显式具名出口，业务模块不使用export *聚合全部内部符号。先外部包后内部绝对路径再局部路径；不能通过barrel隐藏feature循环依赖或副作用导入。 | T/E部分/R |
| CS-83 | 对象合同用interface或明确对象type，联合/映射/工具类型用type；遵循同模块一致写法，不为风格改现有公共类型。互斥业务状态优先字符串字面量判别联合，不新增可随意赋值的状态字符串或位掩码bool组合。 | T/R/U |
| CS-84 | 公开函数、API适配器、composable及store action标明参数/结果；局部简单值允许推断。泛型表达输入输出关系，不能仅为让调用方写任意T后把未知响应强转成T；默认unknown而非any。 | T/R/U |
| CS-85 | unknown先做必要的结构/范围检查，再构造可信视图；as const用于字面量保留，satisfies用于编译期形状核对，两者均不是运行时校验。可选字段、null、空字符串、0、false按API分别保留，禁止统一filter(Boolean)清洗业务数据。 | T/R/U |
| CS-86 | 对封闭联合使用显式分支和never检查新增状态；对开放的服务端枚举保留unknown分支与原始诊断，不用穷尽断言把未来合法值变成无提示崩溃。对外枚举扩展同步消费者。 | T/R/U |
| CS-87 | Number/parseInt/parseFloat转换后按合同检查finite、安全整数、范围和单位；不能让NaN/Infinity或空串转换所得0进入业务命令。大整数/金额沿用服务端合同与已有表示，不先损失精度再转字符串。 | T/R/U |
| CS-88 | 单次请求捕获scope、request sequence和signal；finally更新状态前也校验归属。AbortError/axios取消保留可识别身份；Promise.all仅用于允许一项失败整体失败的并行操作，allSettled必须逐项处理结果，禁止丢弃失败项。 | T部分/R/U |
| CS-89 | 日期保存和传输遵循既有UTC/offset/epoch合同，界面再按用户时区格式化。解析仅日期字符串不能隐式改成某一时区的瞬时点；费用、token、ms/s和百分比的转换集中于具名边界。 | R/U |
| CS-90 | 不用JSON stringify/parse当通用深复制或隐藏undefined/日期/精度差异。使用显式draft投影隔离表单与服务端快照；readonly只表达类型约束，不能据此宣称嵌套对象在运行时不可变。 | T/R/U |

### 12.3 Vue 单文件组件、Pinia 与 CSS

| ID | 写法与边界 | 检查 |
| --- | --- | --- |
| CS-91 | 新SFC按script setup、template、style组织；脚本依次为imports、props/emits、注入与状态、computed、actions、watch和生命周期。允许按紧密职责分小组，禁止为排序搬动无关旧文件；模板不内联请求/权限/价格算法。 | E部分/R |
| CS-92 | Props/Emits写明类型、默认和可变边界；集合/对象默认值按Vue约定隔离实例。更新使用具名事件或update:modelValue，不修改props内部对象；模板事件处理承担错误去向，不能用void掩盖未处理Promise。 | T/E部分/R/U |
| CS-93 | ref用于明确单值，reactive用于有整体寿命的草稿；保持响应式读取语义，不将普通解构误当响应式引用。Pinia读取state/getter用storeToRefs或保留store访问，actions按既有方式调用；不假定任意Vue/编译版本的props解构行为相同。 | T/R/U |
| CS-94 | watch依赖尽量精确，清理注册与请求作用域相匹配；异步watch/事件晚到不能写入已销毁页面。onMounted/activated/visibility事件分别说明启动原因，onScopeDispose/卸载或停用有对应清理，不重复开启同一轮询。 | R/U |
| CS-95 | 领域Pinia只保存确需跨页面共享的事实与操作，不复制query快照、表单草稿和本地派生值；setup store有明确reset与会话失效清理合同，不能假定框架会替业务自动删除私有缓存。 | T/R/U |
| CS-96 | 样式先复用现有Tailwind和tokens；局部规则默认scoped，:deep/全局样式必须说明作用对象。z-index、断点、间距与主题色沿公共tokens，不复制品牌色/魔法层级；不用!important掩盖组件边界错误。 | R/C |
| CS-97 | 按钮/链接使用对应语义元素，非提交按钮明确type；输入有label和可关联错误，对话框管理焦点，异步状态提供可访问反馈。i18n使用完整句子与参数，不拼句；URL、Markdown、iframe安全仍沿CS-41。 | E部分/R/U/C |

### 12.4 SQL、脚本与配置

| ID | 写法与边界 | 检查 |
| --- | --- | --- |
| CS-98 | SQL列/排序/过滤含义明确；列表查询有稳定排序和分页合同，更新显式列出目标字段及必要scope/revision条件并检查实际结果。金额、NULL、时区及空集合沿schema；SQL迁移按既有命名和runner，生成文件不手改，历史已应用迁移不重写。 | R/U/I |
| CS-99 | Python工具使用明确argv运行子进程、检查退出/超时、局部捕获已知异常；Shell遵循文件shebang和目标平台，引用变量/路径并显式处理失败。动态文本、JSON或用户输入不能拼成可执行shell；不打印环境秘密，不以全局通配删除共享文件。 | R/U |
| CS-100 | JSON/YAML/环境模板对键、类型、单位、缺省、优先级和敏感性有唯一schema/owner；缺失与false/0不混用。真实密钥不进示例；新增配置同步读者/示例/退出条件。品牌和项目部署输入按BRANDING，禁止增加隐式兼容别名链。 | R/U/I |

C表示实际浏览器交互记录，仅对受影响的UI行为使用；不能以组件替身代替键盘、焦点和布局证据。这里只记录需要的证据类型，不创建新检查平台。

### 12.5 可照着审查的局部片段

以下为设计示例，非新业务实现、非已运行测试。类型和函数名只说明边界；开发时使用相应CT和现有模块的真实类型，不把片段直接当完整SDK。

**Go：保持原因和资源所有权。**

~~~go
lease, err := leases.Acquire(ctx, request)
if err != nil {
	return Result{}, fmt.Errorf("acquire capacity: %w", err)
}
// 本用例从此负责释放。释放错误处理、收尾上下文及所有退出路径
// 按实际租约合同明确，不用忽略错误的空defer作为完成依据。
return executeWithOwnedLease(ctx, lease, request)
~~~

executeWithOwnedLease是示意的所有权接管点，不要求新增此包装；如果现有用例直接持有lease，应在原处实现有界收尾。不能把多个业务规则迁入这个函数以规避模块边界。

**TypeScript：封闭状态与空值。**

~~~ts
type SaveOutcome =
  | { kind: 'saved'; revision: string }
  | { kind: 'conflict'; currentRevision: string }
  | { kind: 'rejected'; fieldErrors: Readonly<Record<string, string>> }

function outcomeKey(result: SaveOutcome): string {
  switch (result.kind) {
    case 'saved': return 'saved'
    case 'conflict': return 'conflict'
    case 'rejected': return 'rejected'
  }
  const unreachable: never = result
  return unreachable
}

// 保留0/false；只在合同规定的缺省条件下移除字段。
const hasValue = value !== undefined && value !== null
~~~

SaveOutcome仅用于已经有该revision合同的内部用例；不能据此给旧REST新增必填revision。开放服务器枚举另有unknown分支。UI文案由调用方映射i18n，不能把上述内部key直接显示给用户。

**Vue：只提交当前scope的结果。**

~~~ts
let sequence = 0
let controller: AbortController | undefined

async function reload(): Promise<void> {
  const current = ++sequence
  const scopeAtStart = readScopeKey()
  controller?.abort()
  const pending = new AbortController()
  controller = pending
  loading.value = true
  try {
    const result = await query({ signal: pending.signal })
    if (current !== sequence || scopeAtStart !== readScopeKey()) return
    data.value = result
  } catch (error: unknown) {
    if (current !== sequence || scopeAtStart !== readScopeKey()) return
    if (!isCancellation(error)) presentError(error)
  } finally {
    if (current === sequence && scopeAtStart === readScopeKey()) {
      loading.value = false
    }
  }
}

onScopeDispose(() => {
  ++sequence
  controller?.abort()
})
~~~

readScopeKey/query/isCancellation/presentError代表现有会话/领域API能力，不要求新建通用框架。实际组件还需在身份/scope变化时清理旧data/error/loading，并触发新的加载；只有Abort、不检查晚到结果的实现不符合CS-33/88。

**Python/Shell：数据不能成为命令。**

~~~python
# argv元素按参数传递；timeout取所属工具的既定预算。
result = subprocess.run(
    [executable, "--input", str(input_path)],
    check=True, capture_output=True, text=True, timeout=command_timeout,
)
~~~

Shell脚本使用目标解释器允许的数组或位置参数构造命令；不用eval执行外部文本。CI及仓库现有Python/Shell工具按受影响范围验证，不因补规范强制重写或新增脚本。

### 12.6 实施时的语言检查责任

| 范围 | 规则与最小证据 | 单一负责入口 |
| --- | --- | --- |
| Go新核心/领域 | CS-11至28、70至80；gofmt、定向测试，必要静态/并发检查 | WP-01及各后端包；共同装配由integrator |
| TS/API/查询 | CS-29至41、81至90；实际类型与竞态/错误行为 | WP-28及各前端领域包 |
| Vue/Pinia/样式 | CS-29至42、91至97；组件/i18n与受影响浏览器交互 | WP-29至35；共享设施/入口单写者 |
| SQL/部署与工具 | CS-43至56、98至100；对应真实合同与已有工具检查 | 原工作包及现行交付规则，不另建执行器 |

本节没有修改go.mod、package.json、锁文件、ESLint或TS配置。实际语言/依赖版本由WP-00在开工时读取清单、锁文件和工具身份，不能把本规范的版本号误当工具链版本。
