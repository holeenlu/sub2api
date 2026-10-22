# TapModels · 最终品牌覆盖

重建前最终分支：`bf2c1d8580f5a00b3369f43e5edd0f2a5cde2ad4`。
比较基准：重建前最终 main `e6b3b332dc1fba704db6e07157c4b4674bed2437`。

## 需求与方案

本品牌与 KDAN 保持独立站点、部署、数据、名称、标识、域名、法律主体和商业文案。原已交付的服务地址、客户端下载配置、镜像/Compose 项目、发布渠道、页面与多语覆盖按该分支最终文件保留，不能用 main 的完整文件覆盖品牌差异。

共享业务直接继承本轮新 main 的 20 笔提交；本品牌全部最终差异合为一笔品牌覆盖提交，不再拆成 runtime、clients、presentation、delivery、records 等多笔，也不重放历史同步 merge。未来共享需求仍按交付规范普通 merge。

本表保留完整分支差异，包括既有历史说明和生成产物，不把品牌差异的路径数当成独立业务需求数。跨品牌共用的两个 SQL 测试修正随 main 继承，本品牌相对 main 无额外测试修正。

## 逐文件核验

除本轮新增需求归档和两处共享测试预期，重建后的本品牌所有现有路径、blob 和 mode 必须与上述原分支完全一致；品牌功能、配置、资产和已生成下载包不得遗漏。未提交的调查草稿不进入品牌提交。

| 类别 | 路径项 | 新增行 | 删除行 | 二进制项 |
| --- | ---: | ---: | ---: | ---: |
| asset | 3 | 54 | 73 | 0 |
| documentation | 36 | 1100 | 299 | 0 |
| generated | 54 | 2800 | 1958 | 7 |
| source | 127 | 496 | 548 | 0 |
| test | 16 | 107 | 107 | 0 |
| tooling | 27 | 428 | 428 | 0 |

相对最终 main 共 263 个路径项，重命名拆成删除/新增；完整模式、对象和来源见 [files-TapModels.tsv](files-TapModels.tsv)。

文件链接指向 TapModels 的对应路径，避免从 main 阅读归档时误指向另一品牌。

