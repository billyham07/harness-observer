import type {
  FacetsResponse,
  Hierarchy,
  IngestStatus,
  LogPage,
  PricingResponse,
  QueryResponse,
} from './types'

/** Scalar values a request accepts as an extra query parameter. */
export type Param = string | number | undefined

export class ApiError extends Error {
  readonly status: number
  constructor(message: string, status: number) {
    super(message)
    this.name = 'ApiError'
    this.status = status
  }
}

/**
 * getJSON issues one read against the local API.
 *
 * `params` is the caller's already-serialized filter — the one owner of filter
 * serialization is useQueryState, and passing its output through untouched is
 * what keeps the chart, the tables and the Logs page from ever disagreeing
 * about which window they are describing.
 */
export async function getJSON<T>(
  path: string,
  params: URLSearchParams,
  extra: Record<string, Param> = {},
  signal?: AbortSignal,
): Promise<T> {
  const search = new URLSearchParams(params)
  for (const [key, value] of Object.entries(extra)) {
    if (value === undefined || value === '') continue
    search.set(key, String(value))
  }
  const query = search.toString()
  const res = await fetch(query ? `${path}?${query}` : path, {
    headers: { Accept: 'application/json' },
    signal,
  })
  if (!res.ok) {
    const detail = (await res.text()).trim()
    throw new ApiError(detail || res.statusText, res.status)
  }
  return (await res.json()) as T
}

export const api = {
  query: (params: URLSearchParams, extra?: Record<string, Param>, signal?: AbortSignal) =>
    getJSON<QueryResponse>('/api/query', params, extra, signal),

  hierarchy: (params: URLSearchParams, signal?: AbortSignal) =>
    getJSON<Hierarchy>('/api/hierarchy', params, {}, signal),

  facets: (params: URLSearchParams, signal?: AbortSignal) =>
    getJSON<FacetsResponse>('/api/facets', params, {}, signal),

  logs: (params: URLSearchParams, extra: Record<string, Param>, signal?: AbortSignal) =>
    getJSON<LogPage>('/api/logs', params, extra, signal),

  pricing: (params: URLSearchParams, signal?: AbortSignal) =>
    getJSON<PricingResponse>('/api/pricing', params, {}, signal).catch(
      () => ({ available: false }) as PricingResponse,
    ),

  status: (signal?: AbortSignal) => getJSON<IngestStatus>('/api/status', new URLSearchParams(), {}, signal),

  /** Ask for an ingestion run. The outcome is read back from /api/status. */
  scan: async (): Promise<void> => {
    const res = await fetch('/api/refresh', { method: 'POST' })
    if (!res.ok) throw new ApiError((await res.text()).trim() || res.statusText, res.status)
  },
}

/** True when a rejection is just an aborted in-flight request, not a failure. */
export function isAbort(err: unknown): boolean {
  return err instanceof DOMException ? err.name === 'AbortError' : (err as { name?: string })?.name === 'AbortError'
}
