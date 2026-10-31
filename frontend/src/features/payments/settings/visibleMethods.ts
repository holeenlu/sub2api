import type { PaymentVisibleMethod, PaymentVisibleMethodSource, PaymentVisibleMethodSourceOption } from '@/features/system/api'

const PAYMENT_VISIBLE_METHOD_SOURCE_OPTIONS: Record<
  PaymentVisibleMethod,
  PaymentVisibleMethodSourceOption[]
> = {
  alipay: [
    { value: "", labelKey: "ui.notConfigured", },
    {
      value: "official_alipay",
      labelKey: "ui.officialAlipay",
    },
    {
      value: "easypay_alipay",
      labelKey: "ui.easypayAlipay",
    },
  ],
  wxpay: [
    { value: "", labelKey: "ui.notConfigured", },
    {
      value: "official_wxpay",
      labelKey: "ui.officialWechatPay",
    },
    {
      value: "easypay_wxpay",
      labelKey: "ui.easypayWechatPay",
    },
  ],
};
const PAYMENT_VISIBLE_METHOD_SOURCE_ALIASES: Record<
  PaymentVisibleMethod,
  Record<string, PaymentVisibleMethodSource>
> = {
  alipay: {
    official_alipay: "official_alipay",
    alipay: "official_alipay",
    alipay_direct: "official_alipay",
    official: "official_alipay",
    easypay_alipay: "easypay_alipay",
    easypay: "easypay_alipay",
  },
  wxpay: {
    official_wxpay: "official_wxpay",
    wxpay: "official_wxpay",
    wxpay_direct: "official_wxpay",
    wechat: "official_wxpay",
    official: "official_wxpay",
    easypay_wxpay: "easypay_wxpay",
    easypay: "easypay_wxpay",
  },
};
export function getPaymentVisibleMethodSourceOptions(
  method: PaymentVisibleMethod,
): PaymentVisibleMethodSourceOption[] {
  return PAYMENT_VISIBLE_METHOD_SOURCE_OPTIONS[method];
}

export function normalizePaymentVisibleMethodSource(
  method: PaymentVisibleMethod,
  source: unknown,
): PaymentVisibleMethodSource {
  if (typeof source !== "string") return "";

  const normalized = source.trim().toLowerCase();
  if (!normalized) return "";

  return PAYMENT_VISIBLE_METHOD_SOURCE_ALIASES[method][normalized] ?? "";
}

