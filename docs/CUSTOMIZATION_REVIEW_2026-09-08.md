# 定制需求、代码与分支审查（2026-09-08）

审查时间：2026-09-08，Asia/Shanghai。完成 `git fetch --all --no-tags` 后固定下列快照。
第 1-5 节记录首次审查时的代码与结论；同日按用户要求完成的本地修复和验证见第 6 节。
下列 SHA 和第 6 节是提交前的审查与验证快照；后续提交、推送状态以各分支 Git 引用为准。本轮未部署生产。
配套流程见 [项目定制与同步流程](PROJECT_CUSTOMIZATION_WORKFLOW.md)。

## 1. 已确认的问题（修复前快照）

### R1 / P1：预发布版本也会覆盖正式 latest 镜像

- 范围：KDAN、TapModels 的 Docker 发布工作流。
- 位置：`.github/workflows/tapmodels-docker-image.yml:122`；KDAN 同名逻辑在 `origin/KDAN:.github/workflows/kdan-docker-image.yml:122`。
- 触发：发布 `1.0.0-rc.1` 这样的数字开头预发布 tag。它符合 `tags: ['[0-9]*']`。
- 实际行为：`type=raw,value=latest` 只判断引用是否以 `refs/tags/` 开头，预发布同样更新 `latest`。工作流第 223 行却把带连字符的 tag 标为 GitHub prerelease，镜像和 Release 的稳定性语义不一致。
- 影响：使用品牌 Compose 默认 `:latest` 的部署在下一次拉取时可能得到预发布代码。这里没有证据表明生产已经受到影响。
- 建议：严格解析版本，只有无 prerelease 部分的正式版本才能提升 `latest`；分支、SHA、预发布 tag 各自发布；Release 标记与镜像提升使用同一判定。
- 验证：核对两个分支实际 YAML 条件及 Compose 默认镜像。没有推送测试 tag，也没有发布镜像。

### R2 / P2：自定义站名未经转义写入 TOML

- 范围：公共层、KDAN、TapModels。
- 位置：`frontend/src/components/keys/UseKeyModal.vue:1284`；同类插值还在第 1084、1213 行。
- 触发：后台站名为 `Acme "Lab"`，下载 Anthropic 等路由分组的 Codex 配置，或 Grok 配置。
- 实际输出：`name = "Acme "Lab" Anthropic"`，TOML 解析失败；普通站名 `Acme Lab` 的控制样本可以解析。
- 影响：合法的品牌名称使下载/复制的 CLI 配置不可用。已有模板下载测试把站名固定为 `Sub2API`，未覆盖该场景。
- 建议：字符串字段统一通过完整的 TOML 转义或序列化处理；注释中的站名另处理换行。覆盖双引号、反斜线和控制字符，展示、复制、下载继续使用同一份内容。
- 验证：从三个固定 ref 提取实际 Vue 脚本，用 TypeScript AST 提取实际生成函数，再以现有 `smol-toml` 解析输出；三个分支都复现相同错误。不是重新手写一份模板进行测试。

### R3 / P2：通配白名单的 Codex 顺序与模型列表不同

- 范围：公共层、KDAN、TapModels；相关后端文件的 Git blob 完全一致。
- 位置：`backend/internal/service/openai_codex_models_service.go:1451`。
- 触发：管理员按顺序配置 `gpt-6*`、`gpt-5.6-sol`，上游目录含 `gpt-6-astra` 和 `gpt-5.6-sol`。
- 预期：与 `GroupModelAllowlist.FilterForListing` 一致，先 `gpt-6-astra`，后 `gpt-5.6-sol`。
- 实际：排序只查 `selectedIndex[slug]`，通配展开模型落入“未列出”区，变成 `gpt-5.6-sol`、`gpt-6-astra`。后续 priority 重写进一步固定这个错误顺序。
- 原因：上游白名单已经支持通配和大小写归一化，本地排序仍使用早期精确模型列表的匹配方式。这是合并后可编译但语义没有接齐的例子。
- 建议：以统一白名单展开结果确定排序位置，保持同一条目内部的来源顺序，并复用现有 priority 和 ETag 更新机制。测试同时比较模型列表、manifest 数组和客户端 priority 顺序。
- 验证：临时 Go overlay 用实际 `FilterForListing` 作为对照，调用实际 manifest 合并函数，得到：

