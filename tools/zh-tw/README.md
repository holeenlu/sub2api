# 繁體中文（台灣）：語言包產生 + 後端建置時轉換

git 裡的中文原始碼**保持簡體**，與上游 `Wei-Shaw/sub2api` 及其他協作者完全一致，
跟上游合併時沒有任何簡繁衝突。繁體由這裡的工具產生，分兩層：

| 層 | 機制 | 何時發生 | 產物在哪 |
|---|---|---|---|
| 前端介面文案 | `gen-locale.mjs` 依 `locales/zh` 產生 `zh-TW` 語言包 | 開發者改了 zh 之後手動跑一次 | **提交進 git**：`frontend/src/i18n/locales/zh-TW/`、`docs/legal/admin-compliance.zh-TW.md` |
| 後端字串（API 錯誤訊息等） | `convert-go.mjs` 轉換 Go 字串字面值 | 根目錄 `Dockerfile` 的 `zh-tw-converter` 階段，`--build-arg ZH_TW=true` 時 | 只在建置用的副本樹，不進 git |

兩層共用同一份字典（`convert.mjs` 的 `CORRECTIONS` 與 `TW_VOCAB`），改字典兩邊同時受惠。

為什麼分兩層：前端本來就有語言包系統，產生真檔可以在 PR 裡審、測試跑在真檔上、
使用者可在簡體與繁體之間切換；後端沒有語言包系統，建置時轉是唯一不改原始碼的做法。
後端轉換預設**關閉**（`Dockerfile` 的 `ARG ZH_TW=false`）：預設映像的後端訊息與上游逐字相同（簡體）。
要出繁體後端訊息就帶 `docker build --build-arg ZH_TW=true`。前端 zh-TW 語言包不受此開關影響，一律提供。

## 本機安裝

```bash
cd tools/zh-tw && npm ci          # 只需第一次（安裝 opencc-js）
```

## 前端：產生 zh-TW 語言包

改了 `locales/zh` 之後（新增文案、修改文案），重新產生一次：

```bash
node tools/zh-tw/gen-locale.mjs           # 覆寫 zh-TW（在 repo 任何目錄執行皆可）
node tools/zh-tw/gen-locale.mjs --check   # 只比對不寫檔，不同步時離開碼 1（CI 用）
```

`frontend/src/i18n/__tests__/zhTwLocale.spec.ts` 會檢查 zh-TW 的 key 集合與 zh 完全一致、
沒有殘留簡體字；忘了重跑時 `pnpm run test:run` 與 fork 的 CI 都會失敗。

語言選單有「简体中文」「繁體中文」，`zh-TW` 缺 key 時依序回退 `zh` → `en`（`frontend/src/i18n/index.ts`）。

### 修正用語

**不要直接改 zh-TW 檔案**，下次重跑會被蓋掉。依情況改：

| 問題 | 改哪裡 |
|---|---|
| OpenCC 轉錯字／詞（例：账号→賬號、回调→回撥） | `convert.mjs` 的 `CORRECTIONS` |
| 簡繁同形的中國用語（例：配置→設定、令牌→權杖、當前→目前） | `convert.mjs` 的 `TW_VOCAB`，長詞放前面 |
| 只對特定句子成立的修正（例：計量詞 條→筆／則） | `gen-locale.mjs` 的 `OVERRIDES` |

用稽核工具找候選：

```bash
node tools/zh-tw/audit-locale.mjs          # 列出疑似殘留的中國用語與 OpenCC 誤轉（次數＋例句）
node tools/zh-tw/audit-locale.mjs --all    # 印出每一行命中
```

命中不等於一定要改（「生成」「上下文」在台灣 AI 語境也通用），自行判斷。

## 後端：建置時轉換與保護清單

Go 檔裡混有「給人看的訊息」與「程式比對用的值」。後者轉繁會造成隱性 bug，
由 `convert-go.mjs` 的 `PROTECTED_FILES` / `PROTECTED_LITERALS` 保護，
**清單住在這裡而不是後端原始碼裡**，後端原始碼零改動。

