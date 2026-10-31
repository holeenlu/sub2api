# 完整测试执行说明

## 1. 开始条件

完整验收必须等前后端实现完成后开始。先记录共同分支的源码快照和依赖/工具链，再由集成者从同一份输入构建主站、供应商门户及 API 镜像。不能用 commit SHA 代替未提交工作区的文件摘要。

开始时核对：

- 没有仍在写入本次验收范围的协作者；已交接的文件和哈希一致。
- 新实现已接入，旧路径退出；类型、lint、架构、翻译和受影响回归无未决失败。
- 真实路由清单与用例目录相符；新增/移除路由不能静默漏测。
- 准备好 FIXTURES.md 的角色与数据；同一 origin 的账号切换通过真实登录，不直接改 store。
- UI/API 镜像、配置、浏览器及测试数据均有可追溯标识。
- 对供应商 HTTPS、外部 OAuth/支付/上游、邮件、对象存储、WebAuthn及人工动作列出实际可用条件。

条件不足不会让用例自动通过。能独立执行的部分继续；被阻塞的用例准确记录缺少的条件。

## 2. 执行顺序

| 批次 | 工作 | 必须留下的证据 |
| --- | --- | --- |
| A | 类型、lint、架构、翻译、下载包、路由覆盖 | 完整命令、退出码、源码与输入摘要 |
| B | 全部 Vitest；必要的负向类型与合同检查 | JSON结果、失败/跳过/未捕获异常清单，不能只报assertion数量 |
| C | 实际 Docker 主站/API 安装、登录、角色和品牌冒烟 | 镜像ID、版本清单、实际浏览器画面和请求结果 |
| D | 逐页进入/刷新/返回、业务动作、设置面板、redirect和alias | cases.json 中每条用例的独立执行记录 |
| E | 跨角色、取消/失败/重试、未知结果、并发和迟到响应 | 操作前后读取证据、请求次数、目标与session归属 |
| F | 四语言、响应式、无障碍、首屏与生命周期 | 实际环境、viewport、截图/无障碍树及真实指标 |
| G | 获准的外部集成与人工步骤 | 真实sandbox/provider回执；不得使用替身结果代替 |
| H | 缺陷修复复测、影响回归、清理及最终核验 | 缺陷关联、重测记录、剩余阻塞和实测资源清单 |

开发期间只运行必要的定向回归。完整验收开始后，如生产源码、依赖、生成输入或环境改变，按实际影响重测；没有变化的有效证据可以复用，但须保持可核验的输入关系。

## 3. 既有自动化命令

以下是执行说明，不是已执行回执。先把 NODE 指向本机已批准且与冻结环境一致的 Node，将 EVIDENCE_DIR 指向本次隔离证据目录；不要使用会隐式安装依赖的全局包装器。

在 frontend 目录执行：

~~~sh
"$NODE" node_modules/vue-tsc/bin/vue-tsc.js --project tsconfig.json --noEmit
NODE_PATH="$PWD/node_modules/.pnpm/node_modules" "$NODE" node_modules/eslint/bin/eslint.js . --ext .vue,.js,.jsx,.cjs,.mjs,.ts,.tsx,.cts,.mts
"$NODE" node_modules/vitest/vitest.mjs run --maxWorkers=4 --minWorkers=1 --reporter=json --outputFile="$EVIDENCE_DIR/vitest.json"
"$NODE" ../tools/zh-tw/gen-locale.mjs --check
"$NODE" ../tools/zh-tw/audit-ui.mjs
~~~

在仓库根执行下载包验证：

~~~sh
PYTHONDONTWRITEBYTECODE=1 python3 tools/build_docs_downloads.py --check
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest -v tools.tests.test_docs_downloads tools.tests.test_image_skills tools.tests.test_session_repair tools.tests.test_claude_session_recovery tools.tests.test_session_recovery_commands
~~~

使用现有 deploy/local/build-ui.py 和部署模板完成镜像构建，由共同任务的集成者统一操作 Docker。不得把主机测试日志写成容器内测试结果，也不能把 nginx 运行镜像当作已经执行了 Node 单元测试。浏览器端到端必须实际访问记录中的 Docker UI/API。

