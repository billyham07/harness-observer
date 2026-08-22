<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { NConfigProvider } from 'naive-ui'
import { RouterView } from 'vue-router'
import ControlBar from '@/components/ControlBar.vue'
import FilterBar from '@/components/FilterBar.vue'
import FilterChips from '@/components/FilterChips.vue'
import SideRail from '@/components/SideRail.vue'
import { useBanner } from '@/composables/useBanner'
import { state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'
import { useNaiveTheme } from '@/lib/naiveTheme'

const { overrides, theme, locale, dateLocale } = useNaiveTheme()
const { message } = useBanner()
const { start, stop } = useRefresh()
const navOpen = ref(false)

watch(
  () => state.view,
  (view) => (document.title = view === 'logs' ? 'harness-observer · Logs' : 'harness-observer'),
  { immediate: true },
)

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Escape') navOpen.value = false
}

onMounted(() => {
  start()
  window.addEventListener('keydown', onKeydown)
})

onBeforeUnmount(() => {
  stop()
  window.removeEventListener('keydown', onKeydown)
})
</script>

<template>
  <!-- abstract: the provider contributes theme, not a wrapper div. A div
       here would break the height chain between #app and the shell, and the
       whole page would scroll instead of the content area. -->
  <NConfigProvider
    abstract
    :theme="theme"
    :theme-overrides="overrides"
    :locale="locale"
    :date-locale="dateLocale"
  >
    <div class="shell">
      <SideRail :open="navOpen" @close="navOpen = false" />
      <div v-if="navOpen" class="scrim" @click="navOpen = false" />
      <div class="workspace">
        <ControlBar :nav-open="navOpen" @toggle-nav="navOpen = !navOpen" />
        <FilterBar />
        <FilterChips />
        <div v-if="message" class="banner" role="alert">{{ message }}</div>
        <main class="page">
          <RouterView />
        </main>
      </div>
    </div>
  </NConfigProvider>
</template>

<style scoped>
.shell {
  display: flex;
  height: 100%;
  overflow: hidden;
}

.workspace {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

.page {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.banner {
  padding: 10px var(--gutter);
  border-bottom: 1px solid var(--error-soft);
  background: var(--error-soft);
  color: var(--error);
  font-size: 12.5px;
}

.scrim {
  position: fixed;
  inset: 0;
  z-index: 39;
  background: rgba(0, 0, 0, 0.36);
}

@media (min-width: 901px) {
  .scrim {
    display: none;
  }
}
</style>
