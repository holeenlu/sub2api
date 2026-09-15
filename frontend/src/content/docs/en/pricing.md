## Pricing basis

TapModels reference prices multiply each official direct-API Standard item by `0.5`. Batch, Flex, Fast, Priority, regional processing, and built-in tool fees are separate from Standard inference pricing.

```text
Item cost = item tokens / 1,000,000 × TapModels item price
Request cost = input + cache reads + cache writes + output + disclosed extras
```

Regular input, cache reads, and cache writes are distinct categories. Do not bill the same tokens as both regular and cached input. Reasoning tokens already included in output usage are not added a second time.

## Long context and images

For the OpenAI models shown here, a full request over 272,000 input tokens uses the long-context tier: input and cache are 2x, output is 1.5x. The target group must also enable its pricing ladder.

GPT-Image-2.5 has five items: text input, cached text input, image input, cached image input, and image output. Equal rates do not imply equal token consumption between Flare and Sunburst, and the GPT Image 2 calculator does not estimate 2.5 usage.

## Price state

The table below compares dated official 0.5x references with selected-group prices. Differences stay visible. Actual charges follow the target group configuration and usage records.

## Worked examples

- For 10,000 regular input, 5,000 cached input, and 2,000 output tokens on Sol, the official cost is `$0.082` and the TapModels 0.5x reference is `$0.041`.
- For 100 regular text input, 200 regular image input, and 1,000 image output tokens on Flare or Sunburst, the official cost is `$0.0321` and the TapModels 0.5x reference is `$0.01605`.

These examples use the stated token counts only. They exclude cache writes, tools, service tiers, and other fees, and do not represent a fixed image size.

## Sources and updates

The snapshot was checked against [OpenAI models and pricing](https://developers.openai.com/api/docs/models), [GPT-Image-2.5 Flare](https://developers.openai.com/api/docs/models/gpt-image-2.5-flare), [GPT-Image-2.5 Sunburst](https://developers.openai.com/api/docs/models/gpt-image-2.5-sunburst), and [Claude models and pricing](https://platform.claude.com/docs/en/models/overview). When official prices change, update the snapshot and verification date instead of retaining stale 0.5x results.
