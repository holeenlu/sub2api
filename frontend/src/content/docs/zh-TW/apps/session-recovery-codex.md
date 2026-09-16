Claude Code 使用者請使用 [Claude Code 工作階段恢復](/apps/session-recovery-claude)。兩種用戶端的資料格式不同，修復工具不能交叉使用。

## 先判斷是哪一種“丟失”

| 現象 | 優先處理 |
| --- | --- |
| 切換專案後列表空了 | 返回原專案或使用 `codex resume --all`，檢視封存列表 |
| 換作業系統使用者、主機或設定 CODEX_HOME 後不見 | 找回原來的資料目錄；遠端和本地歷史分別儲存 |
| 改了 Provider 標識後舊任務不顯示 | 核對舊設定與目前設定，恢復原 Provider 標識再載入 |
| 能找到 JSONL，但索引路徑指向舊目錄 | 用本站工具診斷，滿足條件時修復路徑 |
| 官方帳號雲端任務或檔案被刪除 | 本地索引修復包無法恢復；回到原帳號或使用自己的備份 |

切換 Key 與更換用戶端帳號、Provider 名稱、資料目錄是不同操作。不要通過刪除 `auth.json`、SQLite 或整個 `.codex` 目錄故障排除。

## 先用 Codex 找回

在原專案目錄執行 `codex resume`，使用 `codex resume --all` 查詢其他目錄的工作階段；知道 ID 時使用 `codex resume <工作階段ID>`。桌面版檢查原專案和已封存任務。不要自動傳送繼續任務的訊息，因為傳送新訊息會發起模型請求。

## Provider 切換的處理

若只是把 `model_provider = "舊名稱"` 改成新名稱，先在舊設定備份中核對舊 Provider 的 ID、模型、地址及認證資訊來源。在同一個舊 Provider 設定項目內更新正確的 TapModels 地址/Key，並恢復頂層原 ID，再重啟嘗試開啟舊工作階段。不要為了顯示歷史把新的 Key 發往舊服務商地址。

不同用戶端版本對帳號/Provider 的過濾不同。本站工具只報告 Provider 分佈，不批次改寫 Provider，也不把一個官方帳號的資料歸給另一個帳號。仍不可見時保留報告聯絡支援。

## 下載與只讀診斷

要求 Python 3.10+。[下載 TapModels Codex 工作階段修復工具](/downloads/tapmodels-codex-session-repair.zip)，解壓後進入 `tapmodels-codex-session-repair` 目錄。

macOS / Linux：

```bash
bash repair-sessions.sh --dry-run
```

Windows PowerShell：

```powershell
.\Repair-TapModelsSessions.ps1 -DryRun
```

腳本讀取實際 `CODEX_HOME`，預設 `~/.codex`。如需指定，使用 `--codex-home "/實際目錄"`，PowerShell 對應 `-CodexHome`。它自動發現唯一的 `state_*.sqlite`；存在多個時不猜測，需透過 `--database state_5.sqlite`（範例檔名）或 `-Database` 指定。

報告包含資料庫完整性、結構、工作階段/封存數量、Provider 分佈及缺失路徑，不輸出工作階段正文或 API Key。報告仍含本機路徑與工作階段 ID，分享前檢查隱私。

## 修復已驗證的索引路徑

先閱讀報告。完全退出使用該目錄的 Codex 桌面版、CLI、IDE 程序後執行：

```bash
bash repair-sessions.sh --apply --client-closed
```

```powershell
.\Repair-TapModelsSessions.ps1 -Apply -ClientClosed
```

工具僅在工作階段目錄內找到唯一候選，且 JSONL 首條 `session_meta.payload.id` 與索引工作階段 ID 完全一致時修改 `rollout_path`。它先取得寫鎖，再完成最終掃描；鎖定期間生成一致 SQLite 備份，並在提交前驗證資料庫全部表、列及資料。資料庫、工作階段與備份路徑均拒絕 `..`、符號連結和操作中被替換的檔案。任何驗證失敗都會回滾事務。原始對話、Key、設定、Provider 和封存標記均不修改。

沒有匹配檔案、結構未知、資料庫損壞、候選重複或路徑含符號連結時不會替你猜測。工具不能從缺失檔案重建訊息，也不修復任意版本的帳戶篩選邏輯。

## 檢查結果與回滾

成功後檢視 `repaired_rollout_paths`，重新執行只讀診斷，再開啟用戶端確認歷史內容。先預覽回滾：

```bash
bash repair-sessions.sh --rollback "/報告中的備份路徑"
```

核對預覽並完全退出所有用戶端後再顯式執行：

```bash
bash repair-sessions.sh --rollback "/報告中的備份路徑" --apply --client-closed
```

PowerShell 對應 `-Rollback "路徑" -Apply -ClientClosed`。回滾使用 compare-and-set 條件，僅撤銷該次修復實際寫入且目前值仍匹配的路徑；路徑後來已改變、備份被替換或歸屬不一致時會拒絕執行。

依據：[Codex CLI resume 參考](https://developers.openai.com/codex/cli/reference/)。工具經過臨時資料庫測試，沒有修改本機真實工作階段資料庫。
