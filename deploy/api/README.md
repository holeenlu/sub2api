# sub2api API 部署

本分支的唯一后端镜像由仓库根 `Dockerfile` 和 `.dockerignore` 构建；本目录提供稳定的运行部署配置。从同一 checkout 的 `backend/cmd/server` 编译，不使用 `embed` tag、Node、frontend、另一工作树或预生成二进制。完整前端由门户会话接入，首页/安装向导页面不包含在 API 镜像中。部署验收必须使用新的隔离数据，不覆盖生产 Compose 或数据卷。

复用基线 `6f48d6ab6fcc29763af84410690530aeaa957403` 的 Go `1.27.3`、PostgreSQL `18-alpine`、Redis `8-alpine`、Alpine `3.21`，不升级依赖。Dockerfile 保留现有 `pg_dump`/`psql` 和定价资源；SQL 迁移、VERSION、Wire/Ent 生成源码及其他 Go embed 资源均来自该 checkout。

## 从干净 checkout 构建和启动

下列构建/启动步骤交由主集成者协调执行；需要 Docker Engine、Compose v2+ 和 BuildKit。实际构建会下载基础镜像和 Go/APK 依赖，启动会初始化数据库。必须使用全新的隔离项目和数据库，不连接生产或其他会话的数据。主集成者提交根构建文件和本目录后，可在一个不存在的新目录检出：

```sh
git clone --branch NewAPI --single-branch https://github.com/holeenlu/sub2api.git sub2api-rebuild
cd sub2api-rebuild/deploy/api
umask 077
cp -n .env.example .env
chmod 600 .env
```

编辑 `.env`：设置未使用的 `COMPOSE_PROJECT_NAME`（如 `sub2api-api-local`），避开已有宿主端口，填写六项必填值 `POSTGRES_PASSWORD`、`REDIS_PASSWORD`、`ADMIN_EMAIL`、`ADMIN_PASSWORD`、`JWT_SECRET`、`TOTP_ENCRYPTION_KEY`。密码/密钥分别用 `openssl rand -hex 32` 生成并安全保存；不要使用样例虚拟值。模板要求非空，具体强度/格式仍由现有后端校验。`.env` 不应提交或输出到日志；不要把密钥传成构建参数。

```sh
# 只解析配置，不连接 Docker daemon，不下载镜像，不启动服务。
docker compose --env-file .env -f compose.yaml config --quiet

# 在同一 checkout 内编译；不需要本机 Go、Node 或 backend/bin。
docker compose --env-file .env -f compose.yaml build --build-arg COMMIT="$(git rev-parse HEAD)" --build-arg DATE="$(date -u +%Y-%m-%dT%H:%M:%SZ)" api

# 此步会运行迁移，仅允许连接本次新建的隔离数据库。
docker compose --env-file .env -f compose.yaml up -d --no-build
docker compose --env-file .env -f compose.yaml ps
docker compose --env-file .env -f compose.yaml logs --tail=100 api
curl --fail http://127.0.0.1:8080/health
curl --fail http://127.0.0.1:8080/setup/status
```

修改端口后相应调整 curl。源码实际端点是 `/health`，**没有 `/healthz`**；它返回 `{"status":"ok"}`，只证明 HTTP 服务存活，不持续检测 DB/Redis 或证明上游/账务可用。正常运行的 `/setup/status` 返回 `needs_setup:false`。`/` 返回 404 属于未嵌入前端的预期行为。

仅构建镜像时也可在仓库根目录执行 `docker build -t sub2api:local .`，不需要 `.env`。`APP_IMAGE` 是构建输出标签及运行引用；API 默认 `pull_policy: never`，须先构建，或显式拉取所指定的可信同角色制品后再 `up --no-build`。不通过历史品牌更新渠道升级这个源码制品。

## 初始化、持久化与权限

