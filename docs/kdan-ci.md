# KDAN CI 与发布

本仓库 `holeenlu/sub2api` 使用三个交付分支：`main`、`TapModels`、`tokensavy`。共享代码先进入 `main`，验证后普通 merge 到两个品牌分支。上游同步使用 `deploy/sync-upstream.sh`，不向 Wei-Shaw upstream 推送私有代码。

## 持续检查

`.github/workflows/quality-gate.yml` 在三个交付分支的 push 和 pull request 上运行：

- 后端 `go build ./...` 与 `go test -tags=unit ./...`；
- 前端类型检查、ESLint、Vitest；
- `zh-TW` 生成物同步检查；
- 下载包可复现性检查。

这条工作流只做验证，不发版、不部署、不执行数据库迁移。

## 手动发版

`.github/workflows/automatic-release.yml` 只接受 `workflow_dispatch`。普通 push、merge 和 tag push 不创建 Release、不更新镜像 `latest`，也不触发生产部署。人工发版时按分支选择渠道：

- `main` → `main/kdan`；
- `TapModels` → `TapModels/tapmodels`；
- `tokensavy` → `tokensavy/tokensavy`。

发版前必须核对目标分支、构建提交、镜像摘要和健康检查结果。生产升级与数据库迁移仍是独立操作。

## 交付边界

推送目标只限本仓库：

```bash
git push origin refs/heads/main:refs/heads/main
git push origin refs/heads/TapModels:refs/heads/TapModels
git push origin refs/heads/tokensavy:refs/heads/tokensavy
```

不使用旧的 `KDAN` 或公共分支，也不把当前分支的跟踪配置当作推送目标。具体归属和交付检查见 `AGENTS.md`、`docs/CHANGE_DELIVERY.md` 与 `tools/change_delivery.py`。
