# 15 · 构建、手动发布、安装与在线更新

归档日期：2026-10-13；官方基线 `01842576b3d5c1e2545b5a99ad555d36e28ed306`；最终快照 `acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286`。


2026-10-14 增量归并：本页已纳入通过验收的 New API 实现，来源和逐文件归属见 [归并分析](NEW-API-INTEGRATION-2026-10-14.md)。原快照 SHA 仅代表 2026-10-13 历史输入。

## 最终需求

1. 仅 holeenlu/sub2api 接收代码。main/kdan、TapModels/tapmodels、tokensavy/tokensavy 映射与 ghcr 镜像保持当前 channels.json，不重新引入独立 KDAN/erwinlin 推送目标。

2. 发布仅允许 workflow_dispatch 人工触发；普通 push/PR 运行质量检查，不发版、不部署、不自动执行生产迁移。前端 CI 安装 tools/zh-tw 依赖后执行测试与生成物检查。

3. 发布计划保留版本预留、重试、已集成官方基线的历史重建识别、发布来源记录及禁止旧版本回退的保护。整理不移动发布标签、不重打镜像，不以新 SHA 自动发版。

4. Compose 在线更新、二进制更新/回滚、备份恢复和安装脚本保留最终品牌行为；数据库备份和生产操作需要独立授权。保留 Docker socket/文件权限及现有服务识别约束。

5. 后续共享变更先在 main，再普通 merge 到品牌；本次是用户明确要求的本地历史重组例外。以后再次重写已同步 main 时，须同时说明品牌历史中旧提交的可达性，不把重复历史当作重复功能。

## 入口与配置归属

.github/workflows、scripts/release、deploy、update_service/update_compose；Git push 与发布是不同动作。

## 最终实现方案

1. channels.json 单一映射 main/kdan、TapModels/tapmodels、tokensavy/tokensavy；保留全手动 workflow_dispatch 发布。

2. 镜像构建保留已修复的 BuildKit 来源，避免 Docker Hub bootstrap；发布计划保留版本预留、重试、来源与防降级保护。

3. 安装、Compose/二进制更新、回滚、权限和资源约束保留既有实现；历史重写只产生本地分支，不执行部署/迁移。

## 边界、失败路径与依赖

关联功能组：05, 06, 07。 同一文件只落一个主提交；关联关系不代表新增模块。跨功能路由、设置、类型和装配应与完整提交集一起使用。此组与关联组共同形成完整运行行为；本次归并不额外改写已验收的业务源码。

## 验收要求

manual workflow、release matrix、品牌渠道/在线更新本地测试；不 dispatch workflow。 本轮实际执行结果在 VALIDATION.md 单列；旧记录中的通过结果仅是来源证据。

## 累计差异统计

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 7 | 316 | 219 | 0 |
| source | 9 | 861 | 519 | 0 |
| test | 21 | 1338 | 62 | 0 |
| tooling | 47 | 2043 | 1060 | 0 |

相对官方 v0.2.16 的累计差异，已包含本批实现；不是本次归并额外编写的代码。

## 来源提交

- `3bd5cd406dd37b332f01a446718caca3a8a4222f`
- `49b29ef158e6319cfd6b1227781307de5f8f870e`
- `4b2cfde513867c40dbcc663a5ddea30cdd286dd2`
- `4c5aa7b49988f5752b01940475530832c188b7ad`
- `5dd818c1389af73a5ee524303dfad1966e0ae926`
- `944fdb1eff8e7ea33ed70ff722b6822f962e9255`
- `c6a92e1501a3ed5bb405c02bd541fcb59366bd28`
- `f9daed217b4d198a26178c1610ca02a8d9d903f9`

原始作者、提交正文和更早来源见 source-commits.json；最终对象见 files-main.tsv。