- `AUTO_SETUP=true` 复用 `internal/setup`：只有 `/app/data/config.yaml` 和 `/app/data/.installed` **均不存在**时，测试 PG/Redis 连接、应用迁移、在用户表为空时创建管理员，再写配置（0600）和安装锁（0400）。已有用户不被环境变量重置密码。不要预先挂入空 config 或设置 `SKIP_SETUP` 来假装安装完成。
- 正常启动的 `repository.InitEnt` 仍会应用待执行迁移（现有 10 分钟上下文）；`SETUP_MIGRATION_TIMEOUT_SECONDS` 仅控制首次 setup，0 采用 60 秒默认值。启动、升级和重启都可能写数据库，不是只读检查。首轮先启动一个 API 副本，完成初始化后再由主集成者验证扩容。
- 默认网络及三个命名卷都由 Compose 项目名限定，不使用 `external` 或全局固定卷名。PostgreSQL/Redis 不发布宿主端口；内部保持现有无 TLS 连接，仅用于本项目隔离网络。这里没有外部数据库连接参数。

| 卷（实际名带项目名前缀） | 容器挂载点 | 实际内容/路径 |
| --- | --- | --- |
| `app_data` | `/app/data` | config、`.installed` 及后端运行数据 |
| `postgres_data` | `/var/lib/postgresql` | 显式 `PGDATA=/var/lib/postgresql/data`；覆盖 PG18 镜像声明的父卷，避免额外匿名卷 |
| `redis_data` | `/data` | Redis RDB/AOF |

镜像使用本目录的最小 `docker-entrypoint.sh` 和中性 `sub2api` 用户（UID/GID 1000），不依赖共享品牌入口，也不创建兼容软链。root 启动时修正 `/app/data` 的归属，再 `su-exec sub2api`；chown 因只读子挂载失败也不会回退为 root 运行。非 root 启动直接 exec，不尝试 chown。无参数或只有 flags 时执行 `/app/sub2api`；显式 `/bin/sh -c ...` 等命令原样传递，root 路径也先降权。Compose 保留 `no-new-privileges`。若改成 bind mount，必须先为 UID/GID 1000 准备可写目录；不要把整个 `/app/data` 设为只读。首次安装后如需自定义 config，保留其真实字段与密钥；本模板的显式环境变量优先，未列入 Compose 的 `.env` 变量不会自动传入容器。

JWT 固定且至少 32 字节，TOTP 使用独立的 64 位十六进制密钥，重启/副本保持一致。显式管理员凭据避免后端生成一次性密码并打印到日志。API 日志只写 stdout，避免多副本轮转同一个文件。OAuth 客户端密钥仅通过运行环境注入，现有登录提供商、PKCE、回调校验与 Cookie 安全逻辑保持不变。外部前端接入时需协同配置真实 HTTPS 回调、可信代理及精确 CORS origin；`SERVER_FRONTEND_URL` 是邮件等外部链接基址，不替代提供商回调配置，也不是公共 `api_base_url` 展示字段。

## 参数清单

下表是本模板读取的全部部署输入；不创建新的后端配置键。必填值空缺时 `compose config` 就失败。

| 输入 | 默认/要求 | 作用 |
| --- | --- | --- |
| `COMPOSE_PROJECT_NAME` | `sub2api`；试验应选新名字 | 容器、默认网络、卷的范围；CLI `-p` 优先 |
| `APP_IMAGE` | `sub2api:local` | API 构建标签/运行镜像引用 |
| `APP_CONTAINER_NAME` | 不设置 | 仅可选单副本覆盖文件读取 |
| `BIND_HOST` | `127.0.0.1` | API 宿主监听地址 |
| `SERVER_PORT` | `8080` | 宿主端口；容器始终 8080，设 0 可由 Docker 分配 |
| `POSTGRES_USER` / `POSTGRES_DB` | 均为 `sub2api` | 新 PG 用户/库及 API 连接参数 |
| `POSTGRES_PASSWORD` | 必填，建议随机 hex | PG 与 API 共享的连接密码 |
| `REDIS_PASSWORD` | 必填，建议随机 hex | Redis requirepass、API 与健康探测认证 |
| `ADMIN_EMAIL` / `ADMIN_PASSWORD` | 必填 | 只用于空数据库管理员初始化 |
| `JWT_SECRET` / `TOTP_ENCRYPTION_KEY` | 必填固定独立密钥 | 认证签名 / 2FA 加密 |
| `JWT_EXPIRE_HOUR` | `24` | 已有 JWT 有效期（小时） |
| `SETUP_MIGRATION_TIMEOUT_SECONDS` | `0`，等价首次 setup 默认 60 秒 | 首次迁移超时（秒） |
| `TZ` | `Asia/Shanghai` | API、PG、Redis 时区 |
| `SERVER_TRUSTED_PROXIES` | 空 | 逗号分隔 IP/CIDR；新部署默认不信任转发 IP |
| `SERVER_FRONTEND_URL` | 空 | 已有后端前端链接基址；后续 UI 接入时设置 |
| `CORS_ALLOWED_ORIGINS` | 空 | 逗号分隔精确 origin；空时跨域预检被拒绝 |
| `GEMINI_OAUTH_CLIENT_ID` / `GEMINI_OAUTH_CLIENT_SECRET` | 空；自定义时成对设置 | 现有 Gemini OAuth 客户端 |
| `GEMINI_OAUTH_SCOPES` | 空 | 现有 Gemini scope 覆盖 |
| `GEMINI_CLI_OAUTH_CLIENT_SECRET` / `ANTIGRAVITY_OAUTH_CLIENT_SECRET` | 空 | 现有内置客户端的外部 secret 注入 |

