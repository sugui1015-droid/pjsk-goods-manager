<script setup lang="ts">
import { computed, ref } from 'vue'
import ColumnFilterButton from './ColumnFilterButton.vue'
import type { ColumnFacetResponse, ColumnFacetValue } from '../api/client'

// The WPS-style value picker: search, 全选, a de-duplicated candidate list with
// per-value counts (including a (空白) entry), and 取消 / 确定.
//
// Candidates are never derived from the rows on screen. They come from the
// server's facet endpoint, computed over the whole filtered result set with
// this column's own selection ignored — so a value stays pickable after it has
// been ticked, and the counts describe the real data rather than the page.

const props = defineProps<{
  label: string
  column: string
  /** The applied selection. Empty means unfiltered; [''] filters for blanks. */
  modelValue: string[]
  loadFacets: (request: { column: string; search: string; page: number }) => Promise<ColumnFacetResponse>
}>()

const emit = defineEmits<{ (event: 'update:modelValue', value: string[]): void }>()

// draft holds the popover's uncommitted ticks. Nothing here reaches
// modelValue until 确定; 取消 / Esc / outside-click simply drop it.
const draft = ref<string[]>([])
const candidates = ref<ColumnFacetValue[]>([])
const search = ref('')
const page = ref(1)
const total = ref(0)
const hasMore = ref(false)
const loading = ref(false)
const errorMessage = ref('')

let searchTimer: ReturnType<typeof setTimeout> | undefined
let requestToken = 0

const active = computed(() => props.modelValue.length > 0)

const allLoadedSelected = computed(
  () => candidates.value.length > 0 && candidates.value.every((candidate) => draft.value.includes(candidate.value)),
)
const someLoadedSelected = computed(
  () => !allLoadedSelected.value && candidates.value.some((candidate) => draft.value.includes(candidate.value)),
)

async function fetchPage(requested: number, append: boolean) {
  // Guards against an out-of-order response overwriting a newer one when the
  // user types quickly.
  const token = ++requestToken
  loading.value = true
  errorMessage.value = ''
  try {
    const response = await props.loadFacets({ column: props.column, search: search.value.trim(), page: requested })
    if (token !== requestToken) return
    const pageValues = response.values.filter((candidate) => !candidate.blank)
    if (!append && response.blank_count > 0) {
      pageValues.unshift({ value: '', label: '(空白)', count: response.blank_count, blank: true })
    }
    candidates.value = append ? [...candidates.value, ...pageValues] : pageValues
    total.value = response.total
    hasMore.value = response.has_more
    page.value = response.page
  } catch (error) {
    if (token !== requestToken) return
    const detail = error instanceof Error ? error.message : '未知错误'
    errorMessage.value = `候选值加载失败：${detail}`
  } finally {
    if (token === requestToken) loading.value = false
  }
}

function onOpen() {
  draft.value = [...props.modelValue]
  search.value = ''
  candidates.value = []
  page.value = 1
  void fetchPage(1, false)
}

function onSearchInput() {
  window.clearTimeout(searchTimer)
  searchTimer = setTimeout(() => void fetchPage(1, false), 250)
}

function retry() {
  void fetchPage(1, false)
}

function loadMore() {
  void fetchPage(page.value + 1, true)
}

function toggleValue(value: string) {
  const index = draft.value.indexOf(value)
  if (index === -1) draft.value = [...draft.value, value]
  else draft.value = draft.value.filter((candidate) => candidate !== value)
}

function toggleAll() {
  const loaded = candidates.value.map((candidate) => candidate.value)
  if (allLoadedSelected.value) {
    draft.value = draft.value.filter((value) => !loaded.includes(value))
  } else {
    draft.value = [...new Set([...draft.value, ...loaded])]
  }
}

function confirm(close: () => void) {
  emit('update:modelValue', [...draft.value])
  close()
}
</script>

<template>
  <ColumnFilterButton :label="props.label" :active="active" @open="onOpen">
    <template #default="{ close, cancel }">
      <div class="column-filter-panel">
        <label class="column-filter-search">
          <span class="visually-hidden">搜索 {{ props.label }} 候选值</span>
          <input v-model="search" type="search" :placeholder="`搜索${props.label}`" @input="onSearchInput" />
        </label>

        <label class="column-filter-option column-filter-option--all">
          <input
            type="checkbox"
            :checked="allLoadedSelected"
            :indeterminate.prop="someLoadedSelected"
            :disabled="candidates.length === 0"
            @change="toggleAll"
          />
          <span class="column-filter-option__label">全选</span>
          <span class="column-filter-option__count">{{ total }}</span>
        </label>

        <div v-if="errorMessage" class="column-filter-state column-filter-state--error">
          <span>{{ errorMessage }}</span>
          <button class="secondary-button ghost-button" type="button" @click="retry">重试</button>
        </div>

        <p v-else-if="loading && candidates.length === 0" class="column-filter-state">候选值加载中…</p>
        <p v-else-if="candidates.length === 0" class="column-filter-state">没有符合条件的候选值。</p>

        <ul v-else class="column-filter-list">
          <li v-for="candidate in candidates" :key="candidate.value">
            <label class="column-filter-option" :data-blank="candidate.blank ? 'true' : 'false'">
              <input
                type="checkbox"
                :checked="draft.includes(candidate.value)"
                @change="toggleValue(candidate.value)"
              />
              <span class="column-filter-option__label" :class="{ 'is-blank': candidate.blank }" :title="candidate.label">
                {{ candidate.label }}
              </span>
              <span class="column-filter-option__count">{{ candidate.count }}</span>
            </label>
          </li>
          <li v-if="hasMore">
            <button class="secondary-button ghost-button column-filter-more" type="button" :disabled="loading" @click="loadMore">
              {{ loading ? '加载中…' : '加载更多候选值' }}
            </button>
          </li>
        </ul>

        <div class="column-filter-actions">
          <button class="secondary-button" type="button" @click="cancel">取消</button>
          <button class="primary-button" type="button" @click="confirm(close)">确定</button>
        </div>
      </div>
    </template>
  </ColumnFilterButton>
</template>
