import type { Bucket } from '@/api/types'
import type { StringKey } from '@/i18n'

/**
 * The four canonical token buckets, in stacking order.
 *
 * Colour in this dashboard is a data channel and nothing else: the chrome is
 * neutral on purpose, so the only saturated things on screen are these four,
 * and the same hue means the same bucket in the anatomy bars, the timeline and
 * every legend.
 *
 * Both palettes were checked with the dataviz validator (lightness band, chroma
 * floor, all-pairs CVD separation, normal-vision floor, contrast against the
 * surface). Worst-case CVD separation lands in the 6-8 floor band, which is why
 * every bar carries a direct label and a 2px gap between segments: identity is
 * never left to colour alone.
 *
 * These values are mirrored by :root in styles/tokens.css and a unit test
 * asserts the two agree, so a change here cannot silently skip the stylesheet.
 */
export interface Series {
  bucket: Bucket
  /** Aggregate row field carrying the token count. */
  tokens: 'input_tokens' | 'cache_read_tokens' | 'cache_write_tokens' | 'output_tokens'
  /** Aggregate row field carrying the estimated dollars for the same slice. */
  cost: 'cost_input_usd' | 'cost_cache_read_usd' | 'cost_cache_write_usd' | 'cost_output_usd'
  label: StringKey
  /** CSS custom property the stylesheet paints this bucket with. */
  cssVar: string
}

export const SERIES: Series[] = [
  { bucket: 'input', tokens: 'input_tokens', cost: 'cost_input_usd', label: 'series.input', cssVar: '--c-input' },
  {
    bucket: 'cache_read',
    tokens: 'cache_read_tokens',
    cost: 'cost_cache_read_usd',
    label: 'series.read',
    cssVar: '--c-read',
  },
  {
    bucket: 'cache_write',
    tokens: 'cache_write_tokens',
    cost: 'cost_cache_write_usd',
    label: 'series.write',
    cssVar: '--c-write',
  },
  { bucket: 'output', tokens: 'output_tokens', cost: 'cost_output_usd', label: 'series.output', cssVar: '--c-output' },
]

export const BUCKET_COLORS: Record<'light' | 'dark', Record<Bucket, string>> = {
  light: {
    input: '#2563c9',
    cache_read: '#0f9a7a',
    cache_write: '#b0499e',
    output: '#b4700f',
  },
  dark: {
    input: '#4b82e6',
    cache_read: '#12a181',
    cache_write: '#be5ca8',
    output: '#be7f1e',
  },
}

/** Chrome colours the chart needs in canvas, where CSS variables do not reach. */
export const CHART_CHROME: Record<
  'light' | 'dark',
  { axis: string; grid: string; text: string; surface: string }
> = {
  light: { axis: '#c3cad6', grid: '#e6eaf1', text: '#5a667a', surface: '#ffffff' },
  dark: { axis: '#2b3543', grid: '#1d2530', text: '#8695a8', surface: '#10161f' },
}

export const bucketColors = (theme: 'light' | 'dark') => SERIES.map((s) => BUCKET_COLORS[theme][s.bucket])
