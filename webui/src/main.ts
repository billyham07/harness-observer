import { createApp } from 'vue'
import App from './App.vue'
import { chartFactoryKey } from './charts/useG2Chart'
import { createG2Chart } from './charts/g2Factory'
import { router } from './router'
import { installQueryState } from './composables/useQueryState'
import { currentLang, setLang } from './i18n'
import './styles/tokens.css'
import './styles/base.css'

setLang(currentLang.value)

const app = createApp(App)
// The chart engine is provided rather than imported by the chart components,
// which keeps the rendering library out of unit tests that only care about the
// mount/update/unmount lifecycle.
app.provide(chartFactoryKey, createG2Chart)
app.use(router)

// The filter is parsed before the first page renders, so a page never queries
// once with defaults and again with the URL it was actually opened with.
void router.isReady().then(() => {
  installQueryState(router)
  app.mount('#app')
})
