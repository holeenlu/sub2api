import adminAccessKeys from './adminAccessKeys'
import vendor from './vendor'
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
  adminAccessKeys,
  vendor,
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
