## 統一口徑

TapModels 參考價按對應廠商官方直接 API 的 Standard 計價項逐項乘 `0.5`。Batch、Flex、Fast、Priority、區域處理和內建工具費用不自動包含在 Standard 表中。

```text
某項費用 = 該項 token 數 ÷ 1,000,000 × 該項 TapModels 單價
請求費用 = 普通輸入 + 快取讀取 + 快取寫入 + 輸出 + 已明確的額外費用
```

## 不重複計費

普通輸入、快取讀取與快取寫入是不同計價分類。同一批 token 不應同時按普通輸入和快取讀取各計一次。推理 token 若已計入輸出 usage，不再額外加一次輸出費用。

## 長上下文

OpenAI 表中輸入超過 272,000 tokens 的完整請求使用長檔：輸入與快取為標準檔 2 倍，輸出為 1.5 倍。目標分組還必須啟用對應階梯；若專案設定為其他分段口徑，頁面應展示實際規則。

## 影像 token

GPT-Image-2.5 分文字輸入、快取文字輸入、影像輸入、快取影像輸入和影像輸出五項。相同單價不表示 Flare 與 Sunburst 對同一請求消耗相同 token；不使用 GPT Image 2 的尺寸計算器估算 2.5 型號。

## 價格狀態

下方表格同時展示帶核價日期的官方 0.5x 參考和目前分組報價。兩者不同不應被隱藏；最終帳單以目標分組設定及用量記錄為準。

## 計算範例

- Sol 普通輸入 10,000、快取讀取 5,000、輸出 2,000 tokens：官方費用為 `$0.082`，TapModels 0.5x 參考費用為 `$0.041`。
- Flare 或 Sunburst 普通文字輸入 100、普通影像輸入 200、影像輸出 1,000 tokens：官方費用為 `$0.0321`，TapModels 0.5x 參考費用為 `$0.01605`。

以上只按給定 token 數計算，不包含快取寫入、工具、服務檔或其他附加費用，也不對應某個固定圖片尺寸。

## 來源與更新

價格快照核對自 [OpenAI 模型與價格](https://developers.openai.com/api/docs/models)、[GPT-Image-2.5 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.5-flare)、[GPT-Image-2.5 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst) 和 [Claude 模型與價格](https://platform.claude.com/docs/en/models/overview)。官方價格變化後需要更新快照與核價日期，不能繼續沿用舊的 0.5x 結果。
