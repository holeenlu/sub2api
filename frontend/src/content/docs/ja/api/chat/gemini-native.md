## サポートされているネイティブ エンドポイント

```http
GET  /v1beta/models
GET  /v1beta/models/{model}
POST /v1beta/models/{model}:generateContent
POST /v1beta/models/{model}:streamGenerateContent?alt=sse
POST /v1beta/models/{model}:countTokens
x-goog-api-key: $TOKENSAVY_API_KEY
```

通常の `/v1beta` サーフェスは Gemini グループのみを受け付けます。別の `/antigravity/v1beta` プレフィックスを指定すると Antigravity プラットフォームが強制的に選択されます。これは、通常の Gemini グループが同じアカウントやモデルを持つことを意味しません。

ハンドラーで許可されているのは `generateContent`、`streamGenerateContent`、`countTokens` のみです。`embedContent`、バッチ埋め込み、キャッシュ済みコンテンツ、ファイル、チューニングは、このゲートウェイ サーフェスでは実装されていません。ワイルドカード ルートが URL に一致する場合でも、アクションは拒否されます。

## モデルの検出

```bash
curl "$TOKENSAVY_BASE_URL/v1beta/models" -H "x-goog-api-key: $TOKENSAVY_API_KEY"
```

レスポンスでは Gemini の `models[]` エンベロープが使用されます。`name` は通常 `models/YOUR_MODEL_ID` です。項目はアップストリームから取得される場合と、選択したアカウント タイプでモデルを検出できない場合にコードで定義されたフォールバックから取得される場合があり、その後グループの許可リストを通過します。表示されていることは、実際にスケジューリング可能であることの証明にはなりません。

## generateContent リクエスト

```bash
curl "$TOKENSAVY_BASE_URL/v1beta/models/YOUR_MODEL_ID:generateContent" \
  -H "x-goog-api-key: $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "systemInstruction":{"parts":[{"text":"Answer briefly."}]},
    "contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}],
    "generationConfig":{"temperature":0.2,"maxOutputTokens":256}
  }'
```

| フィールド | 必須 | 注記 |
| --- | --- | --- |
| URL `{model}` | yes | このキーで許可されている ID。パス文字には英字、数字、`_`、`-`、`.` のみ使用できます |
| `contents` | yes | `role` と空でない `parts` を含む会話配列 |
| `parts[].text` | task-dependent | テキスト。マルチモーダルな `inlineData`/`fileData` にはアカウントおよびモデルのサポートが必要です |
| `systemInstruction` | no | Gemini のトップレベル システム指示 |
| `generationConfig` | no | 温度、出力上限、停止シーケンス、レスポンス MIME/スキーマ。対象によって異なります |
| `tools` / `toolConfig` | no | 関数宣言/選択。結果は `functionResponse` パートとして返します |
| `safetySettings` | no | 受け付けられる値はアカウント タイプとアップストリーム モデルによって異なります |

ゲートウェイは `parts` が空のメッセージを削除し、厳格なアップストリームに対応するため、関数呼び出しの思考シグネチャを追加する場合があります。クライアント側でも、引き続き正規の入力を送信してください。`candidates[]` と `content.parts` を反復処理し、その後 `finishReason`、`usageMetadata`、および存在する場合は `promptFeedback` を確認します。空の `candidates` や通常とは異なる終了理由は、完全な成功を示すものではありません。

## SSE ストリーミング

```bash
curl -N "$TOKENSAVY_BASE_URL/v1beta/models/YOUR_MODEL_ID:streamGenerateContent?alt=sse" \
  -H "x-goog-api-key: $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"contents":[{"role":"user","parts":[{"text":"Explain idempotency."}]}]}'
```

URL のアクションによってストリーミングが選択されます。ボディに `stream` スイッチはありません。各 `data:` は Gemini レスポンスのフラグメントです。候補インデックスとパートごとに蓄積し、最後のフラグメントから終了理由と使用量を読み取ります。完全な終端フラグメントを受信する前に切断された場合、レスポンスが途中で切れている可能性があります。一部の OAuth/Code Assist アカウントは、ストリーミングでない入力に対しても内部的にストリーミングして集約するため、下流の形式からアップストリームのトランスポートを判断することはできません。

## カウント、エラー、制限

`:countTokens` は `contents` ボディを使用し、`totalTokens` を返します。Antigravity OAuth アカウントのパスは現在、入力をカウントせずプレースホルダーの `0` を返します。キャパシティ計画や正確な見積もりには使用しないでください。エラーには Google のエンベロープが使用されます: `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}`。一般的な原因には、キーとグループの不一致、安全でないモデル パス、サポートされていないアクション、空のボディ、許可リスト ポリシー、クォータ/同時実行数、アップストリーム障害などがあります。

`backend/internal/server/routes/gateway.go`、`handler/gemini_v1beta_handler.go`、`service/gemini_messages_compat_service.go`、`service/antigravity_gateway_gemini.go`、`service/vertex_service_account.go` を参照してください。
