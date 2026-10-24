# 2026-10-14 New API 归并验证

| 检查 | 结果 |
| --- | --- |
| 状态 | 2026-10-14 本轮归并检查全部通过 |
| 源码一致性 | 81/81 文件与原验收 SHA-256 相同；每文件仅一个功能提交；1435 项对象清单核对通过 |
| 主线后端 | 全量 unit：60 个含测试包；go build ./...、go vet -tags=unit ./... 通过 |
| 前端 | 8 文件 95 测试、typecheck、改动生产文件 ESLint 通过 |
| 品牌后端 | TapModels 和 tokensavy：config/server 单测与全后端 build 均通过 |
| 增量 lint | 相对旧重建 c65e3de3551f 的实际改动，0 新增问题 |
| 品牌与旧需求 | 两品牌原覆盖差异保持；00/01/04/05/09/11/12/13/14 的功能差异保持 |
| 提交结构 | 主线 19 个；各品牌在主线上追加 5 个覆盖提交 |
| 验证范围 | 本次未重复定向 race、数据库 integration、Vite build 或 96 格基准；保留同一源码此前的通过证据 |
| 发布边界 | 仅本地重建分支归并；未推送、未替换正式分支、未发版部署或生产迁移 |

81 个源码/测试/配置文件与原验收清单逐项一致。原工作区、正式分支和远端均保留，当前本地重建 SHA 以检查点目录 result.json 为准。原实施验证与本次归并检查分别记录，不宣称重跑未执行的检查。新检查日志位于 new-api-fold-validation/。

---

# 本轮验证记录

本页仅记录 2026-10-13 在重建工作区实际运行的检查。历史 docs 中原有通过结果不作为本轮新证据。最终现有源码与工作区检查点逐 mode/blob 相同。

## 实际结果

| 检查 | 结果 | 证据 |
| --- | --- | --- |
| backend-unit | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/backend-unit.log |
| server-build | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/server-build.log |
| frontend-unit | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/frontend-unit.log |
| frontend-typecheck | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/frontend-typecheck.log |
| frontend-build | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/frontend-build.log |
| manual-release | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/manual-release.log |
| release-matrix | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/release-matrix.log |
| docs-downloads | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/docs-downloads.log |
| traditional-chinese | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/traditional-chinese.log |
| new-document-whitespace | 通过 | /Users/luhonglin/Git/sub2api-history-20261013-v0216-210726/new-document-whitespace.log |

后端全量 unit 有 60 个包返回 ok；前端 394 个测试文件、3064 项测试通过。Go 服务端构建、Vue 类型检查和 Vite 生产构建通过；Vite 保留已有 chunk size 提示。

## 命令和环境

实际命令、工作目录、退出码、耗时见本次审计目录的 checks-*.json。Go 1.27.0、Node 24.19.0；使用原工作区已有前端依赖的 CLI，未改 package/lock。最初 pnpm 自动安装校验拒绝跨 worktree 的 node_modules 符号链接，改为直接调用已有 Vitest/vue-tsc/Vite；初始系统/内置 Python 缺 PyYAML，按仓库 requirements-release.txt 的 PyYAML 6.0.3 安装到独立审计目录后重跑发布测试通过。初始环境错误日志另保留为 *.initial-environment.log。

## 完整性与范围

- 1373 个相对官方基线的变更路径唯一归属；源与目标 blob/mode 逐项登记。
- main 现有文件与 acf71dbc6c3f1ffe8ab0200ed8b7499c1b7d0286 完全一致，额外文件仅本次需求归档。
- 既有 migrations、Ent、测试、资源、依赖和构建配置未重写；SQL 没有在生产运行。
- 原 Git 历史 bundle 验证通过；所有原分支和工作区检查点保留。
- TapModels/tokensavy 的最终业务文件树已逐 mode/blob 校验通过，仅继承两项共享规则并新增本次归档；最终引用见审计目录 DELIVERY.md 和 final-verification.json。

## 不作替代证明的部分

本次未重新做真实 PostgreSQL/Redis 迁移/多实例、实际支付退款、真实 BPS/Excel grant/图片 relay 或线上长连接验收；未跑全仓 race。单元测试、构建和对象树一致性不替代这些环境验收。不触发 Release、生产部署或数据库迁移，也不覆盖远端历史。

## 两品牌实际专项验证

| 品牌 | 后端 | 前端 | 其他 |
| --- | --- | --- | --- |
| TapModels | 运行品牌/配置/日志/setup/web/server 与更新服务通过 | 6 个文件、53 项通过 | 类型、下载、繁体与手动发布通过 |
| tokensavy | 运行品牌/配置/日志/setup/web/server 与更新服务通过 | 6 个文件、53 项通过 | 类型、下载、繁体与手动发布通过 |

日志与命令见审计目录 checks-TapModels.json、checks-tokensavy.json。完成上述检查后只修订本归档的分类和验证文档；最终源码对象再次比对相同，因此没有无理由重复整套运行测试。
