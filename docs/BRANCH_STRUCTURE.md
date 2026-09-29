# 分支结构与品牌交付模型

本仓库采用“KDAN 主开发、TapModels 品牌派生、官方上游独立基线”的结构。

| 分支 | 作用 | 允许的首次落地 |
| --- | --- | --- |
| `upstream-base` | `Wei-Shaw/sub2api` 官方上游的本地镜像，供审查和普通 merge 使用 | 只同步官方上游 |
| `KDAN` | 主开发分支、GitHub 默认分支和 KDAN 最终交付分支 | 所有新功能、修复、重构、删除和上游整合 |
| `TapModels` | 从 KDAN 派生的品牌交付分支 | KDAN 已验证内容的普通 merge，以及 TapModels 品牌覆盖 |
| `main` | 兼容保留的公共代码快照和公共发布渠道 | 只有明确要求公共版本时才更新 |

`main` 不再是日常需求的首个落地点，也不作为 KDAN 与 TapModels 的同步中枢。保留它是为了兼容已有公共发布、历史链接和上游基线关系；它的内容不能反向覆盖 KDAN。

公共能力先在 KDAN 完成并验证，然后普通 merge 到 TapModels。TapModels 只提交品牌差异，例如产品名称、域名、Logo、部署服务名、法律文案、繁体覆盖和发布镜像。业务逻辑、数据库迁移、模型协议、测试和安全修复应直接继承 KDAN，不能在 TapModels 重写一份等价实现。

品牌默认值集中在 `backend/internal/service/brand.go` 与 `frontend/src/config/brand.ts`。新增可配置品牌内容时，优先扩展这两个入口或运行时设置，避免在业务代码中写死 KDAN/TapModels 名称。部署文件、下载包和语言覆盖属于品牌层，修改时必须在 TapModels 上单独核对。

## 日常流程

1. 从 `KDAN` 创建工作分支，完成需求、测试和审查。
2. 按 `shared`、`kdan`、`tapmodels` 标记提交归属；公共代码不包含品牌值。
3. 将已验证的公共提交普通 merge 到 `TapModels`。
4. 在 TapModels 单独提交品牌覆盖，并验证品牌入口、文档、部署文件和下载产物。
5. 官方更新先 fetch 到 `upstream-base`，审查后普通 merge 到 `KDAN`，再传播到 TapModels。
6. 仅在明确要求公共版本时，把经过审查的 KDAN 公共内容同步到 `main`。

## 推送目标

推送始终使用完整 refspec：

```bash
git push origin refs/heads/KDAN:refs/heads/KDAN
git push origin refs/heads/TapModels:refs/heads/TapModels
git push erwinlin refs/heads/TapModels:refs/heads/main
```

公共兼容分支需要单独授权时再执行：

```bash
git push origin refs/heads/holeen/main:refs/heads/main
```

不使用裸 `git push`、跟踪分支推断或 force push。普通 merge 保留来源提交和审查关系；只有明确的历史重写授权才允许使用 `--force-with-lease`。

## 迁移检查点

- GitHub 默认分支设为 `KDAN`，保留 `main`、`TapModels` 和 `upstream-base`。
- `scripts/release/channels.json` 继续分别定义 KDAN、TapModels 和公共兼容发布渠道；普通推送不触发发版。
- CI、发布、更新检查和文档中的分支说明都必须以此表为准。
- 切换后的第一次上游同步和第一次 TapModels 传播必须记录源 SHA、目标 SHA 及验证结果。
