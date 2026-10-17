# 独立供应商站入口与 404 排查

## 当前入口

供应商域名的 `/vendor/platform` 是超级管理员工作区，一级、二级仍使用同域名 `/vendor/accounts` 等控制台页面。客户运营站侧栏不再显示供应商平台；旧 `/admin/vendors` 书签返回客户运营首页。供应商身份与采购业务从独立供应商站进入。

后端 `/api/v1/admin/vendors` 并未废弃：供应商站的超级管理员仍通过它管理供应商。不要把旧前端页面 `/admin/vendors` 与该 API 混淆，也不要将超级管理员请求改成一级供应商接口 `/api/v1/vendor/vendors`。

## 2026-10-17 线上只读诊断

未携带凭证的请求得到以下结果；未修改生产配置或数据：

| 域名 | 路径 | 响应 |
| --- | --- | --- |
| supplier.buildtoconnect.com | `/api/v1/settings/public` | 200 JSON，供应商开关开启、Origin 与该域名一致 |
| supplier.buildtoconnect.com | `/api/v1/vendor/me` | 401 JSON，已进入后端鉴权 |
| supplier.buildtoconnect.com | `/api/v1/admin/vendors?page_size=20` | 404，Caddy 返回纯文本 `Not found` |
| supplier.buildtoconnect.com | `/vendor/platform` | 404，Caddy 返回纯文本 `Not found` |
| nextcode.buildtoconnect.com | `/api/v1/admin/vendors?page_size=20` | 401 JSON，接口存在并要求鉴权 |

证据将问题定位到供应商域名的入口路由，而非供应商列表为空或用户角色不足；未读取正在运行的 Caddy 配置，不能据此断言具体哪一条 matcher 写错。截图中的已加载 SPA 也不能证明页面直链或刷新可用。

## 配置修正

仓库原有 [Nginx 示例](../../deploy/examples/vendor-portal.conf) 不能直接作为 Caddy 配置。Caddy 入口使用 [供应商站点示例](../../deploy/examples/vendor-portal.Caddyfile)，将其站点块加入现有客户站点旁边，并替换域名、静态文件目录和后端地址。保留原客户站点配置。

1. 所有 `/api/v1/*` 同源转发至同一已升级后端，保留完整路径、查询参数和供应商 Host；不能使用会剥离前缀的 `handle_path`，也不能只转发 `/api/v1/vendor/*`。
2. `/api/v1/admin/vendors`、供应商范围内的账号/代理、分组选择和合规确认都由后端 `VendorOrigin` 统一白名单处理，再执行原有实时角色、归属、只读及合规校验。不要在代理层复制一套容易过期的业务 API 白名单。
3. `/vendor/platform` 和其他控制台页面均回退到供应商构建的 `index.html`；缺失资源文件和非版本化 API 保持 404。HTML 使用 no-store。
4. 不改变 `VITE_API_BASE_URL` 指向客户域名，不跨域传递 Token，不放宽 CORS，不新增数据库或 Redis。

部署步骤：先升级全部后端实例；用现有 `pnpm build:vendor` 生成静态文件；备份生产 Caddy 配置，按示例修正后执行 `caddy validate --config <实际配置> --adapter caddyfile`，再按独立部署授权重新加载。仅提交代码不会改变线上 Caddy。

验证入口应包括：未登录请求返回后端 JSON 401（不是边缘纯文本 404）；超管登录后列表成功加载；一级/二级仍不能调用超管 API；账号/代理保持真实归属限制；直接打开和刷新 `/vendor/platform` 可用；客户站旧菜单消失、旧书签回到运营首页。未登录 401 只证明转发链路，不等于登录后业务验收。

本地代理回归：`CADDY_BIN=/path/to/caddy python3 -m unittest discover -s deploy/tests -p test_vendor_portal_caddy.py -v`。该测试启动临时 Caddy 与本地测试上游，检查路径、参数、Host、SPA 刷新和错误响应不被 HTML 替换；后端权限另由已有 VendorOrigin / supplier-role 测试覆盖。

## 本轮验证

- Caddy 2.10.2 实际代理回归：3 项通过，覆盖 20 个请求场景；现有主站缓存/流式策略检查通过。
- 前端侧栏、供应商路由守卫和超管工作区：3 个文件、28 项通过；类型检查及改动文件 ESLint 通过。
- Go 中间件 `TestSupplier(Origin|Role)` 回归通过，供应商域名/API 白名单、供应商角色拒绝、预检和合规确认边界保留。首次运行依赖下载遇到 EOF，重试后通过。
- 主站与供应商前端生产构建均通过，保留已有大包提示。
- 未部署生产 Caddy 或静态文件；未进行线上登录后的业务验收。以上修复验证不能表述为线上故障已恢复。

## 2026-10-18 复查与页面调整

供应商根地址 `/` 已返回 200 HTML，公开设置仍为开启且 Origin 正确。但 `/api/v1/admin/vendors?page=1&page_size=20`、`/api/v1/admin/accounts?vendor_id=0` 和 `/api/v1/admin/groups/all` 仍是 Caddy 纯文本 404；相同供应商列表 API 在客户域名返回后端 JSON 401。页面能打开并不代表超管管理 API 已正确转发。

若现有 Caddy 已从后端提供前端 HTML，可以保留当前前端托管方式；关键修正是在供应商站点的拒绝规则之前，将完整 `/api/v1/*` 转发到原共享后端，保留 Host、路径及查询参数。不能把后端 API 的 `/api/v1/admin/*` 与应禁用的旧前端 `/admin/*` 混为一谈，也不需要另建后端、数据库或 Redis。现有静态前端部署则直接采用本文 Caddy 示例。

正式页面移除每个超管菜单上重复的供应商平台介绍、权限边界、结算查询范围和数据留存大卡片，保留菜单自身标题与真实权限校验。同时修正重试按钮：供应商列表、分组目录和指定供应商详情独立记录加载/错误状态；重试重新请求实际失败项，后端仍失败时继续显示错误，不能清空提示假装恢复。

生产 Caddy 与前端尚未在本轮更新。按项目 AGENTS.md，生产部署属于独立授权范围；本地修改或提交推送都不能作为线上已恢复的证据。

本轮验证：超管页面与路由 26 项测试、Caddy 三项测试（24 个请求场景）、类型检查、ESLint 和主站/供应商站构建均通过。业务代码 +11/-31 行，测试 +53/-1 行；没有新增接口、运行配置、迁移或独立业务执行路径。
