import { computed, ref, watch } from 'vue'
import { darkTheme, dateEnUS, dateZhCN, enUS, zhCN, type GlobalThemeOverrides } from 'naive-ui'
import { useTheme } from '@/composables/useTheme'
import { currentLang } from '@/i18n'

const read = (name: string) =>
  getComputedStyle(document.documentElement).getPropertyValue(name).trim() || undefined

/**
 * Naive UI is used for interaction correctness — a date/time range panel, a
 * popover, a drawer, a remote-paginated table — and not for visual identity.
 * Its tokens are therefore read back out of our own stylesheet at runtime
 * rather than copied into TypeScript, so the component library can never drift
 * away from the theme it is sitting inside.
 */
function build(): GlobalThemeOverrides {
  const ink = read('--ink')
  const surface = read('--surface')
  const line = read('--line')
  const accent = read('--accent')
  const muted = read('--muted')
  const common = {
    primaryColor: accent,
    primaryColorHover: accent,
    primaryColorPressed: accent,
    primaryColorSuppl: accent,
    borderColor: line,
    dividerColor: read('--line-soft'),
    textColorBase: ink,
    textColor1: ink,
    textColor2: ink,
    textColor3: muted,
    baseColor: surface,
    popoverColor: surface,
    cardColor: surface,
    modalColor: surface,
    bodyColor: read('--bg'),
    inputColor: surface,
    hoverColor: read('--surface-2'),
    borderRadius: '7px',
    borderRadiusSmall: '4px',
    fontFamily: read('--sans'),
    fontFamilyMono: read('--mono'),
    fontSize: '13px',
    heightMedium: '30px',
    heightSmall: '26px',
  }
  return {
    common,
    DatePicker: { itemFontSize: '12px', calendarTitleFontSize: '12.5px' },
    DataTable: {
      thColor: surface,
      tdColor: surface,
      thTextColor: read('--faint'),
      tdColorHover: read('--surface-2'),
      borderColor: read('--line-soft'),
      thFontWeight: '600',
    },
    Drawer: { color: surface },
    Popover: { color: surface, padding: '0' },
  }
}

export function useNaiveTheme() {
  const { resolved } = useTheme()
  const overrides = ref<GlobalThemeOverrides>({})
  // flush: 'post' so the data-theme attribute is already on <html> when the
  // custom properties are read back; reading them a tick early would hand the
  // component library the previous theme's colours.
  watch(resolved, () => (overrides.value = build()), { immediate: true, flush: 'post' })
  return {
    overrides,
    theme: computed(() => (resolved.value === 'dark' ? darkTheme : null)),
    // The library ships its own strings for the parts of a date panel we do
    // not render ourselves, so the language switch has to reach it too.
    locale: computed(() => (currentLang.value === 'zh' ? zhCN : enUS)),
    dateLocale: computed(() => (currentLang.value === 'zh' ? dateZhCN : dateEnUS)),
  }
}
