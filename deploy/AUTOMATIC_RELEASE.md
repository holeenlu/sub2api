# 手动发版与 Docker Compose 在线更新

发布入口为 `.github/workflows/automatic-release.yml`，GitHub 页面显示为 **Manual versioned release**，仅允许通过 `workflow_dispatch` 手动运行。提交、推送代码或标签、创建 PR、合并分支都不会触发此 Workflow，不会因此分配版本、构建或发布镜像。需要发版时，由用户在 Actions 页面点击 Run workflow，或明确授权后执行下方的 `gh workflow run` 命令。

人工启动后，构建仍采用原作者 Sub2API 的 Release 流程和工具：自动分配版本 → 前端构建一次 → GoReleaser 分平台构建二进制及安装包 → 校验完整构建矩阵 → 用同一批 Linux 二进制打包 Docker 镜像 → 发布 GitHub Release。无需手动改 VERSION、打标签，也不需要 Docker Hub 凭据。单纯提交、推送不应调用手动发版命令；生产部署和数据库迁移仍需独立授权。

复用 `.goreleaser.yaml`、`.github/release-tools/release_matrix.py`、`release-images.sh` 和 `Dockerfile.goreleaser`。标准矩阵与原作者相同：Linux amd64/arm64、macOS amd64/arm64、Windows amd64；镜像支持 Linux amd64/arm64。镜像构建不再重新编译前后端。参考源码：[原作者 Release 工作流](https://github.com/Wei-Shaw/sub2api/blob/a3eb7ef302961cba716dc78b39b93b60c467db0e/.github/workflows/release.yml)。

定制部分仅负责自动版本分配、渠道信息、发布草稿和在线更新清单。四段版本通过 GoReleaser snapshot 的版本模板打包，以独立的发布步骤上传到预留的正式 Release；不会把四段版本交给严格的 SemVer 标签解析。所有平台产物验证完成后才发布，GitHub Release 发布成功后才将对应镜像摘要提升为 `latest`，并把本次 Release 标为仓库级 `Latest`。

GitHub 的 `Latest` 是整个仓库共用的单一标记。当前 `holeenlu/sub2api/main` 只发布 KDAN，因此其 `Latest` 直接对应最新成功的 KDAN Release；需要固定版本时，应使用完整标签，例如 `kdan/v0.2.9.3`。TapModels 单独使用 `erwinlin/TapModels`，其 `Latest` 独立计算。

| 渠道 | 仓库 / 分支 | Release 标签 | GHCR 镜像 |
| --- | --- | --- | --- |
| KDAN | holeenlu/sub2api / main | kdan/v0.2.8.1 | ghcr.io/holeenlu/kdan |
| TapModels | erwinlin/TapModels / main | tapmodels/v0.2.8.1 | ghcr.io/erwinlin/tapmodels |

`main` 是 KDAN 品牌默认、主开发和发布分支；不再维护独立 KDAN 或公共发布分支。origin/TapModels 是代码镜像，不重复发布。标签包含渠道名，但界面版本和镜像标签仅显示数字。不会读取混入的 sub4api v1.1.4 标签，也不会把 TapModels 版安装到 KDAN。两个交付分支只保留 `automatic-release.yml`；独立 CI、安全扫描、CLA、上游定时监看、原作者手动发布和旧品牌镜像工作流均已移除，以减少 Actions 用量。发布所需的版本规则测试、更新界面/API 检查、类型检查、构建和产物校验仍在人工启动的发布流程内执行。

## 版本规则

- 从**已合入代码**可达的 Wei-Shaw/sub2api 官方稳定标签确定基准，不从远端最新版本或本地 VERSION 猜测。
- 初次接入当前定制代码：0.2.8.1。之后针对新提交手动发版时递增：0.2.8.2 … 0.2.8.9、0.2.8.10。只推送代码不递增。
- 合入新正式版本且上游进度正好停在该正式标签：0.2.9（保留现有品牌和定制）。若同时包含自上次发布以来的新定制提交，则首版为 0.2.9.1；同基准后续推送也递增第四位。
- 合入超过正式标签的未发版提交：0.2.9.1。多次提交、推送可以累积到一次手动发版，发布人工选择的分支及本次运行固定的提交。
- 每个渠道独立计数。失败保留 draft 和版本预留，同一 SHA 重试沿用原号；已发布同一 SHA 不重复构建。旧任务不会将 latest 倒退。
- 同一仓库的发版任务由 GitHub Actions 的 `concurrency` 组串行处理，已完成任务不会被取消；GitHub Actions 不支持在工作流中声明 `queue:max`。草稿不是可更新版本。
- VERSION 文件仍代表上游源码基准；实际运行版本、提交 SHA、上游版本由编译参数写入，不创建回写 VERSION 的循环提交。Git 标签不会自动改写已部署二进制的版本号；本地应急构建必须使用该渠道、该提交对应的发布计划版本，不能固定填写 `.1`。标签可能仅是失败构建预留，须核对正式 Release、清单和镜像后才能声称发布成功。

仓库 Actions 需要允许 `contents: write` 和 `packages: write`，GHCR 包需要授予该仓库 Actions 写权限。默认使用 GITHUB_TOKEN；无需 PAT 发版。完整发布在同一次人工启动的流程内完成；创建标签不会触发此 Workflow。

## Actions 用量控制

- `.github/workflows/` 只保留 `automatic-release.yml`，且触发器只能有 `workflow_dispatch`，不配置 push、PR、定时或其他自动触发事件。无需禁用仓库 Actions，否则人工 Run workflow 也不可用。
- GitHub 端已登记但不再使用的工作流应同时停用，防止旧分支或后续合并意外重新触发；历史运行记录不会继续消耗 runner 分钟。
- GitHub 内置的 `dynamic/dependabot/update-graph` 是 Dependabot graph job，不能通过普通工作流接口停用，且不计入 Actions 分钟，因此保留。它与会计费的 Automatic dependency submission 不同；后者如果另行启用，应在仓库设置中关闭。参见 [GitHub 依赖图计费说明](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-graph-data#dependabot-graph-jobs)。
- 合并上游时复核两个交付分支的 `.github/workflows/`，不要重新引入自动触发器或已移除的工作流；`test_auto_release_workflow.py` 在本地和手动发版时检查此约束。本地测试脚本和发布辅助工具继续保留。
- 需要额外检查时在本地运行 `make test-frontend`、后端 `make test-unit` 和 `golangci-lint run ./...`。

## 启用与排障

1. 仓库 Settings → Actions → General 启用 Actions，并允许工作流使用其中的官方 Actions。工作流已按 job 声明所需写权限，不需要将仓库的默认 token 权限整体改成写入。
2. 私有仓库必须有可用的 Actions 额度。若运行页面 Annotations 提示付款失败或 spending limit，且 job 没有执行步骤，需要账号持有人在 Billing & licensing / Billing & plans 处理付款或预算；更换 PAT 或修改构建脚本不能解除该限制。
3. 首次发布会由仓库自身的 `GITHUB_TOKEN` 创建或写入对应 GHCR 包。若出现 package write denied，检查包设置中的 Manage Actions access，确保实际发布仓库具备写权限。
4. 需要发版时，在 Actions → Manual versioned release → Run workflow 中选择分支：KDAN 在 `holeenlu/sub2api` 选择 `main`；TapModels 在 `erwinlin/TapModels` 选择 `main`。`origin/TapModels` 仅是镜像分支，不从这里发布。也可在明确授权发版后执行以下相应命令；这不是普通提交、推送步骤。重试同一提交会沿用已有草稿，不会重复分配版本：

   ```sh
   gh workflow run automatic-release.yml --repo holeenlu/sub2api --ref main
   gh workflow run automatic-release.yml --repo erwinlin/TapModels --ref main
   gh run list --repo holeenlu/sub2api --workflow automatic-release.yml
   ```

5. 验收要求：工作流成功；GitHub Release 为非草稿，包含五个平台安装包、`checksums.txt`、`release-manifest.json`；GHCR 数字版本具有 amd64/arm64 两个平台，`latest` 指向清单中的同一摘要。仅提交或推送成功不代表镜像发布成功。

本机 Fine-grained PAT 用于读取私有仓库和 Actions，不注入发布工作流。Actions API 的读取与 Checks API 的失败注释不同；若细粒度 Token 不能访问 Checks API，可在已登录 GitHub 的运行页面查看 Annotations，无需反复重新生成 Token。

## 生产启用（一次性）

**本功能的代码推送不安装或更新生产服务器。** 需要先把含此功能的版本部署一次，再配置宿主机更新服务。仅适用于 Linux + systemd + Docker Compose v2，且只有一个应用容器。渠道名与 Compose 服务名可以不同，安装时用 `--service` 指定真实服务名。现有 PostgreSQL/Redis 不重建，数据卷保留。

1. 备份数据库和现有 Compose 配置。新版本可能执行数据库迁移；镜像恢复不撤销迁移。
2. 私有 GHCR 镜像：在宿主机以运行更新服务的 root 用户执行 `docker login ghcr.io`，使用有 `read:packages` 权限的凭据。GitHub Release 私有仓库读取另需有 Contents:read 权限的 token，存入仅 root 可读的文件，例如 `/etc/sub2api-updater.github-token`（chmod 600）；同时在应用 `.env` 配置 `UPDATE_GITHUB_TOKEN`。公开仓库可省略 token 文件参数。
3. 在生产服务器的此版本代码目录运行，例如 KDAN：

   ```sh
   sudo python3 deploy/install-compose-updater.py \
     --channel kdan --directory /opt/kdan --project-name kdan \
     --compose-file docker-compose.local.yml \
     --github-token-file /etc/sub2api-updater.github-token
   ```

   `--directory`、`--project-name` 必须取现有部署真实值。默认服务名与渠道相同；历史 KDAN 部署若服务名为 `sub2api`，必须补充 `--service sub2api`，不会因此改变 KDAN 发布渠道。安装器会在写入配置前验证服务存在，并为 `/root` 等受 systemd 保护的部署目录配置限定范围的写入权限。再次运行安装器会重启服务以加载新配置。可用 `docker inspect <应用容器> --format '{{ index .Config.Labels "com.docker.compose.project" }}'` 查询项目名。已有多个 override 时按原顺序重复 `--compose-file`，避免丢失现有端口、网络或挂载配置。目录及配置必须 root 所有、不能组/其他用户可写。默认应用 GID 为 1000；自定义 UID/GID 部署用 `--socket-gid` 指定。
4. 安装器仅启动宿主机更新服务，并打印连接应用所需的完整 Compose 命令。确认使用的是新版本镜像后执行该命令，完成一次应用重建。私有仓库 token/代理也须设置于宿主机服务环境（例如 systemd 的 HTTPS_PROXY）。
5. 管理后台点击版本号 → 检查更新 → 立即更新。宿主机重新读取对应 GitHub 正式 Release，校验渠道、版本、固定镜像和摘要，拉取镜像，只重建应用服务。网页短暂断线后会自动刷新。

应用只挂载受限 Unix socket，**不挂载 Docker socket**。更新服务只接受版本号，不接收命令、镜像地址或 Compose 参数。私有 Release 通过 GitHub API 下载；token 不随跳转发给资产存储域名。

健康检查超时会尝试恢复原镜像 ID。状态持久化在 `/var/lib/sub2api-updater/<渠道>/state.json`，日志用 `journalctl -u sub2api-compose-updater@kdan` 查看。后台也显示失败信息。宿主机更新服务意外中断会标记失败，需检查容器实际状态后重试。

生成的 `compose.online-update.json` 保存当前固定镜像；之后的**所有手动维护命令**也必须包含安装器打印的全部 `-f` 文件（含 socket 和 online-update 文件），否则可能恢复旧镜像。不要删除该文件或以旧的单文件命令重建应用。可以把完整文件列表配置到运维入口的 COMPOSE_FILE。

在线回退同样选择本渠道已发布版本并重建容器。数据库出现不兼容迁移时须先人工恢复数据库备份。系统不会定时自动安装版本：检测更新与管理员点击更新分开。

未安装更新服务的 Docker 部署仍可检测新版本，但禁用在线更新按钮，避免仅替换容器内二进制而在下次重建时丢失更新。直接运行二进制的部署沿用原有校验和下载、原子替换、手动重启机制。

参考：[GitHub 并发队列](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)、[GitHub Release 资产下载](https://docs.github.com/en/rest/releases/assets)。

### 私有仓库返回 404

GitHub 会对无权访问的私有仓库返回 404。先核对应用 `UPDATE_GITHUB_TOKEN` 是否配置，以及该 Token 是否能读取**本品牌的仓库**；能读取 `holeenlu/sub2api` 不代表能读取 `erwinlin/TapModels`。服务器的应用检查、宿主机 Release 清单下载、GHCR 镜像拉取是三个独立连接，分别需要应用环境变量、`--github-token-file` 和宿主机 Docker 登录。无成功发布的渠道也没有可安装版本；创建 Git 标签或提高页面版本号不能代替发布镜像。
