// 登录条款 / 法律文件的图标推断。
//
// 文档标题由管理员自行填写，是单语言的自由文本（LoginAgreementDocument.title），
// 没有类型字段可用，只能按标题关键词猜。所以匹配必须同时覆盖简体、繁体和英文，
// 否则 zh-TW 站点或英文站点的文档一律退化成默认图标。

export type LegalDocumentIcon = 'document' | 'shield' | 'globe' | 'cog'

// 隐私政策 / 服务条款一类 -> 盾牌
const SHIELD_PATTERN = /政策|隐私|隱私|プライバシー|ポリシー|privacy|policy/i
// 支持的国家和地区 -> 地球
const GLOBE_PATTERN = /国家|國家|地区|地區|国|地域|country|countries|region/i
// 服务特定条款 -> 齿轮
const COG_PATTERN = /特定|specific/i

/** 按标题猜图标；认不出来时返回 null，交给调用方决定兜底。 */
export function matchLegalDocumentIcon(title: string): LegalDocumentIcon | null {
  const value = (title || '').trim()
  if (!value) {
    return null
  }
  if (SHIELD_PATTERN.test(value)) {
    return 'shield'
  }
  if (GLOBE_PATTERN.test(value)) {
    return 'globe'
  }
  if (COG_PATTERN.test(value)) {
    return 'cog'
  }
  return null
}

/** 按标题猜图标，认不出来时用默认的文档图标。 */
export function resolveLegalDocumentIcon(title: string): LegalDocumentIcon {
  return matchLegalDocumentIcon(title) ?? 'document'
}
