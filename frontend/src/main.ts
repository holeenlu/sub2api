import { bootstrap } from './app/bootstrap'
import './style.css'

bootstrap().catch((error: unknown) => {
  document.documentElement.dataset.appStatus = 'failed'
  console.error('Application startup failed', error instanceof Error ? error.name : 'UnknownError')
})
