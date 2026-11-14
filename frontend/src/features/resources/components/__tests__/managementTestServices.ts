import type { ResourceEditorServices } from '../../services'
import { resourceEditorServicesKey } from '../../services'
import { editorTestServices } from '../../__tests__/editorTestServices'

export function managementProvide(notices: Partial<ResourceEditorServices['notices']> = {}, services = editorTestServices()) {
  services.notices = { ...services.notices, ...notices }
  return { [resourceEditorServicesKey as symbol]: services }
}
