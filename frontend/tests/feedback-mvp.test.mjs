import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')
const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const statusBarSource = read('src/components/PortalStatusBar.vue')

test('用户反馈路由和低强调度入口不破坏四卡布局', () => {
  assert.match(appSource, /path === '\/query\/feedback'/)
  const portalStart = appSource.indexOf(`<template v-else-if="routeName === 'query'">`)
  const portalEnd = appSource.indexOf(`<template v-else-if="isUserRoute">`, portalStart)
  const portal = appSource.slice(portalStart, portalEnd)
  assert.equal((portal.match(/<ModuleCard /g) ?? []).length, 4, '用户中心必须继续保持四张 ModuleCard')
  assert.match(portal, /使用中遇到问题或有建议？/)
  assert.match(portal, /navigate\('\/query\/feedback'\)/)
  assert.doesNotMatch(statusBarSource, /feedback|意见反馈/, '反馈入口不得加入 PortalStatusBar')
})

test('用户表单限制1000字、显示字数并阻止重复点击', () => {
  assert.match(appSource, /routeName === 'query-feedback'/)
  assert.match(appSource, /<textarea[\s\S]*v-model="feedbackContent"[\s\S]*maxlength="1000"[\s\S]*required/)
  assert.match(appSource, /feedbackCharacterCount }} \/ 1000 字/)
  assert.match(appSource, /:disabled="feedbackSubmitting \|\| feedbackContent\.trim\(\)\.length === 0"/)
  assert.match(appSource, /if \(feedbackSubmitting\.value\) return/)
  assert.match(appSource, /if \(length > 1000\)/)
  assert.match(appSource, /await createFeedback\(content\)[\s\S]*feedbackContent\.value = ''[\s\S]*提交成功/)
  assert.match(appSource, /feedbackContent\.value = ''[\s\S]*feedbackMessage\.value = ''/, '退出登录必须清除上一用户的草稿和提示')
})

test('API只发送正文且管理员接口支持筛选分页和状态更新', () => {
  assert.match(clientSource, /postJSON<FeedbackCreateResponse>\('\/api\/query\/feedbacks', \{ content \}\)/)
  assert.doesNotMatch(clientSource, /\/api\/query\/feedbacks[\s\S]{0,120}user_id/)
  assert.match(clientSource, /getJSON<FeedbackListResponse>\(`\/api\/admin\/feedbacks/)
  assert.match(clientSource, /patchJSON<FeedbackCreateResponse>\(`\/api\/admin\/feedbacks\/\$\{encodeURIComponent\(id\)\}\/status`/)
  assert.match(appSource, /params\.set\('status', adminFeedbackStatus\.value\)/)
  assert.match(appSource, /page_size: String\(adminFeedbackPageSize\.value\)/)
})

test('管理员列表可查看纯文本并修改状态且没有详情路由', () => {
  assert.match(appSource, /path === '\/admin\/feedbacks'/)
  assert.match(appSource, /title="意见反馈"[\s\S]*navigate\('\/admin\/feedbacks'\)/)
  assert.match(appSource, /routeName === 'admin-feedbacks'/)
  for (const label of ['状态', '用户', '时间', '内容摘要', '查看内容', '状态修改']) {
    assert.equal(appSource.includes(`<th>${label}</th>`), true, `管理员反馈表缺少 ${label} 列`)
  }
  assert.match(appSource, /toggleFeedbackContent\(item\.id\)/)
  assert.match(appSource, /changeFeedbackStatus\(item, item\.status === 'new' \? 'processed' : 'new'\)/)
  assert.doesNotMatch(appSource, /admin\/feedbacks\/[^'"`]+\/detail/)
  assert.doesNotMatch(appSource, /v-html/, '反馈正文必须使用 Vue 文本转义，禁止 v-html')
})
