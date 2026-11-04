<script setup lang="ts">
import { computed } from 'vue'
import { currentLang, t, toggleLang } from '@/i18n'
import { SERIES } from '@/lib/palette'
import { navigate, state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'
import { useTheme } from '@/composables/useTheme'
import Icon from './Icon.vue'

const { status, label, tone } = useRefresh()
const { choice, cycle } = useTheme()
const themeIcon = computed(() => (choice.value === 'light' ? 'sun' : choice.value === 'dark' ? 'moon' : 'auto'))
const themeLabel = computed(() => `${t('theme.title')}: ${t(`theme.${choice.value}` as 'theme.auto')}`)

/**
 * Which build is answering and which store it reads. Provenance, not a
 * headline: it rides in the freshness tooltip instead of taking a column.
 */
const statusTitle = computed(() => {
  const lines = [t('status.title')]
  if (status.value?.db) lines.push(`${t('status.db')}: ${status.value.db}`)
  const build: string[] = []
  if (status.value?.version) build.push(status.value.version)
  if (status.value?.schema_version) build.push(t('status.schema', { n: status.value.schema_version }))
  if (build.length) lines.push(build.join(' · '))
  return lines.join('\n')
})
</script>

<template>
  <!-- Two pages do not need a sidebar. The brand ink carries one bar: who
       this is, where you are, how fresh the numbers are. -->
  <header class="appbar">
    <div class="brand">
      <!-- The mark is the colour key: four stripes in bucket order, the same
           four hues every bar and every legend uses. Learn it once here. -->
      <span class="mark" aria-hidden="true">
        <i v-for="s in SERIES" :key="s.bucket" :style="{ background: `var(${s.cssVar})` }" />
      </span>
      <span class="wordmark">harness-observer</span>
    </div>

    <nav class="nav" :aria-label="t('nav.aria')">
      <a href="/" :aria-current="state.view === 'overview' ? 'page' : undefined" @click.prevent="navigate('overview')">
        {{ t('nav.overview') }}
      </a>
      <a href="/logs" :aria-current="state.view === 'logs' ? 'page' : undefined" @click.prevent="navigate('logs')">
        {{ t('nav.logs') }}
      </a>
    </nav>

    <div class="spacer" />

    <!-- Freshness lives in the bar, not in the toolbar: it answers a question
         people have continuously, not one they act on. -->
    <div class="status" :class="tone" :title="statusTitle" role="status">
      <span class="dot" aria-hidden="true" />
      <span class="status-text">{{ label }}</span>
    </div>

    <div class="utils">
      <button type="button" class="util lang" :title="t('lang.title')" :aria-label="t('lang.title')" @click="toggleLang()">
        {{ currentLang === 'zh' ? '中' : 'EN' }}
      </button>
      <button type="button" class="util" :title="themeLabel" :aria-label="themeLabel" @click="cycle()">
        <Icon :name="themeIcon" />
      </button>
    </div>
  </header>
</template>

<style scoped>
.appbar {
  display: flex;
  align-items: center;
  gap: 28px;
  height: 52px;
  padding: 0 var(--gutter);
  background: var(--brand);
  color: var(--on-brand);
}

.appbar :focus-visible {
  outline-color: var(--on-brand);
}

.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: none;
}

.mark {
  display: flex;
  gap: 2px;
  width: 16px;
  height: 20px;
}

.mark i {
  flex: 1;
  border-radius: 1px;
}

.wordmark {
  font-size: 15px;
  font-weight: 700;
  letter-spacing: -0.02em;
}

/* Tabs on the bar: the current page is marked by a rule along the bar's
 * bottom edge, the same way the breakdown tabs mark their cut. */
.nav {
  display: flex;
  align-self: stretch;
  gap: 4px;
}

.nav a {
  position: relative;
  display: flex;
  align-items: center;
  padding: 0 12px;
  color: var(--on-brand-muted);
  text-decoration: none;
  font-size: 14px;
  font-weight: 550;
  transition: color 0.12s ease;
}

.nav a::after {
  content: '';
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 0;
  height: 3px;
  border-radius: 2px 2px 0 0;
  background: transparent;
}

.nav a:hover {
  color: var(--on-brand);
}

.nav a[aria-current='page'] {
  color: var(--on-brand);
  font-weight: 650;
}

.nav a[aria-current='page']::after {
  background: var(--on-brand);
}

.spacer {
  flex: 1;
}

.status {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 0;
  font-size: 12.5px;
  color: var(--on-brand-muted);
  cursor: default;
}

.status-text {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dot {
  flex: none;
  width: 7px;
  height: 7px;
  border-radius: 50%;
  background: var(--on-brand-muted);
}

.status.ok .dot {
  background: var(--brand-ok);
}
.status.failed .dot {
  background: var(--brand-error);
}
.status.failed {
  color: var(--brand-error-text);
}
.status.running .dot {
  background: var(--on-brand);
  animation: pulse 1.4s ease-in-out infinite;
}

@keyframes pulse {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.25;
  }
}

.utils {
  display: flex;
  gap: 2px;
  flex: none;
  margin-left: -12px;
}

.util {
  display: inline-flex;
  align-items: center;
  justify-content: center;
  width: 32px;
  height: 32px;
  padding: 0;
  border: 0;
  border-radius: var(--r-2);
  background: transparent;
  color: var(--on-brand-muted);
  cursor: pointer;
  transition:
    background-color 0.12s ease,
    color 0.12s ease;
}

.util:hover {
  background: var(--brand-hover);
  color: var(--on-brand);
}

.lang {
  font-size: 12.5px;
  font-weight: 650;
}

/* On a narrow window the freshness keeps its dot and gives up its words;
 * the full line is still in the tooltip and the screen reader text. */
@media (max-width: 760px) {
  .appbar {
    gap: 16px;
  }
  .status-text {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
  }
  .utils {
    margin-left: -8px;
  }
}

@media (max-width: 420px) {
  .wordmark {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .status.running .dot {
    animation: none;
  }
  .nav a,
  .util {
    transition: none;
  }
}
</style>
