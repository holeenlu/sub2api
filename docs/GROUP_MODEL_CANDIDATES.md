# 分组模型白名单候选核查

厂商候选核查日期：2026-10-08；业务流程补充核查日期：2026-10-11。范围涵盖分组候选、账号默认模型限制和客户端目录的数据流；内置候选不是上游可用性保证或计费价格表。

## 选择口径

- 精选厂商当前主推、仍提供服务的文本/代码及现有网关支持的图片/视频型号，保留必要的低成本型号和原生辅助请求 ID。
- 不穷举历史代际、日期快照、重复别名、临时免费/匿名实验型号，也不把厂商专用音视频协议直接当成本项目已支持的接口。
- “未推荐”不等于“已停服”。例如 Claude Sonnet 4.5、GPT Image 1/1.5 已宣布退役但在核查日尚未关闭；Gemini 2.5 仍服务已有用户，默认候选优先使用当前面向新项目的型号。
- 官方文档能确认公开服务，不代表每个账号、套餐、地区都有权限；具体资格仍由账号配置和上游响应决定。没有使用生产凭据逐个调用模型。

## 当前候选范围与来源

内置候选数量：Anthropic 5、OpenAI 11、Gemini 7、Antigravity 6、Grok 3、Kimi 8、智谱 4、DeepSeek 2、MiniMax 3、OpenCode Go 15、TypeSafe 1；Composite 去重后 56。账号显式映射和已保存条目另行补充。

