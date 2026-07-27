# Architecture Decision Records

ADR 记录具有长期影响、存在多个合理方案或改变系统不变量的决策。

文件名使用：

```text
NNNN-short-decision-title.md
```

状态使用：`Proposed`、`Accepted`、`Superseded`、`Rejected`。

每份 ADR 至少包含：

```markdown
# ADR NNNN: Title

Status: Proposed
Date: YYYY-MM-DD

## Context
## Decision
## Alternatives considered
## Consequences
## Migration and rollback
## Revisit triggers
```

ADR 不记录实现流水账。实现变化写入提交和需求文档；ADR 解释为什么选择该边界，以及何时应该推翻。
