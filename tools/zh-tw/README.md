# 繁體中文（台灣）前端語言包工具

這裡只保留仍被前端使用的語言包產生器、共用字典及介面稽核工具。舊後端Docker建置時改寫Go字串的轉換器、掃描器和專屬一次性稽核腳本已隨舊建置流程刪除；不再提供ZH_TW鏡像開關。

## 安裝與產生

~~~sh
npm ci --prefix tools/zh-tw
node tools/zh-tw/gen-locale.mjs
node tools/zh-tw/gen-locale.mjs --check
~~~

gen-locale依frontend/src/i18n/locales/zh產生zh-TW語言包及相應法律文字。不要手改產物；修正文案來源、convert.mjs的CORRECTIONS/TW_VOCAB或gen-locale.mjs的OVERRIDES，再由前端會話產生及驗證。

~~~sh
node tools/zh-tw/audit-locale.mjs
node tools/zh-tw/audit-ui.mjs
~~~

convert.mjs與opencc-js仍由實際前端產生器和稽核使用；package.json及package-lock.json保持既有依賴。後端API文字保持Go原始碼內容，不再於製作映像時改寫；這不改變前端使用者選擇語言的能力。
