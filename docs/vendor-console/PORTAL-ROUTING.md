# 独立供应商站入口与 404 排查

## 独立门户镜像（2026-10-19）

结构为：供应商浏览器 → 门户容器（静态页面 + 同源 API 代理）→ 现有运营/网关后端 → PostgreSQL/Redis。门户可与后端同机，或位于 A、后端位于 B、存储位于 C/云服务。门户不运行 Go 后端、后台任务或迁移，不配置数据库/Redis 连接凭证、JWT 签名密钥或 TOTP 加密密钥。

| 路径 | 职责 |
| --- | --- |
| frontend/src/features/vendor、现有路由及共享组件 | 唯一页面实现；build:vendor 输出 frontend/dist-vendor |
| deploy/vendor-portal/Dockerfile | 独立镜像；默认从源码构建，release target 复用已构建产物 |
| deploy/vendor-portal/entrypoint.sh、nginx.conf.template | 启动时校验配置、生成代理配置；非 root、只读根文件系统 |
| deploy/docker-compose.vendor.yml | A 单独运行或叠加使用外部存储的后端，不创建后端或存储 |
| deploy/docker-compose.vendor-local.yml | 与整套后端同机时追加，加入既有后端网络 |
| deploy/docker-compose.vendor-tls.yml | A 没有 HTTPS 入口时的可选 Caddy 层 |
| deploy/docker-compose.standalone.yml | B 连接 C/云存储的现有应用配置 |

### 三个地址分别配置

| 地址 | 含义 |
| --- | --- |
| 现有后台 api_base_url | 运营/网关对外地址，用于“使用密钥”和回调建议；含义不变 |
| PORTAL_PUBLIC_ORIGIN | 门户公开 origin，例如 https://supplier.example.com；与后台 vendor_portal_origin 完全一致 |
| BACKEND_ORIGIN | 门户连接现有后端的 origin；同机例如 http://tokensavy:8080，跨机使用 B 私网地址或 HTTPS 入口 |

部署前在现有运营后台启用 vendor_console_enabled，并将 vendor_portal_origin 配成 PORTAL_PUBLIC_ORIGIN。供应商账号仍由既有超管/上级供应商按权限开通；门户镜像不会自动开户或放宽授权。

两个门户 origin 必填，只接受 HTTP(S) origin，不接受账号密码、路径、查询参数或片段。更换地址只需重建门户容器，不需要重新编译 JS。可选 PORTAL_TRUSTED_PROXY_CIDRS 是空格分隔的可信 TLS 代理 IP/CIDR，默认不信任客户端转发头。容器固定监听 8080；/health 仅检查门户进程，不代表后端或数据库健康。

