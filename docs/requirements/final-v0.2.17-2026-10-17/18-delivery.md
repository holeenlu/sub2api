# 18 · 手动发布、渠道、构建、安装与在线更新

归档日期：2026-10-17；官方基线 `060d0160417782cd962a0c7f460f6cd4565d4916`；重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

目标提交：`chore(delivery): preserve manual workflows and release channels`。这是当前最终需求及其现有实现的整理，不按旧开发过程重放已被覆盖的版本。

## 最终需求

1. 代码仅交付 holeenlu/sub2api：main/kdan、TapModels/tapmodels、tokensavy/tokensavy，完整 refspec 推送。保留品牌镜像、部署项目名、服务识别、安装和回滚行为。
2. 所有 GitHub Workflows（包含质量检查）都仅 workflow_dispatch 手动触发；push、PR、schedule 不自动执行工作流。普通提交不发版、不部署、不迁移，也不自动启动下一次发布。
3. 保留发布版本预留、重试、历史重建后的基线识别、来源记录、禁止回退与镜像 latest 的顺序保护。backend/cmd/server/VERSION 沿用当前 0.2.17；官方标签本身的版本文件仍是 0.2.16，因此该一行差异属于正式版本标记维护。
4. 在线更新、Compose、二进制更新/回滚、数据库备份提示及 Docker socket/文件权限保持。有效已发布标签和 Release 附件不因美化历史而删除或移动，避免再次破坏更新渠道。
5. 当前重建采用仓库外完整备份和精确旧 SHA 的 force-with-lease；三品牌共享同一新 main，派生品牌各一笔最终覆盖。后续共享改动继续普通 merge。

## 入口与配置归属

复用既有入口、配置和授权；具体落点见下方逐文件清单与关联功能组。

## 最终实现方案

1. scripts/release/channels.json 统一三个渠道，原 release plan、安装器及更新器继续使用本仓库数据；质量检查和发版触发器由原测试守护。
2. 不在 refs/heads、refs/backup 或本地发布标签留下重建备份；备份 bundle、工作区补丁和原文件存于仓库外目录。
3. 远端原标签/Release/附件逐项拍照比对；正式分支用精确旧值原子更新并通过 ls-remote 再确认。新基线使用官方 v0.2.17 标签，不触碰其他仓库。

## 边界、失败路径与依赖

本组文件只提交一次，共用文件通过 files-main.tsv 的 related_groups 关联。第 19 组统一收录跨功能配置、路由、DTO、依赖与装配；完整组合是验证与部署单位。新归档不新增业务接口、配置项或执行路径。历史设计和退役能力不能仅因保留说明或迁移记录而重新启用。

## 验收要求

manual workflow 守护、release matrix、发布/安装/更新脚本和下载包校验；只运行本地测试，不 dispatch Actions 或部署。

本轮实际执行与限制见 [VALIDATION.md](VALIDATION.md)。来源文档中的早期通过记录不能代替本轮结果。

## 相对官方 v0.2.17 的累计差异

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| documentation | 8 | 382 | 219 | 0 |
| source | 14 | 959 | 524 | 0 |
| test | 22 | 1543 | 62 | 0 |
| tooling | 45 | 2173 | 1056 | 0 |

这是既有最终代码相对官方基线的累计差异；不等于此次新写代码。重命名按删除和新增分别列出，二进制不折算行数。本归档新增文档不计入表内。

## 来源与最终版本

本组涉及 47 个可达来源提交。完整作者、原提交正文及父子关系见 [source-commits.json](source-commits.json)，每个文件的来源及旧归档来源见 [files-main.tsv](files-main.tsv)。来源列表表示曾触及该文件，不据此声称逐行归属；最终 blob / mode 才是重建依据。

## 文件与方案落点