## 文件与方案落点

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | tooling | 16 | 3 | 15 | [.dockerignore](../../../.dockerignore) |
| M | documentation | 6 | 13 | 15 | [.github/release-tools/README.md](../../../.github/release-tools/README.md) |
| M | tooling | 14 | 5 | 15 | [.github/release-tools/release-images.sh](../../../.github/release-tools/release-images.sh) |
| M | tooling | 27 | 6 | 15 | [.github/release-tools/release_matrix.py](../../../.github/release-tools/release_matrix.py) |
| M | test | 75 | 4 | 15 | [.github/release-tools/test_release_matrix.py](../../../.github/release-tools/test_release_matrix.py) |
| A | tooling | 289 | 0 | 15 | [.github/workflows/automatic-release.yml](../../../.github/workflows/automatic-release.yml) |
| D | tooling | 0 | 95 | 15 | [.github/workflows/backend-ci.yml](../../../.github/workflows/backend-ci.yml) |
| D | tooling | 0 | 69 | 15 | [.github/workflows/cla.yml](../../../.github/workflows/cla.yml) |
| A | tooling | 56 | 0 | 15 | [.github/workflows/quality-gate.yml](../../../.github/workflows/quality-gate.yml) |
| D | tooling | 0 | 408 | 15 | [.github/workflows/release.yml](../../../.github/workflows/release.yml) |
| D | tooling | 0 | 58 | 15 | [.github/workflows/security-scan.yml](../../../.github/workflows/security-scan.yml) |
| M | tooling | 44 | 1 | 05,07,15 | [.gitignore](../../../.gitignore) |
| M | tooling | 9 | 9 | 15 | [.goreleaser.simple.yaml](../../../.goreleaser.simple.yaml) |
| M | tooling | 37 | 37 | 15 | [.goreleaser.yaml](../../../.goreleaser.yaml) |
| M | tooling | 53 | 23 | 15 | [Dockerfile](../../../Dockerfile) |
| M | tooling | 16 | 14 | 15 | [Dockerfile.goreleaser](../../../Dockerfile.goreleaser) |
| M | tooling | 23 | 4 | 15 | [Makefile](../../../Makefile) |
| M | tooling | 19 | 2 | 15 | [backend/Makefile](../../../backend/Makefile) |
| M | source | 4 | 4 | 15 | [backend/internal/pkg/logger/options.go](../../../backend/internal/pkg/logger/options.go) |
| M | test | 2 | 2 | 15 | [backend/internal/pkg/logger/options_test.go](../../../backend/internal/pkg/logger/options_test.go) |
| M | source | 1 | 1 | 15 | [backend/internal/pkg/sysutil/restart.go](../../../backend/internal/pkg/sysutil/restart.go) |
| M | source | 2 | 2 | 15 | [backend/internal/platform/liveattestation/attestation.go](../../../backend/internal/platform/liveattestation/attestation.go) |
| M | source | 3 | 0 | 15 | [backend/internal/repository/backup_pg_dumper.go](../../../backend/internal/repository/backup_pg_dumper.go) |
| M | source | 17 | 3 | 15 | [backend/internal/service/backup_service.go](../../../backend/internal/service/backup_service.go) |
| M | test | 20 | 3 | 15 | [backend/internal/service/backup_service_test.go](../../../backend/internal/service/backup_service_test.go) |
| M | source | 1 | 1 | 15 | [backend/internal/service/data_management_service.go](../../../backend/internal/service/data_management_service.go) |
| A | source | 75 | 0 | 15 | [backend/internal/service/update_compose.go](../../../backend/internal/service/update_compose.go) |
| A | test | 82 | 0 | 15 | [backend/internal/service/update_release_channel_test.go](../../../backend/internal/service/update_release_channel_test.go) |
| M | source | 239 | 44 | 15 | [backend/internal/service/update_service.go](../../../backend/internal/service/update_service.go) |
| M | test | 112 | 4 | 15 | [backend/internal/service/update_service_test.go](../../../backend/internal/service/update_service_test.go) |
| M | tooling | 39 | 15 | 15 | [deploy/.env.example](../../../deploy/.env.example) |
| M | tooling | 1 | 1 | 15 | [deploy/.gitignore](../../../deploy/.gitignore) |
| M | documentation | 38 | 38 | 15 | [deploy/APPLE_CONTAINER.md](../../../deploy/APPLE_CONTAINER.md) |
| A | documentation | 117 | 0 | 15 | [deploy/AUTOMATIC_RELEASE.md](../../../deploy/AUTOMATIC_RELEASE.md) |
| M | tooling | 2 | 2 | 15 | [deploy/Caddyfile](../../../deploy/Caddyfile) |
| M | documentation | 17 | 17 | 15 | [deploy/DATAMANAGEMENTD_CN.md](../../../deploy/DATAMANAGEMENTD_CN.md) |
| M | documentation | 14 | 12 | 15 | [deploy/DOCKER.md](../../../deploy/DOCKER.md) |
| M | tooling | 20 | 16 | 15 | [deploy/Dockerfile](../../../deploy/Dockerfile) |
| M | documentation | 54 | 46 | 15 | [deploy/EDGE_SECURITY.md](../../../deploy/EDGE_SECURITY.md) |
| M | documentation | 70 | 93 | 15 | [deploy/README.md](../../../deploy/README.md) |
| M | tooling | 39 | 39 | 15 | [deploy/apple-container.sh](../../../deploy/apple-container.sh) |
| M | tooling | 2 | 1 | 15 | [deploy/build_image.sh](../../../deploy/build_image.sh) |
| A | tooling | 14 | 0 | 15 | [deploy/compose-updater.example.json](../../../deploy/compose-updater.example.json) |
| A | tooling | 18 | 0 | 15 | [deploy/compose-updater@.service](../../../deploy/compose-updater@.service) |
| A | tooling | 9 | 0 | 15 | [deploy/compose.updater-socket.yml](../../../deploy/compose.updater-socket.yml) |
| A | tooling | 309 | 0 | 15 | [deploy/compose_updater.py](../../../deploy/compose_updater.py) |
| M | tooling | 61 | 24 | 06,10,15 | [deploy/config.example.yaml](../../../deploy/config.example.yaml) |
| M | tooling | 24 | 19 | 15 | [deploy/docker-compose.dev.yml](../../../deploy/docker-compose.dev.yml) |
| M | tooling | 113 | 20 | 15 | [deploy/docker-compose.local.yml](../../../deploy/docker-compose.local.yml) |
| M | tooling | 41 | 10 | 15 | [deploy/docker-compose.standalone.yml](../../../deploy/docker-compose.standalone.yml) |
| A | tooling | 30 | 0 | 15 | [deploy/docker-compose.wsl.yml](../../../deploy/docker-compose.wsl.yml) |
| M | tooling | 51 | 20 | 15 | [deploy/docker-compose.yml](../../../deploy/docker-compose.yml) |
| M | tooling | 6 | 6 | 15 | [deploy/docker-deploy.sh](../../../deploy/docker-deploy.sh) |
| M | tooling | 6 | 6 | 15 | [deploy/docker-entrypoint.sh](../../../deploy/docker-entrypoint.sh) |
| A | tooling | 85 | 0 | 15 | [deploy/install-compose-updater.py](../../../deploy/install-compose-updater.py) |
| M | tooling | 16 | 16 | 15 | [deploy/install-datamanagementd.sh](../../../deploy/install-datamanagementd.sh) |
| M | tooling | 119 | 76 | 15 | [deploy/install.sh](../../../deploy/install.sh) |
| A | tooling | 22 | 0 | 15 | [deploy/kdan-datamanagementd.service](../../../deploy/kdan-datamanagementd.service) |
| A | tooling | 33 | 0 | 15 | [deploy/kdan.service](../../../deploy/kdan.service) |
| A | tooling | 43 | 0 | 15 | [deploy/release_metadata.py](../../../deploy/release_metadata.py) |
| D | tooling | 0 | 22 | 15 | [deploy/sub2api-datamanagementd.service](../../../deploy/sub2api-datamanagementd.service) |
| D | tooling | 0 | 33 | 15 | [deploy/sub2api.service](../../../deploy/sub2api.service) |
| M | test | 31 | 31 | 15 | [deploy/tests/apple-container-test.sh](../../../deploy/tests/apple-container-test.sh) |
| M | test | 1 | 1 | 15 | [deploy/tests/docker-compose-gateway-env-test.sh](../../../deploy/tests/docker-compose-gateway-env-test.sh) |
| M | test | 2 | 2 | 15 | [deploy/tests/docker-compose-security-test.sh](../../../deploy/tests/docker-compose-security-test.sh) |
| M | test | 4 | 1 | 15 | [deploy/tests/docker-compose-simple-mode-env-test.sh](../../../deploy/tests/docker-compose-simple-mode-env-test.sh) |
| M | test | 2 | 2 | 15 | [deploy/tests/docker-runtime-resources-test.sh](../../../deploy/tests/docker-runtime-resources-test.sh) |
| M | test | 4 | 4 | 15 | [deploy/tests/fixtures/bin/container](../../../deploy/tests/fixtures/bin/container) |
| M | test | 8 | 8 | 15 | [deploy/tests/install-github-token-test.sh](../../../deploy/tests/install-github-token-test.sh) |
| A | test | 291 | 0 | 15 | [deploy/tests/test_auto_release.py](../../../deploy/tests/test_auto_release.py) |
| A | test | 62 | 0 | 15 | [deploy/tests/test_auto_release_compose.py](../../../deploy/tests/test_auto_release_compose.py) |
| A | test | 87 | 0 | 15 | [deploy/tests/test_auto_release_installer.py](../../../deploy/tests/test_auto_release_installer.py) |
| A | test | 84 | 0 | 15 | [deploy/tests/test_auto_release_workflow.py](../../../deploy/tests/test_auto_release_workflow.py) |
| A | test | 169 | 0 | 15 | [deploy/tests/test_brand_release.py](../../../deploy/tests/test_brand_release.py) |
| A | test | 25 | 0 | 15 | [deploy/tests/test_runtime_permissions.py](../../../deploy/tests/test_runtime_permissions.py) |
| A | tooling | 39 | 0 | 15 | [deploy/upstream_sync_instructions.py](../../../deploy/upstream_sync_instructions.py) |
| M | source | 519 | 464 | 15 | [frontend/src/components/common/VersionBadge.vue](../../../frontend/src/components/common/VersionBadge.vue) |
| A | test | 140 | 0 | 15 | [frontend/src/components/common/__tests__/VersionBadge.spec.ts](../../../frontend/src/components/common/__tests__/VersionBadge.spec.ts) |
| A | test | 55 | 0 | 15 | [frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts](../../../frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts) |
| A | tooling | 21 | 0 | 15 | [scripts/release/channels.json](../../../scripts/release/channels.json) |
| A | tooling | 17 | 0 | 15 | [scripts/release/notes.py](../../../scripts/release/notes.py) |
| A | tooling | 171 | 0 | 15 | [scripts/release/plan.py](../../../scripts/release/plan.py) |
| A | tooling | 89 | 0 | 15 | [tools/change_delivery.py](../../../tools/change_delivery.py) |
| A | test | 82 | 0 | 15 | [tools/tests/test_change_delivery.py](../../../tools/tests/test_change_delivery.py) |

## 2026-10-14 新增需求与实施边界

示例配置补齐 usage_export.max_range_days=90、max_rows=100000、timeout_seconds=120；不改发版触发器、品牌渠道、部署目标或版本。
