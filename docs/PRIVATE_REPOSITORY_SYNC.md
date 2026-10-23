# 私有仓库与公开上游同步

`holeenlu/sub2api` 保留独立仓库，并脱离 GitHub Fork 网络。
官方代码源仍为 `Wei-Shaw/sub2api`；保留的官方 Git 历史允许继续 fetch 与三方整合，
不依赖 GitHub 的 Fork 同步按钮，也不需要重新加入 Fork 网络。

## 分支边界与持续同步

自 2026-10-22 起，main、TapModels、tokensavy 各以已核对的最新官方 SHA 为唯一父节点，只保留一笔完整定制提交。共享逻辑先在 main 审查，再通过三方文件树整合到品牌；正式结果不保留普通 merge 或多个功能提交。详见 [分支结构](BRANCH_STRUCTURE.md) 与 [交付规则](CHANGE_DELIVERY.md#单一定制提交2026-10-22)。

`upstream-base` 只保存已审查官方基线，代码仅交付 holeenlu/sub2api 的三个品牌分支。选择性来源范围沿用现行功能需求，不整体合入 fork。保留其他任务、已发布标签及 Release；推送、发版与部署分别受各自授权约束。

## 私有访问

私有源码必须通过已认证的 Git 连接获取；匿名 raw GitHub 下载脚本不再适用。
不要把 Token 放入 URL、shell 历史、日志或镜像构建参数。
拉取私有 GHCR 镜像前须登录，凭据须具有对应包的读取权限（`read:packages`）。
CI 使用当前仓库的 `GITHUB_TOKEN` 访问源码、镜像和追踪 issue。

`upstream/main` 仍是官方上游的实时远端跟踪引用，可以直接 fetch、查看和比较。
`upstream-base` 是经过审查后固定的本地基线，用来记录本轮同步的确定输入；它不会取代
`upstream/main`，也不会自动随远端移动。每轮同步先获取最新 `upstream/main`，审查后再将
选定 SHA 固定到 `upstream-base`，最后以这个固定 SHA 为唯一父节点构建完整定制提交。

官方 upstream 可以直接获取：

```bash
git fetch --no-tags upstream '+refs/heads/main:refs/remotes/upstream/main' '+refs/tags/v*:refs/tags/v*'
git merge-base main upstream-base
```

fetch 只更新引用，不会自动更新应用代码。在维护者整合工作区执行：

```bash
./deploy/sync-upstream.sh prepare --branches main
./deploy/sync-upstream.sh status
```

以上入口只在隔离候选中生成三方合并结果并验证，不能直接发布其 merge 历史。按交付规则审查冲突、整理累计 Release Note、构建一笔定制提交和品牌预期树后，使用精确旧 SHA 的 lease 发布最终 refs。`publish` 以及 `run --approve-push` 不再用于旧检查点；新工作站需要另行配置本机技能。

## 工作流触发

仓库所有 GitHub Workflows（包括质量、上游观察及发版）仅允许 `workflow_dispatch`。普通 push/PR 不自动构建、同步、发版或部署；历史定时触发说明不再有效。

## 版本语义

应用版本代表本仓库构建；`upstream_version` 代表其公开 Sub2API 基线，不代表
Fork 关系、更新源或自动替换为上游二进制的许可。该字段保留为接口兼容信息，
不再占用管理员版本弹层的第二行。

版本弹层第二行显示“编译提交 [abcdef012]”，来自当前二进制的 `main.Commit`，
通过 `build_commit` 字段传递。默认显示前 9 位，悬停显示完整提交号，不跳转私有仓库。
关闭在线检查仍显示；缺失、unknown 或非有效十六进制提交号时隐藏。
缓存中的 release 信息不能覆盖当前构建身份。旧构建只有 7 位时保留已有短 SHA。

Docker CI 从 merge-base 最近的上游 `v*` tag 推导并注入 `main.UpstreamVersion`。
未注入时才回退到 embedded VERSION。本地发布构建也尝试注入上游 tag。
在线检查仍访问配置的私有 release 仓库，需要相应运行时凭据，不承担代码同步。

版本发布使用现有人工渠道工作流。累计定制提交说明不等于某次版本相对上一版本的增量发布说明，也不会自动修改已有 Release。

### 已移除协议的同步边界

2026-10-05 的退役基线继续适用于独立 `openai_bps` 平台、OAuth 初始化模板、额外别名模式、独立成本倍率、Codex 打票、Cookie 绑定、额外准入及运营扩展。保留的降智检测复用原生网关；不能以旧 BPS 依赖名义重新导入这些能力。

2026-10-12 用户已明确恢复 BPS 核心、手动 Excel 授权、工具/图片、显式 403 恢复及客户端 WS 桥接。这是上述退役基线的明确例外；按 [当前需求](bps/REQUIREMENTS.md)、[来源清单](bps/integration-state.json)与[适配决定](bps/upstream-changes.md)持续比较并有限移植，不整分支合入 ranxi。自动登录 Worker、Mihomo、自动质量运营和其他退役能力仍不恢复。当前完整边界见 [原生恢复说明](FORK_FEATURE_RETIREMENT.md)。
