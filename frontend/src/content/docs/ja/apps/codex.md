## キーとモデルを準備する

[API keys](/keys) でキーを作成し、グループを選択して **Use key → Codex** を開きます。生成された設定を使用することを推奨します。モデルカタログは OpenAI/Composite グループでのみサポートされています。例の `gpt-6-astra` を有効なモデルに置き換えてください。既存の設定をバックアップし、以下のフィールドをマージしてください。ファイル全体を置き換えないでください。HTTP/SSE と WebSocket の設定はコンソール上の別々のタブにあります。まずは HTTP/SSE から始めてください。

OpenAI/Composite グループでは、**Use key** を開くとデフォルト設定をすぐに表示します。「モデル制限（任意）」のローカル候補一覧から、Codex で利用できる最上位のクラスとバージョン（現在は `gpt-6-astra`）を選び、`model` と `review_model` の両方に設定します。この一覧はフォームの組み込み候補であり、アカウントで選択済みの制限や現在のキーで利用できるモデルをリアルタイムに確認した結果ではありません。ダイアログを開いたりキーを切り替えたりしてもカタログを自動取得せず、バックグラウンド更新やカタログキャッシュの永続保存も行いません。

このガイドでは、Codex の設定を読み取るローカルクライアントについて説明します。リモートホスト、WSL、コンテナでは実行場所に設定が必要です。クラウドタスクではローカルファイルが使用されない場合があります。

## CLI のセットアップ

### オンラインインストール（macOS / Linux）

```bash
export TAPMODELS_API_KEY="YOUR_TAPMODELS_API_KEY"
curl -fsSL https://tapmodels.ai/install/codex.sh | bash
```

このスクリプトは `config.toml` をバックアップし、Responses プロバイダーを書き込み、モードを `600` に設定します。シェルにパイプする前に内容を確認してください。キーをシェル履歴やソース管理に決して保存しないでください。カスタムゲートウェイを使用する場合は `TAPMODELS_BASE_URL` を設定し、例のモデルを上書きする場合は `TAPMODELS_MODEL` を設定します。

このスクリーンショットは、無効なサンプル認証情報と例の URL が設定された、プロジェクトの OpenAI グループ API キー設定を示しています。自身のグループ設定をコピーしてください。[コンソールガイド](/apps/console) では Legacy モードと手順も説明しています。画像をクリックするとフルサイズで表示できます。

![サンプルデータを使用したプロジェクトの Codex API キー設定](/docs-assets/client-codex-en.png)

