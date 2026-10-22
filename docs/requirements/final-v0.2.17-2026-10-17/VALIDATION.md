# 本轮验证记录

日期：2026-10-17。测试输入为固定的最终 main 快照及下述两处测试断言修正；品牌覆盖按完整 blob / mode 核对。归档文档不改变运行代码。

| 检查 | 结果 | 用时（秒） |
| --- | --- | ---: |
| backend-unit | PASS | 14.08 |
| backend-build | PASS | 11.71 |
| frontend-typecheck | PASS | 14.5 |
| frontend-unit | PASS | 43.22 |
| frontend-locales | PASS | 0.27 |
| frontend-build | PASS | 16.85 |
| vendor-build | PASS | 17.14 |
| manual-workflows | PASS | 4.64 |
| release-matrix | PASS | 0.48 |
| generated-downloads | PASS | 0.07 |
| vendor-prototype | PASS | 0.08 |

前端单元测试为 413 个文件、3255 个测试通过；供应商层级原型模型为 12 个测试通过。后端完整 unit 套件、后端构建、前端类型、主站/供应商门户构建、繁体生成、下载产物、手动工作流和发布矩阵均已通过。后端最终复跑复用了 Go 的成功包缓存。

## 命令与环境

各命令的真实日志保存在仓库外备份目录。以下保留可核对的原始命令；本机绝对路径仅用于说明此次工具与 overlay 的位置。

- backend-unit: `go test -overlay=/Users/luhonglin/Git/sub2api-rebuild-v0217-20261017-113657/go-overlay.json -tags=unit ./...`
- backend-build: `go build ./...`
- frontend-typecheck: `node node_modules/vue-tsc/bin/vue-tsc.js --noEmit`
- frontend-unit: `node node_modules/vitest/vitest.mjs run`
- frontend-locales: `node tools/zh-tw/gen-locale.mjs --check`
- frontend-build: `node node_modules/vite/bin/vite.js build`
- vendor-build: `node node_modules/vite/bin/vite.js build --mode production --outDir dist-vendor`
- manual-workflows: `/Users/luhonglin/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 -m unittest discover -s deploy/tests -p 'test_auto_release*.py'`
- release-matrix: `/Users/luhonglin/.cache/codex-runtimes/codex-primary-runtime/dependencies/python/bin/python3 -m unittest discover -s .github/release-tools -p test_release_matrix.py`
- generated-downloads: `python3 tools/build_docs_downloads.py --check`
- vendor-prototype: `node --test docs/vendor-console/tests/hierarchy-model.test.cjs`

## 初次失败与处理

后端首次完整测试发现两个旧 SQL mock 预期未包含供应商归档账号的写入排除条件。生产 SQL 已正确包含该条件；本次只在原有两个测试各替换一行，保留严格 SQL 匹配和回滚验证。修正清单见 [test-corrections.json](test-corrections.json)，合计测试 +2 / −2，业务代码 +0 / −0。补丁先通过 Go overlay 验证，再纳入第 12 组。

本机默认 pnpm 版本与仓库现有安装配置不匹配，因此直接使用既有 node_modules 的 vue-tsc、Vitest 与 Vite；没有改写 package 或 lockfile。Python 检查初次缺少 YAML 依赖，随后按仓库已锁定的 PyYAML 6.0.3 安装到仓库外测试目录，检查全部通过。

## 逐文件核对与范围

新三品牌提交剔除本归档目录后，必须与固定最终目标树逐路径、blob 和 mode 一致；固定目标树只允许上述两个测试预期不同于原已推送快照。所有既有业务文件、SQL、锁文件、品牌资产与下载包均维持原 blob / mode，原文件路径集合不增不减。删除项仅表示相对官方 v0.2.17 的既有删除，不是此次丢弃最终文件。

完整序列共用路由、配置、schema、生成装配与依赖文件。此次验证最终组合，不声称每笔中间提交可单独构建或任意 cherry-pick。

本轮没有运行数据库迁移、生产部署、发布、真实 OAuth/付费调用、真实代理网络验收、浏览器交互验收、race 或长时间 fuzz。旧文档中的 PostgreSQL、浏览器和真实提供方结果只作为历史证据，不计入本轮通过项。

供应商 V2 层级原型测试通过不代表层级鉴权和双边结算后端已实现。
