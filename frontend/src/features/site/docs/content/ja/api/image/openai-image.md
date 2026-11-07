## エンドポイントと認証

```http
POST /v1/images/generations
POST /v1/images/edits
Authorization: Bearer $API_KEY
```

現在紹介されている画像モデルには `gpt-image-2.5-flare` と `gpt-image-2.5-sunburst` が含まれます。アクセス可否は、APIキーのグループと利用可能な互換アカウントによって決まります。ゲートウェイは `gpt-image-*` ファミリーを検証します。`model` を省略すると、コード上は `gpt-image-2` がデフォルトになりますが、このデフォルト値はスケジュール可能であることを保証するものではありません。そのため、本番クライアントでは `GET /v1/models` から返された正確なIDを送信してください。

## 生成リクエスト

生成にはJSONを使用します。

```bash
curl "$API_BASE_URL/v1/images/generations" \
  -H "Authorization: Bearer $API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"gpt-image-2.5-flare","prompt":"A clean product photo of a red desk lamp on a white background","n":1,"response_format":"b64_json"}'
```

| フィールド | 型 | 必須 | ゲートウェイの動作と制限 |
| --- | --- | --- | --- |
| `model` | string | 推奨 | GPT画像ファミリーまたは実装済みのGrok画像モデルである必要があります。省略時のデフォルト値は `gpt-image-2` です |
| `prompt` | string | はい | 生成・編集の指示。空のプロンプトは対象パスで拒否されます |
| `n` | integer | いいえ | デフォルトは1。ゲートウェイは0より大きい値を要求し、最大値はアップストリームが設定します |
| `size` | string | いいえ | アップストリームのサイズまたはティア。請求の正規化では1K/2K/4Kを認識しますが、実際のピクセル数はファイルに由来します |
| `quality` | string | いいえ | ネイティブオプション。値は正確なモデルによって異なります |
| `response_format` | string | いいえ | `b64_json` またはアップストリームがサポートする `url`。URLは有効期限が切れることがあります |
| `background` / `output_format` | string | いいえ | ネイティブオプション。モデルのサポートが必要です |
| `output_compression` | integer | いいえ | 型はゲートウェイで検証され、範囲はアップストリームが定義します |
| `moderation` / `style` | string | いいえ | ネイティブオプション。すべてのモデルがサポートするわけではありません |
| `partial_images` | integer | いいえ | ネイティブストリーミングで返す部分画像の数 |
| `stream` | boolean | いいえ | `true` の場合は画像SSE。booleanである必要があります |

別のGPT Imageバージョンから、品質、サイズ、形式、複数画像の制限を推測しないでください。明示的なモデル/サイズ、ストリーミング、`n != 1`、マスク、ネイティブオプションを利用するには、`images-native` ケイパビリティを持つアカウントが必要です。

## 編集: multipart/form-data

編集エンドポイントは、1つ以上の `image` / `image[n]` パートと、任意の `mask` を受け付けます。アップロードされた各パートは最大20 MiBまで読み込まれ、リクエスト全体にもゲートウェイ設定による上限があります。

```bash
curl "$API_BASE_URL/v1/images/edits" \
  -H "Authorization: Bearer $API_KEY" \
  -F "model=gpt-image-2.5-flare" \
  -F "prompt=Replace the background with a quiet library" \
  -F "image=@input.png;type=image/png" \
  -F "mask=@mask.png;type=image/png" \
  -F "response_format=b64_json"
```

Multipartにはboundaryが必要です。画像なしで編集を実行すると `image file is required` が返されます。`mask` は別の画像としてアップロードされます。透過のセマンティクス、寸法の一致、受け付け可能な形式は、選択したモデルによって検証されます。ゲートウェイはパートのヘッダーだけから寸法を導出しません。

JSON形式の編集も、`"images":[{"image_url":"https://..."}]` として受け付けます。マスクには `{"image_url":"..."}` を使用します。少なくとも1つの `images[].image_url` が必要であり、`images[].file_id` と `mask.file_id` は明示的に拒否されます。

## 非ストリーミングレスポンス

```json
{"created":1789401600,"data":[{"b64_json":"iVBORw0KGgoAAA...","revised_prompt":"A clean product photo of a red desk lamp on white."}],"usage":{"input_tokens":120,"output_tokens":1056,"total_tokens":1176}}
```

