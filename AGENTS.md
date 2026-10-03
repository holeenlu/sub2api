# Tokensavy 分支范围

本分支 `tokensavy` 基于 TapModels 创建，专用于 Tokensavy (`tokensavy.ai`)。Tokensavy 品牌变更只落此分支，不传播到 main / TapModels。部署使用 `deploy/tokensavy/README.md`；推送目标仅为 `origin/tokensavy`；手动发版从本仓库 `tokensavy` 分支构建 `ghcr.io/holeenlu/tokensavy`，在线更新只读 `tokensavy/v*` 渠道。不使用下文旧品牌的推送映射。公共功能需求仍须依照下文规则另行归属。

# 需求归属与交付

新增、修改、修复及合并需求，开始时判断 KDAN/TapModels 品牌归属，完成后依据实际 diff 复核。当前检出分支不是归属依据。

执行“提交、推送”前读取 [需求交付规范](docs/CHANGE_DELIVERY.md)。所有改动先落 `main`（KDAN 品牌主分支），验证后普通 merge 到 TapModels；TapModels 品牌定制仅落对应品牌，混合需求拆提交。用户明确限定范围时遵从该限定。

推送映射：`main → origin/main`；`TapModels → origin/TapModels + erwinlin/main`。独立 `KDAN` 和公共分支不再作为交付目标。必须写完整源/目标 refspec，不能默认推送当前分支或依赖跟踪配置。

公共传播优先普通 merge 并保留 SHA；有限范围移植须说明理由并用 `cherry-pick -x` 记录来源。功能等价与历史祖先关系分开核验，不能仅凭 `git cherry` 断言功能缺失。保留品牌配置与其他任务修改。

本次已授权且归属明确的提交、推送直接完成全部目标，不重复询问；生产部署与数据库迁移属于独立范围。逐一核对远端 SHA 后报告完成。
