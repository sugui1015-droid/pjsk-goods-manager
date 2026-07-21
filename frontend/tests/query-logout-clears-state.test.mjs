import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 退出登录必须无条件清空用户态：曾经清空写在 try 块内、跟在 await 之后，
// 只要 /api/query/logout 慢一点或失败，上一位用户的 CN 与订单就会留在页面上，
// 下一个人登录时先看到的是别人的数据。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const appSource = readFileSync(resolve(frontendRoot, 'src/App.vue'), 'utf8')

const logoutQuery = (() => {
  const start = appSource.indexOf('async function logoutQuery()')
  assert.ok(start > 0, 'could not locate logoutQuery')
  const end = appSource.indexOf('\nfunction resetAnonymousQueryRecovery', start)
  assert.ok(end > start, 'could not locate the end of logoutQuery')
  return appSource.slice(start, end)
})()

// 清空语句必须位于 try/catch 之后，而不是 try 内部。
const afterCatch = (() => {
  const marker = logoutQuery.lastIndexOf('  }\n')
  const catchIndex = logoutQuery.indexOf('} catch (error) {')
  assert.ok(catchIndex > 0, 'logoutQuery no longer has a catch block')
  const catchEnd = logoutQuery.indexOf('\n  }', catchIndex)
  assert.ok(catchEnd > catchIndex && marker > 0, 'could not split logoutQuery')
  return logoutQuery.slice(catchEnd)
})()

const MUST_CLEAR = [
  'queryUser.value = null',
  'queryOrders.value = null',
  'queryCN.value = \'\'',
  'queryCode.value = \'\'',
  'queryRecoveryEmail.value = null',
  'userSubmissions.value = []',
  'queryQRAvailability.value = []',
]

test('退出登录在 try/catch 之外清空全部用户态', () => {
  for (const statement of MUST_CLEAR) {
    assert.ok(
      afterCatch.includes(statement),
      `退出登录未在请求失败时清空 ${statement}；上一位用户的数据会残留给下一位`,
    )
  }
})

test('退出登录不把清空动作藏在 await 之后的 try 块里', () => {
  const tryBlock = logoutQuery.slice(
    logoutQuery.indexOf('try {'),
    logoutQuery.indexOf('} catch (error) {'),
  )
  assert.ok(
    !tryBlock.includes('queryUser.value = null'),
    '清空写在 try 内：logout 请求一旦失败或超时就不会执行',
  )
})

test('退出登录容忍 401（会话本就已失效）', () => {
  assert.ok(
    logoutQuery.includes('error.status === 401'),
    '会话已过期时退出登录应视为成功，而不是报错并保留用户态',
  )
})
