import { ref, shallowRef } from 'vue'
import { api, isAbort } from '@/api/client'
import type { BucketName, QueryRow } from '@/api/types'
import { t } from '@/i18n'
import { Latest } from '@/lib/latest'
import { useBanner } from './useBanner'
import { setFacets, setHierarchy } from './useFilterOptions'
import { autoBucket, filterParams } from './useQueryState'
import { setPricingStatus } from './usePricing'

export interface OverviewData {
  bucket: BucketName
  totals: QueryRow | null
  timeline: QueryRow[]
  tuples: QueryRow[]
  harness: QueryRow[]
  models: QueryRow[]
  projects: QueryRow[]
  clients: QueryRow[]
  basis: QueryRow[]
}

const empty: OverviewData = {
  bucket: 'day',
  totals: null,
  timeline: [],
  tuples: [],
  harness: [],
  models: [],
  projects: [],
  clients: [],
  basis: [],
}

const data = shallowRef<OverviewData>(empty)
const loading = ref(false)
const loaded = ref(false)
const latest = new Latest()

/**
 * load fetches one coherent picture of the current filter.
 *
 * All of it goes out at once against a single serialization of the filter, so
 * the KPI row, the chart and the tables can never be describing three
 * different moments. A response that arrives after the user has moved on is
 * dropped rather than painted.
 */
async function load() {
  const { id, signal } = latest.begin()
  const banner = useBanner()
  const params = filterParams()
  const bucket = autoBucket()
  loading.value = true
  try {
    const [totals, timeline, tuples, harness, models, projects, clients, basis, hierarchy, facets, pricing] =
      await Promise.all([
        api.query(params, {}, signal),
        api.query(params, { bucket }, signal),
        api.query(params, { group: 'harness,model,effort', order: 'tokens', limit: 100 }, signal),
        api.query(params, { group: 'harness', order: 'tokens' }, signal),
        api.query(params, { group: 'model', order: 'tokens', limit: 50 }, signal),
        api.query(params, { group: 'project', order: 'tokens', limit: 50 }, signal),
        api.query(params, { group: 'client', order: 'tokens', limit: 50 }, signal),
        api.query(params, { group: 'basis,semantics', order: 'tokens' }, signal),
        api.hierarchy(params, signal),
        api.facets(params, signal),
        api.pricing(params, signal),
      ])
    if (!latest.isCurrent(id)) return

    setHierarchy(hierarchy)
    setFacets(facets.facets ?? [])
    setPricingStatus(pricing.available && pricing.status ? pricing.status : null)
    data.value = {
      bucket,
      totals: totals.rows?.[0] ?? null,
      timeline: timeline.rows ?? [],
      tuples: tuples.rows ?? [],
      harness: harness.rows ?? [],
      models: models.rows ?? [],
      projects: projects.rows ?? [],
      clients: clients.rows ?? [],
      basis: basis.rows ?? [],
    }
    loaded.value = true
    banner.clear()
  } catch (err) {
    if (isAbort(err) || !latest.isCurrent(id)) return
    banner.set(t('query.failed', { error: (err as Error).message }))
  } finally {
    if (latest.isCurrent(id)) loading.value = false
  }
}

export function useOverview() {
  return { data, loading, loaded, load, cancel: () => latest.cancel() }
}
