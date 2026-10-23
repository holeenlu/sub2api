# TapModels 持续集成与发布

TapModels 代码、版本发布和在线更新统一使用 `holeenlu/sub2api` 的 `TapModels` 分支。共享改动先在 `main` 验证，再普通 merge 到品牌分支并保留品牌配置。详见[需求交付规范](CHANGE_DELIVERY.md)与[私有仓库同步说明](PRIVATE_REPOSITORY_SYNC.md)。

- KDAN：本地 `main` → `origin/main`。
- TapModels：本地 `TapModels` → `origin/TapModels`。
- Tokensavy：本地 `tokensavy` → `origin/tokensavy`。
- 同步入口：`deploy/sync-upstream.sh`，普通 merge 保留来源 SHA；旧 `deploy/sync.sh` 已退役。
- 不向旧品牌仓库或官方 upstream 推送交付代码；推送、版本发布、生产部署分别授权。

## 工作流

仓库仅保留 `automatic-release.yml`（Manual versioned release），由 `workflow_dispatch` 手动触发。普通提交、分支推送和标签推送都不会发版或部署。检查源码与品牌产物后，在本仓库 Actions → Manual versioned release → Run workflow 选择 `TapModels`；工作流根据仓库及分支选择 `tapmodels` 渠道。渠道配置见 `scripts/release/channels.json`。

## 镜像与发版

Compose 和版本界面统一使用 `ghcr.io/holeenlu/tapmodels`，版本和在线更新来源为 `holeenlu/sub2api` 的 `tapmodels` 渠道。私有 GHCR 包须登录并具备读取权限；发布工作流使用仓库配置的认证。

人工发布通过渠道前缀隔离品牌版本；预发布不更新稳定版 `latest`。生产通过 `TAPMODELS_IMAGE` 固定已发布 tag 或 digest。分支推送不提高版本号、不发布镜像，也不自动升级生产。上游 `v*` tag 仅用于基线溯源。

## 徽章中的编译提交

管理员版本弹层保留应用版本，第二行改为“编译提交 [abcdef012]”。
`build_commit` 来自当前运行二进制的 `main.Commit`，显示前 9 位，悬停显示完整 SHA。
即使 `UPDATE_CHECK_ENABLED=false` 仍显示；缺失或无效值时隐藏，不展示 unknown。
CI 注入完整 SHA，旧构建只有 7 位时按实际信息显示，不补造剩余字符。
不链接私有仓库，也不触发上游更新请求。`upstream_version` 仍保留在接口中供兼容使用。

Docker CI 使用 `git merge-base HEAD upstream/main` 最近的上游 `v*` tag，
注入 `main.UpstreamVersion`；无法推导时才回退 embedded VERSION。
转为私有仓库不改变这套 Git 计算方式。

## 升级与兼容

品牌 Docker 工作流发布镜像，不发布二进制安装包。升级走认证拉取及经批准的
Compose 部署。旧安装器默认禁用，显式启用检查必须在停止服务之前通过。
在线私有 release 检查需要运行时凭据，不承担公开上游代码同步。

镜像保留 `/app/sub2api` 兼容入口；本地打包传入 `BINARY_NAME=sub2api`，
GoReleaser 默认使用品牌二进制名。本地发布驱动支持显式 `--branch`，
发布前明确核对目标分支和生产站点；品牌 CI 发布渠道按上表分别运行。

本次兼容修复不迁移现有生产容器名、数据库、卷或 Compose 服务键。
不得未经迁移审查直接用改名后的品牌模板覆盖线上 Compose。
发布验收须核对镜像 ID、内嵌 commit、二进制校验和和健康状态。
