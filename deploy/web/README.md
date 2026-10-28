# 独立主站与供应商 UI

根 Dockerfile 只构建 API。deploy/web/Dockerfile 是唯一前端镜像构建入口：main、vendor 两个 target 共享一次依赖安装、i18n/类型检查及两种 Vite 产物构建。固定 Node 24、nginx 1.30.6、pnpm 9.15.9；不使用会隐式 install 的本机 pnpm fallback，不更新锁文件。VERSION、COMMIT、SOURCE_SHA256 只影响运行镜像的版本层。

当前frontend是单包构建，固定使用pnpm9.15.9。主任务误调用fallback包装器生成的无效pnpm-workspace占位已确认归属并删除；不存在workspace绕过分支。以后若正式引入workspace，必须同时审查依赖输入和构建合同。

网站名称、Logo、公开 API 展示链接由 API 的公共设置提供；两个镜像都使用同源 /api/v1。镜像名和 Compose 项目可配置，不能用网站品牌改代理目标或扩大权限。

## 从正在协作的工作树构建

在仓库根目录执行：

~~~sh
python3 deploy/local/build-ui.py
~~~

默认生成 sub2api-web:rebuild-ui、sub2api-vendor:rebuild-ui。可用 --web-image、--vendor-image、--version、--manifest 覆盖。脚本通过 git ls-files 收集仍存在的 tracked 和新 frontend 源码、docs/legal、两个 backend JSON 输入、必要 Docker 配置和 LICENSE；不复制 node_modules、dist、SDK、缓存、.env 或日志。复制后再次逐文件核验哈希，避免并发保存造成混合快照。构建只消费该任务临时 context，成功或失败都自动删除 context，保留精简 SHA-256 manifest（默认 .local/rebuild-integration/ui-source-manifest.json）。镜像 label 和 /web-build.json、/portal-build.json 记录同一源码摘要。

manifest 记录的是未提交工作树的源码快照，不能当作前端 owner 的提交或全部页面验收。该脚本不执行 git add/commit/push，不改另一会话源码。再次修改后重跑脚本，按下一节仅更新 web/vendor 容器即可；不需重建 API 或存储。

已冻结、无人并发修改的源码也可直接构建：

~~~sh
docker build -f deploy/web/Dockerfile --target main -t sub2api-web:local .
docker build -f deploy/web/Dockerfile --target vendor -t sub2api-vendor:local .
~~~

主站产物明确输出 frontend/dist，门户输出 frontend/dist-vendor。没有依赖外部预编译 dist 的 release target，也不保留旧 vendor Dockerfile 包装壳。

## 接入已有本机 API 项目

以下项目是隔离联调实例，API 已由其 owner 启动。可替换 UI_PROJECT 和 API_ENV，必须指向实际已有项目及其配置。不要打印完整 compose config 或 env 文件。

~~~sh
UI_PROJECT=sub2api-rebuild-ui-20261025-4e2074d6
API_ENV=.local/rebuild-integration/compose.env
docker compose -p "$UI_PROJECT" --env-file "$API_ENV" \
  --env-file deploy/local/ui.env.example \
  -f deploy/api/compose.yaml -f deploy/web/compose.yaml \
  -f deploy/docker-compose.vendor.yml -f deploy/local/compose.ui.yaml config --quiet
docker compose -p "$UI_PROJECT" --env-file "$API_ENV" \
  --env-file deploy/local/ui.env.example \
  -f deploy/api/compose.yaml -f deploy/web/compose.yaml \
  -f deploy/docker-compose.vendor.yml -f deploy/local/compose.ui.yaml \
  up -d --no-deps --no-build --pull never web vendor-portal
~~~

默认主站 http://127.0.0.1:32181、供应商 http://127.0.0.1:32182，只监听回环；API http://127.0.0.1:32180 保持原部署。三个角色共用现有项目 default 网络，UI 连接服务别名 api:8080。两个前端不挂数据卷、不持有数据库/JWT/TOTP 密钥。禁止对该联调项目执行 down -v 或全局 prune。

