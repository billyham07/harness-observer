import { computed, ref, watchEffect } from 'vue'

export const THEMES = ['auto', 'light', 'dark'] as const
export type ThemeChoice = (typeof THEMES)[number]
export type ResolvedTheme = 'light' | 'dark'

const STORAGE_KEY = 'ho-theme'

function stored(): ThemeChoice {
  try {
    const saved = localStorage.getItem(STORAGE_KEY) as ThemeChoice | null
    if (saved && THEMES.includes(saved)) return saved
  } catch {
    /* private mode: Auto is a fine default */
  }
  return 'auto'
}

const choice = ref<ThemeChoice>(stored())
const media = typeof window !== 'undefined' && window.matchMedia ? window.matchMedia('(prefers-color-scheme: dark)') : null
const systemDark = ref(media?.matches ?? false)
media?.addEventListener('change', (e) => (systemDark.value = e.matches))

/**
 * Auto follows the OS and re-resolves when it changes, so every token has one
 * definition per theme instead of a media-query copy that can drift.
 */
const resolved = computed<ResolvedTheme>(() =>
  choice.value === 'auto' ? (systemDark.value ? 'dark' : 'light') : choice.value,
)

watchEffect(() => {
  document.documentElement.setAttribute('data-theme', resolved.value)
})

export function useTheme() {
  function set(next: ThemeChoice) {
    choice.value = next
    try {
      localStorage.setItem(STORAGE_KEY, next)
    } catch {
      /* private mode: the choice lasts for this session only */
    }
  }
  function cycle() {
    set(THEMES[(THEMES.indexOf(choice.value) + 1) % THEMES.length])
  }
  return { choice, resolved, set, cycle }
}