构建参数另有 `VERSION`（空则用源码 VERSION）、`COMMIT`/`DATE`（默认 unknown）、`GOPROXY`（`https://goproxy.cn,direct`）及 `GOSUMDB`（`sum.golang.google.cn`），通过 `build --build-arg` 传入，放进 `.env` 不会自动生效。`TARGETOS`/`TARGETARCH`/`BUILDPLATFORM` 由 BuildKit 提供。Go/基础镜像版本固定，与现有仓库相同。

固定运行约定包括 `AUTO_SETUP=true`、`DATA_DIR=/app/data`、`SERVER_MODE=release`、`RUN_MODE=standard`、内部 DB/Redis 地址及端口、Redis DB 0、日志到 stdout；不暴露 simple mode、跳过 setup、品牌发布器或自动部署开关。

## 命名、多实例与停止

默认完全省略 `container_name`，Compose 自动命名。确需单实例固定名字时，在 `.env` 设置不与其他项目冲突的 `APP_CONTAINER_NAME`，且对所有命令显式附加覆盖文件：

```sh
docker compose --env-file .env -f compose.yaml -f compose.single.yaml config --quiet
docker compose --env-file .env -f compose.yaml -f compose.single.yaml up -d --no-build
```

该覆盖只适用于一个 API 副本，不是伸缩配置。未来多副本验收使用基础文件，去掉单实例覆盖并设置 `SERVER_PORT=0`（或使用另行审核的入口代理/端口覆盖），避免两个副本争用同一宿主端口，再使用 `--scale api=2`。命名可扩容不等于多实例业务、会话/任务恢复已经验收。两个独立试验实例需不同项目名和宿主端口，可共用相同镜像内容。

```sh
# 保留三个数据卷；之后仍使用原来的项目名启动。
docker compose --env-file .env -f compose.yaml down
```

改项目名会选择另一组卷，不是迁移已有数据。不要把生产旧卷直接挂到本模板；本模板 PG 卷挂父目录，不能按旧卷根目录布局直接替换。只有主集成者核实试验资源归属且不再需要数据后，才对同一个明确项目使用 `down --volumes`；不要全机 prune，也不要删除其他 worker 的卷/缓存。

## 验证边界与交接

在仓库根目录执行：

```sh
python3 -B -m unittest deploy.tests.test_runtime_permissions deploy.tests.test_api_compose -v
sh deploy/tests/docker-runtime-resources-test.sh
sh deploy/test-caddyfile-cache.sh
```

迁移检查进行虚拟环境变量下的 `docker compose config`、Dockerfile/源码路径静态核对及 entrypoint 的本地 shell 替身检查；不运行构建、容器、迁移，不下载镜像，不读取真实密钥。shell 替身只验证分支、参数与 exec 行为，不代表容器真实降权已通过。实际镜像编译、依赖加载、`pg_dump`/`psql` 运行、降权后目录可写、首次初始化/迁移、重启持久化、OAuth 及完整 UI 验收由主集成者协调。本次命令与结果以 worker 交接为准，不能把文档中的启动命令视为已执行证据。

`backend/Makefile` 现有 `build` 已是不带 embed 的入口；本 Dockerfile 直接调用相同 `cmd/server`，不要求主代理修改 Makefile。未来若增加 `build-api` 名称，应复用现有 build 实现，本目录没有另建构建命令。
