import { inject, onBeforeUnmount, onMounted, watch, type InjectionKey, type Ref } from 'vue'

/** The slice of a G2 chart this app uses. Narrow on purpose: it is also the
 * contract a test double has to satisfy. */
export interface ChartHandle {
  options(spec: Record<string, unknown>): void
  render(): unknown
  destroy(): void
  forceFit?(): void
}

export type ChartFactory = (container: HTMLElement) => ChartHandle

export const chartFactoryKey: InjectionKey<ChartFactory> = Symbol('g2-chart-factory')

/**
 * useG2Chart owns one chart instance for the life of one component.
 *
 * The lifecycle is the whole point of this file. A chart library that gets a
 * fresh instance on every incidental reactive update leaves its old canvas
 * behind, and the symptom — two charts stacked in one container after a theme
 * or language switch — looks like a CSS bug for a long time before anyone
 * suspects the chart. So: create once, update in place, destroy on unmount,
 * and tear the instance down when the data goes away so the empty state has
 * the container to itself.
 */
export function useG2Chart(container: Ref<HTMLElement | null>, spec: () => Record<string, unknown> | null) {
  const factory = inject(chartFactoryKey, null)
  let chart: ChartHandle | null = null
  let observer: ResizeObserver | null = null

  function destroy() {
    chart?.destroy()
    chart = null
  }

  function draw() {
    const next = spec()
    if (!next) {
      destroy()
      return
    }
    if (!chart) {
      if (!factory || !container.value) return
      chart = factory(container.value)
    }
    chart.options(next)
    chart.render()
  }

  onMounted(() => {
    draw()
    if (container.value && typeof ResizeObserver !== 'undefined') {
      observer = new ResizeObserver(() => chart?.forceFit?.())
      observer.observe(container.value)
    }
  })

  // flush: 'post' so the container has its final size before the chart fits to
  // it; a chart measured mid-layout renders at the wrong width exactly once,
  // which is the version everybody screenshots.
  watch(spec, draw, { flush: 'post' })

  onBeforeUnmount(() => {
    observer?.disconnect()
    observer = null
    destroy()
  })

  return { redraw: draw, current: () => chart }
}
