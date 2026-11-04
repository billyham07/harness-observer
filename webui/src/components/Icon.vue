<script setup lang="ts">
/**
 * The interface's few pictograms, drawn once in one stroke weight so a nav
 * entry, a toolbar button and a table sort mark all come from the same hand.
 * Decorative by default: the control that holds an icon carries the label.
 */
export type IconName =
  | 'overview'
  | 'logs'
  | 'menu'
  | 'sun'
  | 'moon'
  | 'auto'
  | 'close'
  | 'chevron-down'
  | 'chevron-right'
  | 'arrow-down'
  | 'arrow-up'
  | 'scan'
  | 'clock'

defineProps<{ name: IconName }>()

const PATHS: Record<IconName, string[]> = {
  overview: ['M3 13.5h10', 'M4.5 11V8', 'M8 11V4.5', 'M11.5 11V6.5'],
  logs: ['M5.5 4h7.5', 'M5.5 8h7.5', 'M5.5 12h7.5', 'M3 4h.01', 'M3 8h.01', 'M3 12h.01'],
  menu: ['M2.5 4.5h11', 'M2.5 8h11', 'M2.5 11.5h11'],
  sun: [
    'M8 10.5a2.5 2.5 0 1 0 0-5 2.5 2.5 0 0 0 0 5Z',
    'M8 1.5v1.5',
    'M8 13v1.5',
    'M1.5 8H3',
    'M13 8h1.5',
    'M3.4 3.4l1.06 1.06',
    'M11.54 11.54l1.06 1.06',
    'M3.4 12.6l1.06-1.06',
    'M11.54 4.46l1.06-1.06',
  ],
  moon: ['M13.5 9.6A5.5 5.5 0 0 1 6.4 2.5a5.5 5.5 0 1 0 7.1 7.1Z'],
  auto: ['M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z', 'M8 2v12'],
  close: ['M4.5 4.5l7 7', 'M11.5 4.5l-7 7'],
  'chevron-down': ['M4.5 6.25 8 9.75l3.5-3.5'],
  'chevron-right': ['M6.25 4.5 9.75 8l-3.5 3.5'],
  'arrow-down': ['M8 3.5v9', 'M4.5 9 8 12.5 11.5 9'],
  'arrow-up': ['M8 12.5v-9', 'M4.5 7 8 3.5 11.5 7'],
  scan: ['M13.5 8A5.5 5.5 0 1 1 11.9 4.1', 'M13.5 2.5v3h-3'],
  clock: ['M8 14A6 6 0 1 0 8 2a6 6 0 0 0 0 12Z', 'M8 5v3.2l2 1.3'],
}
</script>

<template>
  <svg class="icon" viewBox="0 0 16 16" aria-hidden="true" focusable="false">
    <path v-for="d in PATHS[name]" :key="d" :d="d" />
  </svg>
</template>
