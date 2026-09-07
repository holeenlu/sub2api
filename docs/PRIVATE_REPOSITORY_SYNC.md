# 私有仓库与公开上游同步

`holeenlu/sub2api` 已转为私有独立仓库，并脱离 GitHub Fork 网络。
公开代码源仍为 `Wei-Shaw/sub2api`；保留的 Git 历史允许继续 fetch/rebase，
不依赖 GitHub 的 Fork 同步按钮，也不需要重新加入 Fork 网络。

## 分支边界

| 本地分支 | 发布目标 | 用途 |
| --- | --- | --- |
| `main` | 跟踪 `upstream/main` | 公开上游基线 |
| `holeen/main` | `origin/main` | 公共功能集成，不包含品牌提交 |
| 品牌分支 | `origin/<brand>`，可另有专属远端 | 品牌独有改动 |

禁止把品牌分支覆盖到 `origin/main`。本地同步工具从
`.release/brand-<brand>.json` 读取公共基线和品牌发布远端。
同步工具及清单属于本地文件，新工作站须另行配置；不能只靠 clone 恢复。
推送与部署仍需分别批准，不向公开 upstream 推送私有代码。

## 私有访问

私有源码必须通过已认证的 Git 连接获取；匿名 raw GitHub 下载脚本不再适用。
不要把 Token 放入 URL、shell 历史、日志或镜像构建参数。
拉取私有 GHCR 镜像前须登录，凭据须具有对应包的读取权限（`read:packages`）。
CI 使用当前仓库的 `GITHUB_TOKEN` 访问源码、镜像和追踪 issue。

公开 upstream 可以直接获取：

```bash
git fetch --no-tags upstream '+refs/heads/main:refs/remotes/upstream/main' '+refs/tags/v*:refs/tags/v*'
git merge-base holeen/main upstream/main
```

fetch 只更新引用，不会自动更新应用代码。先将公共集成分支 rebase 到已审查的
上游基线并验证，再将各品牌 rebase 到新的公共层。数据库迁移与部署另行验收。

## 默认分支自动化

`project-ci.yml` 检查私有默认 `main` 的推送与 PR。
`upstream-sync-watch.yml` 每日 UTC 01:00（北京时间 09:00）比较公共层与公开
上游，维护一张追踪 issue，不合并、不推送、不部署。

这些文件必须提交并发布到 `origin/main`，定时任务和手动触发才会注册。
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
生产站使用 `KDAN`，因此标准命令为
`./deploy/release.sh release --branch KDAN --approve-push --approve-deploy`，
发布目标是 `origin/KDAN`。发布阶段只验证目标分支已经包含
`upstream/main`，不会 rebase、改写提交或把品牌分支推送到 `origin/main`；
上游同步必须先通过普通 Git Merge 完成。
品牌发布使用各自 CI 通道；不要为了绕过限制把品牌覆盖到公共分支。
