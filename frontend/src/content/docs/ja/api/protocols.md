## APIキーのグループに使用するプロトコルを選択する

モデルと権限は、APIキーの現在のグループによって異なります。同じキーでモデルを検出し、対象エンドポイントに最小限のリクエストを送信してください。グローバルなモデルカタログや、表示された `/v1/models` の候補だけでは、アカウントがスケジューリング可能であることや、すべてのパラメータが利用できることの証明にはなりません。

| プロトコル | エントリポイント | ゲートウェイ境界 |
| --- | --- | --- |
| OpenAI Chat | `POST /v1/chat/completions` | OpenAI、Grok、Kimi、Zhipu、DeepSeek、MiniMax、OpenCodeGo は OpenAI互換ハンドラーを使用します。その他のグループでは変換が行われる場合があります |
| OpenAI Responses | `POST /v1/responses` | 同じプラットフォームディスパッチを使用します。保護された `/v1/responses/*subpath` では、公式の CRUD パスをすべて公開しているわけではありません |
| Anthropic Messages | `POST /v1/messages` | Anthropic互換グループでは Messages パスを使用します。前記の OpenAI互換グループからブリッジできますが、フィールド単位の同等性は保証されません |
| ネイティブ Gemini REST | `GET /v1beta/models`, `POST /v1beta/models/{model}:{action}` | Gemini グループ。使用できるアクションは `generateContent`、`streamGenerateContent`、`countTokens` のみです |
| 同期/非同期画像 | `/v1/images/generations`, `/edits`, appended `/async`, `GET /v1/images/tasks/{task_id}` | OpenAI/Grok グループのみ。画像権限を持つ対象アカウントに限ります。非同期処理にはオブジェクトストレージが必要です |
| 埋め込み | `POST /v1/embeddings` | OpenAI プラットフォームのグループのみ。その他のプラットフォームでは 404 を返します |
| Grok メディア/検索/音声 | `/v1/videos...`, `/v1/web_search`, `/v1/x_search`, `/v1/tts`, etc. | 主に Grok のみ。一部の動画作成/参照では、Grok に解決される複合グループを使用できます |
| モデル検出 | `GET /v1/models`, `GET /v1/models/{model}` | キーにスコープされた候補。`?client_version=...` を指定すると、別の Codex マニフェストが生成されます |
| トークン数のカウント | `/v1/messages/count_tokens`, `/v1/responses/input_tokens`, Gemini `:countTokens` | アップストリームに転送するか、ローカルで推定できます。ただし、最終的な生成時の使用量を正確に予測するものではありません |

## 認証と制限

OpenAI スタイルのクライアントでは `Authorization: Bearer $TOKENSAVY_API_KEY` を使用し、Anthropic SDK では `x-api-key` と `anthropic-version` を使用します。Gemini SDK では `x-goog-api-key` を使用します。Gemini の認証では Bearer、`x-api-key`、クエリパラメータ `key` も使用できますが、URL に含めたキーはプロキシのログに漏洩する可能性があります。各エンドポイントには、該当するデプロイのリクエストボディ制限とグループの許可リストが適用され、必要に応じて請求、同時実行数、コンテンツポリシーも適用されます。ツール、画像、スキーマ出力、キャッシュ、組み込み機能は、実際のアカウントとアップストリームモデルによって異なります。

複数の `/v1` ルートにはプレフィックスなしの互換エイリアスがあります。新しい統合では `/v1` を使用してください。`/backend-api/codex/*` と `/antigravity/*` はクライアント/プラットフォーム固有です。`GET /v1/responses` には WebSocket upgrade が必要で、Retrieve Response ではありません。通常の GET は 426 を返します。

## レスポンスとエラー

OpenAI スタイルのエラーは通常 `{"error":{"type":"...","message":"..."}}` を使用し、Anthropic は `{"type":"error","error":{...}}`、Gemini は `{"error":{"code":400,"message":"...","status":"INVALID_ARGUMENT"}}` を使用します。SSE クライアントは、プロトコルの終端イベントを待機する必要があります。ストリーム内のエラーや早期切断が発生した場合、結果は不明または途中で切り捨てられた状態です。生成を再試行すると、出力やツールの作用が重複する可能性があるため、アプリケーション側で独自の冪等性戦略を設計する必要があります。

これはソースゲートウェイの仕様を示すものであり、動的なグループについて本番環境での可用性を保証するものではありません。プラットフォームディスパッチは `backend/internal/server/routes/gateway.go` にあり、ハンドラーは `backend/internal/handler/gateway_handler*.go`、`openai_gateway_handler.go`、`gemini_v1beta_handler.go`、`grok_media.go`、`grok_audio.go` にあります。
