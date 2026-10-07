# 分支结构与品牌交付模型

本仓库采用“main 作为 KDAN 品牌主分支、TapModels / tokensavy 品牌派生、官方上游独立基线”的结构。所有交付、版本发布和在线更新只使用 `holeenlu/sub2api`。

| 分支 | 作用 | 允许的首次落地 |
| --- | --- | --- |
| `upstream-base` | `Wei-Shaw/sub2api` 官方上游的本地镜像，供审查和普通 merge 使用 | 只同步官方上游 |
| `main` | KDAN 品牌主开发、最终交付、默认分支和发布分支 | 所有新功能、修复、重构、删除和上游整合 |
| `TapModels` | 从 `main` 派生的品牌交付分支 | `main` 已验证内容的普通 merge，以及 TapModels 品牌覆盖 |
| `tokensavy` | 从 `main` 派生的 Tokensavy 品牌交付分支 | `main` 已验证内容的普通 merge，以及 Tokensavy 品牌覆盖 |

不再维护独立的 `KDAN` 分支或公共分支。现有 KDAN 品牌内容已合入 `main`；`main` 现在就是 KDAN 品牌版本。`upstream/main` 仍是官方上游的实时远端跟踪引用，`upstream-base` 则保存经过审查后固定的同步基线；后者只供审查和普通 merge，不承载本地品牌代码。

共享需求先在 `main` 完成并验证。TapModels、tokensavy 再从 `main` 普通 merge，并只提交品牌差异，例如产品名称、域名、Logo、部署服务名、法律文案、繁体覆盖和发布镜像。品牌专属需求只落对应分支；共享业务逻辑、数据库迁移、模型协议、测试和安全修复应直接继承 `main`，不能在品牌分支重写一份等价实现。

品牌默认值集中在 `backend/internal/service/brand.go` 与 `frontend/src/config/brand.ts`。新增可配置品牌内容时，优先扩展这两个入口或运行时设置，避免在业务代码中写死 KDAN/TapModels 名称。部署文件、下载包和语言覆盖属于品牌层，修改时必须在 TapModels 上单独核对。

## 日常流程

1. 从 `main` 创建工作分支，完成需求、测试和审查。
2. 将已验证的提交合并回 `main`，并推送 `origin/main`。
3. 将 `main` 普通 merge 到 `TapModels`、`tokensavy`，保留品牌覆盖并分别验证品牌产物。
4. 官方更新先 fetch 并固定到 `upstream-base`，审查后普通 merge 到 `main`，再传播到两个品牌分支。

## 推送目标

推送始终使用完整 refspec：

```bash
git push origin refs/heads/main:refs/heads/main
git push origin refs/heads/TapModels:refs/heads/TapModels
git push origin refs/heads/tokensavy:refs/heads/tokensavy
```

不使用裸 `git push`、跟踪分支推断或 force push。不再向 `erwinlin/main` 或其他仓库推送/发版；历史远端可留作只读参考，不作为交付目标。普通 merge 保留来源提交和审查关系；只有明确的历史重写授权才允许使用 `--force-with-lease`。

## 迁移检查点

- GitHub 默认分支为 `main`，交付分支为 `main`、`TapModels`、`tokensavy`；`upstream-base` 仅作本地基线，不作额外代码交付目标。
- `scripts/release/channels.json` 定义本仓库的三个渠道：`main/kdan`、`TapModels/tapmodels`、`tokensavy/tokensavy`；普通推送不触发发版。
- CI、发布、更新检查和文档中的分支说明都必须以此表为准。
- 切换后的第一次上游同步和第一次 TapModels 传播必须记录源 SHA、目标 SHA 及验证结果。
