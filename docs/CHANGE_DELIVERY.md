# 需求归属与提交、推送规范

适用于新增功能、修复、重构、合并需求及其交付。当前采用 `main` 作为 KDAN 品牌主分支、`TapModels` / `tokensavy` 品牌派生和 `upstream-base` 官方基线的结构。目标是先判断改动归属，再选择提交位置和发布目标；当前检出的分支只是工作现场，不能作为归属依据。

## 当前分支结构

共享新增、修改、删除、重构和官方上游整合先在 `main` 完成。`main` 即 KDAN 品牌分支；`TapModels`、`tokensavy` 接收 `main` 的普通 merge，再单独保留品牌覆盖。品牌专属需求只在对应分支完成。远端不再维护独立 `KDAN` 或公共分支；`upstream-base` 只保存官方上游引用，不承载本地品牌代码。完整结构见 [分支结构与品牌交付模型](BRANCH_STRUCTURE.md)。

自 2026-10-03 起，代码推送仅限本仓库 `holeenlu/sub2api` 的 `origin/main`、`origin/TapModels`、`origin/tokensavy`；不再推送 `erwinlin/main` 或其他仓库。推送目的地不能由跟踪关系推断，一律写全源、目标 ref。旧文档、历史整合记录、已保存检查点中的旧目标不能覆盖现行规则。

`git cherry` 的 `+` 表示未找到等价补丁，可能来自品牌适配、拆分或改写，不能直接解释为功能缺失。此前所谓“4 个历史提交未同步”只应视为待逐项审查的候选项。

## 1. 需求开始时判断，完成后复核

| 归属 | 判定依据 | 首次落地分支 | “提交、推送”的默认交付范围 |
| --- | --- | --- | --- |
| 共享能力 | 业务逻辑、安全修复、上游整合和经过审查的跨品牌能力 | main，验证后普通 merge 到品牌分支 | origin/main、origin/TapModels、origin/tokensavy |
| KDAN 定制 | KDAN 专属品牌名称、资产、域名和商业政策 | main | origin/main |
| TapModels 定制 | TapModels 专属名称、域名、资产、商业政策、默认配置、部署、法律主体和繁体覆盖 | TapModels | origin/TapModels |
| Tokensavy 定制 | Tokensavy 专属名称、域名、资产、商业政策、默认配置、部署、法律主体和繁体覆盖 | tokensavy | origin/tokensavy |
| 混合 | 同一需求同时包含共享逻辑和品牌覆盖 | 先 main，再对应品牌提交 | 本仓库 main → TapModels、tokensavy |

目录和关键词只能给出线索。例如 HomeView.vue 既可能是公共布局修改，也可能是品牌营销修改；语言包中的品牌名称不能使整个文件都变成品牌定制。审查实际 diff、用户需求和分支现有差异，不按文件整块归类。

开始实施时简述初步归属。完成需求后、首次提交前，输出简短交付说明：归属及理由、实际改动文件、提交分支、要传播到的品牌、明确远端 ref、验证结果。规则能确定时直接执行，不把这一步变成固定的批准问答。只有未确定的品牌政策或会改变行为的取舍才需用户决定。

用户明确限定某个分支时，以本次限定为准。“提交、推送”承接本次已确定的归属；共享内容默认从 `main` 传播到 `TapModels`、`tokensavy`，品牌定制不扩散到其他品牌。任何推送都不自动授权生产部署、数据库迁移或发布镜像。推送前核对 origin 的 URL 为本仓库及目标分支 CI 的实际副作用；超出本次授权时先完成可审查准备。

三个交付分支的 GitHub 发版 Workflow 仅保留 `workflow_dispatch`。版本发布和在线更新均使用本仓库对应分支/渠道，配置见 `scripts/release/channels.json`：`main/kdan`、`TapModels/tapmodels`、`tokensavy/tokensavy`。普通提交、推送、合并和标签推送不启动 Actions 发版，不创建 Release，也不提高版本号。代理或脚本不得在推送后自动调用 `gh workflow run` 或 dispatch API；只有用户明确要求发版时才执行。人工入口为本仓库 Actions → Manual versioned release → Run workflow，渠道和分支见 `deploy/AUTOMATIC_RELEASE.md`。同步上游时保留此手动触发约束，并运行 `test_auto_release_workflow.py` 防止自动触发器回流。

## 2. 在正确分支形成提交

先核对 `git status`、当前分支、已有暂存、merge/rebase 状态、worktree 占用和目标远端。只暂存本次范围，不使用 `git add .` 混入其他任务。

共享需求的工作分支从 `main` 创建。不要在 TapModels 或 tokensavy 先实现共享逻辑再回搬；若工作目录在品牌分支，应从 main 创建隔离工作分支/worktree，把共享补丁应用并验证后再形成品牌覆盖。不得通过 reset、整文件覆盖或删除其他任务的修改来清场。

