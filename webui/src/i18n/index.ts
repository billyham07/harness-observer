import { computed, ref } from 'vue'
import { en, type StringKey } from './en'
import { zh } from './zh'

export type Lang = 'en' | 'zh'

const LANGS: Lang[] = ['en', 'zh']
const TABLES: Record<Lang, Partial<Record<StringKey, string>>> = { en, zh }
const STORAGE_KEY = 'ho-lang'

function detect(): Lang {
  try {
    const saved = localStorage.getItem(STORAGE_KEY)
    if (saved && LANGS.includes(saved as Lang)) return saved as Lang
  } catch {
    /* private mode: fall through to the navigator */
  }
  return (navigator.language || 'en').toLowerCase().startsWith('zh') ? 'zh' : 'en'
}

const lang = ref<Lang>(detect())

export const currentLang = computed<Lang>(() => lang.value)
export const locale = computed(() => (lang.value === 'zh' ? 'zh-CN' : 'en-US'))

export function setLang(next: Lang) {
  lang.value = LANGS.includes(next) ? next : 'en'
  try {
    localStorage.setItem(STORAGE_KEY, lang.value)
  } catch {
    /* private mode: the choice lasts for this session only */
  }
  document.documentElement.lang = lang.value === 'zh' ? 'zh-Hans' : 'en'
}

export function toggleLang() {
  setLang(lang.value === 'zh' ? 'en' : 'zh')
}

/**
 * t reads the active table with English as the fallback, so a missing
 * translation degrades to a readable label instead of a raw key.
 *
 * Reading `lang.value` here is what makes every caller reactive: switching
 * language repaints from state already in memory and never re-queries, so the
 * numbers on screen cannot change when the language does.
 */
export function t(key: StringKey, params?: Record<string, string | number>): string {
  const table = TABLES[lang.value]
  let value = table[key] ?? en[key] ?? (key as string)
  if (params) {
    for (const [name, replacement] of Object.entries(params)) {
      value = value.split(`{${name}}`).join(String(replacement))
    }
  }
  return value
}

/** Composable form, for components that prefer an explicit import site. */
export function useI18n() {
  return { t, lang: currentLang, locale, setLang, toggleLang }
}

export type { StringKey }
