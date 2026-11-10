<script setup lang="ts">
import { computed, ref } from 'vue'
import { NModal, NSelect } from 'naive-ui'
import { api, ApiError } from '@/api/client'
import type { CatalogModel, PricingOverride, UnpricedModel } from '@/api/types'
import { exact, fmt } from '@/lib/format'
import { t } from '@/i18n'

const emit = defineEmits<{ (e: 'saved'): void }>()

const loading = ref(false)
const error = ref('')
const unpriced = ref<UnpricedModel[]>([])
const overrides = ref<PricingOverride[]>([])
const configPath = ref('')
const refreshing = ref(false)

const editorOpen = ref(false)
const observedModel = ref('')
const upstream = ref<string | null>(null)
const saving = ref(false)
const catalogLoading = ref(false)
const catalog = ref<CatalogModel[]>([])
const catalogError = ref('')
let searchSequence = 0

const upstreamOptions = computed(() =>
  catalog.value.map((model) => ({
    label: model.model,
    value: model.model,
  })),
)

const selected = computed(() => catalog.value.find((model) => model.model === upstream.value) ?? null)

function rate(value: number | undefined) {
  return value === undefined ? '—' : `$${Number((value * 1e6).toFixed(6))}`
}

async function load() {
  loading.value = true
  error.value = ''
  try {
    const [unpricedResult, overrideResult] = await Promise.all([
      api.unpricedModels(new URLSearchParams()),
      api.pricingOverrides(),
    ])
    unpriced.value = unpricedResult.models ?? []
    overrides.value = overrideResult.overrides ?? []
    configPath.value = overrideResult.path
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    loading.value = false
  }
}

async function searchCatalog(query: string) {
  const sequence = ++searchSequence
  catalogLoading.value = true
  catalogError.value = ''
  try {
    const result = await api.pricingCatalog(query)
    if (sequence !== searchSequence) return
    catalog.value = result.models ?? []
  } catch (err) {
    if (sequence !== searchSequence) return
    catalogError.value = (err as Error).message
  } finally {
    if (sequence === searchSequence) catalogLoading.value = false
  }
}

async function openEditor(model: string, existing?: PricingOverride) {
  observedModel.value = model
  upstream.value = existing?.upstream ?? null
  editorOpen.value = true
  await searchCatalog(existing?.upstream ?? model)
}

async function save() {
  if (!upstream.value) return
  saving.value = true
  catalogError.value = ''
  try {
    await api.savePricingOverride({ model: observedModel.value, upstream: upstream.value })
    editorOpen.value = false
    await load()
    emit('saved')
  } catch (err) {
    catalogError.value = (err as ApiError).message
  } finally {
    saving.value = false
  }
}

async function remove(model: string) {
  error.value = ''
  try {
    await api.deletePricingOverride(model)
    await load()
    emit('saved')
  } catch (err) {
    error.value = (err as Error).message
  }
}

async function refreshCatalog() {
  refreshing.value = true
  error.value = ''
  try {
    const result = await api.refreshPricing()
    if (result.error) error.value = result.error
    await load()
    emit('saved')
  } catch (err) {
    error.value = (err as Error).message
  } finally {
    refreshing.value = false
  }
}

void load()
</script>

