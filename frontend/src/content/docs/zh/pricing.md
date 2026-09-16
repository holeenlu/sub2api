## 统一口径

KDAN 参考价按对应厂商官方直接 API 的 Standard 计价项逐项乘 `0.5`。Batch、Flex、Fast、Priority、区域处理和内置工具费用不自动包含在 Standard 表中。

```text
某项费用 = 该项 token 数 ÷ 1,000,000 × 该项 KDAN 单价
请求费用 = 普通输入 + 缓存读取 + 缓存写入 + 输出 + 已明确的额外费用
```

## 不重复计费

普通输入、缓存读取与缓存写入是不同计价分类。同一批 token 不应同时按普通输入和缓存读取各计一次。推理 token 若已计入输出 usage，不再额外加一次输出费用。

## 长上下文

OpenAI 表中输入超过 272,000 tokens 的完整请求使用长档：输入与缓存为标准档 2 倍，输出为 1.5 倍。目标分组还必须启用对应阶梯；若项目配置为其他分段口径，页面应展示实际规则。

## 图像 token

GPT-Image-2.5 分文本输入、缓存文本输入、图像输入、缓存图像输入和图像输出五项。相同单价不表示 Flare 与 Sunburst 对同一请求消耗相同 token；不使用 GPT Image 2 的尺寸计算器估算 2.5 型号。

## 价格状态

下方表格同时展示带核价日期的官方 0.5x 参考和当前分组报价。两者不同不应被隐藏；最终账单以目标分组配置及用量记录为准。

## 计算示例

- Sol 普通输入 10,000、缓存读取 5,000、输出 2,000 tokens：官方费用为 `$0.082`，KDAN 0.5x 参考费用为 `$0.041`。
- Flare 或 Sunburst 普通文本输入 100、普通图像输入 200、图像输出 1,000 tokens：官方费用为 `$0.0321`，KDAN 0.5x 参考费用为 `$0.01605`。

以上只按给定 token 数计算，不包含缓存写入、工具、服务档或其他附加费用，也不对应某个固定图片尺寸。

## 来源与更新

价格快照核对自 [OpenAI 模型与价格](https://developers.openai.com/api/docs/models)、[GPT-Image-2.5 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.5-flare)、[GPT-Image-2.5 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst) 和 [Claude 模型与价格](https://platform.claude.com/docs/en/models/overview)。官方价格变化后需要更新快照与核价日期，不能继续沿用旧的 0.5x 结果。
