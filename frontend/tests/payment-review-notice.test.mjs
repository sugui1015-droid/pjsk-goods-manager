import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'
import { getPaymentNoticeState } from '../src/paymentNotice.ts'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')
const appSource = read('src/App.vue')
const statusBarSource = read('src/components/PortalStatusBar.vue')
const noticeSource = read('src/paymentNotice.ts')

test('审核通过时间晚于本机查看时间时显示提醒', () => {
  assert.deepEqual(getPaymentNoticeState([
    { status: 'approved', reviewed_at: '2026-07-22T10:00:00Z' },
  ], '2026-07-22T09:00:00Z'), {
    hasNotice: true,
    text: '付款审核有更新',
  })
})

test('已查看的旧审核记录不提醒', () => {
  assert.deepEqual(getPaymentNoticeState([
    { status: 'rejected', reviewed_at: '2026-07-15T10:00:00Z' },
  ], '2026-07-22T09:00:00Z'), {
    hasNotice: false,
    text: '',
  })
})

test('多个提交逐条检查，不受待审核记录或数组顺序影响', () => {
  const state = getPaymentNoticeState([
    { status: 'submitted' },
    { status: 'approved', reviewed_at: '2026-07-20T10:00:00Z' },
    { status: 'rejected', reviewed_at: '2026-07-22T10:00:00Z' },
  ], '2026-07-21T10:00:00Z')
  assert.equal(state.hasNotice, true)
})

test('无效时间和未审核状态不能制造提醒', () => {
  const state = getPaymentNoticeState([
    { status: 'submitted', reviewed_at: '2026-07-22T10:00:00Z' },
    { status: 'approved', reviewed_at: 'not-a-time' },
  ], '2026-07-21T10:00:00Z')
  assert.equal(state.hasNotice, false)
})

test('用户中心复用提交列表接口，付款中心成功加载后才记录已查看', () => {
  assert.match(appSource, /async function handleQueryPortalEntered\(\)[\s\S]*if \(queryUser\.value\) await loadUserSubmissions\(\)/)
  assert.match(appSource, /if \(routeName\.value === 'query-payment'\) \{\s*const loaded = await loadUserSubmissions\(\)\s*if \(loaded\) markPaymentSubmissionsViewed\(\)/)
  assert.match(noticeSource, /pjsk:lastPaymentViewedAt:\$\{normalizedCN\}/)
  assert.match(appSource, /writePaymentViewedAt\(queryUser\.value\.cn_code, viewedAt\)/)
  assert.doesNotMatch(appSource, /localStorage\.setItem/, '浏览器存储访问必须收敛在提醒模块，不能污染 App 安全边界')
})

test('提醒只挂在用户中心状态栏，点击进入付款中心', () => {
  assert.match(statusBarSource, /notice\?: \{\s*text: string\s*action\?: string/)
  assert.match(statusBarSource, /\$emit\('notice'\)/)
  assert.match(appSource, /:notice="paymentNoticeState\.hasNotice[^>]*@notice="navigate\('\/query\/payment'\)"/s)
})
