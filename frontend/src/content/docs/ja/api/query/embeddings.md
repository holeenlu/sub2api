## エンドポイントとプラットフォーム

```http
POST /v1/embeddings
Authorization: Bearer $TOKENSAVY_API_KEY
Content-Type: application/json
```

ゲートウェイのルートでは、OpenAI プラットフォームのグループのみ利用できます。その他のグループには、`Embeddings API is not supported for this platform` というメッセージを含む 404 `not_found_error` が返されます。また、モデルはそのグループで有効化されており、埋め込みに対応した利用可能なアカウントと請求容量が必要です。`/v1/models` に表示される ID だけでは、それが埋め込みモデルであることの証明にはなりません。

## リクエストとレスポンス

```bash
curl "$TOKENSAVY_BASE_URL/v1/embeddings" \
  -H "Authorization: Bearer $TOKENSAVY_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"model":"YOUR_EMBEDDING_MODEL_ID","input":"Idempotency means repeatable effects."}'
```

`model` は必須の空でない文字列です。`input` には、対象の OpenAI 互換埋め込みモデルが許可する形式で、テキストまたは配列を指定できます。`encoding_format` や `dimensions` などのその他のフィールドは、アップストリームのサポート状況によって異なります。ゲートウェイは、すべてのアカウントでこれらが利用できることを保証しません。まずは正確な ID を使用して、1 件のテキスト入力を検証してください。

説明用のレスポンス（実際のベクトル次元数ではありません）:

```json
{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.012,-0.008]}],"model":"YOUR_EMBEDDING_MODEL_ID","usage":{"prompt_tokens":8,"total_tokens":8}}
```

バッチ入力は `index` に基づいて対応付け、ベクトルの次元数と有限値であることを検証してください。これは同期 JSON エンドポイントであり、Chat SSE ではありません。

## エラーと実装

空または無効な JSON、あるいは `model` がない場合は、400 `invalid_request_error` が返されます。無効な API キーの場合は 401、プラットフォームが一致しない場合は 404 が返されます。利用可能な埋め込みアカウントがない場合やアップストリームで障害が発生した場合は、スケジューリングエラーまたはアップストリームエラーになることがあります。ハンドラーはモデルマッピング、コンテンツポリシー、同時実行数、請求の利用資格を適用したうえで、Embeddings エンドポイント機能を持つアカウントを選択します。

プラットフォームの制限は `backend/internal/server/routes/gateway.go`、検証と選択は `backend/internal/handler/openai_embeddings.go`、転送は `backend/internal/service/openai_embeddings.go` にあります。このページでは、本番グループの検証やベクトルレスポンスの検証については保証していません。