<template>
  <section class="panel pricing-settings">
    <div class="panel-head">
      <h2>{{ t('pricing.settings') }}</h2>
      <button type="button" class="btn" :disabled="refreshing" @click="refreshCatalog">
        {{ refreshing ? t('pricing.refreshing') : t('pricing.refresh') }}
      </button>
    </div>
    <p class="panel-note">{{ t('pricing.settings.hint') }}</p>
    <p v-if="error" class="error">{{ error }}</p>

    <div v-if="overrides.length" class="override-list">
      <h3>{{ t('pricing.overrides') }}</h3>
      <div v-for="item in overrides" :key="item.model" class="override-row">
        <span class="code">{{ item.model }}</span>
        <span class="arrow">→</span>
        <span class="code">{{ item.upstream || t('pricing.customRates') }}</span>
        <button type="button" class="btn btn-ghost" @click="remove(item.model)">{{ t('pricing.remove') }}</button>
      </div>
    </div>

    <h3 class="unpriced-head">{{ t('pricing.unpriced.head') }}</h3>
    <div v-if="loading" class="empty">{{ t('loading') }}</div>
    <div v-else-if="!unpriced.length" class="empty">{{ t('pricing.allPriced') }}</div>
    <div v-else class="table-wrap">
      <table class="grid">
        <thead>
          <tr>
            <th class="text">{{ t('logs.col.model') }}</th>
            <th>{{ t('col.observed') }}</th>
            <th>{{ t('col.turns') }}</th>
            <th class="text">{{ t('pricing.reason') }}</th>
            <th class="text">{{ t('pricing.action') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-for="model in unpriced" :key="`${model.harness}/${model.observed_model}`">
            <td class="text code">{{ model.observed_model || '—' }}</td>
            <td class="num">{{ fmt(model.observed_tokens) }}</td>
            <td class="num">{{ exact(model.turns) }}</td>
            <td class="text reason">{{ model.reason || '—' }}</td>
            <td class="text">
              <button type="button" class="btn" @click="openEditor(model.observed_model)">
                {{ t('pricing.choose') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <p v-if="configPath" class="config-path code" :title="configPath">{{ configPath }}</p>
  </section>

  <NModal v-model:show="editorOpen" preset="card" class="pricing-modal" :title="t('pricing.editor.title')">
    <div class="editor">
      <label class="field">
        <span class="eyebrow">{{ t('pricing.observed') }}</span>
        <input class="model-input code" :value="observedModel" readonly />
      </label>
      <label class="field">
        <span class="eyebrow">{{ t('pricing.upstream') }}</span>
        <NSelect
          v-model:value="upstream"
          filterable
          remote
          clearable
          :loading="catalogLoading"
          :options="upstreamOptions"
          :placeholder="t('pricing.upstream.ph')"
          @search="searchCatalog"
        />
      </label>
      <p class="hint">{{ t('pricing.upstream.hint') }}</p>
      <p v-if="catalogError" class="error">{{ catalogError }}</p>
      <div v-if="selected" class="rates">
        <div v-for="bucket in ['input', 'output', 'cache_read', 'cache_write']" :key="bucket" class="rate-row">
          <span>{{ t(`pricing.rate.${bucket}`) }}</span>
          <span class="num">{{ rate(selected.rates[bucket as keyof typeof selected.rates]) }} / 1M</span>
        </div>
      </div>
      <div class="editor-actions">
        <button type="button" class="btn" @click="editorOpen = false">{{ t('pricing.cancel') }}</button>
        <button type="button" class="btn btn-primary" :disabled="!upstream || saving" @click="save">
          {{ saving ? t('pricing.saving') : t('pricing.save') }}
        </button>
      </div>
    </div>
  </NModal>
</template>

<style scoped>
.panel-note {
  padding-top: 0;
}
.error {
  color: var(--warn);
  white-space: pre-wrap;
}
h3 {
  margin: 18px 0 8px;
  font-size: 13px;
}
.override-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) auto minmax(0, 1fr) auto;
  align-items: center;
  gap: 10px;
  padding: 7px 0;
  border-bottom: 1px solid var(--line-soft);
}
.override-row .code {
  overflow-wrap: anywhere;
}
.arrow {
  color: var(--faint);
}
.reason {
  max-width: 360px;
  white-space: normal !important;
  color: var(--muted);
}
.config-path {
  margin: 14px 0 0;
  color: var(--faint);
  font-size: 11px;
  overflow-wrap: anywhere;
}
.editor {
  display: flex;
  flex-direction: column;
  gap: 14px;
  min-width: min(540px, 75vw);
}
.field {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.model-input {
  height: 34px;
  padding: 0 10px;
  border: 1px solid var(--line);
  border-radius: var(--r-2);
  background: var(--surface-2);
  color: var(--ink);
}
.hint {
  margin: -6px 0 0;
  color: var(--faint);
  font-size: 12px;
}
.rates {
  border-top: 1px solid var(--line);
}
.rate-row {
  display: flex;
  justify-content: space-between;
  padding: 8px 0;
  border-bottom: 1px solid var(--line-soft);
  font-size: 13px;
}
.editor-actions {
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
</style>