## 4. 单条用例执行协议

1. 选择用例与角色，读取公共defaults和用例差异，记录 run_id、case_id、variant_id、用例目录摘要及实际环境。
2. 根据 route-inventory.json 绑定路径参数、角色和 feature gate。拒绝/重定向负例验证阻断行为，不强行进入不存在的控件。
3. 准备独占合成数据，记录请求路径、实际解析路径及参数fixture引用。OAuth code/state、Key等敏感参数只保留安全引用，不写实际值。
4. 按步骤操作真实界面；每一步观察可见状态和必要请求事实。点击后不能以“已尝试”代替结果。
5. 写操作重新读取验证；区分取消、成功、明确失败、未知结果和部分保存。不能只根据toast判断。
6. 填写每一步的 actual、status、evidence_refs；用variant_id绑定具体entry/panel变体，并记录角色、策略阶段、语言、viewport和浏览器。证据要能说明目标及对应环境。
7. 按用例清理。未知操作先核对，不以清除幂等记录或刷新新revision规避问题。
8. 把结果写入本地证据目录，源码仓库只保留模板及不含秘密的汇总。

设置用例区分两种保存方式：标为 aggregate 的面板通过工作区单次 CAS 保存；标为 independent 的面板沿自身真实 API 操作。不能把多个独立请求称为一个原子事务。只读/无页面权限角色应验证控件和写入被正确拒绝。

## 5. 状态和缺陷

### 并行执行边界

按领域分配用例和独占 fixture；可并行的只读页面使用已经验证隔离的浏览器存储环境。两个 tab 并不等于两个 session，不能无意共享令牌后把串号当作产品缺陷。专门的跨tab用例才共享同一登录存储。

设置、角色策略、全站品牌、安装/备份恢复以及未知结果核对由一个测试执行者顺序控制，避免其他用例同时改变全局状态。每位执行者写独立的用例结果文件；主集成者汇总，不多人覆盖一份结果JSON。源码、镜像或fixture策略变化要标记受影响结果，不能混合多个快照的通过数。

### 结果状态

| 状态 | 含义 |
| --- | --- |
| not_run | 没有执行，或当前源码输入已使旧证据失效且尚未重测 |
| running | 已开始，尚无可核验结论 |
| passed | 本条全部适用步骤和预期都有同一有效输入下的证据 |
| failed | 有实际行为不符合合同，必须关联缺陷和复现材料 |
| blocked | 无法执行必要步骤，明确记录具体缺失条件及已完成部分 |
| not_applicable | 经审阅确认当前明确产品范围不适用；必须写原因和审阅人 |

将权限配置为禁止、隐藏菜单或缺少数据，不能自动把原本要求覆盖的功能改成 not_applicable。跳过、todo、文件加载失败、未捕获异常、超时和外部依赖缺失都应单列，不能只凭 passed assertion 数量判断整批通过。

缺陷记录应包含：用例编号、源码/镜像、实际角色和scope、复现步骤、预期/实际、脱敏证据、影响面、修复位置、定向复测与必要影响回归。数据泄露、越权、错误账务、重复突变和不可恢复的数据损坏属于最高优先级处理范围。

## 6. 最终判定

- 汇总当前目录中每条适用用例；统计 passed/failed/blocked/not_run/not_applicable，并能逐条追溯。
- 页面、设置面板、redirect、alias、角色与环境矩阵没有漏项。
- 所有必须执行的用例通过，缺陷已复测；外部或人工必测项缺证据时仍然不能标整体验收通过。
- 实际来源摘要与最终准备交付的源码/镜像一致；若仅文档变化不影响已测输入，明确记录复用依据。
- 清理本任务可丢弃资源，保护源码、SDK、共享缓存和其他协作者资源；保留项列明原因与实测大小。

最终提交、推送、发布、生产部署和迁移仍服从共同任务的现行交付规则；测试用例不单独授予这些操作权限。