共享功能不能包含 TapModels 或 Tokensavy 专属名称、域名、法律主体或部署默认值；使用 `brand.go`、`brand.ts` 和运行时设置等品牌入口。混合需求拆提交，main 先形成完整共享行为，各品牌再形成可审查的品牌覆盖。

## 3. main 改动传播到品牌分支

默认采用普通 merge 保留来源提交 SHA。选择合并来源前必须审查拟合入的完整差异，不能为了一个新功能无意带入未知历史差异。

- 最佳路径：从 main 创建工作分支，完成共享行为和验证后普通 merge 回 main，再普通 merge 到 TapModels、tokensavy。品牌分支只新增品牌覆盖，不复制共享实现。
- 官方更新先普通 merge 到 `main`；main 验证完成后再普通 merge 到 TapModels、tokensavy。`upstream-base` 只作为来源基线，不直接作为品牌发布分支。
- 历史有大量分叉、此次仅需传播一个已审核需求：先明确报告有限范围移植的理由，可用 `cherry-pick -x <来源 SHA>` 传播该提交或依赖完整的提交序列。这会产生不同 SHA，交付表必须记录来源关系，不称作全历史合并。
- 品牌已存在等价改动：核对代码、品牌适配和行为证据后跳过重复应用，记录对应 SHA。没有新的品牌提交时无需空推送。

无需因历史分叉自动停止；继续核实本次可独立交付的变化。待审历史项另行列明，不能仅凭祖先检查、提交计数或 patch-id 判断缺失。品牌适配造成的补丁差异必须通过实际 diff 和相关测试解释。

上游同步仍使用 `deploy/sync-upstream.sh` 的普通 merge 流程；它同步 upstream，不是 main 向 TapModels 传播的替代入口。不要调用旧 `deploy/sync.sh` 的线性 rebase、固定六提交或 force-push 工作流。

## 4. 明确推送目标并逐项验证

推送前 fetch origin 的所需目标分支，再基于最新远端核对快进关系、实际待推提交及工作流差异。依赖 fetch 结果的检查必须在 fetch 完成后执行。不 fetch 或发布旧 erwinlin 目标来完成当前交付。

```bash
git push origin refs/heads/main:refs/heads/main
git push origin refs/heads/TapModels:refs/heads/TapModels
git push origin refs/heads/tokensavy:refs/heads/tokensavy
```

只执行本次归属和授权所需的行。不使用裸 `git push`、`git push origin <短分支名>` 或依赖当前 HEAD 的目标推断。新 checkout 建议设置 `git config --local push.default nothing`，让遗漏 refspec 的推送直接报错。

完整交付可用 origin 的原子推送一次提交三个完整 refspec；逐项推送时记录成功和失败，部分失败保留成功结果，只修复并重试未完成目标。非快进拒绝后 fetch 并审查原因，不强推。

使用 `git ls-remote --heads origin main TapModels tokensavy` 核对本次应交付分支的实际远端 SHA，分别对应各品牌的本地交付提交；三个品牌 SHA 无须相同。不能只读本地 remote-tracking ref 就声称远端成功。

## 5. 验证、修正与完成条件

按实际行为风险验证，不仅看 Git 是否无冲突。首页类改动检查组件行为、多语键完整性、类型及浏览器布局；main 改动传播到 TapModels、tokensavy 后检查各品牌标识和相关回归。相同产物无需无理由重复全套测试。

误提交到 TapModels 但尚未推送时，先保留提交和用户现场，在正确的 main 基线上重建本次提交。已经推送时保留历史，通过可追溯移植或审核后的合并纠正；若要撤销远端提交，应单独确认并优先 revert，不擅自 reset/force-push。

用户明确要求 rebase/压缩重新提交时，可以在已明确的提交范围内执行：先备份原 tip 和包含未提交内容的完整检查点，再重排或合并提交，核对重写前后的文件树。该指令覆盖默认保留历史规则，但不等于允许覆盖所有远端或重写其他任务提交。重写已发布历史后的远端更新须在明确的覆盖授权下使用精确旧 SHA 的 force-with-lease；未获该授权时交付本地新提交，并说明远端仍保留旧历史。

完成报告包含 main、TapModels、tokensavy 本次应交付的源分支、origin 远端 ref、实际 SHA、合并或等价移植关系、验证及失败项。只有本次应交付的所有目标分支已验证后，才称“品牌同步完成”。

## 只读辅助检查

```bash
python3 tools/change_delivery.py
python3 tools/change_delivery.py --staged
python3 tools/change_delivery.py --commit <sha> --scope shared
```

工具列出当前变更、品牌/部署线索、归属待确认项和明确推送命令；默认不把当前分支当作归属。`--scope` 是审查后的归属结论，不是授权参数。工具不提交、不推送、不验证远端，也不代替实际语义审查。
