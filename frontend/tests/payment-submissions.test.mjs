import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 11H：收肾记录（付款凭证）前端契约。
// 只做静态断言，锁定 UI 结构复用既有组件、隐藏技术字段、金额口径与响应式。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const styleSource = read('src/style.css')

function templateBlock(startMarker, endMarker) {
  const start = appSource.indexOf(startMarker)
  assert.ok(start > 0, `could not locate ${startMarker}`)
  const end = appSource.indexOf(endMarker, start + startMarker.length)
  assert.ok(end > start, `could not locate ${endMarker} after ${startMarker}`)
  return appSource.slice(start, end)
}

const adminListTemplate = templateBlock(
  `<template v-else-if="routeName === 'admin-submissions'">`,
  `<template v-else-if="routeName === 'admin-submission-detail'">`,
)
const adminDetailTemplate = templateBlock(
  `<template v-else-if="routeName === 'admin-submission-detail'">`,
  `<template v-else-if="routeName === 'admin-qr'">`,
)
const userPaymentTemplate = templateBlock(
  `<section class="panel query-submission-panel">`,
  `<section v-if="routeName === 'query-security'"`,
)

// ---- API 层 ----

test('client 暴露收肾记录的用户与管理员接口', () => {
  for (const fn of [
    'listUserPaymentSubmissions',
    'submitPaymentSubmission',
    'listAdminPaymentSubmissions',
    'getAdminPaymentSubmissionFacets',
    'getAdminPaymentSubmissionDetail',
    'rejectPaymentSubmission',
    'approvePaymentSubmission',
  ]) {
    assert.match(clientSource, new RegExp(`export function ${fn}`), `缺少 ${fn}`)
  }
  // 用户 DTO 绝不携带技术字段。
  const userType = clientSource.slice(
    clientSource.indexOf('export type UserPaymentSubmission'),
    clientSource.indexOf('export type UserPaymentSubmissionListResponse'),
  )
  for (const forbidden of ['sha256', 'image_data', 'user_id', 'linked_payment_id', 'reviewed_by']) {
    assert.doesNotMatch(userType, new RegExp(forbidden), `用户提交类型不得包含 ${forbidden}`)
  }
})

// ---- 管理员列表：WPS 表头筛选 ----

test('管理员收肾记录列表复用既有 WPS 组件，不新写筛选组件', () => {
  assert.match(adminListTemplate, /<ColumnValueFilter[^>]*column="cn"/)
  assert.match(adminListTemplate, /<ColumnValueFilter[^>]*column="payment_method"/)
  assert.match(adminListTemplate, /<ColumnValueFilter[^>]*column="status"/)
  assert.match(adminListTemplate, /<ColumnValueFilter[^>]*column="reviewed_by"/)
  assert.match(adminListTemplate, /<ColumnRangeFilter[^>]*label="本金"/)
  assert.match(adminListTemplate, /<ColumnRangeFilter[^>]*label="手续费"/)
  assert.match(adminListTemplate, /<ColumnRangeFilter[^>]*label="本次应付"/)
  assert.match(adminListTemplate, /<ColumnDateFilter[^>]*label="提交时间"/)
  assert.match(adminListTemplate, /<ColumnDateFilter[^>]*label="核对时间"[^>]*allow-blank/)
})

test('管理员列表有结果总数、分页与清空全部筛选', () => {
  assert.match(adminListTemplate, /结果：共 \{\{ submissionTotal \}\} 条收肾记录/)
  assert.match(adminListTemplate, /changeSubmissionPageSize/)
  assert.match(adminListTemplate, /resetSubmissionFilters/)
  assert.match(adminListTemplate, /goToSubmissionPage/)
})

