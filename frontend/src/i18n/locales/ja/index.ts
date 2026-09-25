import ui from './ui'
import landing from './landing'
import common from './common'
import dashboard from './dashboard'
import channelMonitorV2 from './channelMonitorV2'
import batchImage from './batchImage'
import admin from './admin'
import misc from './misc'
import docs from './docs'
import requestTiming from './requestTiming'
import qualityOps from './qualityOps'
import accountOps from './accountOps'

export default {
  ...ui,
  ...landing,
  ...common,
  ...dashboard,
  ...channelMonitorV2,
  ...batchImage,
  admin,
  ...misc,
  ...docs,
  requestTiming,
  qualityOps,
  accountOps,
}
