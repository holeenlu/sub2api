# 需求归属与交付

新增、修改、修复及合并需求，开始时判断公共/品牌归属，完成后依据实际 diff 复核。当前检出分支不是归属依据。

执行“提交、推送”前读取 [需求交付规范](docs/CHANGE_DELIVERY.md)。公共改动先落 `holeen/main`，默认传播至 KDAN、TapModels；品牌定制仅落对应品牌，混合需求拆提交。用户明确限定范围时遵从该限定。

推送映射：`holeen/main → origin/main`；`KDAN → origin/KDAN`；`TapModels → origin/TapModels + erwinlin/main`。必须写完整源/目标 refspec，不能默认推送当前分支或依赖跟踪配置。

公共传播优先普通 merge 并保留 SHA；有限范围移植须说明理由并用 `cherry-pick -x` 记录来源。功能等价与历史祖先关系分开核验，不能仅凭 `git cherry` 断言功能缺失。保留品牌配置与其他任务修改。

本次已授权且归属明确的提交、推送直接完成全部目标，不重复询问；生产部署与数据库迁移属于独立范围。逐一核对远端 SHA 后报告完成。