```text
allowlist and /v1/models order = [gpt-6-astra gpt-5.6-sol]
Codex manifest order          = [gpt-5.6-sol gpt-6-astra]
```

### R4 / P2：仅诊断起点池却返回整次请求的精确 Fable 重置时间

- 范围：公共层、KDAN、TapModels；相关后端文件的 Git blob 完全一致。
- 位置：`backend/internal/service/gateway_model_availability.go:214`；第 339 行起的生产入口直接按此提前返回。
- 触发：起点池所有 Fable 账号因 `7d_oi` 冷却 48 小时，合法备用池的同模型账号只因普通限流冷却 30 秒。
- 实际：起点有模型支持就结束诊断，保留起点的 `anthropic_7d_oi_window_exhausted` 和 48 小时重置时间。`backend/internal/handler/no_account_error.go:320` 再把它输出为 `anthropic_fable_7d_oi_exhausted` 及 `retry_at`、`retry_after_seconds`。
- 影响：备用容量很快恢复，客户端却收到最长两天的精确等待提示；HTTP `Retry-After` 封顶 300 秒并不能修正响应体中的错误归因。
- 需求冲突：旧功能说明要求跨池归因一致才发布 Fable 专属错误；9 月 6 日修订说明又接受“仅起点”优化。两种说法不能同时作为验收标准。
- 建议：保留节省查询的目标，但不能在未证明全链一致时发布精确归因。优先复用选号阶段的信息；证据不足时回退通用 429，并把精确重置时间的覆盖范围写清楚。
- 验证：通过实际 `DiagnoseModelAvailabilityForPlatform` 入口复现。现有跨池归因测试直接调用合并 helper，绕过提前返回，所以原测试绿灯不能证明生产入口正确。

### R5 / P2：上游监看仍向协作者输出旧的 rebase / force-push 操作

- 范围：TapModels 工作流及三分支同步说明；KDAN 说明内部也仍有相互矛盾段落。
- 位置：`.github/workflows/tapmodels-sync-upstream.yml:224` 至第 228 行；`docs/PRIVATE_REPOSITORY_SYNC.md:34`。
- 实际：监看 issue 明确推荐 `deploy/sync.sh`、`git rebase upstream/main`、`git push --force-with-lease erwinlin TapModels:main`。
- 冲突：当前个人 `sync-upstream` 技能和真实历史已经采用普通 merge、保留原 SHA；旧 `deploy/release-local/sync.py` 仍实际执行 rebase 和 lease force-push。
- 影响：协作者照新生成的监看 issue 操作，会重新改写共享历史，破坏提交溯源和后续协作；不是仅有一条过期注释。
- 建议：文档、自动 issue 正文、CLI 入口统一到普通 merge；旧入口明确退役。改正当前 issue 模板后，再处理已经存在的旧 issue 指令。

## 2. 范围与分支证据

| 代码层 | 用户指定的引用 | 审查 SHA |
| --- | --- | --- |
| 公共集成 | `origin/main`、`origin/HEAD`、`holeen/main` | `63130ff7bd581163338a55d1a291d152c58362d2` |
| KDAN | `origin/KDAN`、`KDAN` | `240183ba631621e2c3dc6adee1643e7707ed9047` |
| TapModels | `TapModels`、`origin/TapModels` | `94129cb702dfd768504166d84bfc2dc7f81ec13d` |
| 补充核对的协作镜像 | `erwinlin/main` | 与 TapModels 完全相同 |
| 上游比较基线 | `upstream/main` | `b7dba62678a834080564966c002fd0ca2b328b7a` |

`origin/HEAD` 是指向 `origin/main` 的符号引用，不是独立分支。三个代码层都已包含上述上游 SHA；本地最近可解析上游 tag 为 `v0.2.2`，这不等于核对了 GitHub 最新 Release 页面。

