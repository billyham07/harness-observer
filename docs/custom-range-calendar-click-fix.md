# Custom range calendar click fix

## Bug

The rebuilt Range picker has two custom-range input paths:

1. the custom two-month calendar
2. the native date/time inputs above it

The native date inputs work, but clicking dates in the custom calendar does not reliably change/apply the selected range. In practice the user is forced back to the native date inputs, which defeats the calendar added by PR #4.

This PR is a focused bug fix. Do not redesign the range picker and do not mix it with Logs/sidebar/adapter work.

## Current intended interaction

The existing code is intentionally a two-click range picker:

```text
first day click  -> choose/preview start
second day click -> choose end, commit custom range
```

A committed calendar selection must use the same canonical state as the native fields:

```text
state.from
state.to
state.preset = "custom"
```

and then flow through the same URL/query/render path. There must not be a calendar-only shadow range.

## Reproduction to verify before changing code

On current `main`:

1. Start `harness-observer web`.
2. Open the Range dropdown.
3. Use the custom calendar rather than the native `<input type=date>` fields.
4. Click a valid historical start day and then a valid end day.
5. Observe whether all of these actually update:
   - visible range label
   - native from/to date + time fields
   - `state.from` / `state.to`
   - URL query state
   - subsequent API requests / dashboard data
6. Repeat with the end day before the start day.
7. Repeat after navigating calendar months.

Do not accept a fix based only on calling date helper functions directly. Reproduce the actual click path/event lifecycle.

## Required behavior

### First click

- selects a start day
- provides a clear visible pending/start state
- keeps the Range popover open
- does not issue a query yet if the existing two-click interaction is retained
- does not get cleared by click-outside/dropdown handling triggered by the same click event

### Second click

- commits the inclusive calendar dates as the intended full-day custom range
- updates native fields immediately
- updates the Range button/summary immediately
- sets `preset=custom`
- persists the exact custom range into URL state
- triggers the normal dashboard reload exactly once
- may close the Range popover after commit

Calendar-day defaults remain:

```text
from day -> 00:00:00 local
through day -> end-of-day boundary according to the project's canonical query semantics
```

Respect the existing backend `[from,to)` semantics. If the UI currently represents an inclusive selected end day by sending a local end-of-day value, preserve compatibility unless correcting it is necessary for correctness; do not accidentally lose the final selected day's data at the boundary.

### Native date/time inputs

The existing native inputs must continue to work and remain synchronized with calendar commits. Editing the native inputs after a calendar pick must not resurrect stale calendar state.

### Range state

- calendar and native inputs are two controls over one range state
- no duplicated/shadow committed range state
- no stale `pendingStart` surviving a completed selection
- closing the popover midway through a selection may cancel the pending preview, as currently designed
- reopening after a committed range must render that range correctly
- custom selection survives query refresh, auto refresh, language/theme change, and URL serialization

## Likely area to inspect

The current implementation has:

```text
pendingStart
hoverDay
onDayClick(day)
renderCalendar()
setRange(from, to, preset)
closeDropdowns(...)
wireDropdown(...)
syncControls()
apply()
```

Pay particular attention to DOM replacement and event bubbling/propagation during `renderCalendar()`, plus dropdown click-outside handling. Do not assume the arithmetic is the bug just because date helpers unit-test cleanly.

The existing element helper registers `on...` handlers through `addEventListener`; verify the live calendar buttons receive and retain the intended handlers after rerender.

## Regression tests

Add a test at the highest practical interaction level already supported by this repository. If the repo does not have a browser runner, extract the smallest controller/state layer needed to test the real event/state transition without introducing Playwright/npm/a framework solely for this fix.

Required cases:

- [ ] first calendar click establishes pending start
- [ ] first click does not close/cancel itself
- [ ] second click commits the range
- [ ] ascending range: Aug 10 -> Aug 15
- [ ] descending clicks: Aug 15 -> Aug 10 normalizes correctly
- [ ] same-day selection works
- [ ] committed calendar selection updates native from/to controls
- [ ] committed selection sets `preset=custom`
- [ ] URL receives the committed custom from/to values
- [ ] applying a completed calendar selection causes one data refresh, not zero or two
- [ ] navigating previous/next month does not lose an already committed range
- [ ] native input path still works after the fix
- [ ] closing the popover after only the first click clears only the pending selection, not the previously committed range
- [ ] future dates remain non-selectable

## Non-goals

- no visual redesign of the Range picker
- no new date picker library
- no npm/bundler/framework dependency
- no changes to Overview filter semantics
- no Logs/sidebar implementation in this PR
- no server API redesign unless a concrete boundary bug requires a minimal correction

## Agent execution contract

Implement and verify the bug fix, not another UI rewrite.

1. Reproduce the actual calendar click failure on current code first.
2. Identify the event/state lifecycle that causes the click to be ineffective.
3. Fix the smallest coherent layer.
4. Keep calendar and native date/time inputs backed by the same committed state.
5. Add regression coverage for the click interaction, not only helper arithmetic.
6. Preserve exact custom time editing and existing presets.
7. Preserve URL state, hierarchy/facets, pricing, refresh and offline embedded frontend behavior.
8. Keep `gofmt`, `go vet`, and `go test ./...` green.

## Definition of done

- [ ] clicking two calendar dates reliably changes the custom range
- [ ] native fields visibly synchronize with calendar picks
- [ ] URL/query state uses that range
- [ ] dashboard reload uses the selected range
- [ ] same-day/reverse-order/month-navigation cases work
- [ ] pending selection/dropdown lifecycle is correct
- [ ] native date/time input path remains working
- [ ] no new frontend dependencies
- [ ] regression tests prevent recurrence
- [ ] `go test ./...` passes

## Where these invariants live now

The hand-written calendar this document describes was replaced during the Vue
migration (see [`frontend-rearchitecture.md`](frontend-rearchitecture.md)). The
event-lifecycle bugs it was written against are gone with the imperative
implementation, but the *product* invariants it established are not, and they
are still tested — as behaviour rather than as source-text assertions:

| Invariant | Now asserted by |
| --- | --- |
| a range picked end-first still commits as a range | `webui/src/lib/time.spec.ts` |
| same-day ranges stay a real window | `webui/src/lib/time.spec.ts` |
| exact times survive to the second | `webui/src/lib/time.spec.ts` |
| the committed range reaches the URL and the query | `webui/src/composables/useQueryState.spec.ts` |
| a preset is re-evaluated on load, a hand-picked range is not | `webui/src/composables/useQueryState.spec.ts` |
| a background refresh does not reset the range | `webui/src/composables/useFilterOptions.spec.ts` |

Pending picker state is still separate from committed query state, which is the
one structural rule that made the original bug possible.
