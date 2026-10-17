## Grok固有のインターフェース

Grokのテキスト機能は、OpenAI互換のChat、Responses、Messagesブリッジを使用します。このページでは、Grok専用のメディア、検索、音声エンドポイントについて説明します。

| 機能 | エンドポイント | 対象範囲 |
| --- | --- | --- |
| 画像 | `POST /v1/images/generations`, `/edits` | 画像権限を持ち、対象となるメディアアカウントに紐付いたGrokグループ |
| 非同期画像 | `/async` を付加し、`GET /v1/images/tasks/{task_id}` でポーリング | オブジェクトストレージが有効なOpenAI/Grokグループ |
| 動画 | `/v1/videos`, `/videos/generations`, `/edits`, `/extensions` | 作成およびルックアップではGrokに解決される複合グループを許可する場合があります。編集および拡張にはGrokが必要です |
| 検索 | `POST /v1/web_search`, `/v1/x_search` | Grokグループのみ |
| HTTP音声 | `/v1/tts`, `/v1/stt`, `/v1/custom-voices...` | Grokグループのみ。ネイティブのボディ/レスポンスを中継 |
| リアルタイム音声 | `GET /v1/realtime?model=...` | Grokのみ。WebSocket Upgradeが必要 |

HTTPエンドポイントにはBearerキーを使用します。`/v1/models` におけるモデルの表示だけでは、メディア機能の利用資格があることは証明できません。

## 画像と動画

Grokの画像機能では、OpenAI Imagesのパスを再利用します。JSONまたはマルチパート形式で、`model`、`prompt`、`n`、`size`、`aspect_ratio`、`resolution`、入力画像を指定できます。編集では、入力画像は最大3枚まで受け付けます。正確なサイズ、比率、枚数、マスクのセマンティクスは、アップストリームモデルによって決まります。

動画エンドポイントは次のとおりです。

```http
POST /v1/videos
POST /v1/videos/generations
POST /v1/videos/edits
POST /v1/videos/extensions
GET  /v1/videos/{request_id}
GET  /v1/videos/{request_id}/content
```

作成時には通常、モデルとプロンプトが必要です。`resolution`、`duration`、`aspect_ratio`、入力画像のURLは任意で指定できます。返されたリクエストIDとエイリアスをポーリングしてください。コードでは、`status: "done"` かつ `video.url` が存在する場合にのみ、結果を完了として扱います。コンテンツは、タスクに紐付いたアカウントを介してプロキシされます。

## スタンドアロン検索

```bash
curl "$TOKENSAVY_BASE_URL/v1/web_search" \
  -H "Authorization: Bearer $TOKENSAVY_API_KEY" -H "Content-Type: application/json" \
  -d '{"query":"Tokensavy API updates","max_results":5}'
```

`query` は必須で、エイリアスとして `input` も使用できます。`max_results` のデフォルト値は5で、上限は20です。`/v1/x_search` では、`allowed_x_handles`、`excluded_x_handles`、`from_date`、`to_date`、`enable_image_understanding`、`enable_video_understanding` も受け付けます。レスポンスはゲートウェイによる集約結果（`query/results/provider/max_results`）であり、生のResponsesツールイベントではありません。URLの検証と重複排除を行ってください。

## 音声とリアルタイム機能

TTS、STT、カスタム音声では、クライアントのContent-Typeを保持し、ネイティブのボディ/レスポンスを中継します。フィールドとメディアタイプはアップストリームで定義されます。カスタム音声では、一覧取得、作成、取得、更新、削除、音声取得をサポートしています。Realtimeの `model` のデフォルト値は `grok-voice-latest` で、Upgradeが必要です。通常のGETでは426が返されます。

Grok以外のグループでは、通常これらのエンドポイントに対して404 `not_found_error` が返されます。その他の失敗には、空のボディ、機能の不一致、アカウントなし、クォータ/同時実行数超過、アップストリームの4xx/5xxなどがあります。ソース参照: `routes/gateway.go`、`handler/grok_media.go`、`service/grok_media.go`、`handler/gateway_web_search.go`、`handler/openai_x_search.go`、`handler/grok_audio.go`。