文件链接指向 main 的对应路径；精确归档版本以 TSV 中的 blob 和固定来源 SHA 为准。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 16 | 3 | 18 | [.dockerignore](https://github.com/holeenlu/sub2api/blob/main/.dockerignore) |
| M | documentation | 6 | 13 | 18 | [.github/release-tools/README.md](https://github.com/holeenlu/sub2api/blob/main/.github/release-tools/README.md) |
| M | tooling | 14 | 5 | 18 | [.github/release-tools/release-images.sh](https://github.com/holeenlu/sub2api/blob/main/.github/release-tools/release-images.sh) |
| M | tooling | 27 | 6 | 18 | [.github/release-tools/release_matrix.py](https://github.com/holeenlu/sub2api/blob/main/.github/release-tools/release_matrix.py) |
| M | test | 75 | 4 | 18 | [.github/release-tools/test_release_matrix.py](https://github.com/holeenlu/sub2api/blob/main/.github/release-tools/test_release_matrix.py) |
| A | tooling | 289 | 0 | 18 | [.github/workflows/automatic-release.yml](https://github.com/holeenlu/sub2api/blob/main/.github/workflows/automatic-release.yml) |
| D | tooling | 0 | 95 | 18 | `.github/workflows/backend-ci.yml` |
| D | tooling | 0 | 69 | 18 | `.github/workflows/cla.yml` |
| A | tooling | 53 | 0 | 18 | [.github/workflows/quality-gate.yml](https://github.com/holeenlu/sub2api/blob/main/.github/workflows/quality-gate.yml) |
| D | tooling | 0 | 408 | 18 | `.github/workflows/release.yml` |
| D | tooling | 0 | 58 | 18 | `.github/workflows/security-scan.yml` |
| M | source | 69 | 1 | 05,07,12,14,18 | [.gitignore](https://github.com/holeenlu/sub2api/blob/main/.gitignore) |
| M | tooling | 9 | 9 | 18 | [.goreleaser.simple.yaml](https://github.com/holeenlu/sub2api/blob/main/.goreleaser.simple.yaml) |
| M | tooling | 37 | 37 | 18 | [.goreleaser.yaml](https://github.com/holeenlu/sub2api/blob/main/.goreleaser.yaml) |
| M | tooling | 53 | 23 | 18 | [Dockerfile](https://github.com/holeenlu/sub2api/blob/main/Dockerfile) |
| M | tooling | 16 | 14 | 18 | [Dockerfile.goreleaser](https://github.com/holeenlu/sub2api/blob/main/Dockerfile.goreleaser) |
| M | tooling | 23 | 4 | 18 | [Makefile](https://github.com/holeenlu/sub2api/blob/main/Makefile) |
| M | tooling | 19 | 2 | 18 | [backend/Makefile](https://github.com/holeenlu/sub2api/blob/main/backend/Makefile) |
| M | source | 1 | 1 | 18 | [backend/cmd/server/VERSION](https://github.com/holeenlu/sub2api/blob/main/backend/cmd/server/VERSION) |
| M | source | 4 | 4 | 18 | [backend/internal/pkg/logger/options.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/logger/options.go) |
| M | test | 2 | 2 | 18 | [backend/internal/pkg/logger/options_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/logger/options_test.go) |
| M | source | 1 | 1 | 18 | [backend/internal/pkg/sysutil/restart.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/pkg/sysutil/restart.go) |
| M | source | 2 | 2 | 18 | [backend/internal/platform/liveattestation/attestation.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/platform/liveattestation/attestation.go) |
| M | source | 3 | 0 | 18 | [backend/internal/repository/backup_pg_dumper.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/repository/backup_pg_dumper.go) |
| M | source | 17 | 3 | 18 | [backend/internal/service/backup_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/backup_service.go) |
| M | test | 20 | 3 | 18 | [backend/internal/service/backup_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/backup_service_test.go) |
| M | source | 1 | 1 | 18 | [backend/internal/service/data_management_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/data_management_service.go) |
| A | source | 75 | 0 | 18 | [backend/internal/service/update_compose.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/update_compose.go) |
| A | test | 82 | 0 | 18 | [backend/internal/service/update_release_channel_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/update_release_channel_test.go) |
| M | source | 239 | 44 | 18 | [backend/internal/service/update_service.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/update_service.go) |
| M | test | 112 | 4 | 18 | [backend/internal/service/update_service_test.go](https://github.com/holeenlu/sub2api/blob/main/backend/internal/service/update_service_test.go) |
| M | tooling | 39 | 15 | 18 | [deploy/.env.example](https://github.com/holeenlu/sub2api/blob/main/deploy/.env.example) |
| M | tooling | 1 | 1 | 18 | [deploy/.gitignore](https://github.com/holeenlu/sub2api/blob/main/deploy/.gitignore) |
| M | documentation | 38 | 38 | 18 | [deploy/APPLE_CONTAINER.md](https://github.com/holeenlu/sub2api/blob/main/deploy/APPLE_CONTAINER.md) |
| A | documentation | 131 | 0 | 18 | [deploy/AUTOMATIC_RELEASE.md](https://github.com/holeenlu/sub2api/blob/main/deploy/AUTOMATIC_RELEASE.md) |
| M | tooling | 5 | 2 | 18 | [deploy/Caddyfile](https://github.com/holeenlu/sub2api/blob/main/deploy/Caddyfile) |
| M | documentation | 17 | 17 | 18 | [deploy/DATAMANAGEMENTD_CN.md](https://github.com/holeenlu/sub2api/blob/main/deploy/DATAMANAGEMENTD_CN.md) |
| M | documentation | 14 | 12 | 18 | [deploy/DOCKER.md](https://github.com/holeenlu/sub2api/blob/main/deploy/DOCKER.md) |
| M | tooling | 20 | 16 | 18 | [deploy/Dockerfile](https://github.com/holeenlu/sub2api/blob/main/deploy/Dockerfile) |
| M | documentation | 92 | 46 | 18 | [deploy/EDGE_SECURITY.md](https://github.com/holeenlu/sub2api/blob/main/deploy/EDGE_SECURITY.md) |
| M | documentation | 70 | 93 | 18 | [deploy/README.md](https://github.com/holeenlu/sub2api/blob/main/deploy/README.md) |
| M | tooling | 39 | 39 | 18 | [deploy/apple-container.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/apple-container.sh) |
| M | tooling | 2 | 1 | 18 | [deploy/build_image.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/build_image.sh) |
| A | tooling | 14 | 0 | 18 | [deploy/compose-updater.example.json](https://github.com/holeenlu/sub2api/blob/main/deploy/compose-updater.example.json) |
| A | tooling | 18 | 0 | 18 | [deploy/compose-updater@.service](https://github.com/holeenlu/sub2api/blob/main/deploy/compose-updater%40.service) |
| A | tooling | 9 | 0 | 18 | [deploy/compose.updater-socket.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/compose.updater-socket.yml) |
| A | tooling | 309 | 0 | 18 | [deploy/compose_updater.py](https://github.com/holeenlu/sub2api/blob/main/deploy/compose_updater.py) |
| M | tooling | 61 | 24 | 06,10,18 | [deploy/config.example.yaml](https://github.com/holeenlu/sub2api/blob/main/deploy/config.example.yaml) |
| M | tooling | 24 | 19 | 18 | [deploy/docker-compose.dev.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-compose.dev.yml) |
| M | tooling | 113 | 20 | 18 | [deploy/docker-compose.local.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-compose.local.yml) |
| M | tooling | 41 | 10 | 18 | [deploy/docker-compose.standalone.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-compose.standalone.yml) |
| A | tooling | 30 | 0 | 18 | [deploy/docker-compose.wsl.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-compose.wsl.yml) |
| M | tooling | 51 | 20 | 18 | [deploy/docker-compose.yml](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-compose.yml) |
| M | tooling | 6 | 6 | 18 | [deploy/docker-deploy.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-deploy.sh) |
| M | tooling | 6 | 6 | 18 | [deploy/docker-entrypoint.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/docker-entrypoint.sh) |
| A | tooling | 85 | 0 | 18 | [deploy/install-compose-updater.py](https://github.com/holeenlu/sub2api/blob/main/deploy/install-compose-updater.py) |
| M | tooling | 16 | 16 | 18 | [deploy/install-datamanagementd.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/install-datamanagementd.sh) |
| M | tooling | 119 | 76 | 18 | [deploy/install.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/install.sh) |
| A | tooling | 22 | 0 | 18 | [deploy/kdan-datamanagementd.service](https://github.com/holeenlu/sub2api/blob/main/deploy/kdan-datamanagementd.service) |
| A | tooling | 33 | 0 | 18 | [deploy/kdan.service](https://github.com/holeenlu/sub2api/blob/main/deploy/kdan.service) |
| A | documentation | 14 | 0 | 18 | [deploy/release-notes/0.2.17.2.md](https://github.com/holeenlu/sub2api/blob/main/deploy/release-notes/0.2.17.2.md) |
| A | tooling | 43 | 0 | 18 | [deploy/release_metadata.py](https://github.com/holeenlu/sub2api/blob/main/deploy/release_metadata.py) |
| D | tooling | 0 | 22 | 18 | `deploy/sub2api-datamanagementd.service` |
| D | tooling | 0 | 33 | 18 | `deploy/sub2api.service` |
| M | test | 31 | 31 | 18 | [deploy/tests/apple-container-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/apple-container-test.sh) |
| M | test | 1 | 1 | 18 | [deploy/tests/docker-compose-gateway-env-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/docker-compose-gateway-env-test.sh) |
| M | test | 2 | 2 | 18 | [deploy/tests/docker-compose-security-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/docker-compose-security-test.sh) |
| M | test | 4 | 1 | 18 | [deploy/tests/docker-compose-simple-mode-env-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/docker-compose-simple-mode-env-test.sh) |
| M | test | 2 | 2 | 18 | [deploy/tests/docker-runtime-resources-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/docker-runtime-resources-test.sh) |
| M | test | 4 | 4 | 18 | [deploy/tests/fixtures/bin/container](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/fixtures/bin/container) |
| M | test | 8 | 8 | 18 | [deploy/tests/install-github-token-test.sh](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/install-github-token-test.sh) |
| A | test | 310 | 0 | 18 | [deploy/tests/test_auto_release.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_auto_release.py) |
| A | test | 62 | 0 | 18 | [deploy/tests/test_auto_release_compose.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_auto_release_compose.py) |
| A | test | 87 | 0 | 18 | [deploy/tests/test_auto_release_installer.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_auto_release_installer.py) |
| A | test | 188 | 0 | 18 | [deploy/tests/test_auto_release_notes.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_auto_release_notes.py) |
| A | test | 82 | 0 | 18 | [deploy/tests/test_auto_release_workflow.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_auto_release_workflow.py) |
| A | test | 169 | 0 | 18 | [deploy/tests/test_brand_release.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_brand_release.py) |
| A | test | 25 | 0 | 18 | [deploy/tests/test_runtime_permissions.py](https://github.com/holeenlu/sub2api/blob/main/deploy/tests/test_runtime_permissions.py) |
| A | tooling | 39 | 0 | 18 | [deploy/upstream_sync_instructions.py](https://github.com/holeenlu/sub2api/blob/main/deploy/upstream_sync_instructions.py) |
| M | source | 2 | 0 | 12,18 | [frontend/.eslintignore](https://github.com/holeenlu/sub2api/blob/main/frontend/.eslintignore) |
| A | source | 10 | 0 | 12,18 | [frontend/.gitignore](https://github.com/holeenlu/sub2api/blob/main/frontend/.gitignore) |
| M | source | 519 | 464 | 18 | [frontend/src/components/common/VersionBadge.vue](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/VersionBadge.vue) |
| A | test | 140 | 0 | 18 | [frontend/src/components/common/__tests__/VersionBadge.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/VersionBadge.spec.ts) |
| A | test | 55 | 0 | 18 | [frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts](https://github.com/holeenlu/sub2api/blob/main/frontend/src/components/common/__tests__/VersionBadgeCompose.spec.ts) |
| A | tooling | 21 | 0 | 18 | [scripts/release/channels.json](https://github.com/holeenlu/sub2api/blob/main/scripts/release/channels.json) |
| A | tooling | 189 | 0 | 18 | [scripts/release/notes.py](https://github.com/holeenlu/sub2api/blob/main/scripts/release/notes.py) |
| A | tooling | 189 | 0 | 18 | [scripts/release/plan.py](https://github.com/holeenlu/sub2api/blob/main/scripts/release/plan.py) |
| A | tooling | 89 | 0 | 18 | [tools/change_delivery.py](https://github.com/holeenlu/sub2api/blob/main/tools/change_delivery.py) |
| A | test | 82 | 0 | 18 | [tools/tests/test_change_delivery.py](https://github.com/holeenlu/sub2api/blob/main/tools/tests/test_change_delivery.py) |