| 状态 | 类别 | +行 | −行 | 关联组 | 文件 |
| --- | --- | ---: | ---: | --- | --- |
| M | source | 3 | 3 |  | [.dockerignore](https://github.com/holeenlu/sub2api/blob/TapModels/.dockerignore) |
| M | source | 12 | 4 |  | [.gitignore](https://github.com/holeenlu/sub2api/blob/TapModels/.gitignore) |
| M | tooling | 9 | 9 |  | [.goreleaser.simple.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/.goreleaser.simple.yaml) |
| M | tooling | 35 | 35 |  | [.goreleaser.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/.goreleaser.yaml) |
| M | documentation | 2 | 2 |  | [DEV_GUIDE.md](https://github.com/holeenlu/sub2api/blob/TapModels/DEV_GUIDE.md) |
| M | tooling | 19 | 19 |  | [Dockerfile](https://github.com/holeenlu/sub2api/blob/TapModels/Dockerfile) |
| M | tooling | 13 | 13 |  | [Dockerfile.goreleaser](https://github.com/holeenlu/sub2api/blob/TapModels/Dockerfile.goreleaser) |
| M | documentation | 39 | 37 |  | [README.md](https://github.com/holeenlu/sub2api/blob/TapModels/README.md) |
| M | documentation | 31 | 29 |  | [README_CN.md](https://github.com/holeenlu/sub2api/blob/TapModels/README_CN.md) |
| M | documentation | 30 | 28 |  | [README_JA.md](https://github.com/holeenlu/sub2api/blob/TapModels/README_JA.md) |
| M | asset | 21 | 22 |  | [assets/logo.svg](https://github.com/holeenlu/sub2api/blob/TapModels/assets/logo.svg) |
| M | asset | 12 | 29 |  | [assets/wordmark.svg](https://github.com/holeenlu/sub2api/blob/TapModels/assets/wordmark.svg) |
| M | source | 4 | 4 |  | [backend/internal/config/config.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/config/config.go) |
| M | source | 4 | 4 |  | [backend/internal/pkg/logger/options.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/pkg/logger/options.go) |
| M | test | 2 | 2 |  | [backend/internal/pkg/logger/options_test.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/pkg/logger/options_test.go) |
| M | source | 1 | 1 |  | [backend/internal/pkg/sysutil/restart.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/pkg/sysutil/restart.go) |
| M | source | 2 | 2 |  | [backend/internal/platform/liveattestation/attestation.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/platform/liveattestation/attestation.go) |
| M | test | 2 | 2 |  | [backend/internal/server/api_contract_test.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/server/api_contract_test.go) |
| M | source | 4 | 10 |  | [backend/internal/service/brand.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/service/brand.go) |
| M | source | 1 | 1 |  | [backend/internal/service/data_management_service.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/service/data_management_service.go) |
| M | source | 4 | 4 |  | [backend/internal/service/update_service.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/service/update_service.go) |
| M | source | 3 | 3 |  | [backend/internal/setup/cli.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/setup/cli.go) |
| M | source | 1 | 1 |  | [backend/internal/setup/setup.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/setup/setup.go) |
| M | source | 1 | 1 |  | [backend/internal/web/embed_on.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/web/embed_on.go) |
| M | test | 12 | 12 |  | [backend/internal/web/embed_test.go](https://github.com/holeenlu/sub2api/blob/TapModels/backend/internal/web/embed_test.go) |
| M | documentation | 9 | 9 |  | [backend/pkg/pluginapi/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/backend/pkg/pluginapi/README.md) |
| M | documentation | 1 | 1 |  | [backend/pkg/pluginapi/docs/development.md](https://github.com/holeenlu/sub2api/blob/TapModels/backend/pkg/pluginapi/docs/development.md) |
| M | documentation | 1 | 1 |  | [backend/pkg/pluginapi/docs/package-format.md](https://github.com/holeenlu/sub2api/blob/TapModels/backend/pkg/pluginapi/docs/package-format.md) |
| M | documentation | 2 | 2 |  | [backend/pkg/pluginapi/docs/security.md](https://github.com/holeenlu/sub2api/blob/TapModels/backend/pkg/pluginapi/docs/security.md) |
| M | tooling | 1 | 1 |  | [backend/pkg/pluginapi/v1/manifest.schema.json](https://github.com/holeenlu/sub2api/blob/TapModels/backend/pkg/pluginapi/v1/manifest.schema.json) |
| M | tooling | 15 | 15 |  | [deploy/.env.example](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/.env.example) |
| M | tooling | 1 | 1 |  | [deploy/.gitignore](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/.gitignore) |
| M | documentation | 38 | 38 |  | [deploy/APPLE_CONTAINER.md](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/APPLE_CONTAINER.md) |
| M | tooling | 2 | 2 |  | [deploy/Caddyfile](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/Caddyfile) |
| M | documentation | 17 | 17 |  | [deploy/DATAMANAGEMENTD_CN.md](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/DATAMANAGEMENTD_CN.md) |
| M | documentation | 13 | 13 |  | [deploy/DOCKER.md](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/DOCKER.md) |
| M | tooling | 12 | 12 |  | [deploy/Dockerfile](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/Dockerfile) |
| M | documentation | 10 | 10 |  | [deploy/EDGE_SECURITY.md](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/EDGE_SECURITY.md) |
| M | documentation | 55 | 55 |  | [deploy/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/README.md) |
| M | tooling | 39 | 39 |  | [deploy/apple-container.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/apple-container.sh) |
| M | tooling | 1 | 1 |  | [deploy/build_image.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/build_image.sh) |
| M | tooling | 20 | 20 |  | [deploy/config.example.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/config.example.yaml) |
| M | tooling | 19 | 19 |  | [deploy/docker-compose.dev.yml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-compose.dev.yml) |
| M | tooling | 21 | 21 |  | [deploy/docker-compose.local.yml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-compose.local.yml) |
| M | tooling | 11 | 11 |  | [deploy/docker-compose.standalone.yml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-compose.standalone.yml) |
| M | tooling | 8 | 8 |  | [deploy/docker-compose.wsl.yml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-compose.wsl.yml) |
| M | tooling | 21 | 21 |  | [deploy/docker-compose.yml](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-compose.yml) |
| M | tooling | 6 | 6 |  | [deploy/docker-deploy.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-deploy.sh) |
| M | tooling | 6 | 6 |  | [deploy/docker-entrypoint.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/docker-entrypoint.sh) |
| M | tooling | 16 | 16 |  | [deploy/install-datamanagementd.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/install-datamanagementd.sh) |
| M | tooling | 87 | 87 |  | [deploy/install.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/install.sh) |
| D | tooling | 0 | 22 |  | `deploy/kdan-datamanagementd.service` |
| D | tooling | 0 | 33 |  | `deploy/kdan.service` |
| A | tooling | 22 | 0 |  | [deploy/tapmodels-datamanagementd.service](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tapmodels-datamanagementd.service) |
| A | tooling | 33 | 0 |  | [deploy/tapmodels.service](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tapmodels.service) |
| M | test | 31 | 31 |  | [deploy/tests/apple-container-test.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tests/apple-container-test.sh) |
| M | test | 1 | 1 |  | [deploy/tests/docker-compose-gateway-env-test.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tests/docker-compose-gateway-env-test.sh) |
| M | test | 2 | 2 |  | [deploy/tests/docker-compose-security-test.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tests/docker-compose-security-test.sh) |
| M | test | 2 | 2 |  | [deploy/tests/docker-runtime-resources-test.sh](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tests/docker-runtime-resources-test.sh) |
| M | test | 4 | 4 |  | [deploy/tests/fixtures/bin/container](https://github.com/holeenlu/sub2api/blob/TapModels/deploy/tests/fixtures/bin/container) |
| M | documentation | 6 | 6 |  | [docs/ADMIN_PAYMENT_INTEGRATION_API.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/ADMIN_PAYMENT_INTEGRATION_API.md) |
| A | documentation | 20 | 0 |  | [docs/AI_COORDINATION_BASELINE.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/AI_COORDINATION_BASELINE.md) |
| M | documentation | 9 | 9 |  | [docs/BATCH_IMAGE_MVP.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/BATCH_IMAGE_MVP.md) |
| A | documentation | 182 | 0 |  | [docs/CUSTOMIZATION_REVIEW_2026-09-08.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/CUSTOMIZATION_REVIEW_2026-09-08.md) |
| M | documentation | 6 | 6 |  | [docs/PAYMENT.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/PAYMENT.md) |
| M | documentation | 6 | 6 |  | [docs/PAYMENT_CN.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/PAYMENT_CN.md) |
| M | documentation | 11 | 11 |  | [docs/PLUGIN_DEVELOPMENT.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/PLUGIN_DEVELOPMENT.md) |
| A | documentation | 171 | 0 |  | [docs/PROJECT_CUSTOMIZATION_WORKFLOW.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/PROJECT_CUSTOMIZATION_WORKFLOW.md) |
| A | documentation | 36 | 0 |  | [docs/TAPMODELS_DOCS_ACCEPTANCE.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/TAPMODELS_DOCS_ACCEPTANCE.md) |
| M | documentation | 4 | 4 |  | [docs/legal/admin-compliance.en.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/legal/admin-compliance.en.md) |
| M | documentation | 4 | 4 |  | [docs/legal/admin-compliance.zh-TW.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/legal/admin-compliance.zh-TW.md) |
| M | documentation | 4 | 4 |  | [docs/legal/admin-compliance.zh.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/legal/admin-compliance.zh.md) |
| A | documentation | 23 | 0 |  | [docs/requirements/final-2026-10-07/BRAND.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-2026-10-07/BRAND.md) |
| A | documentation | 25 | 0 |  | [docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B1-runtime.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B1-runtime.md) |
| A | documentation | 70 | 0 |  | [docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B2-clients.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B2-clients.md) |
| A | documentation | 152 | 0 |  | [docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B3-presentation.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B3-presentation.md) |
| A | documentation | 48 | 0 |  | [docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B4-delivery.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B4-delivery.md) |
| A | documentation | 23 | 0 |  | [docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B5-records.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/requirements/final-v0.2.16-2026-10-13/brands/TapModels/B5-records.md) |
| A | documentation | 45 | 0 |  | [docs/tapmodels-ci.md](https://github.com/holeenlu/sub2api/blob/TapModels/docs/tapmodels-ci.md) |
| M | documentation | 1 | 1 |  | [frontend/index.html](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/index.html) |
| M | generated | 4 | 4 |  | [frontend/public/downloads/SHA256SUMS.txt](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/SHA256SUMS.txt) |
| M | generated | - | - |  | [frontend/public/downloads/gpt-image-flare.zip](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/gpt-image-flare.zip) |
| M | generated | - | - |  | [frontend/public/downloads/gpt-image-sunburst.zip](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/gpt-image-sunburst.zip) |
| D | generated | - | - |  | `frontend/public/downloads/kdan-claude-session-recovery.zip` |
| D | generated | 0 | 22 |  | `frontend/public/downloads/kdan-claude-session-recovery/Find-ClaudeSessions.ps1` |
| D | generated | 0 | 60 |  | `frontend/public/downloads/kdan-claude-session-recovery/README.md` |
| D | generated | 0 | 11 |  | `frontend/public/downloads/kdan-claude-session-recovery/find-claude-sessions.sh` |
| D | generated | 0 | 213 |  | `frontend/public/downloads/kdan-claude-session-recovery/find_claude_sessions.py` |
| D | generated | - | - |  | `frontend/public/downloads/kdan-codex-session-repair.zip` |
| D | generated | 0 | 78 |  | `frontend/public/downloads/kdan-codex-session-repair/README.md` |
| D | generated | 0 | 40 |  | `frontend/public/downloads/kdan-codex-session-repair/RepairSessions.ps1` |
| D | generated | 0 | 11 |  | `frontend/public/downloads/kdan-codex-session-repair/repair-sessions.sh` |
| D | generated | 0 | 714 |  | `frontend/public/downloads/kdan-codex-session-repair/repair_sessions.py` |
| D | generated | 0 | 125 |  | `frontend/public/downloads/kdan-codex-session-repair/resume_sessions.py` |
| D | generated | 0 | 38 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-flare/SKILL.md` |
| D | generated | 0 | 4 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-flare/agents/openai.yaml` |
| D | generated | 0 | 1 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-flare/requirements.txt` |
| D | generated | 0 | 285 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-flare/scripts/generate.py` |
| D | generated | 0 | 38 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/SKILL.md` |
| D | generated | 0 | 4 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/agents/openai.yaml` |
| D | generated | 0 | 1 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/requirements.txt` |
| D | generated | 0 | 285 |  | `frontend/public/downloads/kdan-image-skills/gpt-image-sunburst/scripts/generate.py` |
| A | generated | - | - |  | [frontend/public/downloads/session-repair.zip](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/session-repair.zip) |
| A | generated | 77 | 0 |  | [frontend/public/downloads/session-repair/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/session-repair/README.md) |
| A | generated | 40 | 0 |  | [frontend/public/downloads/session-repair/RepairSessions.ps1](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/session-repair/RepairSessions.ps1) |
| A | generated | 11 | 0 |  | [frontend/public/downloads/session-repair/repair-sessions.sh](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/session-repair/repair-sessions.sh) |
| A | generated | 714 | 0 |  | [frontend/public/downloads/session-repair/repair_sessions.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/session-repair/repair_sessions.py) |
| A | generated | - | - |  | [frontend/public/downloads/tapmodels-claude-session-recovery.zip](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-claude-session-recovery.zip) |
| A | generated | 22 | 0 |  | [frontend/public/downloads/tapmodels-claude-session-recovery/Find-ClaudeSessions.ps1](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-claude-session-recovery/Find-ClaudeSessions.ps1) |
| A | generated | 60 | 0 |  | [frontend/public/downloads/tapmodels-claude-session-recovery/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-claude-session-recovery/README.md) |
| A | generated | 11 | 0 |  | [frontend/public/downloads/tapmodels-claude-session-recovery/find-claude-sessions.sh](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-claude-session-recovery/find-claude-sessions.sh) |
| A | generated | 213 | 0 |  | [frontend/public/downloads/tapmodels-claude-session-recovery/find_claude_sessions.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-claude-session-recovery/find_claude_sessions.py) |
| A | generated | - | - |  | [frontend/public/downloads/tapmodels-codex-session-repair.zip](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-codex-session-repair.zip) |
| A | generated | 38 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/SKILL.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/SKILL.md) |
| A | generated | 4 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/agents/openai.yaml) |
| A | generated | 1 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/requirements.txt](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/requirements.txt) |
| A | generated | 285 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/scripts/generate.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-flare/scripts/generate.py) |
| A | generated | 38 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/SKILL.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/SKILL.md) |
| A | generated | 4 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/agents/openai.yaml) |
| A | generated | 1 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/requirements.txt](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/requirements.txt) |
| A | generated | 285 | 0 |  | [frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/scripts/generate.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-image-skills/gpt-image-sunburst/scripts/generate.py) |
| A | generated | 78 | 0 |  | [frontend/public/downloads/tapmodels-session-repair/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-session-repair/README.md) |
| A | generated | 40 | 0 |  | [frontend/public/downloads/tapmodels-session-repair/Repair-TapModelsSessions.ps1](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-session-repair/Repair-TapModelsSessions.ps1) |
| A | generated | 11 | 0 |  | [frontend/public/downloads/tapmodels-session-repair/repair-sessions.sh](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-session-repair/repair-sessions.sh) |
| A | generated | 714 | 0 |  | [frontend/public/downloads/tapmodels-session-repair/repair_sessions.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-session-repair/repair_sessions.py) |
| A | generated | 125 | 0 |  | [frontend/public/downloads/tapmodels-session-repair/resume_sessions.py](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/downloads/tapmodels-session-repair/resume_sessions.py) |
| M | source | 5 | 5 |  | [frontend/public/install/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/install/README.md) |
| M | source | 5 | 6 |  | [frontend/public/install/claude-code.sh](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/install/claude-code.sh) |
| M | source | 9 | 10 |  | [frontend/public/install/codex.sh](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/install/codex.sh) |
| M | asset | 21 | 22 |  | [frontend/public/logo.svg](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/public/logo.svg) |
| M | source | 1 | 1 |  | [frontend/src/components/home/GatewayShowcase.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/components/home/GatewayShowcase.vue) |
| M | source | 24 | 24 |  | [frontend/src/components/keys/UseKeyModal.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/components/keys/UseKeyModal.vue) |
| M | test | 12 | 12 |  | [frontend/src/components/keys/__tests__/UseKeyModal.spec.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/components/keys/__tests__/UseKeyModal.spec.ts) |
| M | source | 2 | 2 |  | [frontend/src/components/layout/AuthLayout.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/components/layout/AuthLayout.vue) |
| M | source | 1 | 1 |  | [frontend/src/components/layout/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/components/layout/README.md) |
| M | source | 16 | 16 |  | [frontend/src/config/brand.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/config/brand.ts) |
| M | test | 1 | 1 |  | [frontend/src/content/docs/__tests__/examples.spec.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/__tests__/examples.spec.ts) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/chat/anthropic-messages.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/en/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/chat/gemini-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/chat/grok-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/chat/openai-chat.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/en/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/chat/openai-responses.md) |
| M | source | 5 | 5 |  | [frontend/src/content/docs/en/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/image/openai-image.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/en/api/protocols.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/protocols.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/query/embeddings.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/api/query/models.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/query/models.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/en/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/api/query/token-count.md) |
| M | source | 10 | 11 |  | [frontend/src/content/docs/en/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/claude-code.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/claude-desktop.md) |
| M | source | 12 | 13 |  | [frontend/src/content/docs/en/apps/codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/codex.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/en/apps/console.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/console.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/en/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/image-skills.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/en/apps/index.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/index.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/en/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/session-recovery-claude.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/en/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/apps/session-recovery-codex.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/en/authentication.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/authentication.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/en/downloads.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/downloads.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/en/errors.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/errors.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/en/limits.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/limits.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/en/pricing.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/pricing.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/en/quickstart.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/en/quickstart.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/examples.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/examples.ts) |
| M | source | 9 | 9 |  | [frontend/src/content/docs/ja/apps/codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/apps/codex.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/ja/apps/console.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/apps/console.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/ja/apps/index.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/apps/index.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/ja/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/apps/session-recovery-claude.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/ja/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/apps/session-recovery-codex.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/ja/pricing.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/ja/pricing.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/chat/anthropic-messages.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/zh-TW/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/chat/gemini-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/chat/grok-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/chat/openai-chat.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/chat/openai-responses.md) |
| M | source | 5 | 5 |  | [frontend/src/content/docs/zh-TW/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/image/openai-image.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh-TW/api/protocols.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/protocols.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/query/embeddings.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/api/query/models.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/query/models.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/zh-TW/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/api/query/token-count.md) |
| M | source | 12 | 13 |  | [frontend/src/content/docs/zh-TW/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/claude-code.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/zh-TW/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/claude-desktop.md) |
| M | source | 14 | 15 |  | [frontend/src/content/docs/zh-TW/apps/codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/codex.md) |
| M | source | 7 | 7 |  | [frontend/src/content/docs/zh-TW/apps/console.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/console.md) |
| M | source | 7 | 7 |  | [frontend/src/content/docs/zh-TW/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/image-skills.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/apps/index.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/index.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/session-recovery-claude.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/apps/session-recovery-codex.md) |
| M | source | 5 | 5 |  | [frontend/src/content/docs/zh-TW/authentication.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/authentication.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/zh-TW/downloads.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/downloads.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh-TW/errors.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/errors.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh-TW/limits.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/limits.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh-TW/pricing.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/pricing.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh-TW/quickstart.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh-TW/quickstart.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/chat/anthropic-messages.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/chat/anthropic-messages.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/zh/api/chat/gemini-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/chat/gemini-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/chat/grok-native.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/chat/grok-native.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/chat/openai-chat.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/chat/openai-chat.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/chat/openai-responses.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/chat/openai-responses.md) |
| M | source | 5 | 5 |  | [frontend/src/content/docs/zh/api/image/openai-image.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/image/openai-image.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh/api/protocols.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/protocols.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/query/embeddings.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/query/embeddings.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/api/query/models.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/query/models.md) |
| M | source | 6 | 6 |  | [frontend/src/content/docs/zh/api/query/token-count.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/api/query/token-count.md) |
| M | source | 12 | 13 |  | [frontend/src/content/docs/zh/apps/claude-code.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/claude-code.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/zh/apps/claude-desktop.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/claude-desktop.md) |
| M | source | 14 | 15 |  | [frontend/src/content/docs/zh/apps/codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/codex.md) |
| M | source | 7 | 7 |  | [frontend/src/content/docs/zh/apps/console.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/console.md) |
| M | source | 7 | 7 |  | [frontend/src/content/docs/zh/apps/image-skills.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/image-skills.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/apps/index.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/index.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh/apps/session-recovery-claude.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/session-recovery-claude.md) |
| M | source | 4 | 4 |  | [frontend/src/content/docs/zh/apps/session-recovery-codex.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/apps/session-recovery-codex.md) |
| M | source | 5 | 5 |  | [frontend/src/content/docs/zh/authentication.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/authentication.md) |
| M | source | 2 | 2 |  | [frontend/src/content/docs/zh/downloads.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/downloads.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh/errors.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/errors.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh/limits.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/limits.md) |
| M | source | 1 | 1 |  | [frontend/src/content/docs/zh/pricing.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/pricing.md) |
| M | source | 3 | 3 |  | [frontend/src/content/docs/zh/quickstart.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/content/docs/zh/quickstart.md) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/en/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/admin/accounts.ts) |
| M | source | 1 | 1 |  | [frontend/src/i18n/locales/en/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/admin/overview.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/en/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/admin/settings.ts) |
| M | source | 1 | 1 |  | [frontend/src/i18n/locales/en/common.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/common.ts) |
| M | source | 5 | 5 |  | [frontend/src/i18n/locales/en/dashboard.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/dashboard.ts) |
| M | source | 6 | 6 |  | [frontend/src/i18n/locales/en/docs.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/docs.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/en/misc.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/en/misc.ts) |
| M | source | 5 | 5 |  | [frontend/src/i18n/locales/ja/docs.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/ja/docs.ts) |
| M | generated | 2 | 2 |  | [frontend/src/i18n/locales/zh-TW/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/admin/accounts.ts) |
| M | generated | 1 | 1 |  | [frontend/src/i18n/locales/zh-TW/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/admin/overview.ts) |
| M | generated | 2 | 2 |  | [frontend/src/i18n/locales/zh-TW/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/admin/settings.ts) |
| M | generated | 1 | 1 |  | [frontend/src/i18n/locales/zh-TW/common.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/common.ts) |
| M | generated | 6 | 6 |  | [frontend/src/i18n/locales/zh-TW/dashboard.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/dashboard.ts) |
| M | generated | 8 | 8 |  | [frontend/src/i18n/locales/zh-TW/docs.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/docs.ts) |
| M | generated | 2 | 2 |  | [frontend/src/i18n/locales/zh-TW/landing.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/landing.ts) |
| M | generated | 2 | 2 |  | [frontend/src/i18n/locales/zh-TW/misc.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh-TW/misc.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/zh/admin/accounts.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/admin/accounts.ts) |
| M | source | 1 | 1 |  | [frontend/src/i18n/locales/zh/admin/overview.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/admin/overview.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/zh/admin/settings.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/admin/settings.ts) |
| M | source | 1 | 1 |  | [frontend/src/i18n/locales/zh/common.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/common.ts) |
| M | source | 6 | 6 |  | [frontend/src/i18n/locales/zh/dashboard.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/dashboard.ts) |
| M | source | 8 | 8 |  | [frontend/src/i18n/locales/zh/docs.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/docs.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/zh/landing.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/landing.ts) |
| M | source | 2 | 2 |  | [frontend/src/i18n/locales/zh/misc.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/i18n/locales/zh/misc.ts) |
| M | source | 1 | 1 |  | [frontend/src/router/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/router/README.md) |
| M | source | 1 | 1 |  | [frontend/src/stores/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/stores/README.md) |
| M | source | 6 | 45 |  | [frontend/src/views/HomeView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/HomeView.vue) |
| M | source | 2 | 9 |  | [frontend/src/views/KeyUsageView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/KeyUsageView.vue) |
| M | source | 1 | 1 |  | [frontend/src/views/admin/AccountsView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/admin/AccountsView.vue) |
| M | source | 1 | 1 |  | [frontend/src/views/admin/ProxiesView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/admin/ProxiesView.vue) |
| M | test | 4 | 4 |  | [frontend/src/views/admin/__tests__/SettingsView.spec.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/admin/__tests__/SettingsView.spec.ts) |
| M | source | 1 | 1 |  | [frontend/src/views/auth/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/auth/README.md) |
| M | source | 2 | 2 |  | [frontend/src/views/auth/USAGE_EXAMPLES.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/auth/USAGE_EXAMPLES.md) |
| M | source | 7 | 7 |  | [frontend/src/views/auth/VISUAL_GUIDE.md](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/auth/VISUAL_GUIDE.md) |
| M | source | 2 | 2 |  | [frontend/src/views/setup/SetupWizardView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/setup/SetupWizardView.vue) |
| M | source | 1 | 1 |  | [frontend/src/views/user/BatchImageGuideView.vue](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/src/views/user/BatchImageGuideView.vue) |
| M | source | 1 | 1 |  | [frontend/vite.config.ts](https://github.com/holeenlu/sub2api/blob/TapModels/frontend/vite.config.ts) |
| M | documentation | 2 | 2 |  | [skills/sub2api-admin/SKILL.md](https://github.com/holeenlu/sub2api/blob/TapModels/skills/sub2api-admin/SKILL.md) |
| M | tooling | 3 | 3 |  | [skills/sub2api-admin/agents/openai.yaml](https://github.com/holeenlu/sub2api/blob/TapModels/skills/sub2api-admin/agents/openai.yaml) |
| M | documentation | 2 | 2 |  | [skills/sub2api-admin/references/admin-cli.md](https://github.com/holeenlu/sub2api/blob/TapModels/skills/sub2api-admin/references/admin-cli.md) |
| M | tooling | 8 | 8 |  | [tools/build_docs_downloads.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/build_docs_downloads.py) |
| M | test | 1 | 1 |  | [tools/tests/test_claude_session_recovery.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/tests/test_claude_session_recovery.py) |
| M | test | 8 | 8 |  | [tools/tests/test_docs_downloads.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/tests/test_docs_downloads.py) |
| M | test | 16 | 16 |  | [tools/tests/test_image_skills.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/tests/test_image_skills.py) |
| M | test | 6 | 6 |  | [tools/tests/test_session_recovery_commands.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/tests/test_session_recovery_commands.py) |
| M | test | 3 | 3 |  | [tools/tests/test_session_repair.py](https://github.com/holeenlu/sub2api/blob/TapModels/tools/tests/test_session_repair.py) |
| M | documentation | 2 | 2 |  | [tools/zh-tw/README.md](https://github.com/holeenlu/sub2api/blob/TapModels/tools/zh-tw/README.md) |
