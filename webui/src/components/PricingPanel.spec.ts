import { config, flushPromises, mount } from '@vue/test-utils'
import { afterAll, beforeEach, describe, expect, it, vi } from 'vitest'
import PricingPanel from './PricingPanel.vue'
import { NSelect } from 'naive-ui'
import type { CatalogModel, UnpricedModel } from '@/api/types'

const { api } = vi.hoisted(() => ({
  api: {
    unpricedModels: vi.fn(),
    pricingOverrides: vi.fn(),
    pricingCatalog: vi.fn(),
    savePricingOverride: vi.fn(),
    deletePricingOverride: vi.fn(),
    refreshPricing: vi.fn(),
  },
}))

vi.mock('@/api/client', () => ({ api, ApiError: Error }))

const kimi: CatalogModel = {
  model: 'moonshot/kimi-k3',
  provider: 'moonshot',
  rates: { input: 0.000003, output: 0.000015, cache_read: 0.0000003, cache_write: 0.000003 },
}

const unpriced: UnpricedModel = {
  observed_model: 'kimi-k3-2',
  observed_tokens: 25_487_402,
  turns: 378,
}

const originalStub = config.global.stubs?.NSelect
config.global.stubs = {
  ...config.global.stubs,
  NSelect: {
    name: 'NSelect',
    props: ['value'],
    template: '<button class="select-upstream" @click="$emit(\'update:value\', \'moonshot/kimi-k3\')">select</button>',
  },
}
afterAll(() => {
  if (config.global.stubs) config.global.stubs.NSelect = originalStub
})

function response() {
  api.unpricedModels.mockResolvedValue({ models: [unpriced], count: 1 })
  api.pricingOverrides.mockResolvedValue({ overrides: [], path: 'C:/local/pricing-overrides.json' })
  api.pricingCatalog.mockResolvedValue({ models: [kimi], count: 1 })
}

async function createPanel() {
  const wrapper = mount(PricingPanel, {
    global: {
      stubs: {
        NModal: {
          props: ['show'],
          template: '<div v-if="show"><slot /></div>',
        },
      },
    },
  })
  await flushPromises()
  return wrapper
}

describe('PricingPanel', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    response()
  })

  it('chooses an official model price and reloads persisted overrides', async () => {
    const wrapper = await createPanel()
    await wrapper.find('tbody .btn').trigger('click')
    await flushPromises()
    expect(api.pricingCatalog).toHaveBeenCalledWith('kimi-k3-2')
    wrapper.findComponent(NSelect).vm.$emit('update:value', 'moonshot/kimi-k3')
    await flushPromises()
    expect(document.body.textContent).toContain('$3 / 1M')
    const saveButton = Array.from(document.body.querySelectorAll('.editor-actions .btn-primary'))[0] as HTMLButtonElement
    saveButton.click()
    await flushPromises()

    expect(api.savePricingOverride).toHaveBeenCalledWith({
      model: 'kimi-k3-2',
      upstream: 'moonshot/kimi-k3',
    })
    // The second read represents what a restarted process reads from disk.
    expect(api.pricingOverrides).toHaveBeenCalledTimes(2)
    expect(wrapper.emitted('saved')).toHaveLength(1)
    wrapper.unmount()
  })
})
