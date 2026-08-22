import { computed, ref, watch } from 'vue'
import { api } from '@/api/client'
import type { IngestStatus } from '@/api/types'
import { t } from '@/i18n'
import { exact } from '@/lib/format'
import { useBanner } from './useBanner'
import { useLogs } from './useLogs'
import { useOverview } from './useOverview'
import { state } from './useQueryState'

const status = ref<IngestStatus | null>(null)
const offline = ref(false)
const scanning = ref(false)
/** Ticks once a second so "scanned 12s ago" ages without a re-query. */
const now = ref(Date.now())

let pollTimer: ReturnType<typeof setInterval> | null = null
let autoTimer: ReturnType<typeof setInterval> | null = null
let clockTimer: ReturnType<typeof setInterval> | null = null

/** A refresh replaces data. Filters, range and open popovers are user state. */
function reloadActiveView() {
  return state.view === 'logs' ? useLogs().load() : useOverview().load()
}

async function pollStatus(): Promise<IngestStatus | null> {
  try {
    status.value = await api.status()
    offline.value = false
    return status.value
  } catch {
    offline.value = true
    return null
  }
}

/**
 * Scan now is ingest-then-query: the point of the button is to pick up session
 * files written since the last scan, not just to re-run the same query.
 */
async function scanNow() {
  if (scanning.value) return
  scanning.value = true
  try {
    await api.scan()
    for (let i = 0; i < 600; i++) {
      const st = await pollStatus()
      if (st && !st.running && st.runs > 0) break
      await new Promise((resolve) => setTimeout(resolve, 500))
    }
    await reloadActiveView()
  } catch (err) {
    useBanner().set(t('scan.failed', { error: (err as Error).message }))
  } finally {
    scanning.value = false
  }
}

function scheduleAuto() {
  if (autoTimer) clearInterval(autoTimer)
  autoTimer = state.auto ? setInterval(reloadActiveView, state.interval * 1000) : null
}

function describeStatus(st: IngestStatus | null, at: number): string {
  if (offline.value) return t('status.offline')
  if (!st) return t('status.unknown')
  if (st.running) return t('status.running')
  if (st.last_error) return t('status.error', { error: st.last_error })
  if (!st.last_finish) return t('status.never')
  const seconds = Math.max(0, Math.round((at - new Date(st.last_finish).getTime()) / 1000))
  const ago =
    seconds < 60
      ? t('ago.seconds', { n: seconds })
      : seconds < 3600
        ? t('ago.minutes', { n: Math.round(seconds / 60) })
        : t('ago.hours', { n: Math.round(seconds / 3600) })
  const added = (st.last_stats ?? []).reduce((n, s) => n + (s.turns_ingested || 0), 0)
  return t('status.ok', { ago, rows: exact(added), ms: st.last_duration_ms })
}

export type StatusTone = 'ok' | 'running' | 'failed' | 'unknown'

export function useRefresh() {
  const label = computed(() => describeStatus(status.value, now.value))
  const tone = computed<StatusTone>(() => {
    if (offline.value || status.value?.last_error) return 'failed'
    if (status.value?.running || scanning.value) return 'running'
    if (status.value?.last_finish) return 'ok'
    return 'unknown'
  })

  function start() {
    void pollStatus()
    if (!pollTimer) pollTimer = setInterval(pollStatus, 5000)
    if (!clockTimer) clockTimer = setInterval(() => (now.value = Date.now()), 1000)
    scheduleAuto()
    watch(() => [state.auto, state.interval], scheduleAuto)
  }

  function stop() {
    if (pollTimer) clearInterval(pollTimer)
    if (autoTimer) clearInterval(autoTimer)
    if (clockTimer) clearInterval(clockTimer)
    pollTimer = autoTimer = clockTimer = null
  }

  return { status, offline, scanning, label, tone, start, stop, scanNow, pollStatus, reloadActiveView }
}