| 平台 | 精简后的候选范围 | 官方核查来源 |
| --- | --- | --- |
| Anthropic | Fable 5.1、Opus/Sonnet/Haiku 5.5；保留 Claude Code 辅助请求所需的 Haiku 4.5 | [模型概览](https://platform.claude.com/docs/en/models/overview)、[生命周期](https://platform.claude.com/docs/en/about-claude/model-deprecations) |
| OpenAI | GPT-6 系列、GPT-5.6 Sol/Terra/Luna、Image 2/2.5；保留现有原生探测 ID `codex-auto-review` | [官方模型目录](https://developers.openai.com/api/docs/models/all)、[退役说明](https://developers.openai.com/api/docs/deprecations)。辅助 ID 来自项目既有 `CodexUsageProbeModel`，不作为公开通用模型宣传 |
| Gemini | 3.8 Flash、3.5 Flash-Lite、3.1 Pro Preview、Nano Banana 2.1、3.1 Flash Image/Flash-Lite Image、3 Pro Image | [模型目录](https://ai.google.dev/gemini-api/docs/models)、[生命周期](https://ai.google.dev/gemini-api/docs/deprecations) |
| Antigravity | 3.8 Flash High/Medium、3.1 Pro High、Sonnet 4.6、Opus 4.6 Thinking、3.1 Flash Image | [可用模型与套餐](https://antigravity.google/docs/models)、[CLI 模型 ID](https://www.antigravity.google/docs/cli/headless/)；仅选择已知原生 ID |
| Grok | 4.7、Imagine Image 2.0、Imagine Video 1.5 | [官方模型目录](https://docs.x.ai/developers/models) |
| Kimi | 公共 API 的 K3、K2.7 Code/HighSpeed、K2.6；Coding 的 `k3`、`k3-256k`、`kimi-for-coding`、`kimi-for-coding-highspeed` | [公共 API](https://platform.kimi.ai/docs/models)、[Coding 模型 ID](https://www.kimi.com/code/docs/en/kimi-code/models.html) |
| 智谱 | GLM-5.3、5.3-Flash/FlashX、5.2 | [5.3](https://docs.bigmodel.cn/cn/guide/models/text/glm-5.3)、[Flash/FlashX](https://docs.z.ai/guides/vlm/glm-5.3-flash)、[5.2](https://docs.z.ai/guides/llm/glm-5.2) |
| DeepSeek | `deepseek-flash`、`deepseek-v4-pro` | [官方模型目录](https://api-docs.deepseek.com/quick_start/pricing/)。旧 V4 Flash 名称仍被接受，但已转到 V4.1 Flash，候选使用官方建议的 `deepseek-flash` |
| MiniMax | M3、M2.7、M2.7-highspeed | [官方模型目录](https://platform.minimax.io/docs/pricing/overview) |
| OpenCode Go | 15 个当前服务型号，覆盖 Grok、GPT、GLM、Kimi、DeepSeek、MiMo、MiniMax、Qwen、LongCat、Hy；不预置限时免费或匿名实验名 | [Go 端点与模型 ID](https://docs.opencode.ai/docs/go/)。网关型号使用 Go 的 ID，不能直接替换成原厂 ID |
| TypeSafe | 保留已有专用 `jev-latest`，不扩展目录 | [官方 Quick Start](https://docs.typesafe.ai/introduction/quickstart)，继续沿用项目已有 System One 适配器 |
| Composite | 合并上述可转发平台候选并去重；TypeSafe 仍只从显式账号映射补充 | 沿用现有复合分组规则 |

Antigravity 官方页面已列出 Claude 5.5，但公开页面未核准本项目转发所需的确切 ID，因此本次不猜测加入；可通过账号上游同步并保存显式映射补充。官方标记其 Claude 4.6 在 2026-11-02 移除，本次核查日仍提供服务。Antigravity 的模型与直连 Gemini/Claude API 不可互换。

## 账号模型限制共用目录（2026-10-11 补充）

账号创建、单个编辑及批量编辑中的模型限制选择器，其候选和“同步最新支持模型”现统一调用已有 `GET /admin/groups/0/model-allowlist-candidates?platform=...`，使用与分组白名单相同的内置目录。混合平台批量编辑合并各平台候选；没有平台时使用 Composite 候选。创建向导打开、切换平台/白名单模式时的默认填充也复用该目录；重置时清空草稿，下次打开重新加载。用于候选和默认填充的旧前端数组及 `getModelsByPlatform` 已删除。协议映射快捷预设继续保留为显式映射规则，不承担候选目录用途。加载期间禁用创建按钮，过期响应或手工修改后的草稿不会被异步结果覆盖。

本次向 OpenAI 内置候选新增 `gpt-reserve`（用户通过账号上游同步确认返回的 ID），由 11 增至 12；Composite 去重后由 56 增至 57。没有移除其他内置候选。此补充不声称该 ID 对所有账号开放，具体权限仍由上游决定。

OpenAI 的 OAuth/API Key 批量编辑现在与 Anthropic 复用上游同步：逐个查询所有目标账号，只追加共同支持的模型，任一查询失败、为空或没有交集时保留原草稿。支持选中账号和筛选结果的所有分页；点击同步不自动保存模型限制。内置目录加载失败可重试，不回退旧前端列表；平台切换或关闭窗口后丢弃过期响应。

## 实现边界与维护

### 业务流程与数据契约（2026-10-11）

| 环节 | 数据来源及处理 | 顺序与保存语义 |
| --- | --- | --- |
| 内置候选 | 后端 `defaultModelsListCandidateIDs`；账号调用分组 ID `0`，实际分组另补账号显式映射键 | 候选是建议，不能自动替代已保存配置；当前没有上游定时更新 |
| 创建账号 | 打开、切换平台或白名单模式时请求内置候选；映射模式使用已有协议预设 | 默认选择只填草稿，提交才生成 `credentials.model_mapping`；加载期间阻止继续/创建，关闭、切换及手工修改后的过期响应丢弃 |
| 单个/批量编辑 | 回显账号已有模型限制；“同步最新支持模型”读内置候选；“同步上游支持的模型”读账号实际上游 | 同步只修改草稿；批量上游同步采用共同支持的交集，失败不覆盖草稿 |
| 分组白名单 | 已保存条目在候选合并时保留；前端按当前行顺序生成 `model_allowlist.models`，后端清理空白并保序去重 | 调整行顺序并保存后才生效；启用状态决定是否限制分组，候选更新不自动启用新模型 |
| 普通 `/v1/models` | 账号映射/平台运行时目录，或固定账号的上游目录；最终调用 `FilterForListing` | 开启白名单后按已保存条目顺序过滤输出；无需按字母重排 |
| `/v1/models?client_version=...` | 返回 Codex `models` 清单；固定账号、调度获取或账号映射本地生成均在合并后执行同一白名单顺序规则 | 保留上游描述符及未知能力字段；最终响应计算 ETag，调整选择顺序后不得返回旧内容的 304 |
| `/keys` 获取和下载 | 使用 API 密钥请求带 `client_version` 的接口；格式化响应后缓存为文本，下载同一文本为 `codex-models.json` | UI 解析和下载均保留响应数组顺序；成功重新获取后使用最新目录，刷新失败沿用界面已有缓存 |

模型候选、账号模型映射、分组准入和客户端目录是不同数据用途。统一的是内置候选来源和分组输出的排序规则：账号映射的对象键不承载展示顺序，不能靠其遍历或字母排序决定最终目录。普通目录与 Codex 目录的成员可能不同，后者仍按已有 Responses 能力规则过滤图片/视频及不支持的协议；两者共有的模型按同一分组顺序展示。

顺序契约复用 `GroupModelAllowlist.FilterForListing`：精确条目按白名单位置输出实际可列出的模型；通配符在其所在位置展开，并保留来源内部顺序；全局去重。白名单未开启时保留原目录行为，不把建议候选误当作账号真实权限或分组限制。

分组保存会失效关联 API 密钥的鉴权缓存；上游原始目录缓存与分组过滤分离，响应使用当前分组配置排序。`/keys` 已获取的文本不会被管理端保存动作实时替换，保存分组后需要成功重新获取再下载。

本轮回归覆盖创建默认值直接提交、请求未完成时提交、失败/切换/关闭/手工修改时的异步响应，以及本地、调度和固定账号目录的非字母顺序、通配展开、元数据保留和 ETag 更新。普通目录及 `/keys` 下载沿用已有的保序与完整内容测试。本地验证没有使用生产 API 密钥，也未验证线上实例是否已部署。

分组与账号选择器共用的内置候选定义仍是 `backend/internal/service/admin_group.go` 的 `defaultModelsListCandidateIDs`。原先复用的各包 `DefaultModels` 同时承担网关回退、协议别名和账号测试用途，不能为清理分组下拉列表直接删改这些运行时目录；本次在已有候选函数内明确其推荐范围，不增加目录服务、配置键、HTTP 接口或定时任务。

Kimi、智谱、DeepSeek、MiniMax 不再回落到 Claude 候选。现有账号补充来源改为显式 `credentials.model_mapping` 的键；不再调用会注入默认旧模型和宽泛通配符的 `GetModelMapping()`。管理员显式配置的旧 ID、自定义名、通配符继续补入，映射目标不作为客户端模型名展示。

已保存的分组白名单由前端原有合并逻辑保留；候选更新不会自动启用新模型或删除旧规则。打开分组编辑时重新加载候选，没有定时向厂商拉取目录。后续更新应复核官方型号、生命周期、套餐 ID，并更新本页核查日期；不得把本页快照当成永久可用性保证。

## 本次逐项增删（相对 c13a38a67）

统计直接比较修改前实际调用的各包默认数组与修改后的分组候选函数。仅统计内置建议，未计入具体账号映射及分组已保存条目；Composite 为跨平台去重，不能与上方平台行相加。移出候选不代表上游已经停止服务，也不删除账号模型配置。

| 平台 | 更新前 | 新增 | 移出内置候选 | 更新后 |
| --- | ---: | ---: | ---: | ---: |
| Anthropic | 14 | +0 | −9 | 5 |
| OpenAI | 20 | +0 | −9 | 11 |
| Gemini | 9 | +5 | −7 | 7 |
| Antigravity | 39 | +0 | −33 | 6 |
| Grok | 14 | +0 | −11 | 3 |
| Kimi | 14 | +8 | −14 | 8 |
| 智谱 | 14 | +4 | −14 | 4 |
| DeepSeek | 14 | +2 | −14 | 2 |
| MiniMax | 14 | +3 | −14 | 3 |
| OpenCode Go | 29 | +4 | −18 | 15 |
| TypeSafe | 1 | +0 | −0 | 1 |
| Composite（去重） | 112 | +16 | −72 | 56 |

### 四个国产平台共同移除的错误候选

修改前 Kimi、智谱、DeepSeek、MiniMax 都回落到同一份 Claude 默认表。以下 14 个 ID 在这四个平台分别全部移除，属于修正厂商归属：

```text
claude-haiku-5-5
claude-fable-5-1
claude-fable-5
claude-opus-4-5-20251101
claude-opus-4-6
claude-opus-4-7
claude-opus-4-8
claude-opus-5-5
claude-opus-5
claude-sonnet-5-5
claude-sonnet-5
claude-sonnet-4-6
claude-sonnet-4-5-20250929
claude-haiku-4-5-20251001
```

### Anthropic

新增（0）：

无。
移出内置候选（9）：

```text
claude-fable-5
claude-opus-4-5-20251101
claude-opus-4-6
claude-opus-4-7
claude-opus-4-8
claude-opus-5
claude-sonnet-5
claude-sonnet-4-6
claude-sonnet-4-5-20250929
```
保留（5）：

```text
claude-fable-5-1
claude-opus-5-5
claude-sonnet-5-5
claude-haiku-5-5
claude-haiku-4-5-20251001
```

### OpenAI

新增（0）：

无。
移出内置候选（9）：

```text
gpt-6
gpt-5.6
gpt-5.5
gpt-5.4
gpt-5.4-mini
gpt-5.3-codex-spark
gpt-5.2
gpt-image-1
gpt-image-1.5
```
保留（11）：

```text
gpt-6-astra
gpt-6.1-sol
gpt-6-luna
gpt-6-sol
gpt-5.6-sol
gpt-5.6-terra
gpt-5.6-luna
codex-auto-review
gpt-image-2.5-sunburst
gpt-image-2.5-flare
gpt-image-2
```

### Gemini

新增（5）：

```text
gemini-3.8-flash
gemini-3.5-flash-lite
gemini-nano-banana-2.1
gemini-3.1-flash-lite-image
gemini-3-pro-image
```
移出内置候选（7）：

```text
gemini-2.0-flash
gemini-2.5-flash
gemini-2.5-flash-image
gemini-2.5-pro
gemini-3.5-flash
gemini-3-flash-preview
gemini-3-pro-preview
```
保留（2）：

```text
gemini-3.1-pro-preview
gemini-3.1-flash-image
```

### Antigravity

新增（0）：

无。
移出内置候选（33）：

```text
claude-fable-5-1
claude-fable-5
claude-opus-4-5-thinking
claude-sonnet-4-5
claude-sonnet-4-5-thinking
claude-opus-4-6
claude-opus-4-7
claude-opus-4-8
gemini-2.5-flash
gemini-2.5-flash-image
gemini-2.5-flash-image-preview
gemini-2.5-flash-lite
gemini-2.5-flash-thinking
gemini-3-flash
gemini-3-pro-low
gemini-3-pro-high
gemini-3.1-pro-low
gemini-3.1-flash-image-preview
gemini-3.6-flash
gemini-3.6-flash-high
gemini-3.6-flash-low
gemini-3.6-flash-medium
gemini-3.6-flash-tiered
gemini-3.7-flash
gemini-3.7-flash-high
gemini-3.7-flash-low
gemini-3.7-flash-medium
gemini-3.7-flash-tiered
gemini-3.8-flash
gemini-3.8-flash-low
gemini-3.8-flash-tiered
gemini-3-pro-preview
gemini-3-pro-image
```
保留（6）：

```text
gemini-3.8-flash-high
gemini-3.8-flash-medium
gemini-3.1-pro-high
claude-sonnet-4-6
claude-opus-4-6-thinking
gemini-3.1-flash-image
```

### Grok

新增（0）：

无。
移出内置候选（11）：

```text
grok-4.6
grok-4.5
grok-4.3
grok-build-0.1
grok-composer-2.5-fast
grok-4.20-0309-reasoning
grok-4.20-0309-non-reasoning
grok-4.20-multi-agent-0309
grok-imagine-image-quality
grok-imagine-image
grok-imagine-video
```
保留（3）：

```text
grok-4.7
grok-imagine-image-2.0
grok-imagine-video-1.5
```

### Kimi

新增（8）：

```text
kimi-k3
kimi-k2.7-code
kimi-k2.7-code-highspeed
kimi-k2.6
k3
k3-256k
kimi-for-coding
kimi-for-coding-highspeed
```
移出内置候选：上述共同的 14 个 Claude ID。

保留（0）：

无。

### 智谱

新增（4）：

```text
glm-5.3
glm-5.3-flash
glm-5.3-flashx
glm-5.2
```
移出内置候选：上述共同的 14 个 Claude ID。

保留（0）：

无。

### DeepSeek

新增（2）：

```text
deepseek-flash
deepseek-v4-pro
```
移出内置候选：上述共同的 14 个 Claude ID。

保留（0）：

无。

### MiniMax

新增（3）：

```text
MiniMax-M3
MiniMax-M2.7
MiniMax-M2.7-highspeed
```
移出内置候选：上述共同的 14 个 Claude ID。

保留（0）：

无。

### OpenCode Go

新增（4）：

```text
gpt-6-luna
deepseek-v4.1-flash
mimo-v2.6-pro
mimo-v2.6-flash
```
移出内置候选（18）：

```text
grok-4.6
gpt-5.6-luna
glm-5.2
glm-5.1
kimi-k2.6
deepseek-v4-flash
deepseek-v4-flash-vision-exp
mimo-v2.5
mimo-v2.5-pro
minimax-m2.7
minimax-m2.5
muse-spark-1.3-contributor
muse-spark-1.2-contributor
qwen3.7-max
qwen3.7-plus
qwen3.6-plus
hy4-preview
omen-alpha
```
保留（11）：

```text
grok-4.7
glm-5.3
glm-5.3-flash
kimi-k3
kimi-k2.7-code
deepseek-v4-pro
minimax-m3
qwen3.8-max
qwen3.8-flash
longcat-2.0
hy3
```

### TypeSafe

新增（0）：

无。
移出内置候选（0）：

无。
保留（1）：

```text
jev-latest
```

### Composite（去重）

新增（16）：

```text
gemini-3.5-flash-lite
gemini-nano-banana-2.1
gemini-3.1-flash-lite-image
kimi-k2.7-code-highspeed
k3
k3-256k
kimi-for-coding
kimi-for-coding-highspeed
glm-5.3-flashx
deepseek-flash
MiniMax-M3
MiniMax-M2.7
MiniMax-M2.7-highspeed
deepseek-v4.1-flash
mimo-v2.6-pro
mimo-v2.6-flash
```
移出内置候选（72）：

```text
claude-fable-5
claude-opus-4-5-20251101
claude-opus-4-6
claude-opus-4-7
claude-opus-4-8
claude-opus-5
claude-sonnet-5
claude-sonnet-4-5-20250929
gemini-2.0-flash
gemini-2.5-flash
gemini-2.5-flash-image
gemini-2.5-pro
gemini-3.5-flash
gemini-3-flash-preview
gemini-3-pro-preview
gpt-6
gpt-5.6
gpt-5.5
gpt-5.4
gpt-5.4-mini
gpt-5.3-codex-spark
gpt-5.2
gpt-image-1
gpt-image-1.5
claude-opus-4-5-thinking
claude-sonnet-4-5
claude-sonnet-4-5-thinking
gemini-2.5-flash-image-preview
gemini-2.5-flash-lite
gemini-2.5-flash-thinking
gemini-3-flash
gemini-3-pro-low
gemini-3-pro-high
gemini-3.1-pro-low
gemini-3.1-flash-image-preview
gemini-3.6-flash
gemini-3.6-flash-high
gemini-3.6-flash-low
gemini-3.6-flash-medium
gemini-3.6-flash-tiered
gemini-3.7-flash
gemini-3.7-flash-high
gemini-3.7-flash-low
gemini-3.7-flash-medium
gemini-3.7-flash-tiered
gemini-3.8-flash-low
gemini-3.8-flash-tiered
grok-4.6
grok-4.5
grok-4.3
grok-build-0.1
grok-composer-2.5-fast
grok-4.20-0309-reasoning
grok-4.20-0309-non-reasoning
grok-4.20-multi-agent-0309
grok-imagine-image-quality
grok-imagine-image
grok-imagine-video
glm-5.1
deepseek-v4-flash
deepseek-v4-flash-vision-exp
mimo-v2.5
mimo-v2.5-pro
minimax-m2.7
minimax-m2.5
muse-spark-1.3-contributor
muse-spark-1.2-contributor
qwen3.7-max
qwen3.7-plus
qwen3.6-plus
hy4-preview
omen-alpha
```
保留（40）：

```text
claude-fable-5-1
claude-opus-5-5
claude-sonnet-5-5
claude-haiku-5-5
claude-haiku-4-5-20251001
gemini-3.8-flash
gemini-3.1-pro-preview
gemini-3.1-flash-image
gemini-3-pro-image
gpt-6-astra
gpt-6.1-sol
gpt-6-luna
gpt-6-sol
gpt-5.6-sol
gpt-5.6-terra
gpt-5.6-luna
codex-auto-review
gpt-image-2.5-sunburst
gpt-image-2.5-flare
gpt-image-2
gemini-3.8-flash-high
gemini-3.8-flash-medium
gemini-3.1-pro-high
claude-sonnet-4-6
claude-opus-4-6-thinking
grok-4.7
grok-imagine-image-2.0
grok-imagine-video-1.5
kimi-k3
kimi-k2.7-code
kimi-k2.6
glm-5.3
glm-5.3-flash
glm-5.2
deepseek-v4-pro
minimax-m3
qwen3.8-max
qwen3.8-flash
longcat-2.0
hy3
```

### 账号动态补充来源的变化

不再把 Antigravity/Grok/Google One 的运行时隐式默认映射当作管理员配置加入候选，避免旧别名与 `gpt-*`、`claude-*` 等宽泛通配重新扩张列表。这里没有一个固定的“删除总数”：原始结果取决于组内账号类型、运行时设置及显式映射。所有显式 `credentials.model_mapping` 的合法字符串条目仍可补充，已保存分组规则也继续保留。

这次没有修改任何厂商运行时 DefaultModels、模型路由、计费、账号测试默认模型或持久化白名单。
