// Shared state and URL encoding for WPS-style column header filters.
//
// This module is deliberately list-agnostic: it knows about "value columns",
// "range columns" and "date columns", not about orders. The order page is the
// first consumer; users/payments/import history are meant to reuse it as-is
// once this reference implementation is accepted.

/** A range filter's inclusive bounds, kept as strings so an empty bound stays
 * distinguishable from zero and no value is rounded through a float on the way
 * to the API. */
export type RangeSelection = { min: string; max: string }

/** A date range. `to` is inclusive from the user's point of view; the backend
 * is responsible for turning it into an exclusive bound.
 *
 * `blank` selects rows that have no date at all (a user who has never logged
 * in). It is a filter target in its own right, not the absence of one, so it
 * travels as a dedicated `<column>_blank=1` flag — the same convention the
 * value columns use. Columns where "no date" is impossible simply leave it
 * false and render no checkbox. */
export type DateSelection = { from: string; to: string; blank: boolean }

/**
 * The full filter state of one table.
 *
 * `values` maps a column key to the values ticked in its popover. An empty
 * array means the column is unfiltered; an array containing '' filters for
 * blank cells, which is why '' can never be treated as "nothing selected".
 */
export type ColumnFilterState = {
  values: Record<string, string[]>
  ranges: Record<string, RangeSelection>
  dates: Record<string, DateSelection>
}

export type ColumnFilterSchema = {
  valueColumns: string[]
  rangeColumns: string[]
  dateColumns: string[]
}

export function createFilterState(schema: ColumnFilterSchema): ColumnFilterState {
  const state: ColumnFilterState = { values: {}, ranges: {}, dates: {} }
  for (const column of schema.valueColumns) state.values[column] = []
  for (const column of schema.rangeColumns) state.ranges[column] = { min: '', max: '' }
  for (const column of schema.dateColumns) state.dates[column] = { from: '', to: '', blank: false }
  return state
}

export function isValueColumnFiltered(state: ColumnFilterState, column: string): boolean {
  return (state.values[column] ?? []).length > 0
}

export function isRangeColumnFiltered(state: ColumnFilterState, column: string): boolean {
  const range = state.ranges[column]
  return !!range && (range.min.trim() !== '' || range.max.trim() !== '')
}

export function isDateColumnFiltered(state: ColumnFilterState, column: string): boolean {
  const dates = state.dates[column]
  return !!dates && (dates.from.trim() !== '' || dates.to.trim() !== '' || dates.blank)
}

/** How many columns currently carry a filter. Drives the "清空全部筛选" control. */
export function activeFilterCount(state: ColumnFilterState): number {
  let count = 0
  for (const column of Object.keys(state.values)) if (isValueColumnFiltered(state, column)) count += 1
  for (const column of Object.keys(state.ranges)) if (isRangeColumnFiltered(state, column)) count += 1
  for (const column of Object.keys(state.dates)) if (isDateColumnFiltered(state, column)) count += 1
  return count
}

export function clearAllFilters(state: ColumnFilterState): void {
  for (const column of Object.keys(state.values)) state.values[column] = []
  for (const column of Object.keys(state.ranges)) state.ranges[column] = { min: '', max: '' }
  for (const column of Object.keys(state.dates)) state.dates[column] = { from: '', to: '', blank: false }
}

/**
 * Encodes the filter state the way the backend reads it.
 *
 * Value columns become repeated parameters (?cn=A&cn=B) rather than a joined
 * string: real values contain commas and vertical bars, and repetition is what
 * URLSearchParams and Go's url.Values both model natively. Empty/whitespace
 * values are ignored. A selected blank candidate uses a separate
 * `<column>_blank=1` flag, so empty accidental inputs stay harmless.
 *
 * `rangeParams` and `dateParams` map a column to its two parameter names, so a
 * column's UI key never has to match its wire names.
 */
export function buildFilterParams(
  state: ColumnFilterState,
  rangeParams: Record<string, [string, string]>,
  dateParams: Record<string, [string, string]>,
): URLSearchParams {
  const params = new URLSearchParams()

  for (const [column, values] of Object.entries(state.values)) {
    for (const value of values) {
      const trimmed = value.trim()
      if (trimmed !== '') params.append(column, trimmed)
      else params.set(`${column}_blank`, '1')
    }
  }

  for (const [column, range] of Object.entries(state.ranges)) {
    const names = rangeParams[column]
    if (!names) continue
    if (range.min.trim() !== '') params.set(names[0], range.min.trim())
    if (range.max.trim() !== '') params.set(names[1], range.max.trim())
  }

  for (const [column, dates] of Object.entries(state.dates)) {
    const names = dateParams[column]
    if (!names) continue
    if (dates.from.trim() !== '') params.set(names[0], dates.from.trim())
    if (dates.to.trim() !== '') params.set(names[1], dates.to.trim())
    if (dates.blank) params.set(`${column}_blank`, '1')
  }

  return params
}
