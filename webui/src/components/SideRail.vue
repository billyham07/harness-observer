<script setup lang="ts">
import { computed } from 'vue'
import { t } from '@/i18n'
import { SERIES } from '@/lib/palette'
import { navigate, state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'

defineProps<{ open: boolean }>()
const emit = defineEmits<{ (e: 'close'): void }>()

const { status, label, tone } = useRefresh()
const dbPath = computed(() => status.value?.db ?? '')
/** The store path is provenance, not a headline: two segments, full path on hover. */
const dbShort = computed(() => {
  const parts = dbPath.value.split(/[\\/]/).filter(Boolean)
  return parts.length > 2 ? `…/${parts.slice(-2).join('/')}` : dbPath.value
})
const schema = computed(() => status.value?.schema_version ?? 0)

/** Which build is answering, and what it can read. One line, quietly. */
const build = computed(() => {
  const parts: string[] = []
  if (status.value?.version) parts.push(status.value.version)
  if (schema.value) parts.push(t('status.schema', { n: schema.value }))
  return parts.join(' · ')
})

function go(view: 'overview' | 'logs') {
  navigate(view)
  emit('close')
}
</script>

<template>
  <aside class="rail" :class="{ open }">
    <div class="brand">
      <!-- The mark is the colour key: four stripes in bucket order, the same
           four hues every bar and every legend uses. Learn it once here. -->
      <span class="mark" aria-hidden="true">
        <i v-for="s in SERIES" :key="s.bucket" :style="{ background: `var(${s.cssVar})` }" />
      </span>
      <span class="wordmark num">
        <span class="w-1">harness</span><span class="w-sep">-</span><span class="w-2">observer</span>
      </span>
      <span class="brand-sub eyebrow">{{ t('brand.sub') }}</span>
    </div>

    <nav class="nav" :aria-label="t('nav.aria')">
      <a
        href="/"
        :aria-current="state.view === 'overview' ? 'page' : undefined"
        @click.prevent="go('overview')"
      >
        <span class="glyph" aria-hidden="true">▣</span>{{ t('nav.overview') }}
      </a>
      <a href="/logs" :aria-current="state.view === 'logs' ? 'page' : undefined" @click.prevent="go('logs')">
        <span class="glyph" aria-hidden="true">≡</span>{{ t('nav.logs') }}
      </a>
    </nav>

    <div class="spacer" />

    <!-- Freshness lives with the instrument, not in the toolbar: it answers a
         question people have continuously, not one they act on. -->
    <div class="status" :class="tone" :title="t('status.title')">
      <span class="dot" aria-hidden="true" />
      <span class="status-text">{{ label }}</span>
    </div>
    <dl class="store">
      <template v-if="dbPath">
        <dt class="eyebrow">{{ t('status.db') }}</dt>
        <dd class="num path" :title="dbPath">{{ dbShort }}</dd>
      </template>
      <dd v-if="build" class="num faint">{{ build }}</dd>
    </dl>
  </aside>
</template>

<style scoped>
.rail {
  flex: 0 0 var(--rail);
  display: flex;
  flex-direction: column;
  gap: 18px;
  padding: 16px 12px 14px;
  border-right: 1px solid var(--line);
  background: var(--surface);
  overflow: auto;
}

.brand {
  display: grid;
  grid-template-columns: auto 1fr;
  grid-template-rows: auto auto;
  align-items: center;
  gap: 2px 9px;
  padding: 0 6px;
}

.mark {
  grid-row: span 2;
  display: flex;
  gap: 1.5px;
  width: 15px;
  height: 22px;
  border-radius: 2px;
  overflow: hidden;
}

.mark i {
  flex: 1;
  border-radius: 1px;
}

.wordmark {
  font-size: 13px;
  font-weight: 600;
  letter-spacing: -0.02em;
}

.w-1 {
  color: var(--muted);
}
.w-sep {
  color: var(--accent);
}
.w-2 {
  color: var(--ink);
}

.brand-sub {
  font-size: 9.5px;
}

.nav {
  display: flex;
  flex-direction: column;
  gap: 1px;
}

.nav a {
  position: relative;
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 10px 7px 12px;
  border-radius: var(--r-2);
  color: var(--muted);
  text-decoration: none;
  font-size: 13px;
}

.nav a:hover {
  background: var(--surface-2);
  color: var(--ink);
}

/* The active page is marked by a rail, not a filled block: it reads as a
 * position indicator on an instrument rather than a selected menu item. */
.nav a[aria-current='page'] {
  color: var(--ink);
  font-weight: 600;
  background: var(--surface-2);
}

.nav a[aria-current='page']::before {
  content: '';
  position: absolute;
  left: 0;
  top: 7px;
  bottom: 7px;
  width: 2px;
  border-radius: 1px;
  background: var(--accent);
}

.glyph {
  width: 14px;
  text-align: center;
  color: var(--faint);
  font-size: 12px;
}

.spacer {
  flex: 1;
}

.status {
  display: flex;
  align-items: flex-start;
  gap: 7px;
  padding: 8px 10px;
  border: 1px solid var(--line-soft);
  border-radius: var(--r-2);
  background: var(--surface-2);
  font-size: 11.5px;
  line-height: 1.4;
  color: var(--muted);
}

.dot {
  flex: none;
  width: 7px;
  height: 7px;
  margin-top: 4px;
  border-radius: 50%;
  background: var(--faint);
}

.status.ok .dot {
  background: var(--ok);
}
.status.failed .dot {
  background: var(--error);
}
.status.failed {
  color: var(--error);
  border-color: var(--error-soft);
  background: var(--error-soft);
}
.status.running .dot {
  background: var(--accent);
  animation: pulse 1.4s ease-in-out infinite;
}

.status-text {
  min-width: 0;
  overflow-wrap: anywhere;
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

.store {
  margin: 0;
  padding: 0 4px;
  font-size: 10.5px;
  color: var(--faint);
}

.store dt {
  margin-bottom: 2px;
}

.store dd {
  margin: 0 0 6px;
  overflow-wrap: anywhere;
  line-height: 1.35;
}

.store .path {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

@media (max-width: 900px) {
  .rail {
    position: fixed;
    z-index: 40;
    top: 0;
    bottom: 0;
    left: 0;
    box-shadow: var(--shadow-2);
    transform: translateX(-100%);
    transition: transform 0.16s ease;
  }
  .rail.open {
    transform: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .status.running .dot {
    animation: none;
  }
  .rail {
    transition: none;
  }
}
</style>
