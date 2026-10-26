# 手动发版与 Docker Compose 在线更新

独立供应商门户镜像与运营后端共用本页的人工发版入口、渠道、版本号和固定源码提交。门户镜像名来自 scripts/release/channels.json 的 portal_image；分别为 kdan-vendor、tapmodels-vendor、tokensavy-vendor。前端构建阶段生成主站与门户两份产物，发布时复用门户静态文件打包 linux/amd64 镜像；release-manifest.json 新增 portal_image / portal_image_digest，原后端 image / image_digest 保持不变。缺少门户产物或有效摘要时，不把本轮 Release 发布为完成。已发布的旧清单不补写新字段，旧版本提升流程仍按原清单处理。

门户与后端 latest 逐个提升，不能作为原子配套部署保证；使用清单中的明确版本或 digest。同项目更新器联动后端与门户；分机门户在本机安装同一更新器的独立门户模式，由超管在供应商站手动同步。更新器不远程控制 Docker，也不重建数据库或 Redis。门户的同机/分机安装、TLS 和独立更新见 [部署说明](../docs/vendor-console/PORTAL-ROUTING.md#独立门户镜像2026-10-19)。当前代码与本地测试不代表镜像已经上传或生产已经部署。

发布入口为 `.github/workflows/automatic-release.yml`，GitHub 页面显示为 **Manual versioned release**，仅允许通过 `workflow_dispatch` 手动运行。提交、推送代码或标签、创建 PR、合并分支都不会触发此 Workflow，不会因此分配版本、构建或发布镜像。需要发版时，由用户在 Actions 页面点击 Run workflow，或明确授权后执行下方的 `gh workflow run` 命令。

每次人工运行固定使用触发时选定的源码提交；准备、构建、镜像发布和 latest 提升不要求该提交仍是渠道分支的最新 HEAD。构建期间继续提交或重写分支不会中止本次发布，也不会把新代码混入已有产物；新提交需要另开一次人工运行。版本预留、标签与产物的提交一致性、同渠道串行执行及已发布版本的保护继续保留。修改发布工作流之前启动的旧任务仍使用旧脚本，需在更新后重新 Run workflow 才采用此行为。

人工启动后，构建仍采用原作者 Sub2API 的 Release 流程和工具：自动分配版本 → 前端类型/i18n 检查一次并构建主站和供应商门户 → GoReleaser 构建 Linux amd64 二进制及安装包 → 校验产物 → 用同一批 Linux 二进制打包 Docker 镜像 → 发布 GitHub Release。无需手动改 VERSION、打标签，也不需要 Docker Hub 凭据。单纯提交、推送不应调用手动发版命令；生产部署和数据库迁移仍需独立授权。

复用 `.goreleaser.yaml`、`.github/release-tools/release_matrix.py`、`release-images.sh` 和 `Dockerfile.goreleaser`。三个品牌的安装包、后端镜像和独立供应商门户镜像统一仅构建、验证和发布 `linux/amd64`。GoReleaser 配置是构建目标来源，发布工具拒绝其他目标或多余产物；不生成 ARM64、macOS、Windows 安装包，也不启用 QEMU。镜像构建不再重新编译前后端。参考源码：[原作者 Release 工作流](https://github.com/Wei-Shaw/sub2api/blob/a3eb7ef302961cba716dc78b39b93b60c467db0e/.github/workflows/release.yml)。

定制部分仅负责自动版本分配、渠道信息、发布草稿和在线更新清单。四段版本通过 GoReleaser snapshot 的版本模板打包，以独立的发布步骤上传到预留的正式 Release；不会把四段版本交给严格的 SemVer 标签解析。Linux amd64 安装包及两个镜像验证完成后才发布，GitHub Release 发布成功后才将对应镜像摘要提升为 `latest`，并把本次 Release 标为仓库级 `Latest`。

GitHub 的 `Latest` 是整个仓库共用的单一标记，可能指向最近发版的任一品牌。三个品牌均在 `holeenlu/sub2api` 发版；在线检查和回滚必须按 `kdan/v*`、`tapmodels/v*`、`tokensavy/v*` 渠道筛选，不能使用仓库 Latest 判断本品牌版本。GHCR 则按品牌使用独立镜像及独立 `latest` 标签。

| 渠道 | 仓库 / 分支 | Release 标签 | GHCR 镜像 |
| --- | --- | --- | --- |
| KDAN | holeenlu/sub2api / main | kdan/v0.2.8.1 | ghcr.io/holeenlu/kdan |
| TapModels | holeenlu/sub2api / TapModels | tapmodels/v0.2.8.1 | ghcr.io/holeenlu/tapmodels |
| Tokensavy | holeenlu/sub2api / tokensavy | tokensavy/v0.2.8.1 | ghcr.io/holeenlu/tokensavy |

`main` 是 KDAN 品牌默认、主开发和发布分支；不再维护独立 KDAN 或公共发布分支。TapModels 与 tokensavy 从本仓库对应品牌分支的固定提交构建并发布。`erwinlin/TapModels` 不再是发版入口；从该仓库运行此 workflow 会跳过发版。标签包含渠道名，但界面版本和镜像标签仅显示数字。不会读取混入的 sub4api v1.1.4 标签，也不会把 TapModels 版安装到 KDAN。三个品牌分支只保留 `automatic-release.yml`；独立 CI、安全扫描、CLA、上游定时监看、原作者手动发布和旧品牌镜像工作流均已移除，以减少 Actions 用量。发布所需的版本规则测试、更新界面/API 检查、类型检查、构建和产物校验仍在人工启动的发布流程内执行。

## 版本规则

- 从**已合入代码**可达的 Wei-Shaw/sub2api 官方稳定标签确定基准，不从远端最新版本或本地 VERSION 猜测。
- 初次接入当前定制代码：0.2.8.1。之后针对新提交手动发版时递增：0.2.8.2 … 0.2.8.9、0.2.8.10。只推送代码不递增。
- 合入新正式版本且上游进度正好停在该正式标签：0.2.9（保留现有品牌和定制）。若同时包含自上次发布以来的新定制提交，则首版为 0.2.9.1；同基准后续推送也递增第四位。
- 合入超过正式标签的未发版提交：0.2.9.1。多次提交、推送可以累积到一次手动发版，发布人工选择的分支及本次运行固定的提交。
- 每个渠道独立计数。失败保留 draft 和版本预留；同一 SHA 仅在 Release ID 唯一、标签及说明基线仍对应原源码时沿用原号，冲突草稿不再反复恢复。已发布同一 SHA 优先复用，不重复构建。准备阶段记录本次运行固定的源码 SHA，后续产物、标签及清单沿用该 SHA；分支推进或重建不影响本轮发布。
- Git 标签与 Release 是独立对象：删标签、列表没有显示某版本，都不代表版本号可复用。选号同时计入列表中的合法渠道标签（不要求旧正文含构建标记）和远端 Git 标签，并在创建前逐个调用按标签查询的 Release API；历史占号自动跳过，版本可能跳号。只有明确 404 才视为不存在，鉴权/限流/服务错误不当作空号；创建标签后再核查一次，出现并发占用时拒绝新增重复草稿。流程不自动删除历史 Release、附件或改写现有标签，最多检查 100 个候选号后明确停止。
- 发布说明只选择当前标签仍绑定原源码的已发布版本作比较基线，跳过删除或重绑标签的历史记录。遇到 `also used by IDs` 时，应更新到修复后的分支重新 Run workflow；不能仅重跑仍携带旧计划和草稿 ID 的失败 job，也不应靠不断删除一个冲突记录来推进下一版。
- 同一品牌由分支级 `concurrency` 串行执行版本预留、构建、发布与镜像晋升；不同品牌可以并行构建。仅发布阶段使用仓库级锁，顺序更新 GitHub 共用的 Latest。两级锁均设置 `queue: max` 和 `cancel-in-progress: false`，最多保留 100 个等待任务，不中断正在运行的发布；超过队列上限的任务会被取消。草稿不是可更新版本。
- `Manual versioned release` 在所有构建、发布和晋升步骤结束后清理运行历史：三个品牌合计按创建时间保留最新 10 条记录，成功、失败和取消均计数。当前运行及仍在运行、排队或等待的记录始终额外保护；完成后由后续清理收敛。删除前再次核对所属 Workflow、完成状态和重试次数，避免清理刚被重新运行的任务。清理任务跨品牌串行且仅授予 `actions: write` 与检出所需 `contents: read`；失败会保留未清理记录，不使已完成发布整体失败，下次手动运行会重试。只删除旧 Actions 运行记录及其日志/临时附件，不删除正式 Release、标签或镜像；旧运行删除后无法继续重跑。人工检查可运行 `GITHUB_REPOSITORY=holeenlu/sub2api python3 scripts/release/prune_runs.py` 预览，明确加 `--apply` 才执行。仍只通过 `workflow_dispatch` 触发，不新增定时或自动发版事件。
- VERSION 文件仍代表上游源码基准；实际运行版本、提交 SHA、上游版本由编译参数写入，不创建回写 VERSION 的循环提交。Git 标签不会自动改写已部署二进制的版本号；本地应急构建必须使用该渠道、该提交对应的发布计划版本，不能固定填写 `.1`。标签可能仅是失败构建预留，须核对正式 Release、清单和镜像后才能声称发布成功。

仓库 Actions 需要允许 `contents: write` 和 `packages: write`，GHCR 包需要授予该仓库 Actions 写权限。默认使用 GITHUB_TOKEN；无需 PAT 发版。完整发布在同一次人工启动的流程内完成；创建标签不会触发此 Workflow。

## 发布说明

每次正式 GitHub Release 都提供当前版本的**完整 Release Note**，结构参考 [tapmodels v1.3.225.0](https://github.com/holeenlu/vm2api/releases/tag/v1.3.225.0)。唯一正文来源为该发布提交中的 [CUSTOMIZATION_RELEASE_NOTES.md](../docs/CUSTOMIZATION_RELEASE_NOTES.md)，按新增功能、优化改进、Bug 修复、删除与退役、兼容性、配置与默认值、升级指南、品牌及来源组织；不以最近提交标题、上次补丁说明或日期日志替代。

新增、修改、优化或删除功能时，同次更新对应章节及配置/升级影响。被替代或删除的功能移出有效能力列表，在退役与兼容性中写明接口、旧数据及退出条件；历史记录通过 Git 和原评审文档追溯。审查实际 diff 和调用链，不能只改日期、标题或机械追加一句话来充当维护。

`scripts/release/notes.py` 读取发布计划绑定的源码提交，原样纳入完整说明，并把仓库相对链接转换成固定源码的 GitHub 链接；工作区未提交文本不能覆盖发布提交。脚本检查必需章节，功能/依赖/配置等源码变化却未更新完整说明时失败；纯版本号、测试或说明文档修改可继续使用仍有效的全文。结构和文件指纹检查不证明语义完整，实际功能取舍仍由代码审查负责。

人工 Workflow 在版本预留后、构建前执行同一完整性检查，正式发布前再次生成正文。已发布标签、Release 和附件保持原状；本次代码调整不自动改写历史 Release 或触发发版。

版本预留仍记录同渠道上一份已发布、非预发布版本的版本、标签和准确源码 SHA，草稿与其他渠道不作为对比基线，重试沿用原区间。可选的 `deploy/release-notes/<版本>.md` 只作为折叠的“相对上一版补充说明”，也必须来自本次发布提交，第一行仍要求 `<!-- previous-version: 上一版本 -->`，首次为 `none`；边界不匹配不显示，避免跳版时错误宣传。`0.2.17.2.md` 保留为当时的区间记录，不代表当前完整能力。

正文另附本次品牌/版本、后端及门户镜像摘要、Linux amd64 附件、校验文件、安装升级步骤及源码对比链接。没有源码差异的版本也携带完整说明；历史重写只影响区间统计，不把旧聚合提交标题重新算作新功能。`release-manifest.json` 和发布计划标记继续供在线更新核验。

本地预览可使用 `notes.py --plan <计划> --output <预览> --manifest <清单>`，通过 `IMAGE_DIGEST` / `PORTAL_IMAGE_DIGEST` 提供对应实际摘要；这只写本地文件，不调用 GitHub 发布接口。不要把占位摘要当成已构建或已发布的产物。

## Actions 用量控制

- `.github/workflows/` 只保留 `automatic-release.yml`，且触发器只能有 `workflow_dispatch`，不配置 push、PR、定时或其他自动触发事件。无需禁用仓库 Actions，否则人工 Run workflow 也不可用。
- GitHub 端已登记但不再使用的工作流应同时停用，防止旧分支或后续合并意外重新触发；历史运行记录不会继续消耗 runner 分钟。
- GitHub 内置的 `dynamic/dependabot/update-graph` 是 Dependabot graph job，不能通过普通工作流接口停用，且不计入 Actions 分钟，因此保留。它与会计费的 Automatic dependency submission 不同；后者如果另行启用，应在仓库设置中关闭。参见 [GitHub 依赖图计费说明](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-graph-data#dependabot-graph-jobs)。
- 合并上游时复核三个品牌分支的 `.github/workflows/`，不要重新引入自动触发器或已移除的工作流；`test_auto_release_workflow.py` 在本地和手动发版时检查此约束。本地测试脚本和发布辅助工具继续保留。
- 需要额外检查时在本地运行 `make test-frontend`、后端 `make test-unit` 和 `golangci-lint run ./...`。

## 启用与排障

1. 仓库 Settings → Actions → General 启用 Actions，并允许工作流使用其中的官方 Actions。工作流已按 job 声明所需写权限，不需要将仓库的默认 token 权限整体改成写入。
2. 私有仓库必须有可用的 Actions 额度。若运行页面 Annotations 提示付款失败或 spending limit，且 job 没有执行步骤，需要账号持有人在 Billing & licensing / Billing & plans 处理付款或预算；更换 PAT 或修改构建脚本不能解除该限制。
3. 首次发布会由仓库自身的 `GITHUB_TOKEN` 创建或写入对应 GHCR 包。若出现 package write denied，检查包设置中的 Manage Actions access，确保实际发布仓库具备写权限。
4. 需要发版时，在 `holeenlu/sub2api` 的 Actions → Manual versioned release → Run workflow 中，通过 **Use workflow from** 选择分支：KDAN 选 `main`、TapModels 选 `TapModels`、Tokensavy 选 `tokensavy`。workflow 按仓库和分支精确匹配品牌，不接受标签、错误大小写或其他分支作为发版入口。也可在明确授权发版后执行以下相应命令；这不是普通提交、推送步骤。重试同一提交会沿用已有草稿，不会重复分配版本：

   ```sh
   gh workflow run automatic-release.yml --repo holeenlu/sub2api --ref main
   gh workflow run automatic-release.yml --repo holeenlu/sub2api --ref TapModels
   gh workflow run automatic-release.yml --repo holeenlu/sub2api --ref tokensavy
   gh run list --repo holeenlu/sub2api --workflow automatic-release.yml
   ```

5. 验收要求：工作流成功；GitHub Release 为非草稿，包含一个 Linux amd64 `.tar.gz` 安装包、`checksums.txt`、`release-manifest.json` 和 `compose.vendor-release.json`；后端和供应商门户的 GHCR 数字版本均只支持 linux/amd64，`latest` 指向清单中的同一摘要。仅提交或推送成功不代表镜像发布成功。

本机 Fine-grained PAT 用于读取私有仓库和 Actions，不注入发布工作流。Actions API 的读取与 Checks API 的失败注释不同；若细粒度 Token 不能访问 Checks API，可在已登录 GitHub 的运行页面查看 Annotations，无需反复重新生成 Token。

## 供应商门户配套升级（2026-10-22）

后端与独立门户必须使用同一品牌、同一发布版本。发布成功只表示产物已就绪，不能证明生产门户容器已经更新；浏览器刷新也不会替换服务器上的旧静态文件。发布回归同时覆盖新增供应商和授权表单，减少前后端请求契约漂移。

- **同机、同 Compose 项目：** 先重新安装本版本 `install-compose-updater.py`，保留包括门户在内的全部 `--compose-file` 参数。安装器和更新器会自动识别 `vendor-portal`；自定义名称用 `--portal-service <真实服务名>`。两份镜像从同一正式 Release 清单读取并校验品牌及摘要，全部拉取和预检成功后才替换；门户 nginx 预检与后端迁移预检都失败即停止。运行中断或任一服务不健康时恢复整对镜像，保留其他 Compose override、数据卷和依赖服务。旧的单后端恢复状态继续可读，仅用于完成已发生的中断恢复。
- **分机或不同 Compose 项目：** 推荐安装下方独立门户更新器，之后在供应商站手动同步到后端运行版本。后端宿主机不操作远程 Docker。未启用网页更新时，发布附件 `compose.vendor-release.json`，只固定配套门户的不可变镜像摘要。在门户主机下载同品牌、同版本的该附件，沿用原项目名和全部 Compose 配置，在文件列表末尾追加 `-f compose.vendor-release.json`，先执行 `pull vendor-portal`，再执行 `up -d --no-deps vendor-portal`。自定义门户名需修改附件的服务键。后续维护保留此 override，回退时使用目标旧版门户镜像摘要。
- **验收：** 核对后端运行版本以及门户容器镜像的 `org.opencontainers.image.version`、`org.opencontainers.image.revision`，对应同一 Release 的版本、源码提交；刷新门户后验证新增供应商表单。门户只运行 nginx，不执行数据库迁移。仅更新后端或仅刷新页面均不算完整升级。

### 超管在供应商门户手动更新

超管登录供应商站 → 左侧栏站点名称下方的版本号。页面分别显示门户与后端的实际运行版本，不将后端版本作为门户版本。目标固定为当前后端版本；不同版本时点击版本号，在与运营站相同的版本面板中检查更新并点击「立即更新」；校验成功后页面自动刷新。一级、二级供应商和普通管理员不显示入口，后端实时超管鉴权仍会拒绝其直接请求。分机部署先更新运营站，再在门户同步；不允许门户独自追踪 latest 而超前于后端。

首次启用需要安装新版宿主机更新器，并让后端和门户都升级到包含此入口的版本。同项目可先重装更新器，再从运营站升级一次，同时带上门户；独立部署须在门户服务器首次手动升级门户镜像并接入更新器。旧静态页面无法自行出现新入口，旧宿主机 Python 文件也不会被应用镜像覆盖。以下为**独立门户项目**示例，目录、项目名和文件必须替换为现有部署值，所有原 Compose/env 文件按原顺序保留：

```sh
sudo python3 deploy/install-compose-updater.py \
  --channel tapmodels --portal-only \
  --directory /opt/tapmodels-portal --project-name existing-portal-project \
  --compose-file docker-compose.vendor.yml --env-file vendor-portal.env
```

自定义服务名加 `--service <门户服务名>`；私有 Release 仍使用既有 `--github-token-file`，GHCR 仍使用宿主机 Docker 登录。安装完成后执行它打印的完整 Compose 命令，将受限 Unix socket 接入门户。该模式使用独立的 `sub2api-compose-updater@tapmodels-portal`、状态文件、`compose.portal-updater-socket.yml` 和 `compose.portal-update.json`，可与后端更新器在同一主机并存。默认门户 socket GID 为 101，可用原有 `--socket-gid` 覆盖；安装器给门户添加对应补充组。KDAN、Tokensavy 仅替换 `--channel` 为 `kdan`、`tokensavy`。

同项目无需再安装独立模式：重新安装原更新器并包含门户 Compose 文件及 `--env-file`，执行打印命令为后端和门户同时接入 socket；运营站升级时自动联动两个镜像，门户入口也能修复版本不一致。禁止为同一门户同时安装两套更新器。

门户 nginx 通过内部鉴权子请求复用现有 `GET /api/v1/admin/system/version` 的实时超管权限，再把服务端确认的后端版本传给宿主机。浏览器不能指定后端地址、镜像、命令或伪造权限；缺少鉴权、后端不可达、更新服务未接入均拒绝启动。门户仅挂载受限更新 socket，不挂载 Docker socket。运行版本来自门户镜像标签与镜像内构建元数据。独立模式使用同次正式 Release 的门户 digest，执行 nginx 预检与健康检查；失败恢复门户旧镜像，不执行后端迁移，也不重建后端或存储。

## 生产启用（一次性）

**本功能的代码推送不安装或更新生产服务器。** 需要先把含此功能的版本部署一次，再配置宿主机更新服务。仅适用于 Linux + systemd + Docker Compose v2，每个受管理服务只能有一个运行容器，可联动同项目的独立门户。渠道名与 Compose 服务名可以不同，安装时用 `--service` 指定真实服务名。现有 PostgreSQL/Redis 不重建，数据卷保留。

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
5. 管理后台点击版本号 → 检查更新 → 立即更新。宿主机重新读取对应 GitHub 正式 Release，校验渠道、版本、固定镜像和摘要，从同一发布清单校验并拉取后端与同项目门户的固定摘要，只重建这些服务。两者都健康后才确认成功；任一失败则恢复两者的原镜像。网页短暂断线后会自动刷新。

应用只挂载受限 Unix socket，**不挂载 Docker socket**。更新服务只接受版本号，不接收命令、镜像地址或 Compose 参数。私有 Release 通过 GitHub API 下载；token 不随跳转发给资产存储域名。

先完成数据库备份及校验，再点击在线更新。更新器拉取候选镜像后，通过当前 Compose 连接参数运行 `--check-model-policy-migration`；预检失败时保留原应用和镜像配置，账号 ID 与原因见宿主机日志。候选二进制不支持此命令时也会在切换前停止，这类旧版本需人工预检、备份后部署。

预检容器沿用应用的网络与数据库配置，但临时清除固定 IPv4/IPv6 地址，使用动态地址，避免与仍在运行的应用冲突。正式应用的固定 IP 和 Compose 配置不变。旧宿主机 updater 若在预检阶段报 `Address already in use`，应更新宿主机 updater；无需停止正式应用来让出地址。仅更新应用镜像不会修复宿主机预检逻辑。

集群宿主协调器可先调用更新器的内部 Python 方法 prepare(version)，在各宿主机并行校验同一发布清单、拉取不可变镜像并完成预检，再逐台调用 start(version, prepared=True)。准备结果仅保存在本进程内、15分钟有效且只用一次；切换前再次核对完整有效 Compose 配置、服务集合及本地镜像 ID。配置变化、缓存过期/丢失或服务重启均须重新准备，不能从网页或 RPC 传入镜像来伪造结果。普通网页更新仍只接收版本号并执行原有完整流程；配套镜像健康核验、失败恢复和存储隔离不变。协调器自身的排空/流量策略由部署配置维护，不由该方法隐式改变。

切换前，每个受管理服务的新旧镜像、版本和阶段持久化到 `/var/lib/sub2api-updater/<渠道>/state.json`。健康检查失败恢复全部受管理服务的原镜像；更新器重启后只有后端与门户全部匹配目标且健康才保留新版本，否则一起恢复。恢复失败或历史记录缺少旧镜像时拒绝后续更新，避免覆盖恢复证据。日志用 `journalctl -u sub2api-compose-updater@kdan` 查看。镜像恢复不等于数据库回滚，不兼容迁移仍需人工恢复备份。

已有服务器须重新安装本版本宿主机 updater 才能使用预检与中断恢复；仅更新应用镜像不会替换宿主机 Python 服务。源码/SDK 编译默认渠道取 `brand.go`，与正式镜像一致，ldflags 可以显式覆盖。

生成的 `compose.online-update.json` 保存当前固定镜像；之后的**所有手动维护命令**也必须包含安装器打印的全部 `-f` 文件（含 socket 和 online-update 文件），否则可能恢复旧镜像。不要删除该文件或以旧的单文件命令重建应用。可以把完整文件列表配置到运维入口的 COMPOSE_FILE。

在线回退同样选择本渠道已发布版本并重建容器。数据库出现不兼容迁移时须先人工恢复数据库备份。系统不会定时自动安装版本：检测更新与管理员点击更新分开。

未安装更新服务的 Docker 部署仍可检测新版本，但禁用在线更新按钮，避免仅替换容器内二进制而在下次重建时丢失更新。直接运行二进制的部署沿用原有校验和下载、原子替换、手动重启机制。

参考：[GitHub 并发队列](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency)、[GitHub Release 资产下载](https://docs.github.com/en/rest/releases/assets)。

### 私有仓库返回 404

GitHub 会对无权访问的私有仓库返回 404。先核对应用 `UPDATE_GITHUB_TOKEN` 是否配置，以及该 Token 是否能读取 `holeenlu/sub2api`；仅能读取旧 `erwinlin/TapModels` 的 Token 不再适用。服务器的应用检查、宿主机 Release 清单下载、GHCR 镜像拉取是三个独立连接，分别需要应用环境变量、`--github-token-file` 和宿主机 Docker 登录。无成功发布的渠道也没有可安装版本；创建 Git 标签或提高页面版本号不能代替发布镜像。

## TapModels 迁移与 Tokensavy 首次启用

代码提交、推送本身不会创建上述新镜像；首次人工发版成功后镜像才可拉取。旧站不会因本次代码修改自动切换仓库或升级，Tokensavy 也不会继续发布本地 `tokensavy:local` 镜像。

1. 在本仓库对应分支手动发版，核对该渠道正式 Release 和 GHCR 镜像已存在。TapModels 原仓库已发布至 `0.2.13.2`（2026-10-03 核对）；渠道配置保留该版本下限，相同上游基准的首次新仓库发布从 `0.2.13.3` 起，避免版本倒退。原仓库的历史标签和产物不自动迁入本仓库，回滚列表只包含新仓库本渠道的已发布版本；首次迁移保留旧镜像摘要用于人工恢复。
2. TapModels 已部署旧版本时，先备份，并将 `.env` 中 `TAPMODELS_IMAGE` 改为 `ghcr.io/holeenlu/tapmodels:<已发布版本>`。清除或更新已有 `compose.online-update.json` 中的旧镜像固定值时，保留其余配置、服务名、项目名和数据卷；使用完整 Compose 文件列表部署一次含新仓库配置的版本。这是生产操作，不由代码推送自动执行。
3. 在部署主机重新运行此版本的 `install-compose-updater.py --channel tapmodels`，沿用原来的 `--directory`、`--project-name`、`--service` 和所有 `--compose-file` 参数。安装器会把宿主机配置中的仓库和镜像迁移到 `holeenlu/sub2api` / `ghcr.io/holeenlu/tapmodels`，并重启更新服务。旧应用只改 updater 配置不足以切换应用自身的版本检查来源。
4. Tokensavy 首次部署使用 `deploy/tokensavy/compose.yaml` 的 GHCR 镜像，然后在 Linux 宿主机安装 updater（用实际绝对路径替换示例）：

   ```sh
   sudo python3 /opt/tokensavy/deploy/install-compose-updater.py \
     --channel tokensavy --directory /opt/tokensavy/deploy/tokensavy \
     --project-name tokensavy --service tokensavy --compose-file compose.yaml \
     --github-token-file /etc/sub2api-updater.github-token
   ```

   公开仓库可省略 token 文件参数。安装器只配置宿主机服务；随后执行它打印的完整 Compose 命令连接应用。应用只挂载受限 Unix socket，同项目门户可联动更新；数据库、Redis 和 Caddy 不参与在线升级。后续手动维护也保留生成的 socket 和 online-update 两个 override。
5. Tokensavy 无已发布镜像时仍可通过 `compose.source.yaml` 显式构建源码镜像作首次安装；之后接入在线更新时必须切回镜像部署配置，不能继续使用源码 override。不会重新生成账号密码或修改已有 `.env` 中的密钥。
