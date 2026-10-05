# 私有仓库与公开上游同步

`holeenlu/sub2api` 保留独立仓库，并脱离 GitHub Fork 网络。
官方代码源仍为 `Wei-Shaw/sub2api`；保留的 Git 历史允许继续 fetch 与普通 merge，
不依赖 GitHub 的 Fork 同步按钮，也不需要重新加入 Fork 网络。

## 分支边界

| 本地分支 | 发布目标 | 用途 |
| --- | --- | --- |
| `upstream-base` | 不作代码交付目标，来源为 `upstream/main` | 官方上游本地基线，只供审查和普通 merge |
| `main` | `origin/main` | KDAN 品牌主开发、默认分支和最终交付 |
| `TapModels` | `origin/TapModels` | 从 main 派生的品牌交付 |
| `tokensavy` | `origin/tokensavy` | 从 main 派生的 Tokensavy 品牌交付 |

不再维护独立 `KDAN` 或公共分支。现行入口是 `deploy/sync-upstream.sh`，
调用个人 `sync-upstream` 技能；官方更新先进入 `main`，再传播到 TapModels、tokensavy。仅向本仓库 origin 的三个交付分支推送，不再向 erwinlin 或其他仓库交付。
检查点保存在 `.release/upstream-sync/<id>/state.json`。技能及入口属于本地文件，
新工作站须另行配置；不能只靠 clone 恢复。旧 `deploy/sync.sh` 已退役。
推送与部署仍需分别批准，不向公开 upstream 推送私有代码。

## 同步来源范围

- `Wei-Shaw/sub2api` 的 `upstream/main` 是完整普通 merge 的上游；同步前核对 remote URL，不能换成 fork 后继续整分支合并。
- 共享新增和修复先落 `main`，再按 [需求交付规范](CHANGE_DELIVERY.md) 普通 merge 到 TapModels、tokensavy，保留品牌及繁体中文。

此规则限制后续同步范围，不自动删除现有功能，也不改写既有历史。早先对两个 fork 的全量同步授权不能作为后续全量合并依据。

## 私有访问

私有源码必须通过已认证的 Git 连接获取；匿名 raw GitHub 下载脚本不再适用。
不要把 Token 放入 URL、shell 历史、日志或镜像构建参数。
拉取私有 GHCR 镜像前须登录，凭据须具有对应包的读取权限（`read:packages`）。
CI 使用当前仓库的 `GITHUB_TOKEN` 访问源码、镜像和追踪 issue。

`upstream/main` 仍是官方上游的实时远端跟踪引用，可以直接 fetch、查看和比较。
`upstream-base` 是经过审查后固定的本地基线，用来记录本轮同步的确定输入；它不会取代
`upstream/main`，也不会自动随远端移动。每轮同步先获取最新 `upstream/main`，审查后再将
选定 SHA 固定到 `upstream-base`，最后把这个固定 SHA 普通 merge 到 `main`。

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

prepare 先把固定上游 SHA 普通 merge 到 `main`，保留原提交并运行验证，
不执行历史重写或自动推送。完成检查点后，再按需求交付规范把验证后的 `main` 传播到 TapModels、tokensavy。

出现冲突后按检查点处理。验证完成并获得该次推送授权后，再使用检查点 ID 执行 publish；
数据库迁移与部署另行验收。

## 默认分支自动化

`project-ci.yml` 检查默认 `main` 的推送与 PR。
`upstream-sync-watch.yml` 每日 UTC 01:00（北京时间 09:00）比较 main 与公开
上游，维护一张追踪 issue，不合并、不推送、不部署。

这些文件必须提交并发布到默认分支 `origin/main`，定时任务和手动触发才会注册。
品牌自己的 CI/构建仍按品牌发布分支触发。转为私有或独立仓库不能证明 Actions
已启用，也不能证明继承的上游工作流已停用；发布后须核对实际仓库设置。

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

本地 `deploy/release.sh` 按当前检出分支发布，也可用 `--branch` 显式指定。
生产站使用 `main`，因此标准命令为
`./deploy/release.sh release --branch main --approve-push --approve-deploy`，
发布目标是 `origin/main`。发布阶段只验证目标分支已经包含
`upstream/main`，不会 rebase 或改写提交；
上游同步必须先通过普通 Git Merge 完成。
品牌发布使用各自 CI 通道；不要把 TapModels 品牌覆盖直接写入 main。

### 已移除协议的同步边界

Excel/BPS、独立 `openai_bps` 平台及同批附带的 OAuth 初始化模板、额外别名模式、独立成本倍率均已移除，后续同步不得恢复协议、账号开关、默认模板、图片中转或自动恢复任务。共享 RPM、原生 WS、Codex 打票和降智检测按各自能力审查，不能以旧 BPS 依赖名义重新导入已删除代码。清除与数据升级说明见 [BPS 移除说明](BPS_REMOVAL.md)。