远端映射：`origin = holeenlu/sub2api`，`upstream = Wei-Shaw/sub2api`，`erwinlin = erwinlin/TapModels`。本地 `main` 跟踪公开上游，不是私有 `origin/main`。

审查清单覆盖公共层相对上游的 498 个变更文件、37 个非 merge 定制提交；KDAN 相对公共层有 112 个文件差异、7 个独有非 merge 提交；TapModels 有 119 个文件差异、6 个独有非 merge 提交。去重后为 50 个非上游、非 merge 提交，另核对 9 个同步 merge。品牌独有提交数包含品牌适配过的公共修复，不表示全部都是新品牌需求。

方法是完整变更清单盘点，加上高风险调用链、需求对应和定点复现；不是声称对全部 498 个文件的每条运行路径完成形式验证。重点覆盖调度/计费归属、错误协议、账号导入和模型同步、manifest、配置下载、国际化、品牌常量、CI 与部署流程。

归属按可达提交和代码差异判断，不只按作者名过滤。`f675351b0` 的作者是 Erwin Lin；另有以 holeen 为作者、正文记录 Erwin 协作的首页、多语、构建和品牌提交。Git 元数据不足以证明每一段代码由哪款 AI 生成，本报告不据此推断模型贡献。

### 公共修复尚未形成统一的传播关系

两个品牌与公共层都有两个 merge-base：`994c85b99` 和 `b7dba6267`。公共层当前 tip 不是任一品牌 tip 的祖先。

- `origin/main...origin/KDAN` 左右各有 6 / 10 个独有提交。
- `origin/main...origin/TapModels` 左右各有 6 / 9 个独有提交。
- 近期 Logo、品牌修复、多语补齐在三个分支分别提交，SHA 不同，不能仅凭提交名相同判断内容完全一致。
- 当前 `sync_upstream.py:311` 给每个分支合入的是固定 `upstream` SHA，并不把最新公共层合入品牌。`--all` 表示三个分支都同步上游，不能当作“公共修复已传播到所有品牌”的证明。

现有同步工具可以继续完成其原有职责。公共修改向品牌传播需要单独验收，目标流程见配套文档。

## 3. 需求文档审查

| 文档 | 当前价值 | 需要校正的状态或边界 |
| --- | --- | --- |
| `docs/LINEAR_16_COMMITS_FUNCTIONAL_SPEC.md` | 16 项早期业务需求、计费与调度约束较完整 | 本机文件未被 Git 跟踪，SHA 已是旧历史；不能继续作为“当前 16 个提交”清单；Fable 全链归因要求与当前实现冲突 |
| `docs/KEYS_PAGE_AND_BRANDING_PLAN.md` | keys、模型默认值、下载一致性、品牌盘点的需求来源 | 仍以旧基线及“方案未实施”叙述为主，当前功能已落地；需把待定项、实际决定、验收结果分开 |
| `docs/REVIEW_FIX_2026-09-06.md` | 旧审查缺陷和性能取舍可用于追溯 | 本机未跟踪，开头仍称补丁未提交；相关代码已经可达；不应再按说明盲目重放旧 patch |
| `docs/I18N_COMPLETENESS_AUDIT.md` | 三语范围、运行时内容边界明确 | 第 74-75 行仍称未提交/推送；三条远端已含相应多语提交，但这不证明已部署 |
| `docs/PRIVATE_REPOSITORY_SYNC.md` | 分支职责、私有访问、构建身份说明有用 | rebase 说明过期；公共层/TapModels 仍称 release 驱动只支持公共分支，实际本机驱动已支持显式分支 |
| `docs/tapmodels-ci.md` / KDAN 的 `docs/kdan-ci.md` | 品牌镜像及发布目标可作为维护入口 | 需要同步普通 merge、正式/预发布语义、本地发布驱动变化及 CI 实际覆盖范围 |
| `tools/zh-tw/README.md` | 源语言、生成物、台湾词典和后端匹配保护边界清楚 | “同步 PR”与仅监看的现状不一致；转换树只 build、不做完整行为测试仍是明确缺口 |

