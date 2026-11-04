# Dashboard visual system

The dashboard looks like an itemised utility bill. A metered quantity
(tokens) is broken into line items and totalled, and an estimate sits beside
the total with a footnote. The look follows from that. The tokens are in
`src/styles/tokens.css`, which is the only place their values are set.

## World

- **One brand ink** (`--brand`, a dark low-chroma navy) fills the app bar
  (brand, page tabs, scan freshness — two pages need no sidebar). In
  `--rule` and `--accent` it also draws section rules, selected states and the
  primary action. It stays dark so it is never read as the input bucket.
- **One sheet.** The workspace is a single flat surface. A section is a heavy
  ink rule (`.panel`) followed by its heading. There are no cards, no
  shadows on content and no nested boxes. Rows are separated by hairlines.
- **Totals close on a double rule.** The period summary (`KpiBand.vue`) shows
  two totals, then dot-leader line items grouped as Usage / Activity /
  Pricing. This is the signature element. Do not turn it back into a tile grid.
- **Data colour is only for data.** The four bucket hues
  (`--c-input/read/write/output`, mirrored in `lib/palette.ts`) are the only
  saturated colours on screen besides state colours (warn, error, ok).

## Type

- Interface face: the system sans stack (`--sans`). Product UI needs no
  display face.
- Figures use `.num` (tabular numerals in the interface face), never
  monospace.
- `.code` (monospace) is only for identifiers and paths: session ids, record
  ids, source files and the store path.
- The scale is fixed rem/px with no fluid type: 36 for totals, 24 for page
  titles, 15 for section headings, 13–13.5 for body and tables, 12 for notes.

## Shape and controls

- Radii are small: `--r-1` 3px and `--r-2` 4px. Controls are rectangular, not
  pills.
- Filter entry points share the `.control` shape. When active, a control gets
  an ink border and an `--accent-soft` fill.
- Tabs are underline tabs that take the place of the section heading.
- Icons come from `components/Icon.vue`: authored 16px SVG, 1.6 stroke. Do not
  use Unicode glyphs as icons.

## Motion

Transitions run 120–180 ms and only show state changes. Every
`transition`/`animation` has a `prefers-reduced-motion` twin using the same
selector.
