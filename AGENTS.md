# Tokensavy 分支范围

本分支 `tokensavy` 基于 TapModels 创建，专用于 Tokensavy (`tokensavy.ai`)。Tokensavy 品牌变更只落此分支，不传播到 main / TapModels。部署使用 `deploy/tokensavy/README.md`；推送目标仅为 `origin/tokensavy`；手动发版从本仓库 `tokensavy` 分支构建 `ghcr.io/holeenlu/tokensavy`，在线更新只读 `tokensavy/v*` 渠道。不使用下文旧品牌的推送映射。公共功能需求仍须依照下文规则另行归属。

# 需求归属与交付

新增、修改、修复及合并需求，开始时判断 KDAN/TapModels/Tokensavy 品牌归属，完成后依据实际 diff 复核。当前检出分支不是归属依据。

执行“提交、推送”前读取 main 的最新 [需求交付规范](docs/CHANGE_DELIVERY.md)，旧 worktree 副本不能覆盖现行规则。共享改动先落 `main`（KDAN 品牌主分支），验证后普通 merge 到 `TapModels`、`tokensavy`；品牌定制仅落对应品牌，混合需求拆提交。用户明确限定范围时遵从该限定。

推送映射仅限本仓库 `holeenlu/sub2api`：`main → origin/main`；`TapModels → origin/TapModels`；`tokensavy → origin/tokensavy`（大小写严格一致）。不再向 `erwinlin/main` 或其他仓库推送、发版；独立 `KDAN` 和公共分支不再作为交付目标。必须写完整源/目标 refspec，不能默认推送当前分支或依赖跟踪配置。

版本发布和在线更新均使用本仓库对应分支/渠道：`main/kdan`、`TapModels/tapmodels`、`tokensavy/tokensavy`。渠道配置见 `scripts/release/channels.json`。仅人工 Run workflow 发版；普通提交、推送不发版、不部署。历史说明及旧检查点不能恢复旧推送目标。

公共传播优先普通 merge 并保留 SHA；有限范围移植须说明理由并用 `cherry-pick -x` 记录来源。功能等价与历史祖先关系分开核验，不能仅凭 `git cherry` 断言功能缺失。保留品牌配置与其他任务修改。

本次已授权且归属明确的提交、推送直接完成全部目标，不重复询问；生产部署与数据库迁移属于独立范围。逐一核对远端 SHA 后报告完成。

# 原生能力边界（2026-10-05）

网关、Codex、WebSocket、调度和运维能力以 Wei-Shaw 官方实现为基准。已移除的 ranxi/sub4api 打票、Cookie/票据绑定、额外准入、BPS、独立目录治理及运营扩展不得在后续同步时重新导入。已确认保留的功能包括现有降智检测及 Anthropic Fable 独立阈值；检测复用原调度器和原生计费网关。可信客户端 IP、图片下载出口保护、WS 客户端限制与响应归属、Grok 语音用户并发和音色归属检查必须保留；品牌、发布渠道、其他安全修复及历史迁移保护继续保留。新的保留例外须来自用户明确需求。当前边界见 `docs/FORK_FEATURE_RETIREMENT.md`。

# Project implementation standards

新增或修改代码遵循以下原则：

- 优先修改现有逻辑；新增入口、字段或辅助层时，必须说明现有实现为什么无法承载需求。
- 替换逻辑时同步清理旧路径；已经废弃的行为不默认永久兼容。
- 统一规则的归属，避免后端、前端和脚本分别维护不断扩张的重复规则。
- 测试覆盖独立风险，优先扩展已有测试，避免为同一行为重复铺设大量用例。
- 交付时分别报告业务代码与测试的增删，以及新增接口、配置项和执行路径数量。
