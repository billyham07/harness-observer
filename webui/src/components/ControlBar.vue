<script setup lang="ts">
import { computed, ref } from 'vue'
import { NPopover, NSelect, NSwitch } from 'naive-ui'
import { currentLang, t, toggleLang } from '@/i18n'
import { shortDate } from '@/lib/format'
import { PRESETS } from '@/lib/time'
import { setAuto, state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'
import { useTheme } from '@/composables/useTheme'
import TimeRangePicker from './TimeRangePicker.vue'

defineProps<{ navOpen: boolean }>()
const emit = defineEmits<{ (e: 'toggle-nav'): void }>()

const { scanning, scanNow } = useRefresh()
const { choice, cycle } = useTheme()
const rangeOpen = ref(false)

const rangeActive = computed(() => !!state.from || !!state.to)

const rangeLabel = computed(() => {
  if (!state.from && !state.to) return t('range.value.all')
  const preset = PRESETS.find((p) => p.key === state.preset)
  if (preset && preset.key !== 'all') return t(preset.label)
  const from = state.from ? shortDate(state.from) : t('range.value.start')
  const to = state.to ? shortDate(state.to) : t('range.value.now')
  return `${from} → ${to}`
})

const refreshLabel = computed(() =>
  state.auto ? t('refresh.everyN', { seconds: state.interval }) : t('refresh.manual'),
)

const intervalOptions = computed(() => [
  { label: t('refresh.15s'), value: 15 },
  { label: t('refresh.30s'), value: 30 },
  { label: t('refresh.1m'), value: 60 },
  { label: t('refresh.5m'), value: 300 },
])
</script>

<template>
  <header class="topbar">
    <button
      type="button"
      class="btn nav-toggle"
      :aria-expanded="navOpen"
      :title="t('nav.menu')"
      @click="emit('toggle-nav')"
    >
      ☰
    </button>

    <NPopover
      v-model:show="rangeOpen"
      trigger="click"
      placement="bottom-start"
      :show-arrow="false"
    >
      <template #trigger>
        <button type="button" class="control" :class="{ active: rangeActive }">
          <span class="control-label">{{ t('range.label') }}</span>
          <span class="control-value">{{ rangeLabel }}</span>
          <span class="caret" aria-hidden="true" />
        </button>
      </template>
      <TimeRangePicker @done="rangeOpen = false" />
    </NPopover>

    <div class="spacer" />

    <NPopover trigger="click" placement="bottom-end" :show-arrow="false">
      <template #trigger>
        <button type="button" class="control" :class="{ active: state.auto }" :title="t('refresh.title')">
          <span class="control-value">{{ refreshLabel }}</span>
          <span class="caret" aria-hidden="true" />
        </button>
      </template>
      <div class="refresh-pop">
        <div class="pop-head">{{ t('refresh.head') }}</div>
        <div class="pop-body">
          <label class="row">
            <NSwitch size="small" :value="state.auto" @update:value="setAuto($event as boolean)" />
            <span>{{ t('refresh.auto') }}</span>
          </label>
          <label class="field">
            <span class="eyebrow">{{ t('refresh.every') }}</span>
            <NSelect
              size="small"
              :value="state.interval"
              :options="intervalOptions"
              @update:value="setAuto(state.auto, $event as number)"
            />
          </label>
        </div>
      </div>
    </NPopover>

    <button type="button" class="btn btn-primary" :disabled="scanning" @click="scanNow()">
      {{ scanning ? t('scan.running') : t('scan.now') }}
    </button>

    <button type="button" class="btn icon" :title="t('lang.title')" @click="toggleLang()">
      {{ currentLang === 'zh' ? '中' : 'EN' }}
    </button>
    <button type="button" class="btn icon" :title="t('theme.title')" @click="cycle()">
      {{ t(`theme.${choice}` as 'theme.auto') }}
    </button>
  </header>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 20px var(--gutter) 8px;
}

.spacer {
  flex: 1;
}

.icon {
  min-width: 44px;
  justify-content: center;
  color: var(--muted);
}

.nav-toggle {
  display: none;
  width: 36px;
  height: 36px;
  justify-content: center;
  padding: 0;
}

.refresh-pop {
  width: 250px;
}

.row {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 12.5px;
  cursor: pointer;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 12px;
}

@media (max-width: 900px) {
  .nav-toggle {
    display: inline-flex;
  }
}
</style>
