# 11 · 构建、手动发布与在线更新

归档日期：2026-10-07。文件快照：main `3669ffb0dc4f5b7ff83bd7bc7a50156dbd187238`；官方比较基线 `3f1a2ea0a760730e3bc528105c00b4ee4f23e469`。

本页登记最终需求与实际文件归属；不把本轮提交整理等同于重新实现或完整安全审计。

## 最终需求

1. 仅 holeenlu/sub2api 接收代码。main/kdan、TapModels/tapmodels、tokensavy/tokensavy 映射与 ghcr 镜像保持当前 channels.json，不重新引入独立 KDAN/erwinlin 推送目标。

2. 发布仅允许 workflow_dispatch 人工触发；普通 push/PR 运行质量检查，不发版、不部署、不自动执行生产迁移。前端 CI 安装 tools/zh-tw 依赖后执行测试与生成物检查。

3. 发布计划保留版本预留、重试、已集成官方基线的历史重建识别、发布来源记录及禁止旧版本回退的保护。整理不移动发布标签、不重打镜像，不以新 SHA 自动发版。

4. Compose 在线更新、二进制更新/回滚、备份恢复和安装脚本保留最终品牌行为；数据库备份和生产操作需要独立授权。保留 Docker socket/文件权限及现有服务识别约束。

5. 后续共享变更先在 main，再普通 merge 到品牌；本次是用户明确要求的本地历史重组例外。以后再次重写已同步 main 时，须同时说明品牌历史中旧提交的可达性，不把重复历史当作重复功能。

## 入口与配置归属

.github/workflows、scripts/release、deploy、update_service/update_compose；Git push 与发布是不同动作。

## 验收边界

发行工作流只手动触发，质量门禁完整；发布矩阵、在线更新、生成下载包及品牌渠道检查；本次没有调用 GitHub workflow 或任何生产机器。

