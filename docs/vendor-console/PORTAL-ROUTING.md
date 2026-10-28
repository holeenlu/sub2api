# 独立供应商门户部署与入口

## 独立门户镜像（2026-10-25）

结构为：供应商浏览器 → 门户容器（静态页面 + 同源 API 代理）→ 现有运营/网关后端 → PostgreSQL/Redis。门户可与后端同机，或位于 A、后端位于 B、存储位于 C/云服务。门户不运行 Go 后端、后台任务或迁移，不配置数据库/Redis 连接凭证、JWT 签名密钥或 TOTP 加密密钥。

| 路径 | 职责 |
| --- | --- |
| frontend/src/features/vendor、现有路由及共享组件 | 唯一页面实现；build:vendor 输出 frontend/dist-vendor |
| deploy/web/Dockerfile --target vendor | 与 main target 共用依赖及源码构建，输出独立门户镜像 |
| deploy/vendor-portal/entrypoint.sh、nginx.conf.template | 启动时校验配置、生成代理配置；非 root、只读根文件系统 |
| deploy/docker-compose.vendor.yml | A 单独运行或叠加使用外部存储的后端，不创建后端或存储 |
| deploy/docker-compose.vendor-tls.yml | A 没有 HTTPS 入口时的可选 Caddy 层 |
| deploy/api/compose.yaml | 当前独立后端API及隔离存储模板 |

### 三个地址分别配置

| 地址 | 含义 |
| --- | --- |
| 现有后台 api_base_url | 运营/网关对外地址，用于“使用密钥”和回调建议；含义不变 |
| PORTAL_PUBLIC_ORIGIN | 门户公开 origin，例如 https://supplier.example.com；与后台 vendor_portal_origin 完全一致 |
| BACKEND_ORIGIN | 门户连接现有后端的 origin；同机例如 http://api:8080，跨机使用 B 私网地址或 HTTPS 入口 |

部署前在现有运营后台启用 vendor_console_enabled，并将 vendor_portal_origin 配成 PORTAL_PUBLIC_ORIGIN。供应商账号仍由既有超管/上级供应商按权限开通；门户镜像不会自动开户或放宽授权。

两个门户 origin 必填，只接受 HTTP(S) origin，不接受账号密码、路径、查询参数或片段。更换地址只需重建门户容器，不需要重新编译 JS。可选 PORTAL_TRUSTED_PROXY_CIDRS 是空格分隔的可信 TLS 代理 IP/CIDR，默认不信任客户端转发头。容器固定监听 8080；/health 仅检查门户进程，不代表后端或数据库健康。

门户完整转发 /api/v1/*，包括超管所需的 /api/v1/admin/*，保留 Host、公开协议、查询参数和认证凭证。角色、归属、只读限制仍在后端执行。HTML 不缓存，带 hash 的资源长期缓存；缺失资源和受限入口返回 404，API 的 401/403/409/5xx 不会被替换为 SPA HTML。

### 同机部署

从仓库根目录执行，先填写真实域名和自行构建且版本匹配的门户镜像：

~~~sh
cp deploy/vendor-portal.env.example deploy/vendor-portal.env
# BACKEND_ORIGIN 使用现有 Compose 服务名，例如 http://api:8080。
docker compose --env-file deploy/api/.env --env-file deploy/vendor-portal.env \
  -f deploy/api/compose.yaml -f deploy/docker-compose.vendor.yml config
docker compose --env-file deploy/api/.env --env-file deploy/vendor-portal.env \
  -f deploy/api/compose.yaml -f deploy/docker-compose.vendor.yml up -d --no-deps vendor-portal
~~~

新API和门户在同一Compose项目中使用default网络，后端服务名为api；旧vendor-local品牌网络覆盖已删除。独立门户仍只代理已有后端，完整新前端由独立会话接入后验收。

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

1. B 使用当前独立 API 镜像及部署者明确配置的外部存储编排。DATABASE_HOST/PORT/USER/PASSWORD/DBNAME/SSLMODE 和 REDIS_HOST/PORT/USERNAME/PASSWORD/DB/ENABLE_TLS 指向 C/云服务；只有 B 持有这些凭证。保留既有 JWT_SECRET 和固定 TOTP_ENCRYPTION_KEY，不因迁移位置重新生成。
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
docker build -f deploy/web/Dockerfile --target vendor -t local/vendor-portal:dev .
~~~

门户和后端按各自角色从同一已验证版本自行构建，镜像引用通过部署配置指定。原品牌发布workflow、Compose updater及宿主机socket升级机制已经删除，不能继续使用旧安装器、发布附件或旧品牌镜像作为本重构的更新来源。

升级时由操作者选择明确版本/摘要并重建对应角色容器；本项目不会下载或替换运行二进制。独立门户和新前端尚需其会话完成集成验收，本次保留其有效代理/鉴权边界与构建源，不宣称新UI已完成。

这套结构没有增加第二个业务后端。未来网关多实例仍需单独解决启动并发清理、BPS 图片所属实例和后台任务协调；门户分离不代表网关双活验收完成。

### 验证命令

~~~sh
VENDOR_PORTAL_TEST_IMAGE=local/vendor-portal:dev \
  python3 -m unittest deploy.tests.test_vendor_portal_image -v
python3 -m unittest deploy.tests.test_api_compose deploy.tests.test_runtime_permissions -v
~~~

容器测试使用受控上游，覆盖同网段、跨网络、HTTPS 和失败路径；业务权限另运行既有 Go/前端回归，不能用代理 fixture 代替真实账号、模型消费或生产跨服务器/云厂商验收。

### 重构分支构建与本机联调

旧版本验收记录保留于 Git 历史，不能作为当前重构 UI 的验证结果。旧 vendor Dockerfile、release 预编译 target 及专属 dockerignore 已删除，唯一前端构建定义是 deploy/web/Dockerfile。Node 24、nginx 1.30.6、pnpm 9.15.9 保持。主站与门户的稳定源码快照、镜像重建、现有 API 项目接入和验收命令见 [独立 UI 部署](../../deploy/web/README.md)。版本、commit、源码摘要由 portal-build.json 提供；网站品牌仍读取公开设置。

## 当前入口

供应商域名的 `/vendor/platform` 是超级管理员工作区，一级、二级仍使用同域名 `/vendor/accounts` 等控制台页面。客户运营站侧栏不再显示供应商平台；旧 `/admin/vendors` 书签返回客户运营首页。供应商身份与采购业务从独立供应商站进入。

后端 `/api/v1/admin/vendors` 并未废弃：供应商站的超级管理员仍通过它管理供应商。不要把旧前端页面 `/admin/vendors` 与该 API 混淆，也不要将超级管理员请求改成一级供应商接口 `/api/v1/vendor/vendors`。


需求、角色、资源和结算设计统一见 [工程基线 R13](../ENGINEERING.md#req-vendor)。历史线上诊断和当时修复不再作为部署步骤；本轮没有执行生产更新。
