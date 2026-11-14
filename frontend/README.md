# sub2api frontend

前端由同一个应用入口装配领域模块，主站和供应商门户是功能角色构建。品牌显示来自部署注入及现有公开设置，不通过品牌分支、页面副本或客户端适配开关实现。

## 源码入口与所有权

| 位置 | 责任 |
| --- | --- |
| src/main.ts、src/app/bootstrap.ts | 唯一浏览器入口、启动顺序、装配失败回收 |
| src/app/router、src/app/navigation | 汇总领域路由及导航贡献、入口守卫、标题和预加载生命周期 |
| src/app/compositions | 跨领域界面与服务接线；保留当前后端的原子命令边界 |
| src/app/layouts、src/app/model | 应用壳、通知和只读控制台投影 |
| src/features/* | 领域 API/DTO、查询、命令、状态、页面、业务组件和翻译 |
| src/shared/http、src/shared/session | 唯一管理请求通道、匿名请求策略、刷新令牌及最小会话事实 |
| src/shared/config | 项目标识、中性品牌默认值、公开设置只读快照 |
| src/shared/ui、src/shared/lib | 无领域反向依赖的界面和技术机制 |
| src/i18n、src/app/i18n | 翻译运行时/公共文案及领域消息装配；业务消息由所属领域提供 |
| tests/acceptance | 项目完成并冻结后执行的完整页面/操作验收定义 |

十四个领域为 identity、keys、resources、access-policy、catalog、pricing、subscriptions、payments、usage、operations、jobs、supply、site、system。领域的具名公开出口是应用装配边界；不要恢复根 api/stores/types/components/views 桶、通配转发出口或跨领域运行时调用。完整依赖规则以 [前端架构](../docs/architecture-rebuild/FRONTEND_ARCHITECTURE.md) 和 [编码规范](../docs/architecture-rebuild/CODING_STANDARDS.md) 为准。

表格、弹窗和领域流程分别拥有自己的请求或命令生命周期。调用者捕获身份、目标和输入；取消只终止等待，不表示服务器撤销写入。晚到响应不能覆盖新的草稿、筛选或身份。设置聚合仍由单一 Workspace 草稿映射为一次现有 revision 条件更新；独立操作按各自真实 API 处理。

## 开发与构建

使用 package.json 声明的工具链及 pnpm-lock.yaml；日常开发不自动升级依赖。现有命令如下：

~~~sh
pnpm dev
pnpm lint:check
pnpm typecheck
pnpm test:run
pnpm build
pnpm build:vendor
~~~

lint 带 --fix，检查请用 lint:check。主站输出 frontend/dist，供应商门户输出 frontend/dist-vendor；两者均不写入后端源码目录。构建校验语言完整性和生产类型；领域改动先运行其现有定向测试，最终集成时再验证整体构建。

管理 API 地址与展示给用户的 api_base_url 分开。网站名称、Logo、文档及联系地址使用当前设置合同；不能把公开品牌链接用作携带管理令牌的请求目标。供应商 origin 和部署反向代理由现有部署合同决定。

翻译修改所属领域的源文案，再使用唯一的根 tools/zh-tw/gen-locale.mjs 生成繁体文案。业务消息通过领域 locales.ts 出口装配，勿重新汇总成一份业务全量词典。文档与下载工具源码归 site/public；修改下载工具时同步既有生成器产物与 SHA256SUMS，保留确有读取合同的历史数据格式。

## 协作与验收

统一分支 codex/sub2api-rebuild，当前共同工作树中的 frontend/** 由前端 owner 管理；后端、根部署/工具和共享架构记录由集成者管理。领域并行开发须声明不重叠写集，app/shared/路由装配由一个 owner 接线。最终 Git 归并由主集成者操作。

[完整前端测试用例](tests/acceptance/README.md) 是页面验收的唯一清单。单元测试、类型检查、静态边界检查和两种构建属于开发证据，不等于 Docker 逐页操作通过；验收结果绑定真实源码指纹、镜像、角色、页面和数据夹具。未执行或缺少真实外部依赖的条目保留 not_run/blocked，不填写 passed。

本文件是源码导航，不维护第二份工作包状态或验收结论。任务状态归 docs/architecture-rebuild/work-items.json，双方合同和联调条件归 [FRONTEND_HANDOFF](../docs/architecture-rebuild/FRONTEND_HANDOFF.md)。
