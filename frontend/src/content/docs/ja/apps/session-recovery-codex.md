既存のローカルセッションを再開するための手順です。通信障害や削除済みのメッセージは復旧できません。Claude Code は [専用ツール](/apps/session-recovery-claude) を使用してください。

## 準備と復旧方法の選択

[API キー](/keys) から現在のグループの有効な設定をコピーし、新しいセッションが動作することを確認します。401、503、残高不足、上流エラーは設定またはサーバー側の調査が必要です。元のセッションディレクトリは保持してください。

Python 3.11 以降と Codex CLI が必要です。[ツールをダウンロード](/downloads/tapmodels-codex-session-repair.zip)して展開し、tapmodels-codex-session-repair に移動します。macOS、Linux、Windows に対応します。

## セッションを探して再開コマンドを確認

最初に読み取り専用のスキャンを実行します。SQLite インデックスがなくても sessions と archived_sessions を直接確認します。メッセージ本文やキーは出力しません。レポートには ID とローカルパスが含まれるため、共有前に確認してください。

```bash
bash repair-sessions.sh --list
```


```powershell
.\Repair-TapModelsSessions.ps1 -List
```


レポート内の ID を指定します。プロジェクトを移動した場合は現在のパスを指定してください。次のコマンドはプレビューのみで、クライアントを起動しません。

```bash
bash repair-sessions.sh --resume "SESSION_ID" --project-dir "/path/to/project"
```


```powershell
.\Repair-TapModelsSessions.ps1 -Resume "SESSION_ID" -ProjectDir "C:\path\to\project"
```


現在の config.toml と選択中の profile の Provider・モデルを、今回の CLI 起動用パラメーターとして指定します。--provider は設定済み Provider ID、--model はキーで利用できるモデルを指定します。ID は大文字と小文字を区別します。設定の不足や構文エラーは先に修正してください。

Provider、モデル、ディレクトリを確認後、--run を追加すると CLI セッションを開きます。プロンプトは自動送信しません。会話を続けると通常の API 使用量が発生します。デスクトップの Provider フィルター、履歴、アーカイブ状態は書き換えません。

## インデックスのパスが無効な場合

JSONL は存在するのにインデックスが古いディレクトリを指す場合は、次の診断を実行します。CODEX_HOME または ~/.codex を使用し、--codex-home で別の場所を指定できます。state_*.sqlite が複数ある場合は --database で現在のクライアントのデータベースを明示してください。

```bash
bash repair-sessions.sh --dry-run
```


repairable_rollout_paths を確認し、そのディレクトリを使用する Codex デスクトップ、CLI、IDE をすべて終了してから適用します。

```bash
bash repair-sessions.sh --apply --client-closed
```


```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```


JSONL 内のセッション ID で一意の候補を検証するため、ファイル名の変更や OS 間の移動に対応します。一貫した SQLite バックアップを作成し、rollout_path のみを変更します。重複候補、シンボリックリンク、不明なスキーマ、破損に対して推測で変更しません。

## 結果の確認とロールバック

repaired_rollout_paths が 0 の場合は変更がなかったという意味で、復旧成功を意味しません。診断を再実行し、セッションを開いて確認してください。ロールバックをプレビューし、クライアントを終了して適用します。

```bash
bash repair-sessions.sh --rollback "/path/from/backup/report"
bash repair-sessions.sh --rollback "/path/from/backup/report" --apply --client-closed
```


PowerShell は -Database、-CodexHome、-Rollback、-Apply、-ClientClosed を使用します。再開用は -Resume、-Provider、-Model、-ProjectDir、-Run です。ロールバックはその修復で変更し、現在も一致するパスだけを戻し、後の無関係な変更は保持します。

削除済みの会話、別のホストまたは公式クラウドだけに保存された会話は、元のホストや自身のバックアップが必要です。不明なデータベース形式は報告のみで、強制的に変更しません。

[Codex CLI resume](https://developers.openai.com/codex/cli/reference/) · 2026-10-02
