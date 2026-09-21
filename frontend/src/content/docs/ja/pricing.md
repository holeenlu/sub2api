## 料金の算定基準

本プロジェクトの基準価格では、公式のダイレクト API における Standard の各項目の価格に `0.5` を乗じています。Batch、Flex、Fast、Priority、リージョン別処理、および組み込みツールの料金は、Standard の推論料金とは別に計算されます。

```text
Item cost = item tokens / 1,000,000 × this project item price
Request cost = input + cache reads + cache writes + output + disclosed extras
```

通常の入力、キャッシュ読み取り、キャッシュ書き込みは、それぞれ異なるカテゴリです。同じトークンを通常の入力とキャッシュ入力の両方として請求しないでください。出力の使用量にすでに含まれている推論トークンを、二重に加算しないでください。

## 長いコンテキストと画像

ここに示す OpenAI モデルでは、入力トークン数が 272,000 を超えるリクエスト全体に長いコンテキストの料金階層が適用されます。入力とキャッシュは 2 倍、出力は 1.5 倍になります。対象グループでは、料金階層も有効にする必要があります。

GPT-Image-2.5 には、テキスト入力、キャッシュ済みテキスト入力、画像入力、キャッシュ済み画像入力、画像出力の 5 つの項目があります。料金が同じでも、Flare と Sunburst のトークン消費量が同じとは限りません。また、GPT Image 2 の計算ツールでは 2.5 の使用量を見積もれません。

## 料金の状態

以下の表では、日付付きの公式 0.5 倍の基準価格と、選択したグループの価格を比較しています。差異はそのまま表示されます。実際の請求額は、対象グループの設定と使用量の記録に従います。

## 計算例

- Sol で通常の入力トークン 10,000、キャッシュ済み入力トークン 5,000、出力トークン 2,000 を使用した場合、公式料金は `$0.082`、本プロジェクトの 0.5 倍の基準価格は `$0.041` です。
- Flare または Sunburst で通常のテキスト入力トークン 100、通常の画像入力トークン 200、画像出力トークン 1,000 を使用した場合、公式料金は `$0.0321`、本プロジェクトの 0.5 倍の基準価格は `$0.01605` です。

これらの例では、記載されたトークン数のみを使用しています。キャッシュ書き込み、ツール、サービス階層、その他の料金は含まれておらず、画像サイズが固定されていることを示すものでもありません。

## 参照元と更新

このスナップショットは、[OpenAI models and pricing](https://developers.openai.com/api/docs/models)、[GPT-Image-2.5 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.5-flare)、[GPT-Image-2.5 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst)、および [Claude models and pricing](https://platform.claude.com/docs/en/models/overview) と照合済みです。公式価格が変更された場合は、古い 0.5 倍の結果を残さず、スナップショットと検証日を更新してください。
