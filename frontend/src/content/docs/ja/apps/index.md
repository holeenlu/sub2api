## APIキーからアプリへ

TapModelsは、OpenAI Responses、Chat Completions、Anthropic Messages、画像APIに互換対応しています。アプリによってラベルは異なりますが、すべてのインテグレーションで必要なのは、コンソールに表示されるAPIベースURL、TapModelsのAPIキー、そのキーのグループで有効化されたモデルIDという3つの実値です。

| アプリ | プロトコル | ベースURL | 認証 |
| --- | --- | --- | --- |
| Codex desktop / CLI | Responses | `/v1` を含むコンソールAPI URL | 生成されたLegacy、APIキー、またはルーティング対象グループの環境変数モード |
| Claude Code | Messages | `/v1/messages` を含まないコンソールAPIルート | `ANTHROPIC_AUTH_TOKEN` |
| OpenAI SDK | OpenAI互換 | `/v1` を含むコンソールAPI URL | Bearer APIキー |

まず [コンソール設定ビルダー](/apps/console) で、選択したキーグループがサポートするクライアント出力を確認し、その後、[Codex](/apps/codex)、[Claude Code](/apps/claude-code)、または [Claude Desktop](/apps/claude-desktop) の完全なガイドに従ってください。画像の生成と編集には、別途 [画像スキル](/apps/image-skills) を使用します。

## 専用キーを作成する

[APIキー](/keys) を開き、使用するグループを選択します。このスクリーンショットはTapModelsのもので、オプションはインストールされているバージョンによって異なります。

![TapModelsのAPIキーを作成し、そのグループを選択する](/docs-assets/create-api-key.png)

## 推奨セットアップ手順

1. コンソールでキーを作成し、そのグループを記録します。
2. そのキーを使って `GET /v1/models` を呼び出し、正確なモデルIDをコピーします。
3. アプリのベースURL、キー、モデルを設定します。
4. アプリを完全に終了して再起動し、新しいテストセッションを作成します。
5. TapModelsの使用量記録で、リクエスト、モデル、コストを確認します。

切り替え後にセッション一覧が空になる場合は、プロジェクト選択、アーカイブ、プロバイダーのフィルタリング、データディレクトリ、または古いインデックスパスが原因の可能性があります。対象クライアントについて [Codexセッションの復旧](/apps/session-recovery-codex) または [Claude Codeセッションの復旧](/apps/session-recovery-claude) に従い、修復を適用する前に読み取り専用の診断を実行してください。ダウンロードとチェックサムは [ダウンロード](/apps/downloads) にまとめられています。