local 覆盖使用 Compose 的 !override 明确替换端口，避免叠加通用模板的发布端口。非本机部署使用 web/compose.yaml 和 docker-compose.vendor.yml，显式配置 WEB_PUBLIC_ORIGIN、PORTAL_PUBLIC_ORIGIN、BACKEND_ORIGIN。只信任实际入口代理的 WEB_TRUSTED_PROXY_CIDRS/PORTAL_TRUSTED_PROXY_CIDRS；API 的 SERVER_TRUSTED_PROXIES 必须由 API owner 配置真实来源，不信任客户端任意转发头。

## 路由与安全边界

- 主站：按 gateway.go 的实际分组和注册路径代理 /api、/v1、/v1beta、/backend-api/codex、antigravity 分组及明确 root 别名。/models 和 /models/:model 确有 root 注册；不把整个 /images、/chat、/messages、/alpha、/contents 前缀交给后端。安装 API 仅匹配 status/test-db/test-redis/install，/setup 页面仍由 SPA 接管。
- 主站 client_max_body_size=0，上传大小由后端当前路由预算唯一决定，不把可达 256 MiB 的图片合同收紧到 64 MiB；vendor 继续保留自己的 64m 限制。主站请求流式转发，不提前把整个上传写入代理临时盘。
- 主站代理采用 HTTP/1.1 Upgrade，支持网关 WS；关闭响应/请求缓冲、代理缓存、自动 upstream 重试和错误拦截，保留 SSE 流式交付。3600 秒是空闲超时，不是强制整轮生成时限。
- vendor 仍仅代理 /api/v1，包括后端授权保护的 /api/v1/admin；/admin 页面和全部网关/安装入口返回 404。域名匹配和服务器端角色、资源归属验证均保留。
- 两个角色的 HTML/公开设置/错误响应 no-store；/assets 内成功的带 hash 产物长期 immutable，缺失资源 404。SPA 刷新返回各自入口，错误 API 不变成 HTML。
- 主站access log将/api/bps-images/:token固定记为/api/bps-images/[redacted]，不记录查询参数。main与vendor的各server关闭会附带原始URI的请求错误日志；保留启动/进程错误，以及脱敏access中的状态、upstream状态和连接/响应耗时。应用诊断仍由API日志提供，避免故障时把能力令牌或OAuth查询参数送进stderr。
- nginx 以 UID/GID 101 运行，只读根文件系统、/tmp tmpfs、cap_drop ALL、no-new-privileges；未知 Host 只有 /health 可用。/health 只证明 UI 进程存活。

## 验证与真实操作

~~~sh
python3 -m unittest deploy.tests.test_api_compose deploy.tests.test_runtime_permissions -v
WEB_TEST_IMAGE=sub2api-web:rebuild-ui VENDOR_PORTAL_TEST_IMAGE=sub2api-vendor:rebuild-ui \
  python3 -m unittest deploy.tests.test_vendor_portal_image -v
~~~

沿用现有 unittest/Docker fixture，补充主站 SPA、缓存、路径/错误/可信代理头、真实 WS 双向帧和 SSE 首事件检查。fixture 仅证明反向代理传输，不能当真实上游或账号隔离证据。

隔离 API 的 health、index、SPA、同源公共设置、匿名授权及 vendor 路径边界另行验证。完整重构仍须逐页真实浏览器操作验收，包括登录/角色、查询、编辑保存、错误状态、刷新、WS/SSE；本部署切片不宣称全前端验收完成。账号/Keys、控制面合同和 root tools 由主代理继续。

通用模板默认端口为API8080、主站8082、vendor8081；既有vendor默认保持，本机overlay明确使用32180/32181/32182。精确API别名允许尾斜杠并交后端执行原有重定向，不能当作SPA页面。
