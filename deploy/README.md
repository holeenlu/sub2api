# sub2api 部署

本分支使用唯一的根 `Dockerfile` 构建独立 API，运行配置位于 [api/README.md](api/README.md)。构建不需要 frontend、Node、预生成二进制或其他工作树。

| 文件 | 用途 |
| --- | --- |
| `../Dockerfile` / `../.dockerignore` | 独立 API 源码构建与最小构建上下文 |
| `api/compose.yaml` / `api/.env.example` | API、PostgreSQL、Redis 的隔离部署 |
| `api/compose.single.yaml` | 可选的单 API 副本容器名；不用于扩容 |
| `api/docker-entrypoint.sh` | UID/GID 1000 的中性 sub2api 运行身份 |
| `config.example.yaml` | 后端配置、测试与开发契约；保留仍有效的共享配置 |
| `codex-instructions.md.tmpl` | 现有后端强制指令配置的可选模板 |
| `Caddyfile` / [EDGE_SECURITY.md](EDGE_SECURITY.md) | API 与独立门户可复用的边缘安全说明 |
| `vendor-portal/`、`docker-compose.vendor*.yml`、`vendor-portal.env.example`、`examples/vendor*` | 独立前端门户，由门户会话维护 |
| `tests/` / `test-caddyfile-cache.sh` | 部署权限、安全与代理回归 |

旧主站多份 Compose、Apple/WSL/standalone 入口、品牌镜像打包/发布、二进制安装器和宿主机 Compose updater 已退役。本分支不提供品牌切换器、Release 发布或镜像推送工作流；GitHub 版本同步功能不属于本次部署资产清理。

## 门户交接边界

门户静态站点与同源代理仍有独立用途。旧vendor-local品牌网络覆盖和portal-updater/socket代理已删除；同一Compose项目使用默认网络。完整门户UI与其更新控件的清理由前端会话负责，不能宣称本轮完成前端联调。

## 本地轻量检查

在仓库根目录执行（Python 3、Docker Compose v2+；不连接 daemon、不下载或构建镜像）：

```sh
python3 -B -m unittest deploy.tests.test_runtime_permissions deploy.tests.test_api_compose -v
sh deploy/tests/docker-runtime-resources-test.sh
sh deploy/test-caddyfile-cache.sh
```

测试只使用虚拟凭据与自动清理的临时目录。现有门户真实镜像/代理测试保留，由门户集成者另行执行；这里的替身和配置检查不能替代真实镜像、数据库、OAuth 或 UI 验收。
