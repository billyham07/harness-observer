import { Runtime, corelib, extend } from '@antv/g2'
import type { ChartFactory, ChartHandle } from './useG2Chart'

/**
 * The G2 runtime, assembled from corelib only.
 *
 * The default `Chart` export carries every mark G2 ships — maps, graphs, 3D,
 * auto-plots — and this dashboard draws stacked intervals. corelib has the
 * marks, the stackY transform, both axes and the tooltip interaction, and
 * leaves roughly a third of the bundle behind. The binary this ends up inside
 * is shipped to a laptop, not a CDN, but a megabyte of dead chart engine is
 * still a megabyte nobody can explain.
 */
const Chart = extend(Runtime, corelib())

/**
 * The real G2 binding, provided once at the app root.
 *
 * It is injected rather than imported by the chart components so a unit test
 * can supply a double and assert the mount/update/unmount lifecycle without a
 * canvas — and, just as usefully, without pulling a rendering engine into a
 * jsdom process that has nothing to render onto.
 */
export const createG2Chart: ChartFactory = (container: HTMLElement): ChartHandle => {
  const chart = new Chart({ container, autoFit: true, paddingLeft: 58, paddingBottom: 28, paddingTop: 12 })
  return {
    options: (spec) => chart.options(spec as never),
    render: () => chart.render(),
    destroy: () => chart.destroy(),
    forceFit: () => chart.forceFit(),
  }
}
