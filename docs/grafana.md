# Grafana / Prometheus integration (optional)

**This is no longer the primary way to look at the data.** `harness-observer web`
serves a built-in dashboard on loopback and owns its own incremental ingestion;
see [`dashboard.md`](dashboard.md). In particular the hierarchical
`harness → model → reasoning_effort` selector is not expressible as Grafana
template variables without losing tuple correlation, which is why it is not
attempted here.

What follows is still supported and cheap to maintain, for two cases: an
existing Grafana install, and long-term Prometheus retention.

Two surfaces, split by what each is good at:

- **SQL** (via the [frser-sqlite-datasource](https://grafana.com/grafana/plugins/frser-sqlite-datasource/) plugin) is the detailed source. Arbitrary time ranges, high-cardinality dimensions (session, project, cwd), per-record provenance.
- **Prometheus** (`/metrics`) is for aggregate time series only. Session ids, project paths and cwd are **not** labels; putting them there would blow up cardinality for no benefit that the store does not already provide.

## Starting the endpoints

```bash
harness-observer ingest
harness-observer serve --addr 127.0.0.1:9464
```

`serve` binds to loopback by default: this is local usage data about your own machine.

- `/api/health` — schema version and database path
- `/api/query` — JSON aggregation, same filters as `harness-observer query`
- `/metrics` — Prometheus text exposition

Ingestion is a separate command on purpose: `serve` never writes and never touches session files. Run `ingest` from a scheduled task/cron as often as you want fresh data (it skips unchanged files, so a frequent run is cheap). `harness-observer web` does this in-process instead, which is why it needs no scheduler.

Prometheus scrape config:

```yaml
scrape_configs:
  - job_name: harness-observer
    scrape_interval: 60s
    static_configs:
      - targets: ["127.0.0.1:9464"]
```

## Exposed metrics

| Metric | Type | Labels |
| --- | --- | --- |
| `harness_observer_tokens_total` | counter | `harness`, `client`, `provider`, `model`, `usage_basis`, `bucket` (`input`/`cache_read`/`cache_write`/`output`/`reasoning`) |
| `harness_observer_turns_total` | counter | `harness`, `client`, `provider`, `model`, `usage_basis` |
| `harness_observer_sessions_total` | gauge | same |
| `harness_observer_tool_calls_total` | counter | same |
| `harness_observer_tool_errors_total` | counter | same |
| `harness_observer_cost_usd_total` | counter | same — cost the source persisted, not an estimate |
| `harness_observer_estimated_api_cost_usd_total` | counter | same — API-equivalent estimate at published rates, **not** billed spend |
| `harness_observer_unpriced_tokens_total` | counter | same — observed tokens no published rate was found for |
| `harness_observer_last_activity_timestamp_seconds` | gauge | same |
| `harness_observer_schema_version` | gauge | — |

Counters are re-derived from the store on every scrape, so they are monotonic as long as the database is not rebuilt.

### Useful PromQL

```promql
# tokens over time, by canonical bucket
sum by (bucket) (rate(harness_observer_tokens_total[5m]))

# cache hit ratio per harness
sum by (harness) (harness_observer_tokens_total{bucket="cache_read"})
  / sum by (harness) (harness_observer_tokens_total{bucket=~"input|cache_read|cache_write"})

# usage by model x harness
sum by (harness, model) (harness_observer_tokens_total)

# strict mode: exclude estimated evidence
sum by (harness) (harness_observer_tokens_total{usage_basis!="estimated"})

# tool error rate per harness
sum by (harness) (rate(harness_observer_tool_errors_total[15m]))
  / clamp_min(sum by (harness) (rate(harness_observer_tool_calls_total[15m])), 1)
```

## SQLite panels

Grafana's native time picker maps onto `ts_unix`, so every panel below honours a fully custom range with `$__from`/`$__to` (milliseconds).

```sql
-- tokens over time, by canonical bucket
SELECT ts_unix AS time,
       SUM(input_tokens)       AS "fresh input",
       SUM(cache_read_tokens)  AS "cache read",
       SUM(cache_write_tokens) AS "cache write",
       SUM(output_tokens)      AS "output",
       SUM(reasoning_tokens)   AS "reasoning (subset of output)"
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY ts_unix / 3600;

-- usage by model x harness
SELECT harness, model,
       SUM(prompt_tokens + output_tokens) AS observed_tokens,
       COUNT(*) AS turns,
       COUNT(DISTINCT session_id) AS sessions
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY harness, model
ORDER BY observed_tokens DESC;

-- cache hit ratio per harness
SELECT harness,
       1.0 * SUM(cache_read_tokens) / NULLIF(SUM(prompt_tokens), 0) AS cache_hit_ratio
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY harness;

-- strict mode: drop estimated evidence
SELECT harness, SUM(prompt_tokens + output_tokens) AS observed_tokens
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
  AND usage_basis <> 'estimated'
GROUP BY harness;

-- tool activity by harness
SELECT harness, SUM(tool_calls) AS tool_calls, SUM(tool_errors) AS tool_errors
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY harness;

-- model x harness x reasoning effort (the dashboard's primary comparison)
SELECT harness, model, COALESCE(reasoning_effort, '(unrecorded)') AS effort,
       SUM(prompt_tokens + output_tokens) AS observed_tokens,
       COUNT(*) AS turns
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY harness, model, effort
ORDER BY observed_tokens DESC;

-- where non-assistant usage hides (Pi/OMP nested tool LLM calls, compaction)
SELECT harness, event_kind,
       COUNT(*) AS turns,
       SUM(prompt_tokens + output_tokens) AS observed_tokens
FROM agent_turns
WHERE ts_unix BETWEEN $__from/1000 AND $__to/1000
GROUP BY harness, event_kind;
```

A ready-to-import dashboard using these panels is in [`grafana-dashboard.json`](grafana-dashboard.json). It expects a SQLite datasource pointed at the database path that `harness-observer doctor` prints.

Note the limitation this file exists to illustrate: `$harness`, `$model` and
`$effort` as separate template variables are independent `IN` lists, so
selecting `sol/medium` and `gpt-5.5/xhigh` there also matches `gpt-5.5/medium`
and `sol/xhigh`. The built-in dashboard sends correlated tuples instead.
