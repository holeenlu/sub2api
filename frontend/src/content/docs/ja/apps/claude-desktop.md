## 対象範囲

このガイドは、**Third-Party Inference** 設定を備えた Claude デスクトップ版ビルドに適用されます。デスクトップのルーティングは Claude Code CLI とは別であり、シェルのエクスポートや `~/.claude/settings.json` では設定できません。

以下の手順は、公式ドキュメントと Tokensavy の Messages ルートに基づいています。デスクトップから本番用キーへの実際の接続テストは完了していません。お使いのビルドにこの設定がない場合は、[Claude Code](/apps/claude-code) を使用してください。

## 接続

1. Help → Troubleshooting → Enable Developer Mode を開き、再起動を求められたら再起動します。
2. Developer → Configure Third-Party Inference を開きます。
3. **Gateway** を選択し、以下の値を入力します。
4. アプリの指示に従って保存または適用し、ローカルセッションを開いてテストメッセージを送信します。

| 項目 | 値 |
| --- | --- |
| Gateway のベース URL | `{{API_ROOT}}` |
| 認証情報の種類 | 静的 API キー |
| Gateway API キー | Tokensavy のキー |
| Gateway の認証方式 | Bearer。プロジェクトでは x-api-key も受け付けます |
| モデル | キーのグループで有効化されている Messages 互換モデル |

管理対象の設定によってフォームが読み取り専用になる場合があります。その場合は管理者に問い合わせてください。Gateway のキーを OAuth または OIDC のサインインフィールドに入力しないでください。

## 検証

Tokensavy の使用量記録でテストリクエストと一致することを確認します。モデルへのアクセス権はキーのグループに従います。Messages 互換であることは、すべてのデスクトッププラグインやクラウド機能との互換性を保証するものではありません。

Gateway に到達できない場合は、ルート URL とネットワークを確認してください。401 が返る場合は、キーを確認してください。モデルが見つからない場合は、グループのアクセス権とクライアント側で明示的に設定されたモデルを確認してください。リモートまたはクラウドの制限については、公式のガイダンスを参照してください。公式アカウントのクラウド履歴とローカル Gateway セッションが移行される保証はありません。当社の修復パッケージが処理するのは Codex のローカルインデックスのみです。

参考資料: [Claude desktop gateway](https://claude.com/docs/third-party/claude-desktop/gateway)、[client differences](https://code.claude.com/docs/en/llm-gateway-connect#desktop-app)、確認日 2026-09-15。
