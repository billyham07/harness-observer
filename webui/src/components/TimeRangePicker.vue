<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { NDatePicker, NSelect } from 'naive-ui'
import { t } from '@/i18n'
import { dayEnd, parseLocal, presetRange, rangeFromInstants, PRESETS } from '@/lib/time'
import { setBucket, setRange, state } from '@/composables/useQueryState'
import type { BucketName } from '@/api/types'

const emit = defineEmits<{ (e: 'done'): void }>()

/**
 * Pending picker state is deliberately separate from the committed query.
 * Dragging across a calendar produces a dozen intermediate ranges, and none of
 * them should reach the address bar or re-run seven queries on the way to the
 * one the user meant.
 */
const committed = () => {
  const from = parseLocal(state.from)
  const to = parseLocal(state.to)
  return from && to ? ([from.getTime(), to.getTime()] as [number, number]) : null
}

const pending = ref<[number, number] | null>(committed())
watch(
  () => [state.from, state.to],
  () => (pending.value = committed()),
)

/** No evidence can land in the future, so those days are not pickable. */
const notFuture = (ts: number) => ts > dayEnd(new Date()).getTime()

function commitRange(value: [number, number] | null) {
  const range = rangeFromInstants(value)
  setRange(range.from, range.to, range.preset)
  emit('done')
}

function applyPreset(key: string) {
  const span = presetRange(key)
  setRange(span.from, span.to, key)
  emit('done')
}

const bucketOptions = computed(() => [
  { label: t('bucket.auto'), value: '' },
  { label: t('bucket.hour'), value: 'hour' },
  { label: t('bucket.day'), value: 'day' },
  { label: t('bucket.week'), value: 'week' },
  { label: t('bucket.month'), value: 'month' },
])
</script>

<template>
  <div class="range">
    <div class="presets">
      <div class="eyebrow presets-label">{{ t('range.presets') }}</div>
      <button
        v-for="preset in PRESETS"
        :key="preset.key"
        type="button"
        class="preset"
        :class="{ on: state.preset === preset.key }"
        @click="applyPreset(preset.key)"
      >
        {{ t(preset.label) }}
      </button>
    </div>

    <div class="exact">
      <div class="eyebrow exact-label">{{ t('range.custom') }}</div>
      <!-- Seconds are in the format on purpose: a window that can only be
           picked to the minute cannot isolate one turn. -->
      <NDatePicker
        panel
        type="datetimerange"
        :value="pending"
        :actions="['clear', 'confirm']"
        format="yyyy-MM-dd HH:mm:ss"
        :is-date-disabled="notFuture"
        :start-placeholder="t('range.from.date')"
        :end-placeholder="t('range.to.date')"
        @update:value="pending = $event"
        @confirm="commitRange($event as [number, number])"
        @clear="commitRange(null)"
      />
      <div class="foot">
        <label class="foot-field">
          <span class="eyebrow">{{ t('range.bucket') }}</span>
          <NSelect
            size="small"
            :value="state.bucket"
            :options="bucketOptions"
            :consistent-menu-width="false"
            @update:value="setBucket($event as '' | BucketName)"
          />
        </label>
        <button type="button" class="btn btn-ghost" @click="commitRange(null)">
          {{ t('range.clear') }}
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.range {
  display: flex;
  align-items: stretch;
}

.presets {
  flex: none;
  display: flex;
  flex-direction: column;
  gap: 1px;
  width: 148px;
  padding: 12px 10px;
  border-right: 1px solid var(--line-soft);
  background: var(--surface-2);
}

.presets-label,
.exact-label {
  margin-bottom: 6px;
}

.preset {
  text-align: left;
  padding: 5px 8px;
  border: 1px solid transparent;
  border-radius: var(--r-1);
  background: transparent;
  color: var(--muted);
  font-size: 12.5px;
  cursor: pointer;
}

.preset:hover {
  background: var(--surface);
  color: var(--ink);
}

.preset.on {
  background: var(--surface);
  border-color: var(--accent);
  color: var(--ink);
  font-weight: 600;
}

.exact {
  padding: 12px 12px 10px;
  min-width: 0;
}

.foot {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin-top: 8px;
  padding-top: 10px;
  border-top: 1px solid var(--line-soft);
}

.foot-field {
  display: flex;
  flex-direction: column;
  gap: 4px;
  width: 130px;
}

@media (max-width: 760px) {
  .range {
    flex-direction: column;
  }
  .presets {
    width: auto;
    border-right: none;
    border-bottom: 1px solid var(--line-soft);
  }
}
</style>
