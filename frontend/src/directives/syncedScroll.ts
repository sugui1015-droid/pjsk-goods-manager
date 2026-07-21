// v-synced-scroll gives a horizontally-scrollable table container (.table-scroll)
// a second, synchronized scrollbar pinned above the table, so a user does not
// have to scroll to the very bottom of a tall table to reach the horizontal
// scrollbar. Both bars drive the same scrollLeft; the top bar hides when there
// is no horizontal overflow.
//
// Design notes matching the requirements:
//  - One shared implementation (this directive), applied by adding an attribute
//    — no per-page duplicated logic and no second copy of the table content.
//  - Bidirectional scrollLeft sync via a compare-before-set guard, which cannot
//    loop (setting scrollLeft to its current value fires no scroll event, and
//    the paired handler sees equal values and stops) — no lock, no jitter.
//  - A ResizeObserver keeps the top track's width equal to the table's
//    scrollWidth and toggles visibility on width/viewport changes.
//  - The native bottom scrollbar, sticky header, Shift+wheel, trackpad and touch
//    all keep working because the container itself is unchanged overflow:auto.
//  - Listeners and the observer are torn down and the injected track removed on
//    unmount.
import type { Directive } from 'vue'

interface SyncedScrollState {
  topBar: HTMLDivElement
  spacer: HTMLDivElement
  onTopScroll: () => void
  onContainerScroll: () => void
  observer: ResizeObserver
}

const states = new WeakMap<HTMLElement, SyncedScrollState>()

function refresh(container: HTMLElement, state: SyncedScrollState) {
  const overflow = container.scrollWidth - container.clientWidth
  state.spacer.style.width = `${container.scrollWidth}px`
  // Hide the top bar entirely when the table fits — nothing to scroll.
  state.topBar.style.display = overflow > 1 ? 'block' : 'none'
}

export const vSyncedScroll: Directive<HTMLElement> = {
  mounted(container) {
    if (typeof document === 'undefined' || !container.parentNode) return

    const topBar = document.createElement('div')
    topBar.className = 'table-scroll-topbar'
    topBar.setAttribute('aria-hidden', 'true')
    const spacer = document.createElement('div')
    spacer.className = 'table-scroll-topbar__spacer'
    topBar.appendChild(spacer)
    container.parentNode.insertBefore(topBar, container)

    const state: SyncedScrollState = {
      topBar,
      spacer,
      // Compare-before-set: this cannot create a feedback loop, because writing
      // an unchanged scrollLeft fires no scroll event and the paired handler
      // sees the two already equal.
      onTopScroll: () => {
        if (container.scrollLeft !== topBar.scrollLeft) container.scrollLeft = topBar.scrollLeft
      },
      onContainerScroll: () => {
        if (topBar.scrollLeft !== container.scrollLeft) topBar.scrollLeft = container.scrollLeft
      },
      observer: new ResizeObserver(() => refresh(container, state)),
    }

    topBar.addEventListener('scroll', state.onTopScroll, { passive: true })
    container.addEventListener('scroll', state.onContainerScroll, { passive: true })
    state.observer.observe(container)
    const table = container.querySelector('table')
    if (table) state.observer.observe(table)

    states.set(container, state)
    refresh(container, state)
  },
  updated(container) {
    const state = states.get(container)
    if (state) refresh(container, state)
  },
  unmounted(container) {
    const state = states.get(container)
    if (!state) return
    state.observer.disconnect()
    state.topBar.removeEventListener('scroll', state.onTopScroll)
    container.removeEventListener('scroll', state.onContainerScroll)
    state.topBar.remove()
    states.delete(container)
  },
}
