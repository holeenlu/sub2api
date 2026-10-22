# 本轮验收证据

任务从 2026-10-13 开始，于 2026-10-14 完成。本目录记录未提交实现的本机验证，不是线上部署证明。

- unit.log：最终代码 go test -tags=unit ./... -count=1。
- race.log：service/handler/middleware/apicompat/antigravity 的定向集合；最后一个包无匹配测试，不宣称运行了其 race 用例。
- build.log / vet.log：成功时为空；对应 go build ./... / go vet -tags=unit ./...。
- lint-new.log：最终增量 0；lint-current-full.log / lint-baseline-full.log：默认报告限额各显示 53 条，报告样本可能随并发检查顺序不同。
- lint-current-uncapped.log / lint-baseline-uncapped.log：使用 --max-issues-per-linter=0 --max-same-issues=0，完整报告各 194 条，归一化路径/诊断的多重集合完全相同，新增 0。
- db-integration.log：真实 PostgreSQL/Redis 测试容器，导出和 UsageBilling 集成集合；非生产 DB。
- frontend-tests.log / frontend-typecheck.log / frontend-eslint.log / frontend-build.log：8 文件 95 测试，类型、改动生产文件 lint、构建。大 chunk 提示保留。
- wire.log：go generate ./cmd/server 的实际生成记录。
- changed-files.json / delivery-summary.json：完整修改文件内容 SHA-256、增删和 HEAD；不包含任务开始前的 AGENTS.md / CHANGE_DELIVERY.md。
- validation-summary.json：机器可读结果；报告给出完整边界和未做的验证。
- body-resource-matrix.csv/json：96 组受控 body 资源测量。local_first_frame 不是线上 TTFT，不应推导稳定性能提升百分比。
- run-body-matrix.py：本机复跑入口，先准备 /tmp/new-api-body-baseline.test 和 /tmp/new-api-body-current.test。源码是 backend/internal/service/gateway_body_resource_bench_test.go；使用同一 Go 1.27.2，分别在固定基线的临时 git archive 副本和工作区以 go test -tags=unit -c ./internal/service -o <binary> 编译。基准文件需放入基线副本，不能写回原始基线分支。脚本依赖 macOS /usr/bin/time -l。

所有文件受现有 docs 忽略规则影响，本轮未强制暂存。验收报告位于相邻目录。
