<script setup lang="ts">
import { computed, ref } from 'vue'
import { NPopover, NSelect, NSwitch } from 'naive-ui'
import { t } from '@/i18n'
import { shortDate } from '@/lib/format'
import { PRESETS } from '@/lib/time'
import { rangeChanged, setAuto, state } from '@/composables/useQueryState'
import { useRefresh } from '@/composables/useRefresh'
import Icon from './Icon.vue'
import TimeRangePicker from './TimeRangePicker.vue'

const { scanning, scanNow } = useRefresh()
const rangeOpen = ref(false)

const rangeActive = rangeChanged

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
  <div class="topbar">
    <NPopover
      v-model:show="rangeOpen"
      trigger="click"
      placement="bottom-start"
      :show-arrow="false"
    >
      <template #trigger>
        <button type="button" class="control range" :class="{ active: rangeActive }">
          <Icon name="clock" class="range-icon" />
          <span class="control-label">{{ t('range.label') }}</span>
          <span class="control-value">{{ rangeLabel }}</span>
          <Icon name="chevron-down" class="caret" />
        </button>
      </template>
      <TimeRangePicker @done="rangeOpen = false" />
    </NPopover>

    <div class="spacer" />

    <NPopover trigger="click" placement="bottom-end" :show-arrow="false">
      <template #trigger>
        <button type="button" class="control" :class="{ active: state.auto }" :title="t('refresh.title')">
          <span class="control-value">{{ refreshLabel }}</span>
          <Icon name="chevron-down" class="caret" />
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
      <Icon name="scan" :class="{ spinning: scanning }" />
      {{ scanning ? t('scan.running') : t('scan.now') }}
    </button>

  </div>
</template>

<style scoped>
.topbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 14px var(--gutter) 0;
}

.spacer {
  flex: 1;
}

.range {
  max-width: 360px;
}

.range-icon {
  color: var(--muted);
}

.spinning {
  animation: spin 0.9s linear infinite;
}

@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}

.refresh-pop {
  width: 250px;
}

.row {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 13px;
  cursor: pointer;
}

.field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin-top: 12px;
}

@media (max-width: 640px) {
  .topbar {
    flex-wrap: wrap;
    padding-top: 10px;
  }
  .range {
    order: 10;
    flex: 1 1 100%;
    max-width: none;
  }
  .range .control-label {
    display: none;
  }
}

@media (prefers-reduced-motion: reduce) {
  .spinning {
    animation: none;
  }
}
</style>