結果には代わりに `data[].url` が含まれる場合があります。構造化された `b64_json` フィールドのみをデコードし、base64、MIME、実際の寸法を検証してください。また、大きなペイロードをログに出力しないでください。URLは短期間のみ有効な結果として扱い、速やかに管理下のストレージへ移してください。

## 画像SSE

ネイティブストリームは `image_generation.partial_image` と `image_generation.completed` を送出します（編集パスでは名前が `image_edit.*` に正規化される場合があります）。ペイロードには `b64_json`、`partial_image_index`、`size`、`output_format`、使用量が含まれる場合があります。別のアカウントではResponsesイベントが公開されることもあるため、クライアントはJSONの `type` に基づいて分岐し、`response.*` と `response.image_generation_call.*` イベントを許容してください。

成功とみなせるのは、完了した画像イベント、または有効な画像結果を含む最終レスポンスのみです。HTTPステータスが200であっても、`response.incomplete`、`response.failed`、`error`、空の出力、完了前の切断は失敗または不明として扱います。

## 非同期送信とポーリング

非同期タスクには、管理者が有効化したオブジェクトストレージが必要です。無効な場合、送信は404を返しますが、すでに作成されたタスクはタスクストアが利用可能であればポーリングできます。非同期リクエストでは同期エンドポイントと同じJSONまたはmultipartペイロードを使用し、`stream: true` は拒否されます。

```http
POST /v1/images/generations/async
POST /v1/images/edits/async
GET  /v1/images/tasks/{task_id}
```

受け付けられた送信は、HTTP 202と `Location: /v1/images/tasks/{task_id}`、`Retry-After: 3`、および次のレスポンスを返します。

```json
{"id":"imgtask_abc123","task_id":"imgtask_abc123","object":"image.generation.task","status":"processing","created_at":1789401600,"expires_at":1789488000,"poll_url":"/v1/images/tasks/imgtask_abc123"}
```

同じAPIキーでポーリングしてください。実際のステータスは `processing`、`completed`、`failed` のみです。

```json
{"id":"imgtask_abc123","task_id":"imgtask_abc123","object":"image.generation.task","status":"completed","http_status":200,"image_url":"https://storage.example/result.png","result":{"created":1789401601,"data":[{"url":"https://storage.example/result.png"}]},"created_at":1789401600,"completed_at":1789401601,"expires_at":1789488001}
```

処理中のレスポンスには `Retry-After: 3` が含まれます。失敗したタスクは同期処理のHTTPステータスと `error` を返します。所有者はユーザーとAPIキーで確認され、別のキーを使用すると404が返されます。結果のデフォルトTTLは24時間、実行タイムアウトは30分です。デプロイ設定で両方を上書きできます。このインターフェースではWebhookは提供されません。

## エラーとトラブルシューティング

エラーは `{"error":{"type":"...","message":"..."}}` の形式を使用します。非同期レスポンスでは `error.code` も公開されます。画像ファミリーのモデル、グループ権限、multipartのboundaryとフィールド名、`n`/`stream`/圧縮の型、オブジェクトストレージの有効化、ポーリングに使用した元のキーを確認してください。502/503、空の出力、またはストリームエラーの際は、リクエストID、モデル、エンドポイント、ステータス、最後のイベントタイプを保持し、キー、ソース/マスクデータ、生成されたペイロードは除外してください。

## バッチ画像ジョブ（別API）

```http
POST   /v1/images/batches
GET    /v1/images/batches
GET    /v1/images/batches/models
GET    /v1/images/batches/{id}
GET    /v1/images/batches/{id}/items
GET    /v1/images/batches/{id}/items/{custom_id}/content
POST   /v1/images/batches/{id}/cancel
DELETE /v1/images/batches/{id}
DELETE /v1/images/batches/{id}/outputs
```

少なくとも `model` と空でない `items` を含むJSONを送信してください。各アイテムには `custom_id`、`prompt`、`output_count`、`reference_images` を含めることができます。バッチではさらに `task_name`、`parent_batch_id`、`provider`、`response_mime_type`、`aspect_ratio`、`image_size`、`metadata` を受け付けます。デフォルトは200アイテム、アイテムあたり最大4出力、参照画像1枚あたり10 MiB、バッチあたり1,000件/128 MiBです。デプロイ設定で変更できます。レスポンスには `id`、`status`、`item_count`、`estimated_cost`、`hold_amount` が含まれます。照会には同じキーを使用し、送信の重複排除には `Idempotency-Key` を送信してください。これはゲートウェイのバッチ機能であり、公式のOpenAI Batch APIではありません。
