# 自动发布与 Docker Compose 在线更新

发布入口为 `.github/workflows/automatic-release.yml`。参考 sub4api 的 main 自动发布与原作者的 Release 安装包机制，统一构建 Linux amd64/arm64 镜像、二进制包、checksums.txt、release-manifest.json；每次推送正式交付分支后自动发布。无需手动改 VERSION、打标签，也不需要 Docker Hub 凭据。

| 渠道 | 仓库 / 分支 | Release 标签 | GHCR 镜像 |
| --- | --- | --- | --- |
| 公共 | holeenlu/sub2api / main | sub2api/v0.2.8.1 | ghcr.io/holeenlu/sub2api |
| KDAN | holeenlu/sub2api / KDAN | kdan/v0.2.8.1 | ghcr.io/holeenlu/kdan |
| TapModels | erwinlin/TapModels / main | tapmodels/v0.2.8.1 | ghcr.io/erwinlin/tapmodels |

origin/TapModels 是代码镜像，不重复发布。标签包含渠道名，但界面版本和镜像标签仅显示数字。不会读取混入的 sub4api v1.1.4 标签，也不会把公共版安装到 KDAN。旧品牌镜像工作流只保留手动入口，原作者 release.yml 仅允许原作者仓库运行。

## 版本规则

- 从**已合入代码**可达的 Wei-Shaw/sub2api 官方稳定标签确定基准，不从远端最新版本或本地 VERSION 猜测。
- 初次接入当前定制代码：0.2.8.1。之后每次推送：0.2.8.2 … 0.2.8.9、0.2.8.10。
- 合入新正式版本且上游进度正好停在该正式标签：0.2.9（保留现有品牌和定制）。若同时包含自上次发布以来的新定制提交，则首版为 0.2.9.1；同基准后续推送也递增第四位。
- 合入超过正式标签的未发版提交：0.2.9.1。多个本地修改一次推送视为一次发布。
- 每个渠道独立计数。失败保留 draft 和版本预留，同一 SHA 重试沿用原号；已发布同一 SHA 不重复构建。旧任务不会将 latest 倒退。
- 并发任务排队；GitHub 的 queue:max 上限为 100，超过平台队列上限需重跑。草稿不是可更新版本。
- VERSION 文件仍代表上游源码基准；实际运行版本、提交 SHA、上游版本由编译参数写入，不创建回写 VERSION 的循环提交。

仓库 Actions 需要允许 `contents: write` 和 `packages: write`，GHCR 包需要授予该仓库 Actions 写权限。默认使用 GITHUB_TOKEN；无需 PAT 发版。GitHub 的 token 创建标签不会再次触发普通 push 工作流，因此完整发布在同一流程完成。

## 生产启用（一次性）

**本功能的代码推送不安装或更新生产服务器。** 需要先把含此功能的版本部署一次，再配置宿主机更新服务。仅适用于 Linux + systemd + Docker Compose v2，应用服务名使用 sub2api/kdan/tapmodels，且只有一个应用容器。现有 PostgreSQL/Redis 不重建，数据卷保留。

1. 备份数据库和现有 Compose 配置。新版本可能执行数据库迁移；镜像恢复不撤销迁移。
2. 私有 GHCR 镜像：在宿主机以运行更新服务的 root 用户执行 `docker login ghcr.io`，使用有 `read:packages` 权限的凭据。GitHub Release 私有仓库读取另需有 Contents:read 权限的 token，存入仅 root 可读的文件，例如 `/etc/sub2api-updater.github-token`（chmod 600）；同时在应用 `.env` 配置 `UPDATE_GITHUB_TOKEN`。公开仓库可省略 token 文件参数。
3. 在生产服务器的此版本代码目录运行，例如 KDAN：

   ```sh
   sudo python3 deploy/install-compose-updater.py \
     --channel kdan --directory /opt/kdan --project-name kdan \
     --compose-file docker-compose.local.yml \
     --github-token-file /etc/sub2api-updater.github-token
   ```

   `--directory`、`--project-name` 必须取现有部署真实值。可用 `docker inspect <应用容器> --format '{{ index .Config.Labels "com.docker.compose.project" }}'` 查询项目名。已有多个 override 时按原顺序重复 `--compose-file`，避免丢失现有端口、网络或挂载配置。目录及配置必须 root 所有、不能组/其他用户可写。默认应用 GID 为 1000；自定义 UID/GID 部署用 `--socket-gid` 指定。
4. 安装器仅启动宿主机更新服务，并打印连接应用所需的完整 Compose 命令。确认使用的是新版本镜像后执行该命令，完成一次应用重建。私有仓库 token/代理也须设置于宿主机服务环境（例如 systemd 的 HTTPS_PROXY）。
5. 管理后台点击版本号 → 检查更新 → 立即更新。宿主机重新读取对应 GitHub 正式 Release，校验渠道、版本、固定镜像和摘要，拉取镜像，只重建应用服务。网页短暂断线后会自动刷新。

应用只挂载受限 Unix socket，**不挂载 Docker socket**。更新服务只接受版本号，不接收命令、镜像地址或 Compose 参数。私有 Release 通过 GitHub API 下载；token 不随跳转发给资产存储域名。

健康检查超时会尝试恢复原镜像 ID。状态持久化在 `/var/lib/sub2api-updater/<渠道>/state.json`，日志用 `journalctl -u sub2api-compose-updater@kdan` 查看。后台也显示失败信息。宿主机更新服务意外中断会标记失败，需检查容器实际状态后重试。

生成的 `compose.online-update.json` 保存当前固定镜像；之后的**所有手动维护命令**也必须包含安装器打印的全部 `-f` 文件（含 socket 和 online-update 文件），否则可能恢复旧镜像。不要删除该文件或以旧的单文件命令重建应用。可以把完整文件列表配置到运维入口的 COMPOSE_FILE。

在线回退同样选择本渠道已发布版本并重建容器。数据库出现不兼容迁移时须先人工恢复数据库备份。系统不会定时自动安装版本：检测更新与管理员点击更新分开。

未安装更新服务的 Docker 部署仍可检测新版本，但禁用在线更新按钮，避免仅替换容器内二进制而在下次重建时丢失更新。直接运行二进制的部署沿用原有校验和下载、原子替换、手动重启机制。

参考：[GitHub 并发队列](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)、[GitHub Release 资产下载](https://docs.github.com/en/rest/releases/assets)。
