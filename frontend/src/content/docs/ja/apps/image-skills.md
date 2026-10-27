## ダウンロード可能なスキル

Tokensavy は 2 つの個別スキルを提供します。

| スキル | 固定モデル | 生成 | 編集 |
| --- | --- | --- | --- |
| `gpt-image-flare` | `gpt-image-2.5-flare` | `/v1/images/generations` | `/v1/images/edits` |
| `gpt-image-sunburst` | `gpt-image-2.5-sunburst` | `/v1/images/generations` | `/v1/images/edits` |

[Flare スキル](/downloads/gpt-image-flare.zip) または [Sunburst スキル](/downloads/gpt-image-sunburst.zip) をダウンロードします。各バンドルには `SKILL.md`、Codex インターフェースメタデータ、Python リクエストスクリプトが含まれています。

## Codex デスクトップにインストールする

1. Codex を完全に終了します。
2. バンドルを展開し、最上位ディレクトリが `gpt-image-flare/` または `gpt-image-sunburst/` であり、その直下に `SKILL.md` があることを確認します。
3. そのディレクトリを `~/.agents/skills/` 配下に移動します。最終的なパスは `~/.agents/skills/gpt-image-flare/SKILL.md` または `~/.agents/skills/gpt-image-sunburst/SKILL.md` です。
4. Codex を再起動し、タスク内で `$gpt-image-flare` または `$gpt-image-sunburst` を呼び出します。

## Codex CLI にインストールする

macOS または Linux:

```bash
mkdir -p "$HOME/.agents/skills"
unzip -q -o ./gpt-image-flare.zip -d "$HOME/.agents/skills"
test -f "$HOME/.agents/skills/gpt-image-flare/SKILL.md"
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" "$HOME/.agents/skills/gpt-image-flare/scripts/generate.py" --check-config
```

Windows PowerShell:

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.agents\skills" | Out-Null
Expand-Archive -Force .\gpt-image-flare.zip "$env:USERPROFILE\.agents\skills"
if (-not (Test-Path "$env:USERPROFILE\.agents\skills\gpt-image-flare\SKILL.md")) { throw "Invalid skill archive layout" }
py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" -m pip install -r "$env:USERPROFILE\.agents\skills\gpt-image-flare\requirements.txt"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" "$env:USERPROFILE\.agents\skills\gpt-image-flare\scripts\generate.py" --check-config
```

このスクリプトは、トップレベルの Codex `model_provider` / `model_providers` から `base_url` と `env_key` を読み取ります。プロファイルベースのプロバイダー選択には対応しておらず、生成ファイルに API キーを配置することもありません。`TOKENSAVY_API_KEY` のみが設定されている場合でも、プロバイダーは URL と設定済みの環境変数名の両方を提供します。プロバイダーを意図的に上書きする場合にのみ `TOKENSAVY_BASE_URL` を設定してください。この上書きには `TOKENSAVY_API_KEY` も必要です。プロバイダー URL には HTTPS を使用する必要があります（HTTP はループバックテストの場合に限り使用できます）。リダイレクトは拒否されます。

## ランタイムと認証情報

画像スクリプトは、Codex のログインファイルとは独立して、以下の認証情報ソースを使用します。

| Codex 設定 | 画像スクリプトの動作 |
| --- | --- |
| プロバイダーに `env_key` がある | その変数を使用し、未設定の場合は失敗する |
| プロバイダーに `experimental_bearer_token` がある | そのプロバイダーのトークンを使用する |
| レガシーキーが `auth.json` にのみ存在する | このファイルは読み取られないため、以下の明示的な環境変数による上書きを使用する |

レガシー認証または個別の画像グループを使用する場合は、スキルを実行するプロセスで両方の値を設定します。キーは、画像モデルが有効になっているグループに属している必要があります。キーのみを設定しても、プロバイダーの認証モードは置き換えられません。

```bash
export TOKENSAVY_BASE_URL="{{API_ROOT}}"
export TOKENSAVY_API_KEY="YOUR_IMAGE_GROUP_API_KEY"
```

PowerShell では `$env:TOKENSAVY_BASE_URL="{{API_ROOT}}"` と `$env:TOKENSAVY_API_KEY="YOUR_IMAGE_GROUP_API_KEY"` を使用します。[Codex ガイド](/apps/codex)の説明に従い、そのターミナルから Codex デスクトップを起動してください。

Python 3.11 以降を使用し、同梱の `requirements.txt` から Pillow をインストールします。スキルディレクトリ内の仮想環境を使用することを推奨します。Windows では `py -3` と環境の `Scripts/python.exe` を使用し、macOS/Linux では環境の `bin/python` を使用します。

明示的な `TOKENSAVY_BASE_URL` を設定すると、環境全体の上書きが有効になり、`TOKENSAVY_API_KEY` が必要になります。設定しない場合、スクリプトは上記に記載したアクティブな Codex プロバイダーの URL と認証情報フィールドを読み取ります。設定済みのキー変数がない場合は、別のアカウントのキーを借用せずに失敗します。`--check-config` は設定のみを検証し、`--dry-run` はネットワーク呼び出しなしでペイロードと入力画像を検証します。実際の呼び出しには、認証情報、ネットワーク、モデルへのアクセス権が必要です。出力パスには絶対パスを使用してください。既存のファイルを上書きするには、明示的に `--force` が必要です。

現在の公式ガイダンスでは `~/.agents/skills` が推奨されています。古いクライアントでは `~/.codex/skills` が検出される場合があります。重複した名前を作成せず、実際にクライアントがサポートしているパスを使用してください。`$gpt-image-flare` と入力して検出を確認し、必要に応じて再起動します。

## スキルを使用する

画像を生成します。

```text
$gpt-image-flare Generate a precise product image on white, 1024x1024, high quality.
```

編集する場合は、ローカル画像を添付し、要求する変更と維持する内容の両方を指定します。入力画像がある場合、スキルは編集エンドポイントを使用します。実行ごとに請求対象となるリクエストを 1 件送信し、自動的に再試行することはありません。結果については、実際のファイル形式と画像サイズが検証されます。

インストール元: [Codex スキル](https://developers.openai.com/codex/skills/)。スクリプトはシミュレートされたリクエストを使ってローカルで検証されます。実際に利用できるかどうかはキーグループに依存します。
