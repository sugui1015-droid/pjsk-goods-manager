<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

// The shell every column filter popover shares: the header title, the funnel
// button, and a popover that closes on outside click / Esc and never leaves the
// viewport. It owns no filter semantics — each filter type supplies its own
// body, its own draft state and its own 取消/确定 buttons via the slot.

const props = defineProps<{
  /** Column title rendered in the <th> next to the funnel. */
  label: string
  /** Whether this column currently carries a filter (drives the funnel state). */
  active?: boolean
}>()

const emit = defineEmits<{ (event: 'open'): void; (event: 'cancel'): void }>()

const open = ref(false)
const mobile = ref(false)
const buttonRef = ref<HTMLButtonElement | null>(null)
const popoverRef = ref<HTMLDivElement | null>(null)
const position = ref({ top: 0, left: 0 })

const POPOVER_WIDTH = 260
const VIEWPORT_MARGIN = 8
const MOBILE_BREAKPOINT = 560

// Positioned against the button's viewport rect and clamped on both axes, so a
// funnel near the right edge of a wide scrolling table still opens a fully
// visible popover. Below the mobile breakpoint the popover becomes a bottom
// sheet instead and this maths is unused.
function reposition() {
  mobile.value = window.innerWidth <= MOBILE_BREAKPOINT
  if (mobile.value) return
  const button = buttonRef.value
  if (!button) return
  const rect = button.getBoundingClientRect()
  const width = popoverRef.value?.offsetWidth || POPOVER_WIDTH
  const height = popoverRef.value?.offsetHeight || 0

  let left = rect.left
  const maxLeft = window.innerWidth - width - VIEWPORT_MARGIN
  if (left > maxLeft) left = maxLeft
  if (left < VIEWPORT_MARGIN) left = VIEWPORT_MARGIN

  let top = rect.bottom + 4
  // Flip above the header when there is no room below.
  if (height > 0 && top + height > window.innerHeight - VIEWPORT_MARGIN) {
    const above = rect.top - height - 4
    top = above >= VIEWPORT_MARGIN ? above : Math.max(VIEWPORT_MARGIN, window.innerHeight - height - VIEWPORT_MARGIN)
  }

  position.value = { top, left }
}

async function openPopover() {
  open.value = true
  emit('open')
  await nextTick()
  reposition()
  // Re-measure once the body has rendered so the flip/clamp uses the real
  // height rather than the pre-render estimate.
  await nextTick()
  reposition()
  const focusable = popoverRef.value?.querySelector<HTMLElement>(
    'input, button, select, [tabindex]:not([tabindex="-1"])',
  )
  focusable?.focus()
}

// Closing without applying: this is the path for 取消, outside click and Esc,
// all three of which must discard whatever the popover's draft state holds.
function cancel() {
  if (!open.value) return
  open.value = false
  emit('cancel')
  buttonRef.value?.focus()
}

function toggle() {
  if (open.value) cancel()
  else void openPopover()
}

function onPointerDown(event: MouseEvent) {
  if (!open.value) return
  const target = event.target as Node
  if (popoverRef.value?.contains(target) || buttonRef.value?.contains(target)) return
  cancel()
}

function onKeydown(event: KeyboardEvent) {
  if (event.key === 'Escape') cancel()
}

onMounted(() => {
  mobile.value = window.innerWidth <= MOBILE_BREAKPOINT
  document.addEventListener('mousedown', onPointerDown, true)
  document.addEventListener('keydown', onKeydown)
  window.addEventListener('resize', reposition)
  // Capture phase: the table body scrolls in its own container, so the
  // popover has to follow scrolls that never reach the window.
  window.addEventListener('scroll', reposition, true)
})

onBeforeUnmount(() => {
  document.removeEventListener('mousedown', onPointerDown, true)
  document.removeEventListener('keydown', onKeydown)
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
})

// Applying is the popover body's job; it calls this to dismiss the shell.
function close() {
  open.value = false
  buttonRef.value?.focus()
}

defineExpose({ close, cancel })
</script>

<template>
  <span class="column-header">
    <span class="column-header__label">{{ props.label }}</span>
    <button
      ref="buttonRef"
      class="column-filter-button"
      type="button"
      :class="{ 'is-active': props.active }"
      :aria-label="`筛选 ${props.label}`"
      :aria-expanded="open"
      :data-filtered="props.active ? 'true' : 'false'"
      @click="toggle"
    >
      <svg viewBox="0 0 16 16" aria-hidden="true" focusable="false">
        <path d="M2 3h12l-4.6 5.4v4.1l-2.8 1.4V8.4z" />
      </svg>
    </button>

    <Teleport to="body">
      <div
        v-if="open"
        ref="popoverRef"
        class="column-filter-popover"
        :class="{ 'is-mobile': mobile }"
        :style="mobile ? undefined : { top: `${position.top}px`, left: `${position.left}px` }"
        role="dialog"
        :aria-label="`${props.label} 筛选`"
      >
        <slot :close="close" :cancel="cancel" />
      </div>
    </Teleport>
  </span>
</template>
