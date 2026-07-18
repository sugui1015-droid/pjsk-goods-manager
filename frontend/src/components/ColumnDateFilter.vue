<script setup lang="ts">
import { computed, ref } from 'vue'
import ColumnFilterButton from './ColumnFilterButton.vue'
import type { DateSelection } from '../filters/columnFilters'

// Date range filter for the 创建时间 column.
//
// Both bounds read as inclusive days to the user: picking the same date twice
// means "that whole day". The backend turns the end date into an exclusive
// next-midnight bound, so nothing on the chosen day is cut off.

const props = defineProps<{
  label: string
  modelValue: DateSelection
  /** Show a "no date at all" option (e.g. 从未登录). Columns where every row has
   * a date leave this off and render no checkbox. */
  allowBlank?: boolean
  /** What the blank option is called in this column's terms. */
  blankLabel?: string
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: DateSelection): void }>()

const draft = ref<DateSelection>({ from: '', to: '', blank: false })
const active = computed(
  () => props.modelValue.from.trim() !== '' || props.modelValue.to.trim() !== '' || props.modelValue.blank,
)

function onOpen() {
  draft.value = { ...props.modelValue }
}

function clear() {
  draft.value = { from: '', to: '', blank: false }
}

function confirm(close: () => void) {
  emit('update:modelValue', {
    from: String(draft.value.from ?? '').trim(),
    to: String(draft.value.to ?? '').trim(),
    blank: draft.value.blank,
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
            <span>开始日期</span>
            <input v-model="draft.from" type="date" :disabled="draft.blank" />
          </label>
          <label>
            <span>结束日期</span>
            <input v-model="draft.to" type="date" :disabled="draft.blank" />
          </label>
        </div>
        <p class="column-filter-hint muted">结束日期当天的记录也会包含在内。</p>

        <!-- The blank option and a date range are mutually exclusive: a row
             with no date can never fall inside one, so the inputs disable
             rather than silently returning nothing. -->
        <label v-if="props.allowBlank" class="column-filter-option column-filter-option--blank">
          <input v-model="draft.blank" type="checkbox" />
          <span class="column-filter-option__label">{{ props.blankLabel ?? '(空白)' }}</span>
        </label>

        <div class="column-filter-actions">
          <button class="secondary-button ghost-button" type="button" @click="clear">清除</button>
          <button class="secondary-button" type="button" @click="cancel">取消</button>
          <button class="primary-button" type="button" @click="confirm(close)">确定</button>
        </div>
      </div>
    </template>
  </ColumnFilterButton>
</template>
