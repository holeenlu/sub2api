# 前端重构架构与实施边界

版本：1.0，2026-10-25。对齐总体架构、CT-08/11与BRANDING；本文件是前端结构与消费合同的唯一详细设计。当前为开发前方案，未替换运行代码。输入源码为b7b6a4402b3a82ba8b69308a31d07322cab01683；事实指纹见source-inventory.json。

## 1. 要解决的问题和明确选择

当前SettingsView为11,874行、CreateAccountModal为7,498行、GroupsView为6,902行、EditAccountModal为6,528行；router/index.ts为1,031行。src/features已存在vendor、prompt-audit、channel-monitor-v2，不能将现状描述为完全没有领域化。问题是领域仅局部成立，全局types/api/stores、巨型表单和应用启动仍共同解释权限、平台、配置与品牌。

保留Vue 3、TypeScript、Pinia、Vue Router、Axios、Vite、Tailwind与现有测试工具，不在结构重构中升级依赖。选择一个前端代码库、显式领域模块和薄应用装配层。主控制台与供应商门户是功能入口差异；品牌是运行设置。没有微前端、插件式页面加载器、万能JSON表单引擎、第二份请求框架或长期legacy/v2页面双轨。

重构单位是一个可工作的业务切片：领域API与状态、页面、路由接入、对应旧路径删除一起完成。减少文件行数只是结果；是否只有一个事实所有者、独立变更是否无需修改无关领域，才是结构验收依据。

## 2. 目标目录与依赖

~~~text
frontend/
  src/
    main.ts                         唯一浏览器启动入口
    app/
      bootstrap.ts                  注入设置、身份初始化、依赖装配
      App.vue                       根生命周期与路由出口
      router/                       组合领域route贡献、公共guard、旧URL映射
      navigation/                   从同一route元数据投影菜单
      layouts/                      public/user/admin/vendor壳
      compositions/                 仅跨领域页面与流程装配
        account-editor/             身份/凭据/能力/路由配置组合
        group-editor/               准入/资源/定价/订阅组合
        settings/                   分领域设置入口和导航
        dashboards/                 使用量、账务、健康只读组合
        vendor-portal/              供应商scope下组合资源、用量、结算
      lifecycle/                    登录/登出/换scope的统一销毁接线
    features/
      identity/                     登录、安全、个人资料、用户与角色管理
      keys/                         用户Key管理和使用指南
      resources/                    账号、凭据授权、代理与出口
      access-policy/                准入策略、资源范围与风险限制
      catalog/                      模型、能力、映射与模型广场
      pricing/                      价格方案、渠道定价与成本展示
      subscriptions/                订阅策略、分配、额度与用户订阅
      payments/                     订单、充值、购买、支付及回跳
      usage/                        使用记录、导出与只读用量指标
      operations/                   运行状态、监控、审计、提示审查
      supply/                       供应商关系、授权范围、往来结算
      jobs/                         既有批量图片等作业交互
      site/                         网站设置、首页、公告、文档、法律页面
      system/                       初始化、系统设置、插件与更新状态
    shared/
      ui/                           无业务含义的可访问UI基础件
      http/                         一个传输客户端、错误与取消机制
      session/                      最小身份快照读取接口及scope上下文
      config/                       公共启动配置消费、品牌只读投影
      lib/                          纯格式、URL/HTML安全等技术工具
      i18n/                         i18n引擎、共享UI文案与命名空间装配接口
      styles/                       tokens、基础样式与主题
      testing/                      确有跨域复用的测试工厂
    assets/                         中性公共资产；业务资产随feature
  public/                           中性启动资源
  index.html                        无品牌特判的启动模板
  vite.config.ts                    功能角色构建与既有嵌入产物
~~~

这是目标目录，不要求预建全部空目录。逐切片产生真实文件；没有业务归属的components、composables、stores、types、api、utils根桶最终退出。根main.ts等入口、assets、声明文件可以保留，不以机械目录整齐为目标。

