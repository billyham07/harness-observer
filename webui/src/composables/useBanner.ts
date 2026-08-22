import { ref } from 'vue'

/**
 * One banner for the whole app. A failed query is reported where the user is
 * looking, and it is cleared by the next load that succeeds rather than by a
 * timer, so a persistent failure stays on screen.
 */
const message = ref('')

export function useBanner() {
  return {
    message,
    set: (text: string) => {
      message.value = text
    },
    clear: () => {
      message.value = ''
    },
  }
}
