import modelCatalog from './modelCatalog'
// 此檔案由 tools/zh-tw/gen-locale.mjs 依 locales/zh 自動產生，請勿手動修改。
// 詞彙修正請改 tools/zh-tw/convert.mjs（CORRECTIONS / TW_VOCAB），逐句修正請改 gen-locale.mjs 的 OVERRIDES。
import autoBPSOps from './autoBPSOps'
import ui from './ui'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import docs from './docs'

export default {
  modelCatalog,
  autoBPSOps,
  ...ui,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...docs,
}
