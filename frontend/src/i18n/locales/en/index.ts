import autoBPSOps from './autoBPSOps'
import qualityOps from './qualityOps'
import tokenGuardV2 from './tokenGuardV2'
import priorityScheduling from './priorityScheduling'
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
  qualityOps,
  tokenGuardV2,
  autoBPSOps,
  priorityScheduling,
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
