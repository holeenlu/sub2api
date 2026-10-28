import { defineAsyncComponent, defineComponent, h, type AsyncComponentLoader, type Component } from 'vue'
import { useI18n } from 'vue-i18n'
import LoadingSpinner from '@/shared/ui/LoadingSpinner.vue'

const PanelLoading = defineComponent({
  name: 'PanelLoading',
  inheritAttrs: false,
  props: { show: { type: Boolean, default: true } },
  setup: props => () => props.show ? h(LoadingSpinner) : null
})

const PanelLoadFailure = defineComponent({
  name: 'PanelLoadFailure',
  inheritAttrs: false,
  props: { show: { type: Boolean, default: true } },
  setup(props) {
    const { t } = useI18n()
    return () => props.show
      ? h('div', { role: 'alert', class: 'p-4 text-sm text-red-600 dark:text-red-400' }, t('common.error'))
      : null
  }
})

/** Loading a component never retries or invokes its business commands. */
export function lazyPanel<T extends Component>(loader: AsyncComponentLoader<T>): T {
  return defineAsyncComponent<T>({
    loader,
    loadingComponent: PanelLoading,
    errorComponent: PanelLoadFailure,
    delay: 0,
    timeout: 30_000,
    suspensible: false
  })
}
