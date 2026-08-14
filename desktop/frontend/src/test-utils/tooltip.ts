import type { VueWrapper } from '@vue/test-utils'
import AppTooltip from '../components/AppTooltip.vue'

/**
 * The tooltip text for the element carrying `testid`, read off the AppTooltip
 * wrapping it.
 *
 * These used to be `title` attributes an assertion could read directly. They
 * are not any more, because the platform takes over a second to show one and
 * an icon whose only explanation is its tooltip cannot afford that wait — so
 * the text lives on a component prop instead, and this is how a test gets at
 * it without mounting and hovering.
 *
 * Returns '' when nothing wraps that testid, so a missing tooltip fails the
 * assertion rather than throwing somewhere unhelpful.
 */
// Narrowed to the one method used, so a wrapper around any component satisfies
// it: VueWrapper is generic in its instance type and the concrete wrappers are
// not assignable to each other.
export function tooltipFor(wrapper: Pick<VueWrapper, 'findAllComponents'>, testid: string): string {
  const tip = wrapper
    .findAllComponents(AppTooltip)
    .find((candidate) => candidate.find(`[data-testid="${testid}"]`).exists())
  return tip?.props('text') ?? ''
}