~~~mermaid
flowchart TD
    App[app 入口 路由 装配] --> F[features 领域公开出口]
    App --> S[shared 技术基础]
    F --> S
    F --> API[服务端领域查询与命令]
    S --> Tech[Vue Axios 浏览器设施]
~~~

箭头表示允许的源代码依赖。shared不得导入features/app；feature不得导入app或其他feature的实现。跨域运行组合放app/compositions，由其导入各域公开出口；跨域DTO留在提供者领域，由装配者组装，不搬到shared/types。领域间若确有类型共享需求，先明确提供者和消费者，由合同owner登记最小只读类型依赖；默认不允许跨feature运行时调用，禁止通过全局event bus绕过。

领域页面不直接访问app store；需要的actor/scope、通知或导航能力通过已有Vue注入/props与显式回调获得。不要为每个函数定义抽象端口。只有技术层不能反向导入业务实现等真实边界需要注入。

### 2.1 一个领域内部

~~~text
features/resources/
  index.ts                          小而显式的公开类型/页面/组合面板出口
  routes.ts                         静态路由贡献，页面lazy import
  api/                              手写类型化请求；API DTO在此归属
    accounts.ts
    credentials.ts
    proxies.ts
  model/                            表单草稿、纯映射、领域UI状态
  queries/                          按scope组织的加载/失效/取消
  commands/                         提交动作与结果处理，确有复杂度才建
  components/                       资源域内部展示组件
  panels/                           供app组合的公开窄面板
  pages/                            单领域路由页面
  locales/                          领域命名空间，不回到全局大文件
  __tests__/                        按独立行为，亦可与被测文件邻置
~~~

小领域可使用api.ts/model.ts代替目录，不要求每域有store或commands。index不使用export *，不在导入时注册路由、开启轮询或加载所有页面。app通过routes.ts的类型化贡献组装，避免根barrel导致懒加载失效。

## 3. 领域归属表

| 领域 | 唯一拥有的事实与交互 | 当前输入 | 不承担 |
| --- | --- | --- | --- |
| identity | 登录/MFA/passkey/OAuth、当前身份动作、用户和权限配置 | views/auth、user/Profile、admin/Users、stores/auth | 资源凭据OAuth、供应商业务范围、支付账本 |
| keys | Key列表/创建/限制草稿、复制与客户端指南 | views/user/Keys、components/keys、api/keys | 重新计算权限或把API key当管理JWT |
| resources | 资源身份、CredentialGrant编辑、能力配置面板、代理出口 | components/account、views/admin/Accounts/Proxies、composables/use*OAuth | 调度资格算法、价格计算、自动模式回退 |
| access-policy | Group的准入/资源范围/安全限制编辑及风险配置 | views/admin/Groups/RiskControl、api/groups | 价格方案、资金豁免判定、供应商结算 |
| catalog | 模型/平台目录、能力声明和配置约束展示 | constants/platformCatalog、views/ModelPlaza、useModelWhitelist | 从目录缺项推断无权调用、宣称未知工具支持 |
| pricing | 定价方案、价格规则编辑与服务端报价显示 | views/admin/Channels、utils/pricing、usagePricing | 浏览器权威扣费、重复解释计价模型 |
| subscriptions | 订阅策略、授予、用户配额和有效性展示 | stores/subscriptions、admin/user Subscriptions | 轮询支付终态、定义余额豁免 |
| payments | 订单/支付生命周期、充值购买流程和支付回跳 | components/payment、stores/payment、views/*/orders | 以redirect参数确认已付款、供应商双边账本 |
| usage | 使用记录查询、导出、已知/未知用量与指标格式 | api/usage、usageExport、views/*/Usage | 修改账本、把unknown补零或按TTFT重算TPS |
| operations | 运行诊断、监控、审计、提示审查及只读健康 | admin/ops、AuditLog、features/prompt-audit/channel-monitor-v2 | 重建网关调度器、自动恢复/发送生成请求 |
| supply | 供应商上下级/授权、应收应付与结算 | features/vendor、views/vendor | 复制资源CRUD、普通用户支付规则 |
| jobs | 既有异步/批量图片的提交、进度、取消、产物 | views/user/BatchImageGuide、api/batchImage | 凭UI取消宣称后台任务结束、扩大通用Files/语音产品范围 |
| site | 网站可编辑品牌、首页/公告/文档/法律内容 | stores/app设置段、components/home/docs、content/docs | 容器名、provider地址、更新信任源 |
| system | 初始化、系统配置、插件管理、系统版本状态 | views/setup、admin/Plugins、Settings系统段 | 其他领域的全部配置和所有业务状态 |

