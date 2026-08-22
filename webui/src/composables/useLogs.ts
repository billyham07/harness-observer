import { ref, shallowRef } from 'vue'
import { api, isAbort } from '@/api/client'
import type { LogPage, LogRow } from '@/api/types'
import { t } from '@/i18n'
import { Latest } from '@/lib/latest'
import { useBanner } from './useBanner'
import { setFacets, setHierarchy } from './useFilterOptions'
import { filterParams, state } from './useQueryState'

const page = shallowRef<LogPage | null>(null)
const loading = ref(false)
const error = ref('')
/** The record shown in the detail drawer, or null when it is closed. */
const openRecord = ref<string | null>(null)
const latest = new Latest()

/**
 * load asks the server for exactly one page.
 *
 * Filtering, ordering and paging happen in DuckDB. Nothing here slices a local
 * array: a row-level list that is paginated in the browser is a list that
 * silently lies about its total the moment the store grows.
 */
async function load() {
  const { id, signal } = latest.begin()
  const banner = useBanner()
  loading.value = true
  try {
    const params = filterParams()
    const [result, hierarchy, facets] = await Promise.all([
      api.logs(params, { page: state.logsPage, page_size: state.logsPageSize }, signal),
      api.hierarchy(params, signal),
      api.facets(params, signal),
    ])
    if (!latest.isCurrent(id)) return
    page.value = result
    setHierarchy(hierarchy)
    setFacets(facets.facets ?? [])
    error.value = ''
    banner.clear()
  } catch (err) {
    if (isAbort(err) || !latest.isCurrent(id)) return
    const message = t('query.failed', { error: (err as Error).message })
    page.value = null
    error.value = message
    banner.set(message)
  } finally {
    if (latest.isCurrent(id)) loading.value = false
  }
}

/** Which of the three empty states this is, in the interface's own words. */
export function emptyMessage(result: LogPage | null): string {
  const empty = result?.empty_state
  if (!empty) return t('logs.empty.page')
  if (empty.reason === 'not-ingested') return t('logs.empty.ingest')
  if (empty.reason === 'empty-range') return t('logs.empty.range')
  return t('logs.empty.filters')
}

export function useLogs() {
  const openDrawer = (row: LogRow) => {
    openRecord.value = row.record_id
  }
  const closeDrawer = () => {
    openRecord.value = null
  }
  return { page, loading, error, openRecord, load, openDrawer, closeDrawer, emptyMessage }
}
