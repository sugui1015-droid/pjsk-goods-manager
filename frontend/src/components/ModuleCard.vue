<script setup lang="ts">
// A uniform, centered module card used across the role portals. It is purely
// presentational: the parent passes the label/description/meta and handles
// navigation via the `enter` event.
defineProps<{
  title: string
  description: string
  meta?: string
  accent?: 'blue' | 'green' | 'neutral'
  cta?: string
  disabled?: boolean
}>()
defineEmits<{ (e: 'enter'): void }>()
</script>

<template>
  <button
    type="button"
    class="module-card"
    :class="[`module-card--${accent ?? 'neutral'}`, { 'module-card--disabled': disabled }]"
    :disabled="disabled"
    @click="$emit('enter')"
  >
    <span class="module-card__title">{{ title }}</span>
    <span class="module-card__desc">{{ description }}</span>
    <span v-if="meta" class="module-card__meta">{{ meta }}</span>
    <span class="module-card__cta">{{ cta ?? '进入模块' }}</span>
  </button>
</template>