页面角色不是领域。用户/admin/supplier对同一能力使用同一领域API与面板，actor/scope不同；管理对象写命令仍由后端鉴权。供应商专属上下级、结算主体和往来数据留在supply，不能为了组件复用把它们压成普通用户对象。

## 4. 应用组合与信息架构

控制台采用用户工作台、管理工作台、供应商工作台、公共站点四种layout；同一导航元数据定义route name、title key、所属域、shell和现有权限/功能可见条件。菜单、面包屑和页面标题消费同一贡献，禁止另写一份角色路径列表。

目标管理导航按资源供给、访问策略、模型与定价、用户与账务、运行观测、站点与系统组织。菜单分组是浏览体验，不能改变服务端授权；旧深链、OAuth/payment回调和文档URL先列实际消费者，再由app/router保留具名映射。URL别名不等于保留旧页面实现；有使用方的URL可以是正式稳定合同，移除条件记录在迁移表。

### 4.1 三个巨型编辑器

- AccountEditor：resources持有身份/授权草稿；catalog面板显示服务端能力及已允许组合；access-policy面板只编辑资源关联。app负责顺序、脏状态、提交/取消。Native/BPS/Cloud、平台、凭据方式、传输、操作能力分别表达；禁止让一个isOpenAI布尔值替代六个维度。资源授权和登录OAuth流程分别归属，通用弹窗/剪贴板可共享。
- GroupEditor：access-policy、pricing、subscriptions提供自己的面板与payload投影，app组装一次当前Group用例提交。第一阶段保留当前服务端原子保存合同，不在浏览器把一次保存拆成数个不可回滚HTTP请求。未来后端如拆领域命令，先定义组合事务/失败语义再换前端。
- SettingsWorkspace：将11,874行页面拆成站点品牌、身份安全、资源/目录、访问策略、价格/订阅/支付、监控、系统等独立section。每个section拥有已加载基线、字段级脏状态、校验和保存；后台支持安全分段更新前，由单一control映射器按既有接口合同提交。不得用旧的全量快照覆盖另一人改过的字段。

共享组合只传各域公开的draft/summary/panel属性与事件，不让子组件直接修改父对象或跨域store。由领域命令返回field errors、conflict、success；app把成功结果交回各域更新基线。不能把巨型页面等量搬进一个useAccountEditor或useAllSettings。

## 5. 状态归属与寿命

| 状态 | 所有者/范围 | 持久化与销毁 |
| --- | --- | --- |
| 身份与session epoch | identity负责登录/退出/刷新；shared/session只暴露最小快照读取合同，app装配 | 沿用已核实token存储与跨tab刷新，登出/换身份推进epoch并销毁私有状态；不擅改cookie模式 |
| PublicSettings/BrandView | shared/config唯一只读快照；site提交设置后刷新它 | 同部署的注入配置优先，网络更新有序；私有设置不写公开缓存 |
| 权限、scope、字段可编辑集合 | 服务端返回权威信息；前端只存当前有效投影 | 身份/门户/scope/policy变化使关联查询失效，撤销由后端最终执行 |
| 服务端列表/详情 | 对应领域query owner | 默认页面/流程内存；只有明确跨页面复用才放领域Pinia，禁止api+store+组件三份缓存 |
| 表单草稿 | editor/section组件或其领域composable | dirty baseline独立；离开提醒/成功刷新；凭据草稿不进入持久化偏好 |
| URL筛选/分页 | 路由查询参数与页面解析器 | 可分享且不含秘密；游标与过滤条件绑定，条件变化重置游标 |
| 列宽/主题/语言等偏好 | shared技术偏好或领域本地偏好 | 具名版本/命名空间，个人偏好按实际身份隔离；项目更名不直接换存储键 |
| 通知/浮层/布局 | app+shared/ui | 短生命周期；不是业务事件总线 |
| 支付轮询/作业进度/观测订阅 | 各自领域的流程owner | 卸载/换身份/scope取消订阅与计时器；后台任务继续与否由服务端事实决定 |