验证状态统一见 [总说明](README.md#验证和未关闭边界)。本页列的是验收要求，不是宣称本轮已重新运行这些检查。

## 对比统计

| 文件类型 | 路径数 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| 工具/构建/配置 | 47 | 2000 | 1059 | 0 |
| 已有文档/许可 | 7 | 314 | 219 | 0 |
| 测试/夹具 | 21 | 1325 | 62 | 0 |
| 业务代码 | 9 | 861 | 519 | 0 |

统计为最终文件相对固定官方基线的累计差异，并非本次整理新写的代码。对重命名统一展开为删除/新增；跨域文件只设一个主归属。

## 修改文件

| 状态 | 类型 | +行 | −行 | 文件 |
| --- | --- | ---: | ---: | --- |
| M | 工具/构建/配置 | 16 | 3 | [.dockerignore](../../../.dockerignore) |
| M | 已有文档/许可 | 6 | 13 | [.github/release-tools/README.md](../../../.github/release-tools/README.md) |
| M | 工具/构建/配置 | 14 | 5 | [.github/release-tools/release-images.sh](../../../.github/release-tools/release-images.sh) |
| M | 工具/构建/配置 | 27 | 6 | [.github/release-tools/release_matrix.py](../../../.github/release-tools/release_matrix.py) |
| M | 测试/夹具 | 75 | 4 | [.github/release-tools/test_release_matrix.py](../../../.github/release-tools/test_release_matrix.py) |
| A | 工具/构建/配置 | 282 | 0 | [.github/workflows/automatic-release.yml](../../../.github/workflows/automatic-release.yml) |
| D | 工具/构建/配置 | 0 | 95 | `.github/workflows/backend-ci.yml`（已删除） |
| D | 工具/构建/配置 | 0 | 69 | `.github/workflows/cla.yml`（已删除） |
| A | 工具/构建/配置 | 56 | 0 | [.github/workflows/quality-gate.yml](../../../.github/workflows/quality-gate.yml) |
| D | 工具/构建/配置 | 0 | 408 | `.github/workflows/release.yml`（已删除） |
| D | 工具/构建/配置 | 0 | 58 | `.github/workflows/security-scan.yml`（已删除） |
| M | 工具/构建/配置 | 28 | 1 | [.gitignore](../../../.gitignore) |
| M | 工具/构建/配置 | 9 | 9 | [.goreleaser.simple.yaml](../../../.goreleaser.simple.yaml) |
| M | 工具/构建/配置 | 37 | 37 | [.goreleaser.yaml](../../../.goreleaser.yaml) |
| M | 工具/构建/配置 | 53 | 23 | [Dockerfile](../../../Dockerfile) |
| M | 工具/构建/配置 | 16 | 14 | [Dockerfile.goreleaser](../../../Dockerfile.goreleaser) |
| M | 工具/构建/配置 | 23 | 3 | [Makefile](../../../Makefile) |
| M | 工具/构建/配置 | 19 | 2 | [backend/Makefile](../../../backend/Makefile) |
| M | 业务代码 | 4 | 4 | [backend/internal/pkg/logger/options.go](../../../backend/internal/pkg/logger/options.go) |
| M | 测试/夹具 | 2 | 2 | [backend/internal/pkg/logger/options_test.go](../../../backend/internal/pkg/logger/options_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/pkg/sysutil/restart.go](../../../backend/internal/pkg/sysutil/restart.go) |
| M | 业务代码 | 2 | 2 | [backend/internal/platform/liveattestation/attestation.go](../../../backend/internal/platform/liveattestation/attestation.go) |
| M | 业务代码 | 3 | 0 | [backend/internal/repository/backup_pg_dumper.go](../../../backend/internal/repository/backup_pg_dumper.go) |
| M | 业务代码 | 17 | 3 | [backend/internal/service/backup_service.go](../../../backend/internal/service/backup_service.go) |
| M | 测试/夹具 | 20 | 3 | [backend/internal/service/backup_service_test.go](../../../backend/internal/service/backup_service_test.go) |
| M | 业务代码 | 1 | 1 | [backend/internal/service/data_management_service.go](../../../backend/internal/service/data_management_service.go) |
| A | 业务代码 | 75 | 0 | [backend/internal/service/update_compose.go](../../../backend/internal/service/update_compose.go) |
| A | 测试/夹具 | 82 | 0 | [backend/internal/service/update_release_channel_test.go](../../../backend/internal/service/update_release_channel_test.go) |
| M | 业务代码 | 239 | 44 | [backend/internal/service/update_service.go](../../../backend/internal/service/update_service.go) |
| M | 测试/夹具 | 112 | 4 | [backend/internal/service/update_service_test.go](../../../backend/internal/service/update_service_test.go) |
| M | 工具/构建/配置 | 39 | 15 | [deploy/.env.example](../../../deploy/.env.example) |
| M | 工具/构建/配置 | 1 | 1 | [deploy/.gitignore](../../../deploy/.gitignore) |
| M | 已有文档/许可 | 38 | 38 | [deploy/APPLE_CONTAINER.md](../../../deploy/APPLE_CONTAINER.md) |
| A | 已有文档/许可 | 115 | 0 | [deploy/AUTOMATIC_RELEASE.md](../../../deploy/AUTOMATIC_RELEASE.md) |
| M | 工具/构建/配置 | 2 | 2 | [deploy/Caddyfile](../../../deploy/Caddyfile) |
| M | 已有文档/许可 | 17 | 17 | [deploy/DATAMANAGEMENTD_CN.md](../../../deploy/DATAMANAGEMENTD_CN.md) |
| M | 已有文档/许可 | 14 | 12 | [deploy/DOCKER.md](../../../deploy/DOCKER.md) |
| M | 工具/构建/配置 | 20 | 16 | [deploy/Dockerfile](../../../deploy/Dockerfile) |
| M | 已有文档/许可 | 54 | 46 | [deploy/EDGE_SECURITY.md](../../../deploy/EDGE_SECURITY.md) |
| M | 已有文档/许可 | 70 | 93 | [deploy/README.md](../../../deploy/README.md) |
| M | 工具/构建/配置 | 39 | 39 | [deploy/apple-container.sh](../../../deploy/apple-container.sh) |
| M | 工具/构建/配置 | 2 | 1 | [deploy/build_image.sh](../../../deploy/build_image.sh) |
| A | 工具/构建/配置 | 14 | 0 | [deploy/compose-updater.example.json](../../../deploy/compose-updater.example.json) |
| A | 工具/构建/配置 | 18 | 0 | [deploy/compose-updater@.service](../../../deploy/compose-updater@.service) |
| A | 工具/构建/配置 | 9 | 0 | [deploy/compose.updater-socket.yml](../../../deploy/compose.updater-socket.yml) |
| A | 工具/构建/配置 | 299 | 0 | [deploy/compose_updater.py](../../../deploy/compose_updater.py) |
| M | 工具/构建/配置 | 51 | 24 | [deploy/config.example.yaml](../../../deploy/config.example.yaml) |
| M | 工具/构建/配置 | 24 | 19 | [deploy/docker-compose.dev.yml](../../../deploy/docker-compose.dev.yml) |
| M | 工具/构建/配置 | 113 | 20 | [deploy/docker-compose.local.yml](../../../deploy/docker-compose.local.yml) |
| M | 工具/构建/配置 | 41 | 10 | [deploy/docker-compose.standalone.yml](../../../deploy/docker-compose.standalone.yml) |
| A | 工具/构建/配置 | 30 | 0 | [deploy/docker-compose.wsl.yml](../../../deploy/docker-compose.wsl.yml) |
| M | 工具/构建/配置 | 51 | 20 | [deploy/docker-compose.yml](../../../deploy/docker-compose.yml) |
| M | 工具/构建/配置 | 6 | 6 | [deploy/docker-deploy.sh](../../../deploy/docker-deploy.sh) |
| M | 工具/构建/配置 | 6 | 6 | [deploy/docker-entrypoint.sh](../../../deploy/docker-entrypoint.sh) |
| A | 工具/构建/配置 | 85 | 0 | [deploy/install-compose-updater.py](../../../deploy/install-compose-updater.py) |
| M | 工具/构建/配置 | 16 | 16 | [deploy/install-datamanagementd.sh](../../../deploy/install-datamanagementd.sh) |
| M | 工具/构建/配置 | 119 | 76 | [deploy/install.sh](../../../deploy/install.sh) |
| A | 工具/构建/配置 | 22 | 0 | [deploy/kdan-datamanagementd.service](../../../deploy/kdan-datamanagementd.service) |
| A | 工具/构建/配置 | 33 | 0 | [deploy/kdan.service](../../../deploy/kdan.service) |
| A | 工具/构建/配置 | 43 | 0 | [deploy/release_metadata.py](../../../deploy/release_metadata.py) |
| D | 工具/构建/配置 | 0 | 22 | `deploy/sub2api-datamanagementd.service`（已删除） |
| D | 工具/构建/配置 | 0 | 33 | `deploy/sub2api.service`（已删除） |
| M | 测试/夹具 | 31 | 31 | [deploy/tests/apple-container-test.sh](../../../deploy/tests/apple-container-test.sh) |
| M | 测试/夹具 | 1 | 1 | [deploy/tests/docker-compose-gateway-env-test.sh](../../../deploy/tests/docker-compose-gateway-env-test.sh) |
| M | 测试/夹具 | 2 | 2 | [deploy/tests/docker-compose-security-test.sh](../../../deploy/tests/docker-compose-security-test.sh) |
| M | 测试/夹具 | 4 | 1 | [deploy/tests/docker-compose-simple-mode-env-test.sh](../../../deploy/tests/docker-compose-simple-mode-env-test.sh) |
| M | 测试/夹具 | 2 | 2 | [deploy/tests/docker-runtime-resources-test.sh](../../../deploy/tests/docker-runtime-resources-test.sh) |
| M | 测试/夹具 | 4 | 4 | [deploy/tests/fixtures/bin/container](../../../deploy/tests/fixtures/bin/container) |
| M | 测试/夹具 | 8 | 8 | [deploy/tests/install-github-token-test.sh](../../../deploy/tests/install-github-token-test.sh) |
| A | 测试/夹具 | 278 | 0 | [deploy/tests/test_auto_release.py](../../../deploy/tests/test_auto_release.py) |
| A | 测试/夹具 | 62 | 0 | [deploy/tests/test_auto_release_compose.py](../../../deploy/tests/test_auto_release_compose.py) |
| A | 测试/夹具 | 87 | 0 | [deploy/tests/test_auto_release_installer.py](../../../deploy/tests/test_auto_release_installer.py) |
| A | 测试/夹具 | 84 | 0 | [deploy/tests/test_auto_release_workflow.py](../../../deploy/tests/test_auto_release_workflow.py) |
| A | 测试/夹具 | 169 | 0 | [deploy/tests/test_brand_release.py](../../../deploy/tests/test_brand_release.py) |
| A | 测试/夹具 | 25 | 0 | [deploy/tests/test_runtime_permissions.py](../../../deploy/tests/test_runtime_permissions.py) |
| A | 工具/构建/配置 | 39 | 0 | [deploy/upstream_sync_instructions.py](../../../deploy/upstream_sync_instructions.py) |
| M | 业务代码 | 519 | 464 | [frontend/src/components/common/VersionBadge.vue](../../../frontend/src/components/common/VersionBadge.vue) |
| A | 测试/夹具 | 140 | 0 | [frontend/src/components/common/__tests__/VersionBadge.spec.ts](../../../frontend/src/components/common/__tests__/VersionBadge.spec.ts) |
| A | 测试/夹具 | 55 | 0 | [frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts](../../../frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts) |
| A | 工具/构建/配置 | 21 | 0 | [scripts/release/channels.json](../../../scripts/release/channels.json) |
| A | 工具/构建/配置 | 17 | 0 | [scripts/release/notes.py](../../../scripts/release/notes.py) |
| A | 工具/构建/配置 | 171 | 0 | [scripts/release/plan.py](../../../scripts/release/plan.py) |
| A | 工具/构建/配置 | 89 | 0 | [tools/change_delivery.py](../../../tools/change_delivery.py) |
| A | 测试/夹具 | 82 | 0 | [tools/tests/test_change_delivery.py](../../../tools/tests/test_change_delivery.py) |
