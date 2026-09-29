# 需求归属与提交、推送规范

适用于新增功能、修复、重构、合并需求及其交付。当前采用 `KDAN` 主开发、`TapModels` 品牌派生、`upstream-base` 官方基线和 `main` 公共兼容快照的结构。目标是先判断改动归属，再选择提交位置和发布目标；当前检出的分支只是工作现场，不能作为归属依据。

## 当前分支结构

所有新增、修改、删除、重构和官方上游整合先在 `KDAN` 完成。`TapModels` 接收 KDAN 的普通 merge，再单独保留品牌覆盖；`main` 只作为公共兼容快照，除非明确要求公共版本，不参与日常集成。`upstream-base` 只保存官方上游引用，不承载本地品牌代码。完整结构见 [分支结构与品牌交付模型](BRANCH_STRUCTURE.md)。

推送目的地不能由跟踪关系推断。TapModels 同时要推送 `origin/TapModels` 和 `erwinlin/main`，后续一律写全源、目标 ref。

`git cherry` 的 `+` 表示未找到等价补丁，可能来自品牌适配、拆分或改写，不能直接解释为功能缺失。此前所谓“4 个历史提交未同步”只应视为待逐项审查的候选项。

## 1. 需求开始时判断，完成后复核

| 归属 | 判定依据 | 首次落地分支 | “提交、推送”的默认交付范围 |
| --- | --- | --- | --- |
| KDAN 主开发 | 所有公共能力、KDAN 功能、上游整合和经过审查的需求 | KDAN | origin/KDAN |
| TapModels 定制 | TapModels 专属名称、域名、资产、商业政策、默认配置、部署、法律主体和繁体覆盖 | TapModels | origin/TapModels、erwinlin/main |
| 公共兼容 | 明确需要公共版本的通用代码快照 | holeen/main | origin/main |
| 混合 | 同一需求同时包含共享逻辑和品牌覆盖 | 先 KDAN，再 TapModels 品牌提交 | KDAN → TapModels 两个远端 |

目录和关键词只能给出线索。例如 HomeView.vue 既可能是公共布局修改，也可能是品牌营销修改；语言包中的品牌名称不能使整个文件都变成品牌定制。审查实际 diff、用户需求和分支现有差异，不按文件整块归类。

开始实施时简述初步归属。完成需求后、首次提交前，输出简短交付说明：归属及理由、实际改动文件、提交分支、要传播到的品牌、明确远端 ref、验证结果。规则能确定时直接执行，不把这一步变成固定的批准问答。只有未确定的品牌政策或会改变行为的取舍才需用户决定。

用户明确限定某个分支/远端时，以本次限定为准。“提交、推送”承接本次已确定的归属；按当前结构，KDAN 已验证内容默认传播到 TapModels 两个远端，公共 `main` 需要单独明确。任何推送都不自动授权生产部署、数据库迁移或发布镜像。推送前核对目标分支 CI 的实际副作用；超出本次授权时先完成可审查准备。

三个交付分支的 GitHub 发版 Workflow 仅保留 `workflow_dispatch`。普通提交、推送、合并和标签推送不启动 Actions 发版，不创建 Release，也不提高版本号。代理或脚本不得在推送后自动调用 `gh workflow run` 或 dispatch API；只有用户明确要求发版时才执行。人工入口为 Actions → Manual versioned release → Run workflow，渠道和分支见 `deploy/AUTOMATIC_RELEASE.md`。同步上游时保留此手动触发约束，并运行 `test_auto_release_workflow.py` 防止自动触发器回流。

## 2. 在正确分支形成提交

先核对 `git status`、当前分支、已有暂存、merge/rebase 状态、worktree 占用和目标远端。只暂存本次范围，不使用 `git add .` 混入其他任务。

工作分支从 `KDAN` 创建。不要在 TapModels 先实现共享逻辑再回搬；若工作目录在 TapModels，应从 KDAN 创建隔离工作分支/worktree，把共享补丁应用并验证后再形成品牌覆盖。不得通过 reset、整文件覆盖或删除其他任务的修改来清场。

共享功能不能包含某个品牌的名称、域名、法律主体或部署默认值；使用 `brand.go`、`brand.ts` 和运行时设置等品牌入口。混合需求拆提交，KDAN 先形成完整行为，TapModels 再形成可审查的品牌覆盖。

## 3. KDAN 改动传播到 TapModels

默认采用普通 merge 保留公共提交 SHA。选择合并来源前必须审查拟合入的完整差异，不能为了一个新功能无意带入未知历史差异。

