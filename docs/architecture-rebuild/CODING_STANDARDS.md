# Sub2API 详细编码规范

规范版本：1.1。日期：2026-10-25。适用：本次重构及其后续新增、实际修改的 Go / Vue / TypeScript / 数据访问与测试代码，三品牌共用。业务重构尚未开始，本轮只制定规范及接入开发资料，不修改运行代码、lint/编译配置或依赖。

本文件是语言级编码、文件组织及审查标准的唯一来源。业务行为以[需求](REQUIREMENTS.md)、模块边界和生命周期以[契约](CONTRACTS.md)为准，顶层布局见[架构](ARCHITECTURE.md)，授权与交付仍遵循[AGENTS](../../AGENTS.md)和[交付规范](../CHANGE_DELIVERY.md)。规范不替代实际客户端、协议和历史数据合同。

## 1. 规则等级与执行方式

未标为“建议”的规则均用于新增和本次实际修改代码的审查。既有代码不能仅因为不符合新风格就全仓重写；迁移切片内失效实现的删除仍按RETIREMENT执行。

检查标记：**F** 格式化，**G** Go静态检查，**T** TypeScript类型检查，**E** ESLint，**U** 定向单元/组件/替身测试，**I** 必要的真实依赖/多实例/上游验证，**R** 代码审查。标记表示验证方式，**不表示工具已经完整实现该规则，也不表示本轮运行通过**。

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

### 2.2 前端feature内部组织

~~~text
frontend/src/features/<feature>/
  api.ts             调用已有共享HTTP客户端的端点封装
  contracts.ts       该feature的外部DTO或共享DTO的明确入口
  model.ts           UI状态/视图模型；确有差异才建立
  components/        该领域独有组件
  composables/       useXxx.ts，拥有明确副作用和销毁责任
  views/             页面编排
  store.ts           确有跨页面共享状态时才创建
  __tests__/         同领域组件、请求和状态测试
  index.ts           必要的少量公开导出；不默认export全部内部模块
~~~

单文件即可表达的feature不制造完整骨架。已有features/vendor、全局认证、brand、共享HTTP客户端和基础UI组件优先复用。公共router/API/type出口按COLLABORATION交由单一集成者处理。

| ID | 规范 | 检查 |
| --- | --- | --- |
| CS-05 | 核心model/protocol/execution不导入Gin、Ent、Redis客户端或具体provider；具体设施经端口注入。新包禁止反向依赖旧service万能实体；确需过渡转换只放集成边界并登记退出条件，不得流入核心或长期保留。 | G部分/R |
| CS-06 | 接口通常由消费者定义；跨模块真正共享的值对象归model/合同owner。不为每个struct生成对应interface，不使用I前缀或空泛BaseService接口。 | R |
| CS-07 | 类型和方法名称表达责任与行为：业务ID保持明确含义，Do/Handle/Manager等泛名仅在已有明确上下文中使用。同一方法不能直接实现多个领域的具体规则；CT定义的主编排可以依次调用各owner端口，但不复制其规则，也不为形式拆分制造空转发层。 | R |
| CS-08 | feature可依赖共享基础设施和其他feature明确公开的能力，不能深层导入别的feature内部组件/store。路由壳只在实际需要时保留，迁完不留下多层空转发。 | T部分/R |
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
| CS-57 | Go测试与被测包同目录并沿现有unit/integration/e2e标签；TS沿现有__tests__/xxx.spec.ts等约定。优先扩展现有测试，不为命名统一搬动无关测试。 | G/T部分/R |
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

现有命名、any/ts-comment、feature边界和新核心依赖的缺口先由规则审查与相应行为测试承担。WP-01负责新核心边界与合同，WP-21负责前端领域迁移时必要的类型/lint补齐，WP-24核对旧规则退出；只有实际实施这些任务后才更新“已自动覆盖”状态。本次不修改这些工具配置，也不追溯性开启全仓新门禁。

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
