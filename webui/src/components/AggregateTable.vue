<script setup lang="ts">
import { computed, ref } from 'vue'
import type { QueryRow } from '@/api/types'
import { t, type StringKey } from '@/i18n'
import { exact, fmt, money } from '@/lib/format'
import { ALL_COLUMNS, columnValue, type Column } from '@/lib/columns'
import { COVERAGE_FLOOR, costKnown, costPriced } from '@/composables/usePricing'

const props = defineProps<{ dims: string[]; rows: QueryRow[] }>()

const sort = ref<{ key: string; dir: 1 | -1 }>({ key: 'observed_tokens', dir: -1 })

const columns = ALL_COLUMNS
const sortColumn = computed(() => columns.find((c) => c.key === sort.value.key))

const sorted = computed(() => {
  const { key, dir } = sort.value
  const rows = [...props.rows]
  if (props.dims.includes(key)) {
    // A dimension sorts by name; ascending reads top-down alphabetically, so
    // the arrow and the order agree.
    return rows.sort(
      (a, b) => ((a.group ?? {})[key] || '').localeCompare((b.group ?? {})[key] || '') * dir * -1,
    )
  }
  const read = (r: QueryRow) =>
    sortColumn.value ? columnValue(sortColumn.value, r) : Number((r as unknown as Record<string, number>)[key] || 0)
  return rows.sort((a, b) => (read(a) - read(b)) * dir)
})

const maxObserved = computed(() => Math.max(1, ...props.rows.map((r) => Number(r.observed_tokens || 0))))

function toggleSort(key: string) {
  sort.value = sort.value.key === key ? { key, dir: (-sort.value.dir as 1 | -1) } : { key, dir: -1 }
}

const ariaSort = (key: string) =>
  sort.value.key === key ? (sort.value.dir < 0 ? 'descending' : 'ascending') : 'none'

function cellText(col: Column, row: QueryRow): string {
  const value = columnValue(col, row)
  if (col.format) return col.format(value, row)
  // A row the catalog could not price has an unknown cost, not a zero one.
  if (col.money) return costKnown(row) ? money(value, col.digits ?? 2) : '–'
  return fmt(value)
}

function cellTitle(col: Column, row: QueryRow): string {
  if (!col.cost) return exact(columnValue(col, row))
  if (!costPriced(row)) return t('cost.unavailable')
  if (!costKnown(row)) return t('cost.unknownRow')
  return t('cost.tip')
}

const isPartial = (col: Column, row: QueryRow) =>
  !!col.cost && Number(row.pricing_coverage_ratio) < COVERAGE_FLOOR
</script>

<template>
  <div class="table-wrap">
    <div v-if="!rows.length" class="empty">{{ t('empty.filters') }}</div>
    <table v-else class="grid">
      <thead>
        <tr>
          <th
            v-for="dim in dims"
            :key="dim"
            class="text"
            :class="{ sorted: sort.key === dim }"
            :aria-sort="ariaSort(dim)"
            @click="toggleSort(dim)"
          >
            {{ t(`dim.${dim}` as StringKey) }}
            <span v-if="sort.key === dim" aria-hidden="true">{{ sort.dir < 0 ? '↓' : '↑' }}</span>
          </th>
          <th
            v-for="col in columns"
            :key="col.key"
            :class="{ cost: col.cost, sorted: sort.key === col.key }"
            :aria-sort="ariaSort(col.key)"
            @click="toggleSort(col.key)"
          >
            {{ t(col.label) }}
            <span v-if="sort.key === col.key" aria-hidden="true">{{ sort.dir < 0 ? '↓' : '↑' }}</span>
          </th>
        </tr>
      </thead>
      <tbody>
        <tr v-for="(row, index) in sorted" :key="index">
          <td v-for="(dim, i) in dims" :key="dim" class="text" :class="{ share: i === 0 }">
            <span
              v-if="i === 0"
              class="bar"
              :style="{ width: `${((100 * Number(row.observed_tokens || 0)) / maxObserved).toFixed(2)}%` }"
            />
            <span class="v" :class="{ unknown: !(row.group ?? {})[dim] }">{{
              (row.group ?? {})[dim] || t('none')
            }}</span>
          </td>
          <td
            v-for="col in columns"
            :key="col.key"
            class="num"
            :class="{ cost: col.cost, partial: isPartial(col, row) }"
            :title="cellTitle(col, row)"
          >
            {{ cellText(col, row) }}
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<style scoped>
.table-wrap {
  max-height: 460px;
  overflow: auto;
}
</style>
