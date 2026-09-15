## 可下載 Skills

TapModels 提供兩個獨立 Skill：

| Skill | 固定模型 | 生成 | 編輯 |
| --- | --- | --- | --- |
| `gpt-image-flare` | `gpt-image-2.5-flare` | `/v1/images/generations` | `/v1/images/edits` |
| `gpt-image-sunburst` | `gpt-image-2.5-sunburst` | `/v1/images/generations` | `/v1/images/edits` |

下載 [Flare Skill](/downloads/gpt-image-flare.zip) 或 [Sunburst Skill](/downloads/gpt-image-sunburst.zip)。每個包包含 `SKILL.md`、Codex 展示後設資料與 Python 呼叫腳本。

## Codex 桌面版安裝

1. 完全退出 Codex。
2. ZIP 自帶 `gpt-image-flare/` 或 `gpt-image-sunburst/` 頂層目錄；該目錄內直接包含 `SKILL.md`。
3. 把整個目錄放入 `~/.agents/skills/`，最終路徑應為 `~/.agents/skills/gpt-image-flare/SKILL.md` 或 `~/.agents/skills/gpt-image-sunburst/SKILL.md`。
4. 重啟 Codex，在任務中輸入 `$gpt-image-flare` 或 `$gpt-image-sunburst`。

## Codex CLI 安裝

macOS / Linux 範例：

```bash
mkdir -p "$HOME/.agents/skills"
unzip -q -o ./gpt-image-flare.zip -d "$HOME/.agents/skills"
test -f "$HOME/.agents/skills/gpt-image-flare/SKILL.md"
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" "$HOME/.agents/skills/gpt-image-flare/scripts/generate.py" --check-config
```

Windows PowerShell 範例：

```powershell
New-Item -ItemType Directory -Force "$env:USERPROFILE\.agents\skills" | Out-Null
Expand-Archive -Force .\gpt-image-flare.zip "$env:USERPROFILE\.agents\skills"
if (-not (Test-Path "$env:USERPROFILE\.agents\skills\gpt-image-flare\SKILL.md")) { throw "壓縮包目錄結構無效" }
py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" -m pip install -r "$env:USERPROFILE\.agents\skills\gpt-image-flare\requirements.txt"
& "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv\Scripts\python.exe" "$env:USERPROFILE\.agents\skills\gpt-image-flare\scripts\generate.py" --check-config
```

腳本讀取目前 Codex Provider 的 `base_url` 和 `env_key`，不會把 Key 寫入生成檔案。僅設定 `TAPMODELS_API_KEY` 時，URL 和環境變數名仍由 Provider 提供；只有明確設定 `TAPMODELS_BASE_URL` 才啟用完整環境覆蓋，此時也必須設定 `TAPMODELS_API_KEY`。

## 依賴與認證資訊

要求 Python 3.11+ 和 Pillow。在 Skill 目錄建立虛擬環境並安裝隨包列明的依賴：

```bash
python3 -m venv "$HOME/.agents/skills/gpt-image-flare/.venv"
"$HOME/.agents/skills/gpt-image-flare/.venv/bin/python" -m pip install -r "$HOME/.agents/skills/gpt-image-flare/requirements.txt"
```

Windows 用 `py -3 -m venv "$env:USERPROFILE\.agents\skills\gpt-image-flare\.venv"`，再使用 `.venv\Scripts\python.exe` 執行相同的 `-m pip install -r` 與腳本命令。Sunburst 將路徑中的目錄名對應替換。

明確設定 `TAPMODELS_BASE_URL` 時啟用完整環境覆蓋，並要求同時設定 `TAPMODELS_API_KEY`；沒有該 URL 時讀取目前 Codex Provider 的 `base_url` 和 `env_key`，因此只設置 `TAPMODELS_API_KEY` 也可用。缺少指定環境變數會錯誤，不回退到其他帳號 Key。`--check-config` 只檢查設定結構；實際呼叫還需要認證資訊、網路和模型權限。

新版官方推薦 `~/.agents/skills`。使用舊版或本專案既有 `~/.codex/skills` 的用戶端，可保留其實際發現路徑；同名 Skill 不要裝兩份。安裝後在任務輸入框輸入 `$gpt-image-flare` 檢查是否出現，未出現則重啟並檢查目錄層級。

## 使用

生成圖片：

```text
$gpt-image-flare 生成一張白色背景的精密產品圖，1024x1024，高品質。
```

編輯圖片時附上本地圖片，並明確寫出需要修改及必須保留的內容。Skill 會在有輸入圖時使用編輯介面。每次執行會產生一次計費請求，不會自動重試；結果會驗證實際檔案格式與尺寸。

安裝依據：[Codex 官方 Skills](https://developers.openai.com/codex/skills/)。下載腳本僅做本地和模擬請求測試；實際模型呼叫取決於 Key 分組。
