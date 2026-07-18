import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')

test('user order and payment-history filters are independent state', () => {
  assert.match(appSource, /const queryOrderFilters = ref\(\{ category: '', role: '', series: '', paymentStatus: '' \}\)/)
  assert.match(appSource, /const queryPaymentFilters = ref\(\{ method: '', status: '', dateFrom: '', dateTo: '' \}\)/)
  // Separate clear functions.
  assert.match(appSource, /function clearQueryOrderFilters\(\)/)
  assert.match(appSource, /function clearQueryPaymentFilters\(\)/)
})

test('user order filter renders the filtered list and does not mutate source', () => {
  assert.match(appSource, /v-for="\(order, orderIndex\) in filteredQueryOrders"/)
  // filteredQueryOrders builds new objects (spread), never mutates queryOrders.
  assert.match(appSource, /\.\.\.order,\s*\n\s*items: order\.items\.filter/)
  // Independent from payment history: payment table uses filteredQueryPayments.
  assert.match(appSource, /v-for="\(payment, paymentIndex\) in filteredQueryPayments"/)
})

test('filters have a visible clear control and empty-state', () => {
  assert.match(appSource, /clearQueryOrderFilters/)
  assert.match(appSource, /没有符合当前筛选条件的订单明细/)
  assert.match(appSource, /没有符合当前筛选条件的付款记录/)
})

test('summary scope is disclosed: totals stay whole-data while list is filtered', () => {
  assert.match(appSource, /筛选仅改变下方明细，汇总金额始终采用后端完整结果/)
  assert.match(appSource, /formatMoney\(queryOrders\.total_amount\)/)
  assert.match(appSource, /formatMoney\(queryOrders\.paid_amount\)/)
  assert.match(appSource, /formatMoney\(queryOrders\.remaining_amount\)/)
})

test('admin unpaid-detail filter is display-only and preserves selection/totals', () => {
  // The table renders the filtered list…
  assert.match(appSource, /v-for="item in filteredCnPaymentItems"/)
  // …but selection and amounts still key off the full cnPayment.items set.
  assert.match(appSource, /selectedPaymentItems = computed\(\(\) => \(cnPayment\.value\?\.items \?\? \[\]\)/)
  assert.match(appSource, /筛选只影响下方明细的显示，不影响已勾选项与合计/)
  assert.match(appSource, /function clearCnPaymentItemFilters\(\)/)
})

test('admin payment records keep their own filter block with a reset/clear', () => {
  // The payment-records filter form is a distinct block with its own reset.
  assert.match(appSource, /resetPaymentFilters/)
})
