## 端点与鉴权

```http
POST /v1/images/generations
POST /v1/images/edits
Authorization: Bearer $TAPMODELS_API_KEY
```

当前产品展示的图像模型包括 `gpt-image-2.5-flare` 与 `gpt-image-2.5-sunburst`，实际权限由 API Key 分组和运行中的兼容账号决定。网关验证 `gpt-image-*` 模型族；如果省略 `model`，代码默认使用 `gpt-image-2`，但默认值不代表当前分组一定可调度，生产集成应始终显式发送 `GET /v1/models` 返回的精确 ID。

## 生成请求

生成接口使用 JSON：

```bash
curl "$TAPMODELS_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model":"gpt-image-2.5-flare",
    "prompt":"A clean product photo of a red desk lamp on a white background",
    "n":1,
    "response_format":"b64_json"
  }'
```

| 字段 | 类型 | 必需 | 网关行为与限制 |
| --- | --- | --- | --- |
| `model` | string | 建议是 | 必须属于 `gpt-image-*` 或已实现的 Grok 图像族；省略时默认 `gpt-image-2` |
| `prompt` | string | 是 | 生成或编辑指令；空提示最终会被目标链拒绝 |
| `n` | integer | 否 | 默认 1，网关要求大于 0；最大值由上游决定 |
| `size` | string | 否 | 可传上游支持的尺寸或档位；计费归一化识别 1K/2K/4K，实际像素以结果文件为准 |
| `quality` | string | 否 | 原生选项；允许值由精确模型决定 |
| `response_format` | string | 否 | `b64_json` 或目标链支持的 `url`；URL 可能有有效期 |
| `background` | string | 否 | 背景选项；需模型支持 |
| `output_format` | string | 否 | 输出编码；需模型支持 |
| `output_compression` | integer | 否 | 压缩设置；网关验证类型，上限由上游决定 |
| `moderation` / `style` | string | 否 | 原生选项；并非所有模型支持 |
| `partial_images` | integer | 否 | 流式部分图数量；需原生能力 |
| `stream` | boolean | 否 | `true` 返回图像 SSE；必须是 boolean |

不要从其他 GPT Image 版本推断 `quality`、尺寸、格式或多图上限。显式模型、显式尺寸、`stream`、`n != 1`、mask 以及上述原生选项都会要求具备 `images-native` 能力的账号。

## 图像编辑：multipart/form-data

编辑的文件上传形式接受一个或多个 `image` / `image[n]` part，以及可选 `mask`。每个上传 part 最多读取 20 MiB；总请求大小还受服务端网关配置限制。

```bash
curl "$TAPMODELS_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" \
  -F "model=gpt-image-2.5-flare" \
  -F "prompt=Replace the background with a quiet library" \
  -F "image=@input.png;type=image/png" \
  -F "mask=@mask.png;type=image/png" \
  -F "response_format=b64_json"
```

multipart 必须带 boundary；编辑缺少图片会返回 `image file is required`。`mask` 会作为独立图片上传，具体透明区域语义、尺寸匹配和文件格式由目标模型验证。网关不会仅凭 part header 预先得出图像宽高。

JSON 编辑也受支持，格式为 `"images":[{"image_url":"https://..."}]`，mask 使用 `{"image_url":"..."}`。至少需要一个 `images[].image_url`；`images[].file_id` 和 `mask.file_id` 会被明确拒绝。

## 非流式响应

```json
{
  "created": 1789401600,
  "data": [{
    "b64_json": "iVBORw0KGgoAAA...",
    "revised_prompt": "A clean product photo of a red desk lamp on white."
  }],
  "usage": {
    "input_tokens": 120,
    "output_tokens": 1056,
    "total_tokens": 1176
  }
}
```

结果也可能在 `data[].url`。只解码 JSON 中的 `b64_json` 字段，先校验 base64、MIME 和真实宽高；不要把大型 payload 写进日志。若请求 `url`，网关在部分处理链会把 data URI 或下载结果回填，仍应把 URL 当作短期结果并及时转存。

## 图像 SSE

原生流会产生 `image_generation.partial_image` 和 `image_generation.completed`（编辑路径的下游事件名可能归一化为 `image_edit.*`），payload 可包含 `b64_json`、`partial_image_index`、`size`、`output_format` 和 usage。不同账号也可能经 Responses 事件转换，因此客户端应以 JSON 的 `type` 为准，兼容 `response.*` 和 `response.image_generation_call.*` 事件。

只有收到 completed 图像事件或带有效图像结果的最终响应才算成功。`response.incomplete`、`response.failed`、`error`、空 output 或完成前断流均应视为失败/未知，不能仅因 HTTP 已是 200 就保存结果。

## 异步提交与轮询

异步功能依赖管理员启用对象存储；关闭时提交返回 404，但已创建任务在任务存储可用时仍可轮询。异步请求与同步端点使用相同 JSON 或 multipart payload，且禁止 `stream: true`。

```http
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks/{task_id}
```

提交成功返回 HTTP 202，并带 `Location: /v1/images/tasks/{task_id}`、`Retry-After: 3` 和：

```json
{
  "id":"imgtask_abc123",
  "task_id":"imgtask_abc123",
  "object":"image.generation.task",
  "status":"processing",
  "created_at":1789401600,
  "expires_at":1789488000,
  "poll_url":"/v1/images/tasks/imgtask_abc123"
}
```

使用提交时同一把 API Key 轮询。当前实际状态只有 `processing`、`completed`、`failed`：

```json
{
  "id":"imgtask_abc123",
  "task_id":"imgtask_abc123",
  "object":"image.generation.task",
  "status":"completed",
  "http_status":200,
  "image_url":"https://storage.example/result.png",
  "result":{"created":1789401601,"data":[{"url":"https://storage.example/result.png"}]},
  "created_at":1789401600,
  "completed_at":1789401601,
  "expires_at":1789488001
}
```

`processing` 响应带 `Retry-After: 3`；`failed` 返回原同步调用的 `http_status` 和 `error`。任务按用户及 API Key 双重归属，其他 Key 查询同一 ID 返回 404。默认结果 TTL 为 24 小时，执行超时为 30 分钟；部署配置可以覆盖，不应把这两个默认值当成永久 SLA。此接口当前不提供 webhook。

## 错误与排障

错误使用 `{"error":{"type":"...","message":"..."}}`；异步接口同时回显 `error.code`。重点检查：模型是否属于图像族；分组是否开放图像权限；编辑请求的 Content-Type/boundary 和图片字段；`n`/`stream`/压缩字段类型；异步对象存储是否启用；轮询是否使用原 API Key。

遇到 502/503、空 output 或流内 error 时保留 request ID、模型、端点、HTTP 状态和最后事件类型。不要记录 API Key、原图 base64、mask 或生成结果正文。
