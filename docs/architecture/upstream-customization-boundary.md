# Upstream Customization Boundary

## 仓库角色

```text
upstream = Wei-Shaw/sub2api，官方只读来源
origin   = holeenlu/sub2api，本地定制和发布来源
```

`main` 可以跟踪 `upstream/main` 以观察官方基线，但默认 push 必须指向 `origin`。官方远端禁止推送。

## 同步流程

1. `git fetch upstream`；
2. 从 `origin/main` 创建 `sync/upstream-YYYYMMDD`；
3. merge `upstream/main`，不重写已经发布的本地历史；
4. 解决冲突并运行完整验证；
5. 检查官方是否新增与本地定制重复的能力；
6. 通过 PR 合并到 `origin/main`。

## 定制触点规则

优先顺序：

```text
复用官方扩展点
  → 在独立文件中增加实现并做单点注册
    → 对官方共享接入点做小改动
      → 经 ADR 批准修改核心生命周期
```

旁路 `custom_*` 表或包只在新状态拥有独立生命周期和权威所有者时采用，不能单纯为了避免 Git 冲突拆散领域模型。

每项 M/L 定制记录：

- 修改的官方文件和接入点；
- 为什么不能只新增文件；
- 保护该触点的合同测试；
- 官方出现同类能力时的迁移和删除方式。

## 提交规则

- 部署、治理、迁移、后端、前端和测试尽量分成原子提交；
- 不混入全文件格式化和无关重命名；
- 不复制官方模块形成第二套实现；
- 兼容层必须有 owner 和 removal trigger；
- 每次同步后更新 `docs/architecture-health.md` 中已发生变化的风险。

## API Key 允许触点

当前允许的最小触点：

- APIKey service/DTO 的派生无限配额语义；
- 管理员 API Key 路由和独立 Handler 文件；
- 统一 usage 查询与写入 repository；
- 现有 KeysView 和管理员导航；
- P1 的共享 ConcurrencyHelper。

不允许在每个协议 Handler 分别实现 Key 配额、权限、并发或快照。
