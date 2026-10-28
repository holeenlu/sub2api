# NewAPI 完整部署包

本发布包含同一源码提交构建的 Linux amd64 API、主站前端、独立供应商门户，以及三个独立镜像、Compose 和发布清单。三个镜像均固定到 digest，前端无需服务器安装 Node 或重新编译。API 不内嵌页面；由独立 web/vendor 镜像提供网页和同源 API 代理。

## 全新部署

1. 下载 `NewAPI-<version>-deployment-linux-amd64.tar.gz`、`checksums.txt` 和 `release-manifest.json`。将完整附件放在同一目录后运行 `sha256sum -c checksums.txt`（也可只校验下载的部署包对应行）。解压部署包进入一个新目录。
2. `umask 077; cp .env.example .env; chmod 600 .env`。设置独立的项目名、POSTGRES_PASSWORD、REDIS_PASSWORD、ADMIN_EMAIL、ADMIN_PASSWORD、JWT_SECRET 和 TOTP_ENCRYPTION_KEY。各密码/密钥独立生成，建议 `openssl rand -hex 32`，不要沿用示例或生产其他项目凭据。
3. 设置 `WEB_PUBLIC_ORIGIN=https://你的主站域名`、`PORTAL_PUBLIC_ORIGIN=https://你的供应商域名`、`SERVER_FRONTEND_URL=https://你的主站域名`，BACKEND_ORIGIN 保持 `http://api:8080`。精确设置 API 的 SERVER_TRUSTED_PROXIES，以及需要时的 CORS_ALLOWED_ORIGINS。不要无条件信任公网客户端提供的转发头。
4. `docker compose --env-file .env -f compose.yaml config --quiet`。
5. `docker compose --env-file .env -f compose.yaml pull`。
6. `docker compose --env-file .env -f compose.yaml up -d --no-build --wait --wait-timeout 360`。首次启动会初始化本项目的新 PostgreSQL、Redis 和管理员，并执行数据库迁移。
7. 配置宿主 Nginx/Caddy：主站域名代理至 127.0.0.1:8082，供应商域名代理至 127.0.0.1:8081；HTTPS 证书按真实域名配置。两个 UI 容器负责与 API 的同源代理。API 默认仅监听宿主 127.0.0.1:8080；PostgreSQL/Redis 不发布宿主端口。
8. 检查 `docker compose ps`，访问两个域名，并使用设置的管理员凭据登录主站。API `/health` 只证明进程存活；完整业务和真实上游需要配置账号后另行验收。

## 更新与数据

先备份 PostgreSQL、app_data、Redis 以及密钥。使用新包的 Compose（新的镜像 digest），保留原项目名、`.env` 和数据卷，再 pull/up。更新启动可能应用数据库迁移，不能把换旧镜像当作一定可逆的数据库回滚。不要 `down -v` 或全机 prune。新包不是旧维护版数据库的自动迁移器。

API、web、vendor 三个镜像仅 linux/amd64；完整部署包同时包含后端二进制、定价资源和两套静态网页，便于审计或受控的独立进程部署。PostgreSQL/Redis 基础镜像由 Compose 拉取。本包不是离线镜像归档；离线环境还需单独导出/传输所有镜像。

## 发布与验证

Workflow 仅手动触发。阶段与 main 对齐：prepare → build-frontend → build-binaries → publish → promote。前端安装依赖、i18n/类型检查各一次，Vite 输出主站和供应商两套；Go 模块/编译缓存复用。镜像只消费这批已验证产物，不重复编译。发布前以临时隔离数据库启动五个服务，验证主站/供应商页面、API 代理、管理员登录、API 重启后的登录和安装状态；另执行现有真实 WebSocket/SSE 代理 fixture 测试。它们不替代真实上游、完整逐页产品或生产迁移验收。

Release 仍只使用 `NewAPI/latest`，更新只作用于 NewAPI 自己的标签和附件，不修改维护分支 Release/Latest。三个镜像的数字版本及 latest 在 Release 成功后才提升；Compose 固定 digest，不依赖跨镜像非原子的 latest 更新。