查询身份至少包含有效session epoch、portal role、显式业务scope、资源ID与规范化过滤条件；只有确实影响数据的locale/policy revision才入key。使用稳定逻辑session标识，不能把每次access token轮换当换用户，更不能将原始token写入key/log。公共查询与匿名Key查询有独立范围，不能因query key同名共享私有结果。

先取消旧请求，再用请求序号和捕获的epoch/scope检查晚到结果；Abort本身不能保证旧响应不会到达。所有退出路径释放loading，取消不显示业务失败toast。不创建通用全局查询缓存框架；复用当前useTableLoader等行为，迁入无业务依赖的机制，其业务参数和失效由领域定义。

突变默认等待服务端确认。订单、扣费、资源撤销、权限和凭据不做乐观成功；普通偏好可乐观更新但明确回滚。成功后只失效本域和app显式列明的相关投影，不全站refetch。跨领域失效由组合者调用公开refresh/invalidate，不用字符串event bus。

## 6. API和控制面消费合同

### 6.1 一个请求通道

shared/http接管现有client、tokenRefresh与adminUIRequest有效行为，不重写成第二套fetch/Axios客户端。identity维护认证动作并复用shared/http中的旋转令牌协调机制；app把读取身份、调用刷新、会话失效处理注入transport，避免shared反向import领域store。刷新请求走同一设施内显式无refresh拦截路径，防递归；同一逻辑session的single-flight和跨tab旋转令牌保护保留。

每个领域api模块集中endpoint、DTO、最小响应归一化与错误映射；组件不拼URL、不读token、不直接调用axios。request context可表达scope、signal和请求关联，不能用可任意扩张的全局bag携带所有业务状态。管理UI认证、公开API Key、供应商凭据和第三方支付SDK按已知边界分开，不能跨origin转发Authorization。

运行时管理API地址使用已有部署合同；api_base_url仅供用户复制和文档示例。公开Logo/doc/contact URL按既有URL安全规则验证，不反向修改管理API、provider目标或更新源。品牌展示不得在未加载时闪出旧KDAN等fallback。

### 6.2 查询、命令与错误

查询返回服务端视图；命令提交明确字段与目标。DTO归领域api，UI draft归该领域model；语义相同直接复用类型，不机械建立DTO/domain/view三份等价对象。确有历史字段、单位或snake/camel转换只在一个边界映射并列退出依据，禁止页面各自normalize。

默认不引入OpenAPI代码生成、query库或新的schema依赖。若当前缺失的CT-11字段必须新增，由WP-20/合同owner先核定服务端输出和真实调用者；前端不得伪造完整capability/revision，也不把目标设计写成已有接口。

| 情况 | UI行为 | 禁止 |
| --- | --- | --- |
| 401且当前session可刷新 | 现有单次受控刷新；只按当前有效身份决定能否恢复请求 | 切换账号后重放旧mutation；无限401循环 |
| 403/字段不可编辑 | 保留可读数据与明确拒绝，必要时刷新权限投影 | 自动换身份或隐藏错误当成功 |
| step-up/合规确认 | 现有有界流程、确认范围与目标动作绑定 | 全局自动确认或缓存任意操作授权 |
| revision conflict | 展示新旧差异，重新加载并由用户合并脏字段 | 反复无条件覆盖、静默last-write-wins |
| 验证失败 | 领域字段定位，保留草稿 | 仅弹全局toast后丢失输入 |
| 超时/断网/提交结果不明 | 提示状态未知；有查询ID则查服务端结果 | 自动再次提交付款/生成/配置命令 |
| 429/503或GET可重试 | 使用既有明确策略、有界且可取消 | Axios统一重试所有方法 |

