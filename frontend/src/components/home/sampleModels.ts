/**
 * 首页「模型与费率」的示意资料。
 *
 * 本站的模型广场没有公开模型（或暂时载入失败）时，改以这六张示意卡片撑起版面，
 * 每张都会挂上「示意资料」标签，并在格线下方注明正式费率以模型广场为准。
 * 模型 ID 与价格是资料不是文案，不进语言包；分组名称由 home.models.sampleGroup 插值。
 *
 * 价格单位：USD / 1M tokens。
 */
export interface SampleModel {
  /** 模型 ID，不翻译 */
  model: string
  /** 供应商名称，插进 home.models.sampleGroup 组成分组标签 */
  provider: string
  input: number
  output: number
}

export const SAMPLE_MODELS: readonly SampleModel[] = [
  { model: 'claude-sonnet-4.5', provider: 'Claude', input: 3.0, output: 15.0 },
  { model: 'gpt-5.4', provider: 'OpenAI', input: 1.75, output: 12.0 },
  { model: 'gemini-2.5-pro', provider: 'Gemini', input: 1.25, output: 10.0 },
  { model: 'grok-4', provider: 'Grok', input: 3.0, output: 15.0 },
  { model: 'claude-opus-4.1', provider: 'Claude', input: 15.0, output: 75.0 },
  { model: 'gpt-5.4-mini', provider: 'OpenAI', input: 0.25, output: 2.0 }
]
