# 统一模型价格管理

迁移来源：`7771bc4b9` 的 `frontend/src/api/admin/models.ts`、`ModelPriceDialog.vue`、`TokenPriceFields.vue` 和 `ModelsView.vue` 中的价格功能。

2026-10-28 已接入正式 `/admin/models` 组合路由及模型价格 GET/PUT/sync 后端。权限、持久化 CAS 与运行时价格读取共用现有设置和账务链。自动化、真实 Docker API 和浏览器验收分别记录在 `frontend/.local/model-management-integration-20261028/` 及根 `.local/model-management-integration-20261028/`，不得将源码接线或替身测试当作真实上游验收。API 失败不生成示例价格或固定版本作为回退。

## 组合入口

```ts
const { ManagedPricesPanel } = await import('@/features/pricing/management')
```

组件 props：

| 属性 | 含义 |
| --- | --- |
| `platform: 'openai' \| 'anthropic'` | 当前平台 |
| `scope: string` | 当前身份、授权及用例范围的失效标识；变化同步取消所有在途读写并清空旧范围数据 |
| `canRead: boolean` | app 计算 `models.read && billing.cost.read` |
| `canManage: boolean` | app 计算 `models.manage && billing.rates.update` |
| `imageModelIds?: readonly string[]` | app 从目录事实提供图片模型 ID；不推断 ID 名称、不导入 catalog 运行实现。已存在的 image 字段始终可见可编辑 |

公开领域入口 `features/pricing/management.ts` 由主集成者维护；本目录不保留冗余 index，不修改 pricing 根 index，保持已注册的管理页按路由懒加载，避免编辑器进入全站启动 bundle。

具名事件：`dirty-change(boolean)`、`busy-change(boolean)`。挂载时发出当前状态；草稿更改、恢复原值、取消、保存成功、权限变化及卸载时更新 dirty；查询和命令（包括写后的重新读取）更新 busy。app 负责切换平台与 route 的草稿保护、忙时禁止切换。面板自身负责请求、失败提示、重新读取和草稿保存；不会向父级抛出待处理的异步错误。权限/身份变化优先清除私有草稿，不能保留旧授权数据。

## 接入的接口合同

保留旧版路径及请求结构，均复用 `@/shared/http` 的既有 `apiClient` 与当前会话生命周期：

| 请求 | 输入 | 响应 |
| --- | --- | --- |
| `GET /admin/models/pricing` | query: `platform`；AbortSignal | `{ version, synced_at?, entries }` |
| `PUT /admin/models/pricing` | `{ platform, model, version, price }`；AbortSignal | `{ version }` |
| `POST /admin/models/pricing/sync` | `{ platform, version }`；AbortSignal | `{ version }` |

HTTP 公共前缀/认证/step-up 由 shared/http 和 app 现有装配负责，`scope` 不作为可伪造授权字段发送。空 scope（含仅空白）即使短暂保留了 true 权限也禁止发起任何请求或命令。服务端仍须权威验证身份、平台与对象范围、两项读取权限、两项写入权限，以及同一事务中的 pricing CAS。模型策略版本与价格版本独立；本切片不修改 model policy，不发送或伪造 model policy version。pricing version 全平台共用，切换平台后重新读取当前权威值，不使用分平台版本缓存。

- version 必须来自成功读取，且为 `>= 0` 的安全整数；seed version 0 合法，首次 GET 0 → PUT version 0 → 返回及重读 1。写入仅使用捕获的 pricing version，不固定或伪造 0，不自动覆盖、重试或重放。不能用 HTTP 取消保证已发送写入在服务端回滚。
- `price: null` 仅表示移除该模型覆盖。GET 的单项价格可缺失或为 null，数值 `0` 表示已知免费；这不代表任意读取值都可提交。普通已定价 base（含定价 tier、非 null mode、非 null image）的 `input/output/cache_read` 必须为非 null 的有限数，范围 `0..1e9`；`cache_5m/cache_1h` 可省略或为 null，补值由服务端负责。显式未定价仅用 `modes.fast/flex/priority: null` 或 `tiers[].unpriced: true`；image 缺失/null 表示无独立图片价格，不能把任意 token 的 null 当作可保存价格。完整保留五个 token 字段、tiers（含 `above` / `unpriced`）、modes 和 image；不在浏览器重新计算账务或缓存补值。
- 写入与同步串行。成功响应后重新读取服务端快照；刷新失败保留草稿，阻止盲目再次发送。刷新不得退回已观察到或写入响应确认的旧版本。
- 仅已确认的 409 识别为版本冲突；412/428 不属于旧合同定义的冲突。旧同步 handler 可能把内部 CAS 失败包装为无 reason 的 502，不能由任意 502 推断冲突，面板按普通请求失败处理。失败时草稿及原 CAS 条件保留。对话框内提供重新读取按钮，无须关闭草稿操作背后的面板。显式重新读取后若版本变更，草稿显示已过期并禁止保存；用户关闭重开后再基于最新值编辑，不自动重设旧草稿的 CAS。
- scope/platform/read/manage 变化和卸载取消 AbortSignal；即使传输层忽略取消，也丢弃晚到成功/错误及写后的查询，不释放其他范围的新命令。

## 已执行的局部验证

在 `frontend` 下使用已有 `node_modules`，Node PATH 为 `/Users/luhonglin/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin`：

```sh
./node_modules/.bin/vitest run src/features/pricing/model-management/__tests__ --no-cache
./node_modules/.bin/eslint src/features/pricing/model-management --ext .ts,.vue --max-warnings 0
```

最终定向执行：3 个测试文件、84 项测试通过（API 37、状态 25、组件 22）；Vitest 耗时 2.14 秒；目录 ESLint 零错误零警告。初版曾以临时局部配置执行 Vue/TypeScript 检查通过；review 后已删除该专用配置，最终全量 types 由主集成合跑，不把旧检查当作 review 修改后的类型验证。测试覆盖旧 API/DTO 保真、seed version 0、严格价格写入、空 scope、查询与命令权限、重复命令、冲突与刷新失败、草稿保留及失效、取消与晚到响应、dirty/busy 事件、四语局部消息。HTTP 与后端命令使用测试替身，这些结果不是实际后端、页面操作或线上验收。

仅保留该目录内的实现、测试、四语消息和此交接说明。没有安装/升级依赖、生成构建产物、修改 Git 索引或写入其他业务目录；测试关闭 Vitest 缓存。

价格同步读取固定公开价格目录，不使用账号或供应商凭据；抓取期间的权限撤销和版本冲突由原管理鉴权及 CAS 处理，409 不包装成 502。
