# 繁體中文（台灣）前端語言包工具

這裡只保留仍被前端使用的語言包產生器、共用字典及介面稽核工具。舊後端Docker建置時改寫Go字串的轉換器、掃描器和專屬一次性稽核腳本已隨舊建置流程刪除；不再提供ZH_TW鏡像開關。

## 安裝與產生

~~~sh
npm ci --prefix tools/zh-tw
node tools/zh-tw/gen-locale.mjs
node tools/zh-tw/gen-locale.mjs --check
~~~

gen-locale掃描frontend/src/i18n/locales/zh及frontend/src/features/*/locales/zh，按/locales/zh/映射至/locales/zh-TW/。預設仍產生site/docs/content與根legal文字；--locales-only只寫語言包，適用分域來源遷移期間。不要手改產物；唯一轉換規則仍是convert.mjs的CORRECTIONS/TW_VOCAB及gen-locale.mjs的OVERRIDES。

~~~sh
node tools/zh-tw/audit-locale.mjs
node tools/zh-tw/audit-ui.mjs
~~~

convert.mjs與opencc-js仍由實際前端產生器和稽核使用；package.json及package-lock.json保持既有依賴。後端API文字保持Go原始碼內容，不再於製作映像時改寫；這不改變前端使用者選擇語言的能力。

## 分域來源與舊產物核對

~~~sh
node tools/zh-tw/gen-locale.mjs --locales-only
node tools/zh-tw/gen-locale.mjs --locales-only --check
node --test tools/zh-tw/gen-locale.test.mjs
~~~

--check會同時指出不一致語言包和沒有zh來源的舊TW檔，不修改任何檔案。來源遷移完成並核實後，才可顯式執行--locales-only --prune-generated-orphans；它只刪除帶完整既有generator標記的普通檔，未確認或symlink檔案保留並報告。此清理不能和--check合用，預設生成不會自動刪檔。
