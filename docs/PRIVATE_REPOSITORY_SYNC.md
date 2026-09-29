# 私有仓库与公开上游同步

`holeenlu/sub2api` 已转为私有独立仓库，并脱离 GitHub Fork 网络。
公开代码源仍为 `Wei-Shaw/sub2api`；保留的 Git 历史允许继续 fetch 与普通 merge，
不依赖 GitHub 的 Fork 同步按钮，也不需要重新加入 Fork 网络。

## 分支边界

| 本地分支 | 发布目标 | 用途 |
| --- | --- | --- |
| `upstream-base` | `origin/upstream-base`，来源为 `upstream/main` | 官方上游基线，只供审查和普通 merge |
| `KDAN` | `origin/KDAN` | 主开发、默认分支和 KDAN 最终交付 |
| `TapModels` | `origin/TapModels`、`erwinlin/main` | KDAN 派生的品牌交付 |
| `holeen/main` | `origin/main` | 公共兼容快照，不作为日常集成中枢 |

禁止把品牌分支覆盖到 `origin/main`。现行入口是 `deploy/sync-upstream.sh`，
调用个人 `sync-upstream` 技能；官方更新先进入 `KDAN`，再传播到 TapModels。
检查点保存在 `.release/upstream-sync/<id>/state.json`。技能及入口属于本地文件，
新工作站须另行配置；不能只靠 clone 恢复。旧 `deploy/sync.sh` 已退役。
推送与部署仍需分别批准，不向公开 upstream 推送私有代码。

## 同步来源范围

- `Wei-Shaw/sub2api` 的 `upstream/main` 是完整普通 merge 的上游；同步前核对 remote URL，不能换成 fork 后继续整分支合并。
- BPS 仅从 `ranxi2001/sub2api` 审查并同步 OpenAI OAuth Excel / BPS 相关功能、修复和严格必要的依赖。sub4api 独立 BPS 平台已移除，后续不得通过同步恢复。禁止整分支、整版本或整 tag 合入，也不能通过共享文件或依赖带入无关的打票、Mihomo、账号质量、部署等功能。
- 从已有 Excel / BPS 整合记录的来源 SHA 审查到固定源 tip，按实际 diff 和调用依赖判断范围，记录采用与排除的来源提交；不能仅按提交标题或文件名筛选。已存在等价行为时不重复移植。
- 完整属于 Excel / BPS 的提交可审查后使用 `cherry-pick -x`；混合提交仅适配必要代码块，在提交或整合记录中注明源 SHA 和排除内容。依赖无法独立分离时，先说明取舍并让用户选择。
- 所有新增和修复先落 `KDAN`，再按 [需求交付规范](CHANGE_DELIVERY.md) 普通 merge 到 TapModels，保留品牌及繁体中文。需要公共版本时才从 KDAN 生成 `holeen/main` 快照。

此规则限制后续同步范围，不自动删除现有功能，也不改写既有历史。早先对两个 fork 的全量同步授权不能作为后续全量合并依据。

## 私有访问

私有源码必须通过已认证的 Git 连接获取；匿名 raw GitHub 下载脚本不再适用。
不要把 Token 放入 URL、shell 历史、日志或镜像构建参数。
拉取私有 GHCR 镜像前须登录，凭据须具有对应包的读取权限（`read:packages`）。
CI 使用当前仓库的 `GITHUB_TOKEN` 访问源码、镜像和追踪 issue。

公开 upstream 可以直接获取：

```bash
git fetch --no-tags upstream '+refs/heads/main:refs/remotes/upstream/main' '+refs/tags/v*:refs/tags/v*'
git merge-base KDAN upstream-base
```

fetch 只更新引用，不会自动更新应用代码。在维护者整合工作区执行：

```bash
./deploy/sync-upstream.sh prepare --branches kdan
./deploy/sync-upstream.sh status
```

prepare 先把固定上游 SHA 普通 merge 到 KDAN，保留原提交并运行验证，
不执行历史重写或自动推送。KDAN 检查点完成后，再为 TapModels 建立独立检查点：

```bash
./deploy/sync-upstream.sh prepare --branches tapmodels
./deploy/sync-upstream.sh status
```

每次 prepare 都会完成所选分支的合并和验证并回到原工作分支；出现冲突后按对应检查点处理。
公共 KDAN 提交向 TapModels 的传播仍按 [需求交付规范](CHANGE_DELIVERY.md) 审查后执行，
不能用 TapModels 的上游检查点替代。两个检查点都验证完成并获得该次推送授权后，
再分别使用检查点 ID 执行 publish；公共 `main` 不会因上游或品牌同步自动更新，
只有明确需要公共兼容版本时才单独生成快照。
数据库迁移与部署另行验收。

## 默认分支自动化

`project-ci.yml` 检查私有默认 `KDAN` 的推送与 PR。
`upstream-sync-watch.yml` 每日 UTC 01:00（北京时间 09:00）比较公共层与公开
上游，维护一张追踪 issue，不合并、不推送、不部署。

这些文件必须提交并发布到默认分支 `origin/KDAN`，定时任务和手动触发才会注册。
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

### Excel / BPS 关联依赖的判定

除协议专属文件外，影响 Excel / BPS 实际请求的 OpenAI OAuth RPM、共享调度、重试/错误分类和同账号原生回退，属于可审查的必要依赖；不能仅按提交标题或文件名漏掉这些内容。这些控制仅用于保留的 OpenAI OAuth 路径，独立 `openai_bps` 平台已退役。

RPM 应覆盖实际发送边界：主请求、重试、工具修复及已批准计入的附件上传；本地校验未通过、未发送和附件缓存命中不计数。保持并发原子性、父子账号共享、限流与上游错误区分，以及未配置限额账号在计数服务故障时的正常候选资格。RPM 不等于 TPM。

同步记录应区分源代码已有行为与本地补齐、已提交功能与未提交草稿，并记录最后审查的源 SHA 和实际纳入/排除的提交。具体工作流见已安装 `sync-upstream` 的 `references/excel-bps.md`；公共先落地、普通 merge 传播品牌，推送与发版另按本轮授权执行。
