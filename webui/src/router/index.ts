import { createRouter, createWebHistory } from 'vue-router'
import LogsPage from '@/pages/LogsPage.vue'
import OverviewPage from '@/pages/OverviewPage.vue'

/**
 * Two routes, both served by the same embedded page: the Go handler rewrites
 * /logs to / before reaching the file server, so a direct link, a browser
 * refresh and back/forward all land on the real route rather than a 404.
 */
export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/', name: 'overview', component: OverviewPage },
    { path: '/logs', name: 'logs', component: LogsPage },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
