import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'
import { BUCKETS } from '@/api/types'
import { BUCKET_COLORS, SERIES } from './palette'

const css = readFileSync(resolve(process.cwd(), 'src/styles/tokens.css'), 'utf8')

/** Pull one custom property out of a specific rule block. */
function cssVar(selector: string, name: string): string {
  const block = css.split(selector)[1]?.split('}')[0] ?? ''
  const match = block.match(new RegExp(`${name}:\\s*(#[0-9a-fA-F]{3,8})`))
  return match ? match[1].toLowerCase() : ''
}

describe('token bucket palette', () => {
  it('stacks the buckets in canonical order', () => {
    expect(SERIES.map((s) => s.bucket)).toEqual([...BUCKETS])
  })

  it('names a token field and a cost field for the same slice of a turn', () => {
    expect(SERIES.map((s) => s.tokens)).toEqual([
      'input_tokens',
      'cache_read_tokens',
      'cache_write_tokens',
      'output_tokens',
    ])
    expect(SERIES.map((s) => s.cost)).toEqual([
      'cost_input_usd',
      'cost_cache_read_usd',
      'cost_cache_write_usd',
      'cost_output_usd',
    ])
  })

  // The chart draws to a canvas and cannot read a custom property, so the hues
  // live in TypeScript as well as in the stylesheet. This is the guard that
  // keeps the two from drifting into a dashboard where the legend and the bars
  // disagree about which colour means cache read.
  it('agrees with the stylesheet in both themes', () => {
    for (const series of SERIES) {
      expect(cssVar(':root {', series.cssVar)).toBe(BUCKET_COLORS.light[series.bucket])
      expect(cssVar("html[data-theme='dark'] {", series.cssVar)).toBe(
        BUCKET_COLORS.dark[series.bucket],
      )
    }
  })
})
