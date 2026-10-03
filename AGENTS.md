# 需求归属与交付

新增、修改、修复及合并需求，开始时判断 KDAN/TapModels/Tokensavy 品牌归属，完成后依据实际 diff 复核。当前检出分支不是归属依据。

执行“提交、推送”前读取 main 的最新 [需求交付规范](docs/CHANGE_DELIVERY.md)，旧 worktree 副本不能覆盖现行规则。共享改动先落 `main`（KDAN 品牌主分支），验证后普通 merge 到 `TapModels`、`tokensavy`；品牌定制仅落对应品牌，混合需求拆提交。用户明确限定范围时遵从该限定。

推送映射仅限本仓库 `holeenlu/sub2api`：`main → origin/main`；`TapModels → origin/TapModels`；`tokensavy → origin/tokensavy`（大小写严格一致）。不再向 `erwinlin/main` 或其他仓库推送、发版；独立 `KDAN` 和公共分支不再作为交付目标。必须写完整源/目标 refspec，不能默认推送当前分支或依赖跟踪配置。

版本发布和在线更新均使用本仓库对应分支/渠道：`main/kdan`、`TapModels/tapmodels`、`tokensavy/tokensavy`。渠道配置见 `scripts/release/channels.json`。仅人工 Run workflow 发版；普通提交、推送不发版、不部署。历史说明及旧检查点不能恢复旧推送目标。

公共传播优先普通 merge 并保留 SHA；有限范围移植须说明理由并用 `cherry-pick -x` 记录来源。功能等价与历史祖先关系分开核验，不能仅凭 `git cherry` 断言功能缺失。保留品牌配置与其他任务修改。

本次已授权且归属明确的提交、推送直接完成全部目标，不重复询问；生产部署与数据库迁移属于独立范围。逐一核对远端 SHA 后报告完成。
