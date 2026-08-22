import { computed, ref } from 'vue'
import type { PricingStatus, QueryRow } from '@/api/types'

/**
 * Where the dollar figures came from. A cost estimate whose provenance is not
 * on the same screen is a number nobody can check, so this is loaded with the
 * data rather than hidden behind an about box.
 */
const status = ref<PricingStatus | null>(null)

export function setPricingStatus(next: PricingStatus | null) {
  status.value = next
}

/**
 * COVERAGE_FLOOR is where an estimate stops being a total and starts being a
 * partial one. Below it the cost is marked, because a confident-looking dollar
 * figure over three quarters of the tokens is a misleading number.
 */
export const COVERAGE_FLOOR = 0.995

/**
 * A row the catalog could not price has an unknown cost, not a zero one. This
 * is the whole reason the coverage numbers exist, so "$0.00" is never printed
 * for a model nobody found a rate for.
 */
export const costPriced = (r?: QueryRow | null) => !!r && r.pricing_coverage_ratio !== undefined
export const costKnown = (r?: QueryRow | null) => costPriced(r) && Number(r?.priced_tokens || 0) > 0
export const costPartial = (r?: QueryRow | null) =>
  costPriced(r) && Number(r?.pricing_coverage_ratio || 0) < COVERAGE_FLOOR

export function usePricing() {
  return { status, available: computed(() => status.value !== null) }
}