当前需求可以归纳为：保留上游能力与升级路径，在公共层维护共享定制，品牌层维护各自展示及发布渠道；前端提供 `en / zh / zh-TW`，台湾用语由规则生成；不因借用账号改变客户计费/权限，不因品牌或翻译改变协议、凭据、现有存储契约。

需要用户作出的产品决定，与已确认 Bug 分开：

1. 是否采用“上游 -> 公共层 -> 两品牌”的目标传播拓扑，而不再长期对三条分支重复维护公共修复。
2. 后端最终要求“按构建固定繁中”还是“按请求语言返回消息”。现在只有前者，不能把前端三语等同于后端三语。
3. 公共层是否会单独运营。其 `DefaultReleaseRepo` 仍是 `Wei-Shaw/sub2api`，默认在线检查开启；若作为定制生产版本使用，需定义自己的升级策略。
4. 品牌专属文案、资产、文档链接的最终验收人；现有历史提交包含占位品牌素材说明，源码测试不能替代品牌确认。

## 4. 首次审查验证记录

| 检查 | 结果与实际范围 |
| --- | --- |
| 远端刷新及引用盘点 | `git fetch --all --no-tags` 成功；本地/远端三组引用一致 |
| TapModels 全量前端测试 | 从 `frontend/` 运行 Vitest：281 个文件、2,115 项通过 |
| TapModels 类型检查 | `vue-tsc --noEmit` 通过 |
| 后端既有测试 | Go 1.27，`-tags=unit`；service、handler、handler/admin、handler/dto、repository、config、web、ent/runtime、migrations 共 9 包通过 |
| 三分支繁中生成检查 | 均通过；公共层和 KDAN 使用各自固定 ref 的临时快照 |
| 品牌打包静态测试 | KDAN 3 项通过，TapModels 3 项通过；公共层 1 项通过、2 项品牌专用检查跳过 |
| 后端转换审查 | 对原始树运行 `convert-go.mjs backend --dry --report` 和 `audit.mjs backend`；仅静态候选审查 |
| R2 定点复现 | 三个 ref：普通站名解析成功，带双引号站名解析失败 |
| R3 / R4 定点复现 | 两个 Go overlay 用例均按预期暴露问题，未写入业务源码目录 |

复现材料位于本机 `/tmp/sub2api-review-20260908/`：`review_regressions_test.go`、`overlay.json`、`regressions.log`、`check-site-name.mjs`。这些是临时审查材料，不是仓库新增回归测试。正式修复时应把相应用例放回代码所属测试目录。

运行记录：第一次从仓库根目录启动 Vitest 没有加载前端配置，错误收集了 `.release` 副本；该结果已废弃，以上前端结论来自随后在 `frontend/` 的正确运行。

未执行完整前后端生产构建、带 `embed` 标签的后端测试、数据库迁移集成测试、真实浏览器端到端、镜像发布和线上验收。没有读取生产数据库。也没有查询 GitHub Actions 的实际运行状态；源码中的工作流存在不等于远端已启用且绿灯。

## 5. AGENTS.md 与旧模型约定

本轮现场确认：项目根目录 `AGENTS.md` 和 `~/.codex/AGENTS.md` 均为 0 字节；项目 `CLAUDE.md` 只有多语、SDK 和发布提示，没有强制子代理模型。

“主代理只编排，必须通过 gpt-5.6-sol 子代理执行”的来源是旧的跨会话记忆，不是当前空白 `AGENTS.md`。现行 `sync-upstream` 技能反而明确写着在当前会话直接执行、不使用子代理。当前用户的纠正优先，本轮直接审查，没有调用子代理。

