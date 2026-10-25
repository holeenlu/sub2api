# Sub2API 重构开发资料入口

版本：0.4，2026-10-25。范围：sub2api 独立重构开发。2026-10-25 已按用户指令启动，分支 codex/sub2api-rebuild；main 和原品牌分支继续独立维护。前端由用户另一会话负责。

本资料包让开发者或子智能体可以领取一个边界清楚的工作包，知道读什么、做什么、允许改哪些文件、依赖谁、如何验证以及何时删除旧路径。当前准备结果与未采集证据见[READINESS](READINESS.md)。

## 快速开始

1. 首次接手读[需求与不变量](REQUIREMENTS.md)、[总体架构](ARCHITECTURE.md)、[模块契约](CONTRACTS.md)、[详细编码规范](CODING_STANDARDS.md)和[品牌配置规范](BRANDING.md)。
2. 从[工作包视图](WORK_PACKAGES.md)选择任务，主集成者在[任务清单](work-items.json)核对依赖并登记唯一owner。
3. 使用[协作协议](COLLABORATION.md)确定写集和共享文件负责人，再读任务相关的协议、数据及验收条目。
4. 当前实施输入已冻结；先做已领取且依赖明确的工作切片，完整父工作包不得因局部通过提前验收。实际状态见任务JSON与[第一波实施记录](IMPLEMENTATION.md)。

## 文档地图与唯一规则来源

| 文档 | 负责的问题 | 使用者 |
| --- | --- | --- |
| [BASELINE](BASELINE.md) / [源码清单](source-inventory.json) | 当前系统范围、固定输入、已交付并发修复和来源范围 | 所有人 |
| [REQUIREMENTS](REQUIREMENTS.md) | 19项需求及不变量、非功能目标、范围与排除项 | 所有人 |
| [ARCHITECTURE](ARCHITECTURE.md) | 总体结构、职责、依赖方向和取舍 | 架构、各领域 |
| [DECISIONS](DECISIONS.md) | 15项ADR、工程默认、需证据的局部事项 | 集成者、合同owner |
| [CONTRACTS](CONTRACTS.md) | 11项CT的输入输出、状态、错误、取消、幂等和资源寿命 | 所有实现者 |
| [CODING_STANDARDS](CODING_STANDARDS.md) | 100项编码规则、Go/Vue/TS结构与写法、数据/安全/测试、实际工具覆盖及审查方式 | 所有实现者、评审者 |
| [BRANDING](BRANDING.md) | sub2api统一项目、部署参数、网站品牌设置、广告清理与更名迁移 | 控制面、前端、部署、集成与评审 |
| [PROTOCOLS](PROTOCOLS.md) | 真实入口/平台/mode/transport矩阵、协议场景和有效兼容 | 协议、provider、ingress |
| [DATA_AND_CONFIG](DATA_AND_CONFIG.md) | PG/Redis/进程状态、owner、TTL、敏感信息和配置来源 | 数据、会话、账务、控制 |
| [RISKS](RISKS.md) | 信任边界、严重失败及可观察控制 | 所有人、评审者 |
| [COLLABORATION](COLLABORATION.md) | 工作分配、单一写者、公共文件、变更和冲突交接 | 集成者、人类开发者、子智能体 |
| [WORK_PACKAGES](WORK_PACKAGES.md) / [work-items.json](work-items.json) | 30项任务、依赖、职责与写集；JSON为状态唯一来源 | 任务owner |
| [VALIDATION](VALIDATION.md) | REQ/CT/WP到场景、现有夹具、命令和证据层级 | 实现者、评审者 |
| [MIGRATION](MIGRATION.md) | 垂直切换、滚动版本、排空、数据兼容、对账和回退 | 集成者、数据、运维 |
| [RETIREMENT](RETIREMENT.md) | 12类旧实现的替换和删除证据 | 各切片owner、集成者 |
| [快速接手](NOTES_FOR_IMPLEMENTERS.md) | 常见误判与第一步工作 | 新加入者 |
| [交接模板](templates/HANDOFF.md) / [验收记录模板](templates/ACCEPTANCE_RECORD.md) | 交付实际diff、验证、未完成项和资源记录 | 各任务owner |
| [READINESS](READINESS.md) | 文档准备、运行验证与上线准备分别处于何种状态 | 所有人 |

## 并行开发依赖

~~~mermaid
flowchart LR
    A[WP00 实施输入] --> B[WP01 可编译合同]
    B --> C[协议 授权 会话 账务 目录 Host]
    C --> D[调度与执行引擎]
    B --> E[原生适配]
    D --> F[WP10 原生垂直集成]
    E --> F
    F --> G[BPS Cloud 其他平台 媒体 实时并行]
    C --> H[控制面与前端]
    H --> P[WP27 中性部署参数]
    P --> I
    G --> I[共享入口与装配串行集成]
    H --> I
    I --> J[旧路径收敛 风险验收 独立分支交付]
~~~

这张图概括波次；精确依赖以JSON为准。并行意味着分离写集与稳定合同，不表示所有任务同时启动。公共类型、旧共享service/handler、路由/Wire和品牌交付始终单一写者。

## 现行规范与证据边界

[根AGENTS](../../AGENTS.md)和[需求交付规范](../CHANGE_DELIVERY.md)继续管理授权、最小充分验证、发布和资源收尾；根AGENTS的独立重构分支条款优先，重构不执行维护分支的三品牌传播或历史改写。本包不复制其详细流程，不新增产品准入或验证平台。

文档准备完成不代表重构运行完成；测试存在不代表本轮测试通过；替身不代表真实上游；源码交付不等于发版、生产部署或数据迁移。具体缺口及负责工作包已登记，未验证项不能被写成passed。

前端详细规划见[FRONTEND_ARCHITECTURE](FRONTEND_ARCHITECTURE.md)，开发接手流程见[IMPLEMENTATION_PLAYBOOK](IMPLEMENTATION_PLAYBOOK.md)；这两份已制定资料随已验证前端补丁一并整合，不引入维护版发布策略。
