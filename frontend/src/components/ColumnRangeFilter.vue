<script setup lang="ts">
import { computed, ref } from 'vue'
import ColumnFilterButton from './ColumnFilterButton.vue'
import type { RangeSelection } from '../filters/columnFilters'

// Numeric range filter for the quantity and money columns.
//
// Bounds stay strings all the way to the API: an empty bound has to remain
// distinguishable from 0, and money must not be rounded through a float on its
// way out of the browser.

const props = defineProps<{
  label: string
  modelValue: RangeSelection
  /** Input step: 1 for counts, 0.01 for money. */
  step?: string
  /** Extra hint under the inputs, e.g. the unit. */
  hint?: string
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: RangeSelection): void }>()

const draft = ref<RangeSelection>({ min: '', max: '' })
const active = computed(() => props.modelValue.min.trim() !== '' || props.modelValue.max.trim() !== '')

// Only 确定 commits; 取消 / Esc / outside-click leave modelValue untouched.
function onOpen() {
  draft.value = { ...props.modelValue }
}

function clear() {
  draft.value = { min: '', max: '' }
}

function confirm(close: () => void) {
  // Vue normalises values from <input type="number"> to numbers at runtime
  // even though the API state intentionally stores decimal strings. Convert
  // explicitly before trimming so clicking 确定 never throws.
  emit('update:modelValue', {
    min: String(draft.value.min ?? '').trim(),
    max: String(draft.value.max ?? '').trim(),
  })
  close()
}
</script>

<template>
  <ColumnFilterButton :label="props.label" :active="active" @open="onOpen">
    <template #default="{ close, cancel }">
      <div class="column-filter-panel">
        <div class="column-filter-fields">
          <label>
            <span>最小值</span>
            <input v-model="draft.min" type="number" min="0" :step="props.step ?? '0.01'" placeholder="不限" />
          </label>
          <label>
            <span>最大值</span>
            <input v-model="draft.max" type="number" min="0" :step="props.step ?? '0.01'" placeholder="不限" />
          </label>
        </div>
        <p v-if="props.hint" class="column-filter-hint muted">{{ props.hint }}</p>

        <div class="column-filter-actions">
          <button class="secondary-button ghost-button" type="button" @click="clear">清除</button>
          <button class="secondary-button" type="button" @click="cancel">取消</button>
          <button class="primary-button" type="button" @click="confirm(close)">确定</button>
        </div>
      </div>
    </template>
  </ColumnFilterButton>
</template>
