## Prices for your selected group

KDAN offers flexible model choices and clear group pricing. The table below loads the selected group's prices. Offers, multipliers, and model-specific rates follow that group's current settings; no fixed discount is promised.

The group selected on a public page may differ from the one assigned to your API key. Confirm it in [API Keys](/keys) and check billing units and rates in the [Model Plaza](/model-plaza). Charges follow the rules applicable to each request and its usage record.

## Reading the table

- **Group price**: the rate after the group's multiplier or model-specific pricing. Do not apply another discount to an already adjusted price.
- **Official reference price**: upstream pricing information for comparison, not an additional charge or a guarantee of your key's rate.
- **Token billing**: input, output, cache reads, and applicable cache writes are billed separately, usually per million tokens.
- **Per-image billing**: multiply the billed image count by the effective price for the model, size, or configured tier. Do not use the token formula.
- **Missing price**: does not mean free. Confirm configuration and availability before making a request.

```text
Token charge = billed tokens for an item / 1,000,000 × its effective unit price
Image charge = billed image count × the effective price for the applicable tier
```

## Tiers and additional charges

Long context, cache duration, service tiers, and tools can have different rates. Apply these only when the model and group define them; one model's threshold or multiplier does not apply to every model. Avoid counting the same billing item twice. Retried requests may create additional usage.

Official references: [OpenAI pricing](https://openai.com/api/pricing/) and [Anthropic pricing](https://platform.claude.com/docs/en/about-claude/pricing). KDAN rates may differ from direct API prices.