清空文件不会删除独立保存的记忆或当前会话已经收到的历史文本。官方文档也说明空 `AGENTS.md` 会被跳过，指令在运行开始时装载：[OpenAI 官方 AGENTS.md 说明](https://learn.chatgpt.com/docs/agent-configuration/agents-md)。这只能解释文件装载；本次旧约定的具体来源由现场记忆内容确认。

用户若要明确要求永久清理该记忆，可使用：

> 请永久撤销并更新跨会话记忆：在 /Users/luhonglin/Git/sub2api 中，不再要求主代理只负责编排，不再强制使用 Agent 或 gpt-5.6-sol 子代理。默认由当前主代理直接完成阅读、修改和验证；只有我明确要求时才使用子代理。此指令覆盖此前关于该项目的同类约定，AGENTS.md 保持为空。

本次只解释来源、应用当前纠正和提供提示词，没有把永久记忆清理伪报为已完成。

## 6. 五项问题的本地修复

2026-09-08 后续修复已完成，原始发现保留用于追溯。所有适用代码都已更新到对应分支的工作区，没有重写已发布历史。

| 问题 | 实际修复 | 回归验证 |
| --- | --- | --- |
| R1 | 新增共享 `deploy/release_metadata.py` 严格校验数字 SemVer；正式/预发布使用同一判定；关闭 Docker metadata 的自动 latest，显式只提升正式版本；GitHub Release 同步设置 prerelease；不推镜像时不创建 Release | 正式、rc、纯数字 prerelease、build metadata、非法版本；执行实际 workflow 的标签校验步骤 |
| R2 | 站名写入 TOML 字段时转义引号、反斜线和控制字符；注释中的换行/控制字符转为空格，避免污染下一行 | 三个分支分别用真实 Vue 组件下载文件并解析，覆盖路由 Codex、Grok、Grok Codex、mac/Windows；用例已纳入 Makefile 的 CI 关键测试 |
| R3 | 排序按首个匹配白名单条目排名，复用 `GroupModelAllowlist.Allows`，统一通配/大小写规则，保持条目内部来源顺序 | 通配优先、精确优先、大小写、多通配和重叠条目；priority、未知元数据、ETag 与重复处理稳定性 |
| R4 | 精确 Fable 归因需要遍历合法备用链；混合限流清除精确归因并采用更早恢复时间；分组读取失败时回退通用结果 | 真实诊断入口覆盖单池、普通备用池、Fable 备用池及起点/目标分组读取失败；原有普通限流不额外查链的测试仍通过 |
| R5 | 公共和品牌监看统一调用 `deploy/upstream_sync_instructions.py`；修正分支发布目标、文档与注释；本机旧 shell/Python 同步入口明确退役 | 指令生成、正确远端、禁止旧 rebase/force-push 示例；旧入口实际调用仅返回退役错误，不执行同步 |

工作区位置：

| 分支 | 保留修复的位置 |
| --- | --- |
| `TapModels` | `/Users/luhonglin/Git/sub2api` |
| `holeen/main` | `/Users/luhonglin/Git/sub2api-fix-public` |
| `KDAN` | `/Users/luhonglin/Git/sub2api-fix-kdan` |

此次验证结果：

- TapModels 全量前端：281 个文件、2,118 项通过；类型检查、修改文件 ESLint 和前端生产构建通过。
- 公共层、KDAN：各 77 项配置下载测试通过，各自类型检查通过。
- TapModels 后端：`service`、`handler`、`handler/admin` 完整 unit 测试通过；`service`、`handler` 的 vet 通过。
- 公共层、KDAN 后端：模型合并/排序、通用和 OpenAI 可用性诊断定点测试通过。
- 品牌/发布/同步检查：TapModels 与 KDAN 各 7 项通过；公共层 7 项中 2 项品牌专用检查跳过，其余通过。
- 三个分支的繁中生成检查与 `git diff --check` 通过；共享后端修复、脚本、测试、公共监看及 Makefile 已做内容一致性校验。
- 生产构建保留原有分包体积提示。此次没有数据库迁移、镜像发布、生产操作或远端 issue 修改。

工作流修复随对应分支推送生效；已经生成的旧 issue 正文未在本轮修改，生产也未部署。普通限流保留起点池的保守重试提示，精确 Fable 归因才核对完整备用链；这是有意保留的查询边界。
