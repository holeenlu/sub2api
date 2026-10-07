## クライアントツール

ダウンロードする前に、[コンソール設定ビルダー](/apps/console) と [アプリガイド](/apps) を読み、キーグループとクライアントのサポート状況を確認してください。

| ファイル | 用途 | デフォルトの動作 |
| --- | --- | --- |
| [TapModels Codex session repair bundle](/downloads/tapmodels-codex-session-repair.zip) | macOS、Linux、Windows における Codex セッションの診断と修復 | 読み取り専用。書き込みには明示的な Apply が必要 |
| [TapModels Claude Code session recovery bundle](/downloads/tapmodels-claude-session-recovery.zip) | ローカルの Claude セッションを検索し、正確な再開コマンドを生成 | 読み取り専用。Claude の起動やセッションの変更は行わない |
| [GPT Image 2.5 Flare skill](/downloads/gpt-image-flare.zip) | Codex による画像生成と編集 | Flare モデルに固定 |
| [GPT Image 2.5 Sunburst skill](/downloads/gpt-image-sunburst.zip) | Codex による画像生成と編集 | Sunburst モデルに固定 |

## ダウンロード後の確認

これらのアーカイブは、このサイトが公開している静的アセットです。インストールする前に展開し、`README.md` またはスキルのトップレベルにある `SKILL.md` を確認してください。どのスクリプトにも APIキーを含めないでください。Codex バンドルはローカルの Codex データに対してのみ動作します。Claude バンドルはローカルの Claude セッションを読み取り専用でスキャンし、再開コマンドを出力します。画像スキルは、アクティブな Codex プロバイダーを使用して、明示的に指定された請求対象のリクエストを 1 件実行します。

展開する前に、ダウンロードしたアーカイブを [SHA256SUMS.txt](/downloads/SHA256SUMS.txt) と照合してください。

チャットの添付ファイル、ファイル共有ミラー、または無関係なサイトから、同名のスクリプトをインストールしないでください。アップグレードする前に、以前のスキルディレクトリと `~/.codex` のバックアップを保持してください。
