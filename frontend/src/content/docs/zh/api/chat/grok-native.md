## Grok 专属协议面

Grok 文本走 OpenAI 兼容的 Chat Completions、Responses 和 Messages 桥接；本页只列 Grok 分组特有的媒体、搜索和语音端点。

| 功能 | 端点 | 边界 |
| --- | --- | --- |
| 图像 | `POST /v1/images/generations`、`/edits` | Grok 分组，需图像权限和媒体账号能力 |
| 异步图像 | 同步路径追加 `/async`，轮询 `GET /v1/images/tasks/{task_id}` | OpenAI/Grok 分组且对象存储已启用 |
| 视频 | `/v1/videos`、`/videos/generations`、`/edits`、`/extensions` | 生成/查询可允许解析到 Grok 的复合分组；编辑/扩展要求 Grok |
| 搜索 | `POST /v1/web_search`、`POST /v1/x_search` | 仅 Grok 分组 |
| HTTP 语音 | `/v1/tts`、`/v1/stt`、`/v1/custom-voices...` | 仅 Grok 分组，转发原生请求/响应 |
| Realtime 语音 | `GET /v1/realtime?model=...` | 仅 Grok，必须 WebSocket Upgrade |

Bearer Key 是所有 HTTP 端点的建议鉴权。`GET /v1/models` 可见不等于媒体账号具备相应能力。

## 图像与视频

Grok 图像复用 OpenAI Images 路径，JSON/multipart 可携带 `model`、`prompt`、`n`、`size`、`aspect_ratio`、`resolution` 和输入图片；编辑最多三个源图。具体尺寸、比例、数量、mask 语义由上游模型决定。

视频端点：

```http
POST /v1/videos
POST /v1/videos/generations
POST /v1/videos/edits
POST /v1/videos/extensions
GET  /v1/videos/{request_id}
GET  /v1/videos/{request_id}/content
```

请求通常需要模型和提示，可能包含 `resolution`、`duration`、`aspect_ratio`、输入图像 URL。创建返回 request ID，轮询状态别名；仅 `status: "done"` 且 `video.url` 非空被认定完成，content 路径通过创建任务绑定账号代理下载。

## 独立搜索

```bash
curl "$TAPMODELS_BASE_URL/v1/web_search" \
  -H "Authorization: Bearer $TAPMODELS_API_KEY" -H "Content-Type: application/json" \
  -d '{"query":"TapModels API updates","max_results":5}'
```

`query` 必填，也支持 `input`；`max_results` 默认 5、上限 20。`/v1/x_search` 另支持 `allowed_x_handles`、`excluded_x_handles`、`from_date`、`to_date`、`enable_image_understanding`、`enable_video_understanding`。响应是网关聚合的 `query/results/provider/max_results`，不是 Responses 工具事件原样透传；应用应验证 URL 和去重。

## 语音与 Realtime

TTS、STT、自定义声音保留客户端 Content-Type 并转发原生 body/响应；字段和媒体类型由上游决定。自定义声音支持列表、创建、读取、更新、删除和音频读取。Realtime 的 `model` 默认 `grok-voice-latest`，必须 Upgrade；普通 GET 返回 426。

非 Grok 分组访问专属端点通常返回 404 `not_found_error`。其他错误包括空体、能力不匹配、无账号、额度/并发和上游 4xx/5xx。源码对应 `routes/gateway.go`、`handler/grok_media.go`、`service/grok_media.go`、`handler/gateway_web_search.go`、`handler/openai_x_search.go`、`handler/grok_audio.go`。