- 最佳路径：从 KDAN 创建工作分支，完成共享行为和验证后普通 merge 回 KDAN，再普通 merge 到 TapModels。TapModels 只新增品牌覆盖，不复制共享实现。
- 官方更新先普通 merge 到 `KDAN`；KDAN 验证完成后再普通 merge 到 TapModels。`upstream-base` 只作为来源基线，不直接作为品牌发布分支。
- 需要公共兼容版本时，从 KDAN 审查出不含品牌值的公共提交，再普通 merge 或生成 `main` 快照；不得把完整 KDAN 分支覆盖到 `main`。
- 历史有大量分叉、此次仅需传播一个已审核需求：先明确报告有限范围移植的理由，可用 `cherry-pick -x <公共 SHA>` 传播该提交或依赖完整的提交序列。这会产生不同 SHA，交付表必须记录来源关系，不称作全历史合并。
- 品牌已存在等价改动：核对代码、品牌适配和行为证据后跳过重复应用，记录对应 SHA。没有新的品牌提交时无需空推送。

无需因历史分叉自动停止；继续核实本次可独立交付的变化。待审历史项另行列明，不能仅凭祖先检查、提交计数或 patch-id 判断缺失。品牌适配造成的补丁差异必须通过实际 diff 和相关测试解释。

上游同步仍使用 `deploy/sync-upstream.sh` 的普通 merge 流程；它同步 upstream，不是本节所述公共需求向品牌传播的替代入口。不要调用旧 `deploy/sync.sh` 的线性 rebase、固定六提交或 force-push 工作流。

## 4. 明确推送目标并逐项验证

推送前分别 fetch origin、erwinlin，再基于最新远端核对快进关系、实际待推提交及工作流差异。依赖 fetch 结果的检查必须在 fetch 完成后执行。

```bash
git push origin refs/heads/KDAN:refs/heads/KDAN
git push origin refs/heads/TapModels:refs/heads/TapModels
git push erwinlin refs/heads/TapModels:refs/heads/main
# 只有明确交付公共兼容版本时：
git push origin refs/heads/holeen/main:refs/heads/main
```

只执行本次归属和授权所需的行。不使用裸 `git push`、`git push origin <短分支名>` 或依赖当前 HEAD 的目标推断。新 checkout 建议设置 `git config --local push.default nothing`，让遗漏 refspec 的推送直接报错。

两个仓库之间没有原子推送：逐项记录成功和失败，部分失败时保留成功结果，只修复并重试未完成目标。非快进拒绝后 fetch 并审查原因，不强推。

使用 `git ls-remote --heads <remote> <branch>` 核对实际远端 SHA。TapModels 的两个远端应为同一交付提交，或明确记录远端后续已有包含该提交的新变化；不能只读本地 remote-tracking ref 就声称远端成功。

## 5. 验证、修正与完成条件

按实际行为风险验证，不仅看 Git 是否无冲突。本次首页类改动检查组件行为、三语键完整性、类型及浏览器布局；公共改动落到不同品牌上下文后检查品牌标识和相关回归。公共共享的相同产物无需无理由重复全套测试。

误提交到品牌但尚未推送时，先保留提交和用户现场，在正确公共基线上重建本次提交。已经推送时保留历史，通过可追溯移植或审核后的合并纠正；若要撤销远端提交，应单独确认并优先 revert，不擅自 reset/force-push。

用户明确要求 rebase/压缩重新提交时，可以在已明确的提交范围内执行：先备份原 tip 和包含未提交内容的完整检查点，再重排或合并提交，核对重写前后的文件树。该指令覆盖默认保留历史规则，但不等于允许覆盖所有远端或重写其他任务提交。重写已发布历史后的远端更新须在明确的覆盖授权下使用精确旧 SHA 的 force-with-lease；未获该授权时交付本地新提交，并说明远端仍保留旧历史。

完成报告包含 KDAN、TapModels 两个远端及（如授权）公共 `main` 的源分支、远端 ref、实际 SHA、合并或等价移植关系、验证及失败项。只有本次应交付的所有远端已验证后，才称“品牌同步完成”。

## 只读辅助检查

```bash
python3 tools/change_delivery.py
python3 tools/change_delivery.py --staged
python3 tools/change_delivery.py --commit <sha> --scope shared
```

工具列出当前变更、品牌/部署线索、归属待确认项和明确推送命令；默认不把当前分支当作归属。`--scope` 是审查后的归属结论，不是授权参数。工具不提交、不推送、不验证远端，也不代替实际语义审查。
