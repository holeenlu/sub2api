# sub2api

独立重构中的 AI API 网关项目。技术项目名统一为 sub2api；网站品牌使用运行设置，镜像、容器和部署实例使用编排配置。

开发分支：NewAPI。原 main 与品牌分支继续独立维护。本分支采用普通增量开发，不包含旧品牌发布矩阵、自更新安装器或并存的旧部署选择器。

## 当前状态

已开始多子智能体协作：执行预算移入唯一内核、SSE纯协议包接入现有消费链、项目身份集中管理，后端可独立编译并以PostgreSQL/Redis部署。完整执行引擎和其余领域仍在迁移，不能将第一批完成视为整个重构完成。

frontend目录由用户另建会话负责，已有页面不是新前端已完成的证明。默认Docker制品是独立API，不嵌入前端；完整UI部署在前端交接后集成。

## 构建与部署

~~~sh
# 从此分支根目录构建API镜像；不依赖Node或另一工作树。
docker build -t sub2api:local .

# 或使用与backend/go.mod相符的Go SDK编译本机后端。
make build-backend
~~~

首次部署、必要密钥、独立数据库/数据卷、可选容器名与停止方法见[API部署说明](deploy/api/README.md)。默认从本分支构建自己的制品，升级通过重新构建并部署，不下载旧维护项目二进制。不要把生产数据库或旧卷直接用于重构试验。

网站名称、Logo和公开API链接复用site_name、site_logo、api_base_url等站点设置；公开网关链接不决定浏览器管理API、提供商目标或更新来源。部署标识APP_IMAGE、COMPOSE_PROJECT_NAME和可选APP_CONTAINER_NAME独立配置。

## 开发资料

- [架构、需求及工作包](docs/architecture-rebuild/README.md)
- [编码规范](docs/architecture-rebuild/CODING_STANDARDS.md)
- [实施与实际验证](docs/architecture-rebuild/IMPLEMENTATION.md)
- [旧实现退出记录](docs/architecture-rebuild/RETIREMENT.md)
- [前端交接](docs/architecture-rebuild/FRONTEND_HANDOFF.md)
- [分支交付与资源收尾](docs/CHANGE_DELIVERY.md)
- [业务与协议资料索引](docs/README.md)

旧实现随对应功能迁移同步删除；仍被现有请求使用的模块在接管前保持运行。历史源代码从Git基线追索，不复制旧项目目录作为兼容层。

## 许可证与来源

保留[LICENSE](LICENSE)、代码中的版权声明及[来源和适配记录](docs/SOURCE_ADAPTATIONS.md)。无关广告、赞助名单和品牌导流不属于项目功能；法律文件、有效支付/订阅和供应商功能不按营销关键词误删。
