# TapModels CI 与发布

## 私有仓库与分支边界

公共集成仓库 `holeenlu/sub2api` 已转为私有独立仓库，脱离 GitHub Fork 网络。
它仍通过保留的 Git 历史同步公开 `Wei-Shaw/sub2api`。
详见[私有仓库同步说明](PRIVATE_REPOSITORY_SYNC.md)。

- 公共代码：本地 `holeen/main` → `origin/main`。
- 品牌代码：本地 `TapModels` → `origin/TapModels`，另同步到 `erwinlin/main`。
- 同步入口：`deploy/sync-upstream.sh`，普通 merge 并保留原 SHA；检查点位于 `.release/upstream-sync/<id>/state.json`。
- 推送、部署按该次明确授权分别执行；旧 `deploy/sync.sh` 已退役。
- 不向公共 main 合入品牌提交，也不向公开 upstream 推送私有代码。

## 工作流

| 工作流 | 触发与发布目标 |
| --- | --- |
| `tapmodels-ci.yml` | push/PR 到 `main` 或 `TapModels` |
| `tapmodels-docker-image.yml` | push `main`、数字版本 tag、手动触发；仅在品牌发布仓库推镜像 |
| 上游监看 | 专属仓库默认 main 上每日运行 |

CI 包含后端单元测试、生成代码检查、前端 lint/typecheck/测试/构建和繁中同步检查。
监看只更新追踪 issue，不合并、不推送、不部署。
定时和手动触发要求工作流已在默认分支注册；品牌分支 push 不依赖 Fork 网络。
公共 `project-ci.yml`、`upstream-sync-watch.yml` 须先发布到 `origin/main`。
不要假设新私有仓库已停用继承的上游工作流，须核对 Actions 设置。

## 镜像与发版

Compose 和版本界面统一使用 `ghcr.io/erwinlin/tapmodels`。
私有源码须使用认证 Git；匿名 raw GitHub 安装命令不适用。
私有 GHCR 包须登录并具备读取权限，CI 则使用仓库自带的 `GITHUB_TOKEN`。

分支构建发布分支 tag 和 SHA tag；仅正式发版更新 `latest`。
数字 SemVer tag（如 `1.0.0-rc.1`）中的预发布标识同时决定镜像通道与 GitHub prerelease。
预发布不更新 `latest`；非法版本在构建前拒绝，手动选择不推送时也不创建 Release。
带 build metadata 的版本将镜像 tag 中的 `+` 转为 `_`，Release 保留原始版本名。
生产通过 `TAPMODELS_IMAGE` 固定 release tag 或 digest，不应把分支推送当成 latest 更新。

仅从已审查的品牌提交创建版本：
```bash
git switch TapModels
git tag -a 1.0.0 -m "TapModels 1.0.0"
git push erwinlin refs/tags/1.0.0
```

数字版本 tag 属于其发布仓库对应的品牌。TapModels tag 必须推到 `erwinlin`，
不能推到公共 `origin`。上游 `v*` tag 仅用于基线溯源，不是私有品牌发版。
CI 不自动部署生产。

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
