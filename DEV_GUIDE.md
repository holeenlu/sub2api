# Sub2API 开发指南

本指南只说明开发入口与操作；需求、架构、设计、模块、接口和验收统一见 [重构资料](docs/architecture-rebuild/README.md)，交付规则见 [CHANGE_DELIVERY](docs/CHANGE_DELIVERY.md)。

## 工具链与输入

- 后端：Go 1.27.3，以 `backend/go.mod` 和所选构建 SDK 为准；Gin、Ent、Wire 版本由 go.mod/go.sum 固定。
- 前端：Vue/TypeScript/Vite；使用 `frontend/package.json` 声明的 `pnpm@9.15.9` 和 pnpm-lock.yaml。默认API Docker不构建前端；前端工具链由独立会话维护。
- PostgreSQL/Redis、环境配置和启动方式见 [部署手册](deploy/README.md)。生产凭据不写入仓库、夹具或日志。
- 本项目只在codex/sub2api-rebuild增量开发与交付，不向维护分支传播。

## 开发前

1. 读取本分支 AGENTS.md 与当前交付规范；核对工作区、暂存、worktree 占用和任务边界。
2. 定位工程基线 R01–R20 中受影响需求及真实调用链，更新相应输入/输出、权限、数据、状态、错误、兼容和验收设计。
3. 现有 owner 可承载时直接修改；确需新增接口/字段/层时写明原因，列出被替代代码与同步清理范围。
4. 不复制新的日期版方案，不将未实现规划标为已实现，也不混入其他任务未完成代码。

## 编码规范

新增与实际修改代码遵循[详细编码规范](docs/architecture-rebuild/CODING_STANDARDS.md)：包/feature结构、Go与Vue/TypeScript写法、状态与错误、并发和资源、数据与安全、测试及审查。它定义实现标准，不表示新架构已经落地；现有工具覆盖和人工检查缺口在规范中分别说明，不据此执行无关全仓重写。

品牌相关开发另遵循[统一项目与品牌配置](docs/architecture-rebuild/BRANDING.md)：不要以运营品牌分叉代码，不要把网站设置与部署/上游/更新权限混用。

## 常用本地操作

以下为按需使用的命令，不是每次变更都运行的固定套餐。

```bash
# 前端安装和开发（在已确认需要安装依赖时）
cd frontend
pnpm install --frozen-lockfile
pnpm dev
# 定向测试/类型/构建根据实际变化选择
pnpm exec vitest run <受影响测试文件>
pnpm typecheck
pnpm build
# 独立门户在需要验证其构建时使用
pnpm build:vendor
```

```bash
# 后端：在 backend 目录使用适用的已有测试标签与包
cd backend
go test -tags=unit ./internal/gateway/protocol/sse
# 真实数据库、race、全量构建只在实际风险需要时运行
```

前端 build/build:vendor 已含其声明的语言和类型步骤；相同输入不要在每个阶段机械重复。Ent/Wire 生成仅在 schema/provider 变化需要时执行，检查生成差异，不能通过删除生成错误掩盖契约不一致。

## 验证与交付

- 文档/注释：差异、链接、引用/矩阵和必要格式检查；不执行无关业务测试与构建。
- 业务变化：按受影响入口和独立风险选现有回归；协议涉及真实报文、第二轮、取消/失败、输出和账务。
- 所有 GitHub Workflow 仅 `workflow_dispatch`，包括质量检查；push/PR 不自动启动 CI 或发版。
- 已有 `tools/change_delivery.py` 管理验证输入与回执、交付互斥；不建立第二套验证/发布工具。
- 提交、推送与人工发版/部署/迁移分开；核对实际差异、唯一目标ref及远端/本地一致性。
- 测试临时输出和构建产物按任务归属登记、清理并实测；限额内共享缓存与 SDK 保留，不清理其他任务资源。

## 维护资料

[工程入口](docs/README.md) · [来源与许可](docs/SOURCE_ADAPTATIONS.md) · [插件开发](backend/pkg/pluginapi/docs/development.md) · [数据库迁移](backend/migrations/README.md) · [API部署](deploy/api/README.md)