| 位置 | 為什麼不能轉 |
|---|---|
| `internal/payment/provider/easypay.go` | 比對 EasyPay 金流閘道回傳的「订单编号不存在」，對方永遠回簡體；轉了退款判斷永久失效 |
| `internal/service/grok_upstream_failure.go` | 關鍵字表，比對 Grok 上游錯誤文字 |
| `internal/service/openai_images_responses.go` | 比對 OpenAI 拒絕文案 |
| `internal/service/ratelimit_cn_providers.go` | 比對智譜／Kimi 回應 body |
| `internal/service/grok_model_quota_block.go` | 比對 Grok 文字 |
| `internal/handler/auth_dingtalk_client.go` | 釘釘 API 的欄位名 `ext["企业邮箱"]` |
| `internal/service/admin_compliance.go` | 管理員合規確認短語：前端逐字比對、後端 `expectedAdminCompliancePhrase` 也比對，轉繁會讓簡體介面顯示繁體短語（繁體介面的短語由 `frontend/src/stores/adminCompliance.ts` 提供） |
| SQL／Redis Lua 的原始字串 | 中文只在 `--` 註解裡；改動會變更 Lua 腳本的 SHA1 |
| `ent/` | 產生碼與 schema 的 `.Comment()` 必須一致，不是使用者可見文字 |

另有兩處在**原始碼裡**做成簡繁並列（真正的程式碼修改，會進 `main`）：

- `internal/handler/ops_error_logger.go` — 錯誤分類
- `cmd/cleanup-ingress-reject-logs/main.go` — 歷史日誌清理

原因：資料庫裡的歷史日誌是簡體，而訊息的產生端（`internal/server/middleware/api_key_auth*.go`）建置後會輸出繁體，兩種都要能歸類。這兩個檔案在 `PROTECTED_FILES` 裡，建置時不會被動到。

**新增了拿中文去比對外部系統回應的程式碼時**（自己寫的或上游同步進來的），要把它加進 `PROTECTED_LITERALS`。
fork 若有每日同步上游的自動化流程，可用 `node convert-go.mjs --list-protected` 列出候選；手動稽核：

```bash
cd tools/zh-tw
node audit.mjs ../../backend      # 鄰接比對（==、strings.Contains、switch/case…）
node audit2.mjs ../../backend     # 間接比對（先存變數再比）
node audit3.mjs ../../backend     # 關鍵字切片表
node verify-matchers.mjs          # 產生端與比對端是否一致
node convert-go.mjs ../../backend --dry --report   # 預覽後端會轉哪些字面值
node convert-go.mjs --list-protected               # 印出保護清單（backend 相對路徑）
```

轉換後的後端樹**只用來 `go build`**，不支援 `go test`：測試一律在簡體原始碼上跑。
`convert-go.mjs` 轉換完成後會在目標樹寫入 `.zh-tw-converted` 標記；對同一棵樹再跑一次會直接拒絕
（轉換不是嚴格冪等，這是硬性防護）。Docker 每次都從乾淨原始碼複製所以不受影響。

## 已知取捨

- 「文件」歧義：簡體「文件」可指 file（→檔案）或 document（→文件），一律當 file，法律文件常見的
  「本文件／協議文件」由 `CORRECTIONS` 補回。
- 「應用」歧義：動詞 apply 轉「套用」、名詞 app 保留「應用」，靠上下文規則區分，新增文案若判斷錯，補一條規則。
- 「通過」歧義：via 轉「透過」、pass 保留「通過」，同上。
- 產生檔跟上游的 locale 變更合併時一定衝突：不要手解，先合 `zh`，再重跑 `gen-locale.mjs` 覆寫。

## CLI（就地轉換，一般不需要）

```bash
node tools/zh-tw/convert.mjs --dry "some/glob/**/*.ts"   # 預覽會被轉的檔案
node tools/zh-tw/convert.mjs "some/file.ts"              # 就地轉換（逐行、對已是繁體的行安全）
```