[公式手順](https://developers.openai.com/codex/cli/) に従って CLI をインストールし、`codex --version` で確認します。

macOS / Linux:

```bash
export TAPMODELS_API_KEY="your TapModels API key"
mkdir -p "${CODEX_HOME:-$HOME/.codex}"
```

Windows PowerShell:

```powershell
$env:TAPMODELS_API_KEY="your TapModels API key"
$codexConfigDir = if ($env:CODEX_HOME) { $env:CODEX_HOME } else { Join-Path $env:USERPROFILE '.codex' }
New-Item -ItemType Directory -Force $codexConfigDir | Out-Null
notepad (Join-Path $codexConfigDir 'config.toml')
```

このルーティングされたグループの例を `~/.codex/config.toml`、または `CODEX_HOME` ディレクトリにマージしてください。これは、Responses 経由で Codex にルーティングされる Anthropic、Gemini、Grok、その他のグループに適用されます。トップレベルのフィールドはテーブルより前に記述し、各プロバイダーテーブルは 1 回だけ記述してください。現在のモデルと `base_url` はこの例から推測せず、モーダルからコピーしてください。

```toml
model_provider = "tapmodels"
model = "gpt-6-astra"

[model_providers.tapmodels]
name = "TapModels"
base_url = "{{API_ROOT}}/v1"
env_key = "TAPMODELS_API_KEY"
wire_api = "responses"
requires_openai_auth = false
supports_websockets = false
```

同じターミナルで `codex` を実行します。WebSockets を無効にすると HTTP/SSE の開始点になりますが、ゲートウェイに WebSocket のルートがないという意味ではありません。

OpenAI グループでは、異なる形式が生成されます。プロバイダー ID `OpenAI`、メインモデルとレビューモデル、`[features]`、リモートカタログのアドレス `model_catalog_url` が含まれ、コピーするだけで使えます。ダイアログの既定は **Codex CLI (WebSocket)** と **API key** モードで、`requires_openai_auth = false` と `experimental_bearer_token` が設定されるため `config.toml` だけで済みます。**Legacy** モードでは `requires_openai_auth = true` が設定され、`auth.json` もダウンロードされます。切り替え後は Codex を完全に再起動してください。2 つのモードを組み合わせたり、ルーティングされた `tapmodels` プロバイダーテーブルを OpenAI グループの `OpenAI` プロバイダーにマージしたりしないでください。

## デスクトップ: キーを利用可能にする

Dock またはスタートメニューから起動したデスクトップアプリは、別のターミナルでエクスポートした変数を自動的には継承しません。既存のプロセスを終了し、キーを含むターミナルからインストール済みの実行ファイルを直接起動してください。macOS に `/Applications/Codex.app` としてインストールされている場合:

```bash
"/Applications/Codex.app/Contents/MacOS/Codex"
```

最初に実際のインストール先を確認してください。Windows では、`$env:TAPMODELS_API_KEY` を含む PowerShell からインストール済みの Codex `.exe` を呼び出します。

アイコンから起動する場合は、OpenAI グループの Use key モーダルで **API key** を選択し、完全な `config.toml` をダウンロードしてください。これによりファイル内にシークレットが保存されます。アクセスを制限し、決してコミットしないでください。その他のグループでは、生成された `env_key` 設定をそのまま使用し、自分で 2 つ目の認証フィールドを追加しないでください。

## グループモデルカタログ

このセクションは OpenAI/Composite グループにのみ適用されます。その他のルーティングされたグループには専用カタログがないため、`model_catalog_url` を設定しないでください。通常の `GET /v1/models` を実行して `model` に完全一致する ID を設定してください。そのリストのレスポンスを Codex マニフェストとして扱わないでください。

生成される `config.toml` のプロバイダーテーブルには次が含まれます。

```toml
model_catalog_url = "{{API_ROOT}}/v1/models"
```

Codex 0.156.0 以降は起動時に現在のキーでこのアドレスを読み込み、このサイトがキーごとに算出したカタログを取得します。グループのアクセス権が変わったら Codex を再起動すると新しいカタログが反映されます。「接続確認 / このキーで使えるモデルを表示」は確認用で、ダイアログの `model`、`review_model`、推論設定を更新する場合がありますが、必須手順ではありません。ローカルのカタログファイルはサポートを終了し、`codex-models.json` のダウンロードと更新スクリプトは削除されました。古い設定の `model_catalog_json` は削除してください。権限を回避するためにモデル名を編集しないでください。

## 確認とトラブルシューティング

新しいタスクで簡単なメッセージを送信し、TapModels の使用量レコードでタイムスタンプ、キー、モデルが一致することを確認します。その後、既存の作業を再開してください。履歴が消えた場合は、[Codex セッションの復旧](/apps/session-recovery-codex) を参照してください。

| 症状 | 確認項目 |
| --- | --- |
| 401 / API_KEY_REQUIRED | 実際のプロセスにキー変数があるか、アクティブなプロバイダーにトークンがあるか |
| 不明なモデル | キーのグループ、完全一致する ID、古いカタログ |
| 設定が無視される | CODEX_HOME、選択したプロファイル、プロジェクトの上書き設定、リモートホスト |
| Speed/Fast コントロールがない | カタログの機能。請求倍率によってクライアントのコントロールが作成されることはありません |
| 古いタスクが見つからない | 元のプロジェクト、アーカイブ、プロバイダー、ローカルデータディレクトリ |

プロジェクトの Use key ジェネレーターと [Codex 設定リファレンス](https://developers.openai.com/codex/config-reference/) に基づき、2026-09-15 に確認済み。
