import { vi } from 'vitest'

// jsdom has no layout engine and therefore no ResizeObserver. The chart
// composable and the component library both observe their containers, so a
// no-op stand-in keeps the lifecycle under test instead of the polyfill.
class ResizeObserverStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}

vi.stubGlobal('ResizeObserver', ResizeObserverStub)

if (!('matchMedia' in window)) {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener() {},
    removeEventListener() {},
  }))
}
