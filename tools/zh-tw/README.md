# 繁體中文（台灣）語言包產生工具

`frontend/src/i18n/locales/zh-TW/` 與 `docs/legal/admin-compliance.zh-TW.md` **不是手寫的**，
是由這裡的工具依簡體語言包（`locales/zh`）自動產生：OpenCC `s2twp` 做簡→繁與基本台灣用語，
再疊兩層修正（`convert.mjs` 的 `CORRECTIONS` 修 OpenCC 誤判、`TW_VOCAB` 換台灣慣用語）。

轉換器來自 [erwinlin/TapModels](https://github.com/erwinlin/TapModels)（該 fork 在建置時整包轉繁），
本專案改為產生獨立的 `zh-TW` 語言包並提交進 git，語言選單多一個「繁體中文 🇹🇼」，
`zh-TW` 缺 key 時依序回退 `zh` → `en`。

## 日常流程

改了 `locales/zh` 之後（新增文案、修改文案），重新產生一次：

```bash
cd tools/zh-tw && npm ci          # 只需第一次（安裝 opencc-js）
node tools/zh-tw/gen-locale.mjs   # 覆寫 zh-TW（在 repo 任何目錄執行皆可）
```

`frontend/src/i18n/__tests__/zhTwLocale.spec.ts` 會檢查 zh-TW 的 key 集合與 zh 完全一致，
忘了重跑時 `pnpm run test:run` 直接失敗。也可以只比對不寫檔：

```bash
node tools/zh-tw/gen-locale.mjs --check   # 不同步時離開碼 1
```

## 修正用語

**不要直接改 zh-TW 檔案**，下次重跑會被蓋掉。依情況改：

| 問題 | 改哪裡 |
|---|---|
| OpenCC 轉錯字／詞（例：账号→賬號） | `convert.mjs` 的 `CORRECTIONS` |
| 簡繁同形的中國用語（例：配置→設定、令牌→權杖、當前→目前） | `convert.mjs` 的 `TW_VOCAB`，長詞放前面 |
| 只對特定句子成立的修正（例：計量詞 條→筆／則） | `gen-locale.mjs` 的 `OVERRIDES` |

改完重跑 `gen-locale.mjs`。用稽核工具找候選：

```bash
node tools/zh-tw/audit-locale.mjs          # 列出疑似殘留的中國用語與 OpenCC 誤轉（次數＋例句）
node tools/zh-tw/audit-locale.mjs --all    # 印出每一行命中
```

命中不等於一定要改（「生成」「上下文」在台灣 AI 語境也通用），自行判斷。

## 已知取捨

- 「文件」歧義：簡體「文件」可指 file（→檔案）或 document（→文件），一律當 file，法律文件常見的
  「本文件／協議文件」由 `CORRECTIONS` 補回。
- 「應用」歧義：動詞 apply 轉「套用」、名詞 app 保留「應用」，靠上下文規則區分，新增文案若判斷錯，補一條規則。
- 「通過」歧義：via 轉「透過」、pass 保留「通過」，同上。
- 後端訊息（API 錯誤文字）仍是簡體，不在本工具範圍。

## CLI

```bash
node tools/zh-tw/convert.mjs --dry "some/glob/**/*.ts"   # 預覽會被轉的檔案
node tools/zh-tw/convert.mjs "some/file.ts"              # 就地轉換（逐行、對已是繁體的行安全）
```
