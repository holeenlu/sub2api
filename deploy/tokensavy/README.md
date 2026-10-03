# Tokensavy 全新服务器部署

Tokensavy 从 TapModels 提交 `798c65276` 派生，分支 `codex/tokensavy`。本站域名为 `tokensavy.ai`，管理员为 `ikung1970@gmail.com`。品牌、示例和下载工具独立；上游 Sub2API 来源和许可证保留。

本目录是 Tokensavy 的部署入口。仓库其他 deploy 安装脚本、发版 workflow 和 Compose 文件是继承的 TapModels/KDAN 工具，不用于 Tokensavy。Tokensavy 暂无独立镜像仓库或在线发版渠道：从本分支源码构建 `tokensavy:local`，在线更新关闭。

## 1. 准备服务器

使用安装了 Docker Engine、Compose v2 插件和 Python 3 的 Linux 服务器（amd64 或 arm64）。构建包含完整前后端，建议至少 4 核、8 GB 内存，并预留 20 GB 可用磁盘。服务器需能下载 Docker Hub 基础镜像、npm 和 Go 依赖。

将 `tokensavy.ai` 的 A 记录指向服务器公网 IPv4；如设置 AAAA，必须指向可访问的 IPv6。开放入站 TCP 80/443（可选 UDP 443）。Caddy 会申请、保存并自动续期 HTTPS 证书。无需另装 Nginx。8080 只绑定本机；数据库和 Redis 不发布公网端口。

把本分支的完整源码传到服务器，例如 `/opt/tokensavy`。请勿复制旧站的 `.env`、`config.yaml`、data 目录或数据库。源码包不含账号密码，凭据单独传送。

## 2. 配置凭据

本次已经生成的私密文件为 `deploy/tokensavy/.env` 和 `deploy/tokensavy/credentials.txt`，均为权限 600，且被 Git / Docker 构建上下文忽略。把这两个文件安全复制到服务器源码中的同名目录；不要重新生成。`credentials.txt` 列出管理员初始密码、PostgreSQL/Redis 密码和 JWT/TOTP 密钥。

如果是在另一套全新实例上独立安装，且未复制任何凭据文件，可运行：

```bash
cd /opt/tokensavy/deploy/tokensavy
python3 init.py --domain tokensavy.ai --admin-email ikung1970@gmail.com
```

脚本拒绝覆盖已有文件，每次新安装生成不同随机密码。`.env.example` 只有配置项，不含可用密码。管理员邮箱只是应用登录名；不会创建邮箱，也未配置 SMTP、支付或第三方 OAuth 密钥。

## 3. 构建并启动

```bash
cd /opt/tokensavy/deploy/tokensavy
chmod 600 .env credentials.txt
docker compose --env-file .env -f compose.yaml config --quiet
docker compose --env-file .env -f compose.yaml build tokensavy
docker compose --env-file .env -f compose.yaml up -d --wait --wait-timeout 600
docker compose --env-file .env -f compose.yaml ps
curl --fail http://127.0.0.1:8080/health
curl --fail https://tokensavy.ai/health
```

如果服务器无法访问 npm 官方仓库，可在 `.env` 添加 `NPM_CONFIG_REGISTRY=https://registry.npmmirror.com` 后重新构建。

首次启动会初始化空数据库、执行应用 migrations、创建管理员。打开 `https://tokensavy.ai/login`，使用 `ikung1970@gmail.com` 和 `credentials.txt` 中的管理员密码登录。首次登录后修改密码；管理员合规确认需要你本人阅读并完成。随后按需添加上游账号、分组、API Key、SMTP、支付及登录 OAuth 配置。

若 HTTPS 尚未就绪，检查 DNS 和 80/443 入站规则：

```bash
docker compose --env-file .env -f compose.yaml logs --tail=100 caddy tokensavy
```

域名尚未生效时，可通过 SSH 隧道临时访问本机 8080：在自己电脑执行 `ssh -L 18080:127.0.0.1:8080 USER@SERVER_IP`，再打开 `http://127.0.0.1:18080`。

## 4. 数据与后续维护

Compose 项目名固定为 `tokensavy`，数据卷为 `tokensavy_tokensavy_data`、`tokensavy_postgres_data`、`tokensavy_redis_data`、`tokensavy_caddy_data`、`tokensavy_caddy_config`。重建容器保留数据；不要运行 `docker compose down -v`，它会删除数据卷。

`.env` 中的 JWT_SECRET / TOTP_ENCRYPTION_KEY 必须固定并随备份保管。数据库初始化后，修改 `.env` 中 ADMIN_PASSWORD 不会重置管理员密码；POSTGRES_PASSWORD 也必须与数据库实际密码同步，不能只改文件。

后续升级：备份数据库、应用数据和 `.env`，获取审核后的 Tokensavy 源码，再重复 build / up。不要运行旧站的在线更新器、`deploy/install.sh` 或旧发版 workflow。原有 KDAN/TapModels 远端交付映射不适用于此分支；本次没有发布镜像或部署生产服务器。

## 本次交付验证（2026-10-03）

已通过前端类型检查、生产构建、164 项前端测试；Go 配置/初始化/日志、嵌入页面及指定的品牌相关 service 测试；55 项下载工具/安装配置 Python 测试。Docker 已实际构建 `linux/arm64` 镜像，使用独立临时数据库完成首次初始化、管理员登录、品牌/文档/下载包检查，并验证三项服务重建后账号数据仍然保留。临时测试资源已清理。

新服务器的 DNS、证书签发、网络连通性和上游账号尚需在实际部署时验证；没有连接或改动生产服务器。