Abort仅取消等待/传输，不代表服务端撤销命令。只有现有幂等合同允许的请求才可重放；请求身份或内容变化不能沿用原幂等标识。分页空数据、加载、失败、权限不足、未配置、未知事实必须分别展示。

### 6.3 与网关六个维度对齐

前端展示client protocol、provider、credential kind、execution mode、transport、operation capability；组合是否有效由后端CT-08/11决定。unknown capability不是unsupported；未知工具不得标支持；资源已存在不表示当前用户可访问。不把OpenAI平台等同Native，不把客户端WS等同上游WS。

观测视图如服务端提供operation/attempt/session/billing root，按来源关联展示，不用前端生成ID补齐。发送状态、交付状态、用量可信度和账务状态分栏，不压成一个success标签。Confirmed lower_bound显示已确认下界及既有结算状态；unknown显示未知，不补0。TPS继续使用已有raw duration口径；价格、余额豁免与费用只显示服务端权威结果，必要估算必须明确为估算。

### 6.4 本轮源码核实的保留合同

以下为主代理读取当前源码及测试得到的迁移输入，测试本轮未执行。保留的是有效行为；当前全局store导入或DOM事件的组织方式应在新owner中替换。

| 已核实行为 | 新归属与迁移约束 | 证据 |
| --- | --- | --- |
| 刷新失败为网络/429/5xx时保留登录态；相同用户重新登录也是新session，旧响应不能接纳；同session正常轮换可继续 | identity处理登录生命周期，shared/http保留single-flight/peer rotation与取消判别；不能将暂时不可用当作凭据失效 | [client](../../frontend/src/api/client.ts)、[tokenRefresh测试](../../frontend/src/api/__tests__/tokenRefresh.spec.ts) |
| 管理403刷新权限后重新核对session；step-up仅针对原session受控重试一次；423合规确认不清登录 | typed结果交app装配identity/system明确流程，旧事件语义有对应消费者后删除CustomEvent耦合；禁止把所有403当可重试 | [client](../../frontend/src/api/client.ts)、[client测试](../../frontend/src/api/__tests__/client.spec.ts) |
| Admin/User UI标记、locale、GET timezone及withCredentials有现有消费者；供应商角色制品使用同源API | shared/http保留头部精确适用范围，显式timezone优先；不把所有请求或公开支付都打同一标记，不由网站API展示链接更换认证目标 | [adminUIRequest](../../frontend/src/api/adminUIRequest.ts)、[client测试](../../frontend/src/api/__tests__/client.spec.ts) |
| supplier origin含scheme/port精确匹配；超管可进入platform而无需冒充supplier；二级供应商不能管理children | app/router消费当前role与capability；supply拥有业务scope规则，resources接收已确定scope，但后端仍逐对象验证 | [portalGuard测试](../../frontend/src/features/vendor/__tests__/portalGuard.spec.ts)、[scope测试](../../frontend/src/features/vendor/__tests__/scope.spec.ts) |
| vendor_id=0仅明确安全aggregate；children仅只读；scope最后写入防旧query覆盖；admin更新PUT、vendor更新PATCH有区别 | 资源API集中拥有角色端点映射，不能将所有请求机械合成一种方法；0/false保留，空可选filter依现有规则省略 | [vendor API](../../frontend/src/features/vendor/api.ts)、[scope测试](../../frontend/src/features/vendor/__tests__/scope.spec.ts) |
| backend-only模式保留pending-auth注册/验证与公开payment回调例外；微信恢复优先opaque token，并清除URL恢复参数 | identity/payments分别定义真实流程，app只组合guard；旧回调/恢复格式核实调用者后再决定退出，不能因含legacy名称直接删除 | [router guards](../../frontend/src/router/__tests__/guards.spec.ts)、[微信恢复测试](../../frontend/src/views/user/__tests__/paymentWechatResume.spec.ts) |

