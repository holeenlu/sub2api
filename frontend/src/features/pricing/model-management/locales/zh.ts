export default {
  title: '统一模型价格',
  readDenied: '读取价格需要有效会话、models.read 和 billing.cost.read 权限。', unit: '单位：美元 / 百万 token。图片价格按百万图片 token 计。',
  search: '搜索模型 ID', refresh: '重新读取价格', sync: '同步价格', loading: '正在读取价格…', empty: '没有匹配的模型价格。',
  model: '模型', source: '来源', version: '价格版本：{version}', synced: '最近同步：{time}', details: '完整价格明细',
  edit: '覆盖价格', cancel: '取消', save: '保存价格', reset: '移除覆盖', saved: '命令完成，已重新读取价格。',
  input: '输入', output: '输出', cache_read: '缓存读取', cache_5m: '缓存写入（5 分钟）', cache_1h: '缓存写入（1 小时）',
  unknown: '未知 / 缺失', unpriced: '未定价（null）', priced: '设置价格', inherit: '继承 / 缺失', custom: '自定义价格',
  fallback: '服务端补值（null）', noImage: '无独立图片价格（null）',
  tiers: '上下文阶梯', above: '超过 token 数', addTier: '添加阶梯', remove: '删除', modes: '服务模式', image: '图片 token 价格',
  stale: '价格版本已变化。草稿已保留，请关闭后重新打开模型，以最新版本编辑。',
  reloadRequired: '再次操作前请重新读取价格。草稿已保留，请核对服务端状态后重试。',
  working: '正在执行命令…', invalidDraft: '每组已定价基础字段的输入、输出、缓存读取均必填，且为 0 到 1e9 的有限数；可选缓存写入可缺失或为 null。阶梯最多 32 个，阈值为互不重复的正整数。',
  errors: {
    conflict: '价格版本冲突。草稿已保留，请重新读取并核对最新值。',
    denied: '权限不足，请核对 models.read、billing.cost.read、models.manage 和 billing.rates.update。',
    unavailable: '暂时无法获取模型价格。', invalid: '价格版本或价格数据无效或缺失。',
    request: '请求失败，执行结果可能未知；重试前请重新读取价格。',
    refresh: '命令已接受，但重新读取失败。草稿已保留，再次操作前请重新读取。'
  }
}