test('管理员列表主表不渲染 SHA / 内部 ID 等技术字段', () => {
  for (const forbidden of ['sha256', 'submission.id }}', 'submission.user_id', 'submission.linked_payment_id', 'image_data']) {
    assert.doesNotMatch(adminListTemplate, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `主表不得出现 ${forbidden}`)
  }
  // 宽表只在容器内部横向滚动。
  assert.match(adminListTemplate, /class="table-scroll[^"]*submission-records-table/)
  assert.match(styleSource, /\.submission-records-table table \{ min-width:/)
})

// ---- 管理员详情：图片居中 + 通过/驳回 + 技术区 ----

test('管理员详情图片居中显示，且预览与图片不拉伸', () => {
  assert.match(adminDetailTemplate, /class="submission-image-slot"/)
  assert.match(adminDetailTemplate, /class="submission-image"/)
  assert.match(styleSource, /\.submission-image-slot[\s\S]*justify-content: center/)
  assert.match(styleSource, /\.submission-image[\s\S]*object-fit: contain/)
})

test('管理员详情提供驳回（原因必填）与核对通过（复用明细分配）', () => {
  // 驳回：必填校验 disable。
  assert.match(adminDetailTemplate, /submissionRejectReason/)
  assert.match(adminDetailTemplate, /:disabled="submissionRejecting \|\| submissionRejectReason\.trim\(\) === ''"/)
  // 通过：复用录入付款的明细分配（勾选 + 分摊金额 + 后端创建付款）。
  assert.match(adminDetailTemplate, /loadSubmissionAllocation/)
  assert.match(adminDetailTemplate, /setPaymentItemSelected/)
  assert.match(adminDetailTemplate, /paymentAmounts\[item\.id\]/)
  assert.match(adminDetailTemplate, /approveSubmission/)
  // 仅 submitted 才出现通过/驳回。
  assert.match(adminDetailTemplate, /v-if="submissionDetail\.submission\.status === 'submitted'"/)
})

test('管理员详情技术字段在默认收起的技术标识区', () => {
  assert.match(adminDetailTemplate, /class="panel nested-panel technical-section"/)
  assert.match(adminDetailTemplate, /▶ 技术标识/)
  assert.match(adminDetailTemplate, /仅供技术排查/)
  // 技术区之外不得出现 sha256 明文渲染。
  const beforeTechnical = adminDetailTemplate.slice(0, adminDetailTemplate.indexOf('technical-section'))
  assert.doesNotMatch(beforeTechnical, /submission\.sha256/)
})

// ---- 普通用户付款中心：提交入口 ----

test('用户付款中心有提交收肾记录入口、预览与金额展示', () => {
  assert.match(userPaymentTemplate, /提交收肾记录/)
  assert.match(userPaymentTemplate, /id="submission-file-input"/)
  assert.match(userPaymentTemplate, /accept="image\/png,image\/jpeg,image\/webp"/)
  assert.match(userPaymentTemplate, /class="submission-preview-slot"/)
  // 付款方式 / 本金 / 手续费 / 本次应付 全部展示。
  assert.match(userPaymentTemplate, /本金/)
  assert.match(userPaymentTemplate, /手续费/)
  assert.match(userPaymentTemplate, /本次应付/)
  assert.match(userPaymentTemplate, /formatMoney\(queryFeeAmount\)/)
  assert.match(userPaymentTemplate, /formatMoney\(queryPayableAmount\)/)
})

test('用户未选图或未选方式时提交按钮禁用，上传中防重复点击', () => {
  assert.match(userPaymentTemplate, /:disabled="!canSubmitProof \|\| queryQRMethod === ''"/)
  assert.match(userPaymentTemplate, /submissionUploading \? '提交中…' : '提交收肾记录'/)
  assert.match(appSource, /const canSubmitProof = computed\(\(\) => submissionFile\.value !== null && !submissionUploading\.value\)/)
})