门户完整转发 /api/v1/*，包括超管所需的 /api/v1/admin/*，保留 Host、公开协议、查询参数和认证凭证。角色、归属、只读限制仍在后端执行。HTML 不缓存，带 hash 的资源长期缓存；缺失资源和受限入口返回 404，API 的 401/403/409/5xx 不会被替换为 SPA HTML。

### 同机部署

从仓库根目录执行，先填写真实域名和已发布的同品牌、同版本镜像：

~~~sh
cp deploy/vendor-portal.env.example deploy/vendor-portal.env
# BACKEND_ORIGIN 使用现有 Compose 服务名，例如 http://tokensavy:8080。
docker compose --env-file deploy/.env --env-file deploy/vendor-portal.env \
  -f deploy/docker-compose.yml -f deploy/docker-compose.vendor.yml \
  -f deploy/docker-compose.vendor-local.yml config
docker compose --env-file deploy/.env --env-file deploy/vendor-portal.env \
  -f deploy/docker-compose.yml -f deploy/docker-compose.vendor.yml \
  -f deploy/docker-compose.vendor-local.yml up -d --no-deps vendor-portal
~~~

整套后端使用显式品牌网络，同机时必须追加 vendor-local 文件；它只把门户接入既有网络，不改变后端和存储。仅使用 docker-compose.standalone.yml 时，后端和门户共享默认网络，不追加 vendor-local。

沿用现有 Compose project name（必要时加 -p）和全部原 override 文件，不能以新 project 重建既有后端/数据卷。首次安装整套系统时才按原部署步骤启动后端和存储。门户默认绑定宿主机 127.0.0.1:8081；把既有供应商域名整站反代到该地址。不要再把 /api/v1 分流至另一套后端。

宿主机 Caddy 站点示例：

~~~caddyfile
supplier.example.com {
    reverse_proxy 127.0.0.1:8081 {
        header_up Host {host}
        header_up X-Forwarded-For {remote_host}
    }
}
~~~

Caddy 若在容器内，应加入可达网络并使用 vendor-portal:8080；容器的 127.0.0.1 不是其他容器。PORTAL_TRUSTED_PROXY_CIDRS 填门户实际看到的 Caddy 来源；B 的 SERVER_TRUSTED_PROXIES 填 B 实际看到的门户/受控代理来源（逗号分隔），不能信任任意互联网来源。

### A / B / C 或云服务分离

1. B 使用现有运营后端镜像和 docker-compose.standalone.yml。DATABASE_HOST/PORT/USER/PASSWORD/DBNAME/SSLMODE 和 REDIS_HOST/PORT/USERNAME/PASSWORD/DB/ENABLE_TLS 指向 C/云服务；只有 B 持有这些凭证。保留既有 JWT_SECRET 和固定 TOTP_ENCRYPTION_KEY，不因迁移位置重新生成。
2. PostgreSQL 使用可写主端点及厂商要求的 TLS 校验；自定义 CA 通过只读卷挂载，PGSSLROOTCERT 填容器内路径。Redis 使用当前单端点客户端支持的服务，按厂商要求配置 ACL/TLS。原生 Redis Cluster/Sentinel 不在本轮改造范围。
3. A 的 BACKEND_ORIGIN 指向 B。跨公网使用验证证书的 HTTPS；私有 CA 可将合并的可信 CA bundle 只读挂载到门户 /etc/ssl/certs/ca-certificates.crt。TLS SNI/证书验证使用 B 的连接主机名，业务 Host 保持供应商域名。B 暂停时页面可用、API 返回真实 502/504，B 恢复后按运行环境 DNS 重新解析。
   如果 B 前方还有 Nginx/Caddy/负载均衡，它必须把供应商 Host 路由到同一个 Go 后端；TLS 连接主机名和 HTTP Host 可不同，不能将业务 Host 强制改成运营站域名。
4. A 已有 HTTPS 入口时，只启动门户：

~~~sh
docker compose --env-file deploy/vendor-portal.env -f deploy/docker-compose.vendor.yml up -d
~~~

A 没有入口时，可启用附带 Caddy（使用标准 HTTP/HTTPS 端口，需真实 DNS、可达 80/443；非标准公开端口使用自己的入口配置）：

~~~sh
docker compose --env-file deploy/vendor-portal.env \
  -f deploy/docker-compose.vendor.yml -f deploy/docker-compose.vendor-tls.yml config
docker compose --env-file deploy/vendor-portal.env \
  -f deploy/docker-compose.vendor.yml -f deploy/docker-compose.vendor-tls.yml up -d
~~~

可选 TLS 层使用独立 portal-edge 网络，仅信任 Caddy 固定地址。默认网段 172.30.88.0/24、地址 172.30.88.2；若冲突，同时修改 PORTAL_EDGE_SUBNET 和 PORTAL_EDGE_PROXY_IP。证书卷在更新时保留。C/云存储的网络白名单仅允许 B，A 和浏览器无需访问存储端口。

### 构建、版本与更新

~~~sh
docker build -f deploy/vendor-portal/Dockerfile -t local/vendor-portal:dev .
~~~

门户镜像为 ghcr.io/holeenlu/kdan-vendor、tapmodels-vendor、tokensavy-vendor。现有人工 Run workflow 在同一个固定源码提交下构建主站和供应商前端，门户/后端共用版本号，分别记录镜像摘要。完整产物成功才发布 Release，随后提升 latest；跨镜像标签不是原子切换，部署使用明确版本或 digest。

代码调整和本地构建不会上传镜像或部署生产。首次使用前需人工发布对应版本，不把示例版本占位符或本地 tag 当作已发布镜像。原后台“API 端点地址”语义保留。新版宿主机更新器会联动同 Compose 项目的 vendor-portal（自定义名称通过 --portal-service 配置），对两份镜像一起预检、健康检查及失败恢复；已有主机须重新安装更新器。A/B 分机或不同项目可在 A 安装 `install-compose-updater.py --portal-only`，超管登录供应商站后从顶部「门户更新」同步到后端实际运行版本；首次仍须升级镜像并接入宿主机 socket。未接入时可下载同版本发布附件 compose.vendor-release.json，追加到原 Compose 文件列表后拉取并重建门户。所有后续维护保留此固定摘要 override；详细步骤见 [配套升级](../../deploy/AUTOMATIC_RELEASE.md#供应商门户配套升级2026-10-22)。回退门户不回滚数据库。

这套结构没有增加第二个业务后端。未来网关多实例仍需单独解决启动并发清理、BPS 图片所属实例和后台任务协调；门户分离不代表网关双活验收完成。

### 验证命令

~~~sh
VENDOR_PORTAL_TEST_IMAGE=local/vendor-portal:dev \
  python3 -m unittest deploy.tests.test_vendor_portal_image -v
python3 -m unittest discover -s deploy/tests -p 'test_auto_release*.py' -v
python3 -m unittest discover -s .github/release-tools -p test_release_matrix.py -v
~~~

容器测试使用受控上游，覆盖同网段、跨网络、HTTPS 和失败路径；业务权限另运行既有 Go/前端回归，不能用代理 fixture 代替真实账号、模型消费或生产跨服务器/云厂商验收。

### 本轮实现验证（2026-10-19）

- 验证基线：1118a5363911 加本轮部署改动，使用隔离检出，未混入其他任务的未提交业务代码。
- 默认源码 Docker 构建与 release 产物构建均成功；Linux arm64 实际容器回归通过，Linux amd64 镜像构建及非 root/只读运行配置检查通过。
- 9 项真实 Docker 检查通过，包含四种 Compose 拓扑、API 路径/参数/认证转发、真实错误码、域名边界、客户端 IP、无后端启动、HTTPS 信任与主机名验证。后端 fixture 与真实业务检查分别记录。
- 独立 Go 进程 + 新建 PostgreSQL/Redis + 门户镜像完成 32 项真实 HTTP 检查：超管、一级/二级登录和范围限制，父子开户，供应商列表/账号/代理/用量/账单入口，直连后端与门户视图一致，原 api_base_url 保持不变。
- Chrome 真实登录、供应商列表及刷新通过，页面脚本错误为 0。Go 构建和供应商 Origin/Role 中间件竞态回归通过，前端守卫/API 客户端 64 项通过。
- 发布及更新回归 39 项中 38 项通过，1 项既有 opt-in 在线更新器 Docker 集成未启用；发布矩阵 16 项通过。所有 workflow 仍只允许手动运行。
- 主站和门户在镜像内连续构建通过，校验确认门户构建没有覆盖主站产物；已有前端大包提示保留。Caddy 可选入口语法校验通过，没有申请生产证书。
- 本机证据目录：/tmp/sub2api-vendor-portal-audit-20261019/。包含构建/测试日志、真实 API 与浏览器结果、截图和本轮补丁；本地验收凭证不入库，临时业务服务与数据容器已清理。
- 本轮不修改 Go/Vue 业务代码，不新增业务 API 或迁移；增加门户进程健康检查 /health 和 3 个门户运行参数。生产推送、镜像发布、服务器部署及云服务商实际连接未执行。
- 实现阶段改动统计（交付复查补充见下）：业务代码 +0/-0；部署/构建/发布代码 +319/-4；测试 +356/-0。新增门户运行参数 3 项、Compose 部署参数 5 项，构建复用 NODE_IMAGE 并新增 NGINX_IMAGE；发布元数据新增 portal_image 和 portal_image_digest。后端业务执行路径不变，新增门户容器运行路径。

### 交付复查（2026-10-19）

基于已发布的 f037c6fe7c9d 重新叠加本轮范围，保留其账号用量、BPS 与恢复修复。发现整套后端使用显式品牌网络、门户默认网络与其不通，因此增加仅同机叠加的 docker-compose.vendor-local.yml，避免为接入门户重建既有后端。Compose 回归改为检查实际共享网络，并通过真实 Compose 启动门户和受控后端验证服务名解析与 API 转发；三个品牌使用各自既有网络和示例镜像。

初始业务 HTTP 与浏览器验收仍对应上方基线；交付时另对最新基线重新构建门户镜像、执行容器和发布回归，不把历史验收表述为生产部署结果。

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

若现有 Caddy 已从后端提供前端 HTML，使用 [后端托管前端的 Caddy 示例](../../deploy/examples/vendor-portal-backend.Caddyfile) 替换现有供应商站点块，保留当前前端托管方式，不需要额外静态目录或供应商构建。关键修正是在供应商站点的拒绝规则之前，将完整 `/api/v1/*` 与 `/vendor/platform` 等页面转发到原共享后端，保留 Host、路径及查询参数。不能把后端 API 的 `/api/v1/admin/*` 与应禁用的旧前端 `/admin/*` 混为一谈，也不需要另建后端、数据库或 Redis。独立静态前端部署仍使用原 Caddy 示例。两个示例是不同托管方式的替代方案，不应在同一域名下同时配置。

正式页面移除每个超管菜单上重复的供应商平台介绍、权限边界、结算查询范围和数据留存大卡片，保留菜单自身标题与真实权限校验。同时修正重试按钮：供应商列表、分组目录和指定供应商详情独立记录加载/错误状态；重试重新请求实际失败项，后端仍失败时继续显示错误，不能清空提示假装恢复。

生产 Caddy 与前端尚未在本轮更新。按项目 AGENTS.md，生产部署属于独立授权范围；本地修改或提交推送都不能作为线上已恢复的证据。

本轮验证：超管页面与路由 26 项测试、Caddy 三项测试（24 个请求场景）、类型检查、ESLint 和主站/供应商站构建均通过。业务代码 +11/-31 行，测试 +53/-1 行；没有新增接口、运行配置、迁移或独立业务执行路径。

## 2026-10-18 再次线上复查

14:09–14:11（Asia/Shanghai）的只读请求与已有超管登录会话确认：根路径可加载新版前端，并在浏览器内跳转到 `/vendor/platform`；供应商管理显示 `Request failed with status code 404`；直接请求或刷新 `/vendor/platform` 则得到 Caddy 的纯文本 `Not found`。`/vendor/accounts` 返回 200 HTML，但超管登录后的路由会跳到 `/vendor/platform`，因此改用一级供应商页面地址不能修复问题。

`/api/v1/admin/vendors?page=1&page_size=20`、`/api/v1/admin/accounts?vendor_id=0` 与 `/api/v1/admin/groups/all` 都仍返回 Caddy 404；公开设置开启、Origin 正确，`/api/v1/vendor/me` 返回后端 JSON 401，同一管理供应商 API 在客户域名返回后端 JSON 401。问题仍在供应商域名入口，刷新浏览器、重新登录、仅升级应用或更换超级管理员均不会让未转发的路径进入后端。未读取到现行线上 Caddy 配置，不推断具体 matcher 或线上后端地址。

本次补充后端托管前端的配置示例，是因为原静态示例要求独立静态目录，不能直接用于现行托管方式。新示例统一转发版本化 API 和供应商页面，仍由共享后端校验角色和归属；客户页面、注册、初始化和网关入口继续在供应商域名返回 404。现有真实 Caddy 测试同时覆盖两种托管方式，检查超管各菜单、直链刷新、API 完整路径/查询参数/Host、错误响应和入口隔离。

部署时先读取并备份现行完整配置，将示例中的域名改为 `supplier.buildtoconnect.com`，上游替换成现行供应商站点已经使用的上游；只替换供应商站点块，不额外叠加同域名站点块。验证完整配置后热加载；若使用 Docker，应在实际 Caddy 容器内验证并 reload，不能假设宿主机有同一路径。异常时恢复备份并重新加载。验收应同时包含未登录 API JSON 401、超管已登录列表与各菜单、刷新页面，以及一级/二级的越权拒绝。

已按现有生产连接配置尝试只读 SSH，连接在密钥交换阶段被远端关闭，尚未读取或修改生产配置。生产热加载仍须按项目规则取得独立部署授权；本地示例和测试通过不等于线上已恢复。

验证：两种托管方式的真实 Caddy 回归 7 项通过（86 个请求场景）；现有 Go `TestSupplier(Origin|Role)` 权限回归通过。此次业务代码无增删，新增 1 份部署示例；没有新增 API、业务运行配置、前端路由或数据库迁移。

## 2026-10-18 已授权生产入口修复

用户明确授权“仅修正并热加载供应商站点的 Caddy 配置”，并指定 `root@95.169.12.225`。在该服务器确认实际配置是 `/opt/kdan/Caddyfile`，单文件只读挂载到 `kdan-caddy` 容器的 `/etc/caddy/Caddyfile`；上游为原 `kdan:8080`。运行中配置与磁盘配置一致。现行 CEL 白名单确实遗漏 `/vendor/platform` 和超管所需的 `/api/v1/admin/*`，与前述两个 404 一致。

仅将供应商站点中的旧 CEL 路径枚举改为版本化 API 和供应商页面路径匹配，保留原上游、客户端 IP 处理、传输参数、拒绝规则及其他所有站点；原 `/uploads/*` 继续可用，并包含前端所需的 `/theme-init.js`。实际 Caddy 配置 +4/-6 行，业务代码、测试无增删；没有新增接口、运行配置项或业务执行路径。

先在实际 Caddy 2.11.7 容器校验完整候选配置；比较适配 JSON，唯一行为差异是供应商路由的 `expression` matcher 改为 `path` matcher。修改前同时核验磁盘摘要与运行中配置，备份至 `/opt/kdan/Caddyfile.backup.supplier-20261018T142940`。因是单文件 bind mount，原位写入并 fsync，保持 inode 和 0600 权限；再次校验挂载内完整配置后执行 `caddy reload`。已核对实际运行配置与候选一致，应用与 Caddy 容器的启动时间均未变化。

线上验收 27 项通过：供应商根页面、超管各菜单直链、直属二级页面和主题资源返回 200；未登录的供应商管理、账号/代理池、分组及合规 API 返回后端 JSON 401；无范围账号列表、用户和设置 API 继续返回 JSON 403；旧后台页面、注册及网关入口继续返回 404。客户站根页面和账号页返回 200，管理 API 继续要求登录。

另使用已有超级管理员浏览器会话验收：供应商管理真实列表成功加载，手动刷新后再次成功，原界面 404 与刷新后的纯文本 `Not found` 均已消失；分组供给也观察到真实列表成功加载。没有创建或修改供应商业务资料，未操作数据库或 Redis，未部署应用版本。本地变更、校验输出、部署回执、配置摘要及验收截图保存在 `/tmp/vendor-caddy-95-169-12-225-20261018T142940/`。
