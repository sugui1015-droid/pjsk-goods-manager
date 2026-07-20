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
  // When true the meta badge row is always rendered (invisible if meta is
  // empty), so sibling cards keep their title/desc/meta/cta rows aligned.
  reserveMeta?: boolean
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
    <span v-if="meta || reserveMeta" class="module-card__meta" :class="{ 'module-card__meta--empty': !meta }">{{ meta || '·' }}</span>
    <span class="module-card__cta">{{ cta ?? '进入模块' }}</span>
  </button>
</template>