## 7. 共享准入清单

| 可进shared | 留在领域或app |
| --- | --- |
| Button/Dialog/Input/DataTable外壳、无业务列定义的分页与选择 | AccountTable、ProviderBadge、BalanceCard、GroupSelector |
| URL/HTML安全、日期/数字基本格式、剪贴板、技术debounce | usage定价、调度额度、BPS默认值、供应商层级规则 |
| HTTP取消/错误形状、最小session读取接口 | endpoint DTO、角色路径表、密码流程和业务授权判断 |
| i18n实例、主题tokens、公共配置只读投影 | 各域locales、可编辑网站设置、业务公告状态 |

共享需要语义相同、确有两个独立消费者、无领域反向依赖；仅出现两次不自动抽取。第一次复用优先props/event和小纯函数；不同业务生命周期不能通过十几个布尔props塞进一个万能组件。公共类型命名体现含义，不再建立types/index.ts汇总所有业务实体。

UI基础件明确键盘、焦点圈/焦点恢复、label/错误关联、禁用态、读屏和窄屏布局；大表沿用现有虚拟化能力按实际瓶颈采用。品牌色与中性主题tokens分离，状态色保留可辨识语义。业务文案使用领域命名空间，禁止拼接翻译句子；i18n迁移保持现有语言集合与缺键检测。

## 8. 路由 启动与资源寿命

启动顺序：加载服务端注入公共设置或中性默认 → 初始化品牌/主题/i18n → 装配唯一请求设施 → 恢复身份并确认当前权限/scope → 注册角色入口与路由 → 挂载。配置失败显示可恢复状态，不因为缓存缺失泄漏旧品牌或默认授予权限。公共页不必等待与其无关的私有查询；登录前后只启动当前壳需要的订阅。

app/bootstrap只调用明确服务，不能吞并订阅、支付、公告和运营规则。各域公开start/stop仅在确有长期任务时提供，app负责连接身份/scope变化与销毁；单页查询由组件scope管理。HMR、重复挂载、登出均应可幂等停止，禁止模块import即启动定时器。

供应商origin/角色、backend-only模式、待完成认证、公共payment回调和setup重定向按现有有效合同集中组合guard。route meta是UI元数据，不携带秘密，也不替代服务器scope校验。VITE_VENDOR_PORTAL若继续作为功能产物选择，只区分功能角色；不同品牌不增加构建分支。文档/页面懒加载保留，不引入微前端运行时。

## 9. 迁移和旧实现删除

| 切片 | 旧来源 | 目标owner | 同次删除/保留依据 |
| --- | --- | --- | --- |
| 基础装配 | main/App、router、stores/app、api/client/tokenRefresh | app与shared；identity负责认证动作 | 替换重复初始化/refresh/cache；保留稳定存储键和正式深链 |
| 身份与Key | views/auth、stores/auth、components/auth/keys | identity、keys | 旧store和根API出口在全消费者迁走后删，MFA/OAuth回调合同保留 |
| 资源编辑 | Create/Edit/BulkEditAccount、Accounts、Proxies、OAuth composables | resources+app/account-editor | 同页切换旧表单/模式特判/props/专属文案和测试全部处理 |
| Group与目录 | Groups、Channels、ModelPlaza、平台常量 | access-policy/catalog/pricing/subscriptions+app/group-editor | 消除重复能力/价格解释；已有公开Group接口不因内部拆分失效 |
| 财务与使用量 | payment/subscription stores、Usage/Orders、usage helpers | payments/subscriptions/usage | 只保留一种查询/轮询/指标口径；历史账务字段映射有来源 |
| 供应商 | features/vendor、views/vendor | supply+app/vendor-portal | 资源与用量共用所属域投影，供应商scope/结算独立；原v2别名不永久并存 |
| 观测与作业 | ops、prompt-audit、channel-monitor-v2、BatchImage | operations、jobs | 按实际已启用调用者收敛旧/新monitor，不按名称盲删独立能力 |
| 网站与系统 | Settings、home/docs、config/brand、i18n全局文案 | site/system/shared/config+app/settings | 网站设置可配置；无关广告及专属资产退出，LICENSE/NOTICE保留 |