test('用户历史显示状态、时间、驳回原因，且不含技术字段', () => {
  assert.match(userPaymentTemplate, /我的收肾记录/)
  assert.match(userPaymentTemplate, /submissionStatusLabel\(submission\.status\)/)
  assert.match(userPaymentTemplate, /formatDate\(submission\.submitted_at\)/)
  assert.match(userPaymentTemplate, /submission\.status === 'rejected' && submission\.reject_reason/)
  // id 只作 :key，不得作为技术字段渲染；sha / 内部 id / 管理员一律不出现。
  assert.match(userPaymentTemplate, /:key="submission\.id"/)
  assert.doesNotMatch(userPaymentTemplate, /\{\{ submission\.id \}\}/)
  for (const forbidden of ['sha256', 'submission.user_id', 'submission.linked_payment_id', 'reviewed_by']) {
    assert.doesNotMatch(userPaymentTemplate, new RegExp(forbidden.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')), `用户历史不得出现 ${forbidden}`)
  }
})

test('提交成功文案为“已交肾（待管理员核对）”口径', () => {
  assert.match(appSource, /已交肾（待管理员核对）/)
})

// ---- 一键全选（最小功能补丁）----

test('全选按钮存在且空列表 / 已全选时禁用，取消全选在无选中时禁用', () => {
  assert.match(adminDetailTemplate, /class="page-actions submission-select-actions"/)
  assert.match(adminDetailTemplate, /:disabled="selectableCnPaymentItems\.length === 0 \|\| allCnPaymentItemsSelected"[^>]*@click="selectAllCnPaymentItems">全选待付明细</)
  assert.match(adminDetailTemplate, /:disabled="selectedPaymentItems\.length === 0"[^>]*@click="clearAllCnPaymentItemSelection">取消全选</)
  // 已选 N / M 条即时反映选中状态。
  assert.match(adminDetailTemplate, /已选 \{\{ selectedPaymentItems\.length \}\} \/ \{\{ selectableCnPaymentItems\.length \}\} 条/)
})

test('全选逐行复用 setPaymentItemSelected，不另写分配算法、不绕过校验', () => {
  const fnStart = appSource.indexOf('function selectAllCnPaymentItems()')
  const fnEnd = appSource.indexOf('function clearAllCnPaymentItemSelection()')
  assert.ok(fnStart > 0 && fnEnd > fnStart, 'select-all functions missing')
  const selectAll = appSource.slice(fnStart, fnEnd)
  const clearAll = appSource.slice(fnEnd, appSource.indexOf('}', appSource.indexOf('setPaymentItemSelected', fnEnd) + 1) + 1)
  // 两个方向都只调用既有的逐行勾选入口。
  assert.match(selectAll, /setPaymentItemSelected\(item, true\)/)
  assert.match(clearAll, /setPaymentItemSelected\(item, false\)/)
  // 全选函数内不得出现任何金额写入 / 分配计算（金额由 setPaymentItemSelected 统一填默认值）。
  for (const forbidden of ['paymentAmounts.value =', 'formatMoney(', 'remaining_amount)', 'round', 'toFixed']) {
    assert.ok(!selectAll.includes(forbidden), `selectAllCnPaymentItems 不得包含 ${forbidden}`)
  }
  // 可选集合只包含 remaining_amount > 0 的行——与复选框禁用条件一致。
  assert.match(appSource, /selectableCnPaymentItems = computed\(\(\) => \(cnPayment\.value\?\.items \?\? \[\]\)\.filter\(\(item\) => item\.remaining_amount > 0\)\)/)
  // 原有超额校验入口未被改动。
  assert.match(appSource, /function paymentAmountInvalid\(item: PaymentItemRow\)/)
  assert.match(appSource, /cents > moneyToCents\(item\.remaining_amount\)/)
})

test('响应式：用户提交区窄屏折叠为单列', () => {
  assert.match(styleSource, /\.query-submission-grid \{[\s\S]*grid-template-columns: repeat\(2/)
  assert.match(styleSource, /@media \(max-width: 640px\) \{[\s\S]*\.query-submission-grid \{ grid-template-columns: minmax\(0, 1fr\)/)
})
