# NewAPI 在线更新器

在线更新从 `holeenlu/sub2api` 的 `main` 手工发版产物中读取 `NewAPI/latest`。宿主机服务只监听 Unix socket，API 容器仅挂载该目录并转发受现有管理员权限、二次验证和幂等保护的操作，不获得 Docker socket、宿主路径参数或命令执行入口。更新由管理员明确点击触发，不定时自动升级。

## 安装与启用

以下文件来自经校验的完整部署包 `updater/`（源码目录为 `deploy/updater/`）。安装前核对本机 Compose 项目名、原始 Compose 文件和服务角色。`config.example.json` 展示四应用实例部署；常规三实例部署可删除 `web-direct` 条目，必须仍包含 api/web/vendor 三个角色。供应商门户与主站两个前端角色成套更新。

1. 使用 root 安装宿主脚本、unit 与启动目录规则：

   ```sh
   install -d -m 0755 /opt/newapi-updater
   install -m 0644 updater/newapi_updater.py /opt/newapi-updater/newapi_updater.py
   install -d -m 0700 /etc/newapi-updater /var/lib/newapi-updater
   install -m 0600 updater/config.example.json /etc/newapi-updater/config.json
   install -m 0644 updater/newapi-updater.service /etc/systemd/system/newapi-updater.service
   install -m 0644 updater/newapi-updater.tmpfiles.conf /etc/tmpfiles.d/newapi-updater.conf
   ```

2. 在宿主机编辑 `/etc/newapi-updater/config.json`。文件必须 root 所有、0600；路径与服务名由此文件固定，不接受浏览器覆盖。`project_name` 必须与现有容器的 Compose 项目一致。API 镜像默认进程 UID/GID 为 1000；修改运行用户时同步调整 `api_uid`、`socket_gid`、tmpfiles 目录组和容器附加组。私有 GitHub Release 可增加 `github_token_file` 指向 root:root 0600 的独立 token 文件；token 不进入 API 容器或 API 响应。Docker 拉取私有镜像使用宿主原有只读 registry 登录配置。

3. 确认当前 `/opt/newapi/release-manifest.json` 与实际运行镜像相符。新包 manifest v2 包含 `repository`、`branch` 和每个 SQL 迁移文件的 SHA256。旧版安装没有迁移清单时，需要从**该次实际部署源码提交**核对 SQL 清单并补齐基线；不能复制最新版本清单冒充当前数据库状态。没有可信基线时更新器只能展示状态，拒绝更新。

4. 先创建并启动宿主控制目录/服务：

   ```sh
   systemd-tmpfiles --create /etc/tmpfiles.d/newapi-updater.conf
   systemctl daemon-reload
   systemctl enable --now newapi-updater.service
   systemctl status newapi-updater.service
   ```

   `/run/newapi-updater` 为 root:1000 0750，socket 为 root:1000 0660。unit 使用 `RuntimeDirectoryPreserve=yes`，保留目录 inode，重启服务只重建 socket；tmpfiles 确保重启主机时目录在 Docker 恢复应用容器前存在。不要只绑定单个 socket 文件，否则重建后容器会继续连接旧 inode。

5. 再启用 API 挂载。`compose.yaml` 是可选 overlay，设置 `NEWAPI_UPDATER_SOCKET=/run/newapi-updater/control.sock`，只读绑定整个目录，且 `create_host_path:false`。**请将其合并进长期使用的原始 JSON Compose 文件**，或确保每个运维启动命令和 systemd 单元都使用同一组 Compose 文件；当前宿主更新器只接受单个原始 JSON Compose 文件，并只修改其中的应用 `image` 字段，不能只在一次启动命令中临时使用 overlay。保留既有网络、端口、静态地址、密钥、`$$` 转义和数据卷。确认后按既有运维流程重建 API 使挂载和环境生效。普通源码构建且未设置 socket 时，更新功能保持未启用。

6. 超级管理员在现有版本信息入口检查更新、阅读对应 Release Note，并明确确认更新。更新将替换 API、web、vendor 三个镜像及映射到这些角色的所有应用实例；短时重建和连接排空按原 Compose 的停止宽限设置执行。数据库、Redis、Caddy 及其他项目不在重建服务列表内。

## 校验与安全边界

- 固定仓库、main、产品标签和 linux/amd64。要求已发布稳定 Release、manifest asset 的 GitHub SHA256、标签解析到同一个源码提交、三个角色各自固定 digest。拒绝角色交换、跨仓库、HTTP/不可信资产跳转、降级及同版本换源码/换镜像。GitHub API token 不随跳转发送到资产域名。
- 点击更新前再次获取完整清单，要求与管理员检查时的清单相同。接单时冻结当前 Compose/manifest 的 SHA256；worker 开始、拉取完成和提交结果前继续核对漂移。所有候选镜像全部拉取后，核对真实本地镜像 digest、OCI 源码/版本标签和平台，再开始替换。
- 只接受与当前可信迁移清单**完全一致**的更新。新增、删除或更改 SQL 时，显示“需手动迁移”并拒绝一键更新；需要单独评审、备份、停止财务写入/排空与迁移验收后建立新的部署基线。此限制不能用修改 manifest 绕过。
- 原 Compose 与 manifest 按原始字节保存到 `/var/lib/newapi-updater/backups/<operation_id>/`，root 0600；其中包含真实配置，不能公开。状态文件不包含 Compose 内容、token 或 Docker 输出。状态写入失败时不继续切换。
- 只有实际四/三个应用容器都使用预期镜像 ID 且健康时才记录成功。健康失败会尝试恢复所有旧镜像；不恢复数据库、Redis 或运行时数据。即使 SQL 清单相同，镜像回退也不证明业务数据完全可逆；真实调用、计费与页面业务验收应按更新内容另行执行。
- 回旧前、回旧后发现新的宿主配置改动时不会覆盖，转入 `manual_intervention_required`。更新期间服务进程中断：若仍在 preparing/pulling/preflight，记录失败且保留原服务；在切换、验收、提交或回退阶段中断，一律阻止后续更新并要求人工检查，不自动重放，不自动还原数据库。
- 文件锁防止两个宿主更新器同时接管，同一 `operation_id` 重试读取原任务，不重复替换。运行状态和已接单操作持久化，API 重启后仍可查询。API 与备份/恢复等系统操作的长期互斥由现有系统操作保护执行。

## 手工处置

`/var/lib/newapi-updater/state.json` 保存任务阶段与备份路径；`systemctl status newapi-updater.service` 检查宿主服务。处理 `manual_intervention_required` 时先停止更新器，核对原 Compose、实际容器镜像 ID、健康状态、当前 manifest、精确备份和新产生的业务写入。确认部署一致后，由操作员修正状态或更换到经过验证的新 manifest，再启动服务。不要通过删除整个状态文件绕过未完成更新，也不要复活旧 KDAN 更新器或运行旧品牌 Compose。

本目录内的宿主程序和 systemd 单元须通过审查后由操作员更新；应用在线更新不会执行 Release 附带的任意宿主安装脚本。备份只按明确保留策略人工清理，更新器不执行 Docker prune 或数据库清理。

## 验证

```sh
python3 -B -m unittest deploy.tests.test_online_updater -v
```

测试使用真实 Unix HTTP 和持久文件，Docker/GitHub 部分为显式夹具；这些结果不代表镜像构建、生产切换、真实数据库、浏览器或上游调用验收。
