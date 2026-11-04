<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { NConfigProvider } from 'naive-ui'
import { RouterView } from 'vue-router'
import AppHeader from '@/components/AppHeader.vue'
import ControlBar from '@/components/ControlBar.vue'
import FilterBar from '@/components/FilterBar.vue'
import FilterChips from '@/components/FilterChips.vue'
import { useBanner } from '@/composables/useBanner'
import { state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'
import { useNaiveTheme } from '@/lib/naiveTheme'

const { overrides, theme, locale, dateLocale } = useNaiveTheme()
const { message } = useBanner()
const { start, stop } = useRefresh()

watch(
  () => state.view,
  (view) => (document.title = view === 'logs' ? 'harness-observer · Logs' : 'harness-observer'),
  { immediate: true },
)

onMounted(() => start())
onBeforeUnmount(() => stop())
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
      <AppHeader />
      <div class="workspace">
        <div class="masthead">
          <ControlBar />
          <FilterBar />
          <FilterChips />
        </div>
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
  flex-direction: column;
  height: 100%;
  overflow: hidden;
}

.workspace {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
}

/* The controls stay put while the statement scrolls under them; one hairline
 * says where the chrome ends and the evidence begins. */
.masthead {
  flex: none;
  border-bottom: 1px solid var(--line);
}

.page {
  flex: 1;
  min-height: 0;
  overflow: auto;
}

.banner {
  margin: 12px var(--gutter) 0;
  padding: 10px 14px;
  border: 1px solid var(--error-soft);
  border-radius: var(--r-2);
  background: var(--error-soft);
  color: var(--error);
  font-size: 13px;
}
</style>