每个切片合入前记录旧文件、调用者、API/持久化字段、可删测试和保留理由；提交路由切换时同步撤掉旧执行路径。开发分支可短暂出现未接入文件，但不以此宣称切片已完成。不能增加永久useLegacyX/newX开关、全局兼容导出或同功能双路由作为完成状态。

新前端必须等待其消费的控制合同可用；不得把缺失的服务端领域规则临时复制到浏览器。先做基础与身份/Key纵向切片，再并行领域，集成组合页并收口全局桶；发布切换依MIGRATION进行，不执行数据库/生产操作。回退以既有制品/配置和数据合同为依据，不保留双引擎长期运行。

## 10. 并行开发与验收

工作包状态与精确写集只在work-items.json维护；WP-21调整为前端最终装配/切片清理的单一集成owner，前端基础与领域实现拆为WP-28至WP-35。共享app/router、HTTP/session、配置、styles、i18n装配、包/锁/构建文件不得由多个领域worker同时写。WP-28基础按可编译合同和定向机制测试验收，其运行入口切换与WP-29身份/Key纵向切片一起完成；不单独发布未接入的新架构。领域worker在本目录交付实现、公开出口、route/i18n贡献和旧路径删除建议，集成者逐切片接入及删除，不能等所有模块完成才第一次接线。

最小验证覆盖FR-01至FR-12对应AT：依赖方向/公开出口；scope与晚到响应；跨tab刷新与退出；配置冲突；供应商/RBAC/step-up；支付/异步任务状态；六维能力和unknown/lower_bound；旧路由与清理；品牌首屏/URL隔离；键盘/窄屏/i18n；懒加载/资源释放；完整纵向切片。实际场景与证据级别见VALIDATION。

迁入目录使用现有ESLint能力逐步约束import边界，TypeScript检查生产类型，Vitest验证独立行为，既有i18n检查覆盖文案；不能声称这些边界检查已经启用。构建只在共享入口/懒加载/角色产物受影响时执行，检查主站与vendor功能产物，不按品牌重复构建。浏览器交互验收与mock分别记录；本次仅文档静态检查。

### 前端设计规则索引

| ID | 规则 | 章节 |
| --- | --- | --- |
| FR-01 | app/features/shared单向依赖、公开出口及无全局桶 | 2、7 |
| FR-02 | 领域事实唯一owner，角色不复制业务实现 | 3 |
| FR-03 | 巨型编辑器按领域草稿和原子用例拆分 | 4 |
| FR-04 | 会话/scope/请求代际隔离，退出清理 | 5 |
| FR-05 | 单一HTTP、刷新、错误与命令重放边界 | 6.1、6.2 |
| FR-06 | 六维能力与真实执行/用量状态 | 6.3 |
| FR-07 | 服务端权限/字段/scope权威，前端只做展示 | 3、6、8 |
| FR-08 | 配置revision与脏字段保留，不造新公共合同 | 4、6.2 |
| FR-09 | 设置驱动品牌、角色制品与URL隔离 | 6.1、8 |
| FR-10 | 同一导航元数据、可访问UI、领域i18n | 4、7、8 |
| FR-11 | 切片接入即清理，不保留长期兼容实现 | 9 |
| FR-12 | 明确写集、单一集成者、证据分级 | 10 |

FR是本设计的可追踪索引；语言/风格仍以CODING_STANDARDS的CS为唯一来源，品牌以BRANDING的BR为准，服务端语义以CT为准。不要把同一规则在多个文件复制出不同版本。
