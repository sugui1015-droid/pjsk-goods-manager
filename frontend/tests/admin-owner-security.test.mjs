import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 2H-2B：系统所有者账号与安全恢复机制的前端源码断言。
//
// 覆盖：reauth 拦截与全局重验证弹窗、账户安全页路由与表单的密码管理器
// autocomplete 语义、恢复码一次性展示、SMTP disabled 显式不可用文案、
// 登录页恢复码重置入口。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')

const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')

test('api/client 定义 reauth_required 拦截并在重验证成功后重试一次', () => {
  assert.match(clientSource, /export const REAUTH_REQUIRED = 'reauth_required'/)
  assert.match(clientSource, /export function setReauthHandler/)
  assert.match(clientSource, /error\.status === 403 && error\.message === REAUTH_REQUIRED/)
  // 每个请求助手都必须经过带重试的 execute 包装。
  for (const helper of ['getJSON', 'postJSON', 'patchJSON', 'putJSON', 'deleteJSON', 'postForm']) {
    const body = clientSource.slice(clientSource.indexOf(`export async function ${helper}`))
    assert.match(body.slice(0, 400), /execute<T>/, `${helper} 必须走 execute 拦截`)
  }
})

test('api/client 暴露安全相关 API 且恢复码重置不签发会话', () => {
  for (const fn of [
    'adminReauth', 'changeAdminPassword', 'getAdminSecurityRecoveryEmail',
    'requestAdminRecoveryEmailBind', 'confirmAdminRecoveryEmailBind',
    'getAdminAuditSummary', 'getOwnerRecoveryCodesStatus',
    'generateOwnerRecoveryCodes', 'adminRecoveryCodeReset',
  ]) {
    assert.match(clientSource, new RegExp(`export function ${fn}`), `缺少 ${fn}`)
  }
  assert.match(clientSource, /\/api\/admin\/recovery\/code-reset/)
})

test('账户安全页有独立路由并挂到管理门户', () => {
  assert.match(appSource, /'admin-security'/)
  assert.match(appSource, /if \(path === '\/admin\/security'\) return 'admin-security'/)
  assert.match(appSource, /title="账户安全"[^>]*@enter="navigate\('\/admin\/security'\)"/)
  assert.match(appSource, /if \(routeName\.value === 'admin-security'\) await loadAdminSecurity\(\)/)
})

test('改密表单具备密码管理器语义（隐藏用户名 + current/new-password）', () => {
  const section = appSource.slice(appSource.indexOf('aria-label="修改密码"'))
  assert.match(section.slice(0, 1600), /autocomplete="username"/, '必须包含 username 字段供密码管理器识别账号')
  assert.match(section.slice(0, 1600), /autocomplete="current-password"/)
  const newPasswordCount = (section.slice(0, 1600).match(/autocomplete="new-password"/g) ?? []).length
  assert.equal(newPasswordCount, 2, '新密码与确认新密码都必须标记 new-password')
})

test('恢复码只展示一次且需要确认已保存', () => {
  assert.match(appSource, /这批恢复码只显示这一次/)
  assert.match(appSource, /我已妥善保存/)
  assert.match(appSource, /关闭后将无法再次查看这批恢复码/)
  assert.match(appSource, /重新生成会立即作废现有的/)
})

test('SMTP 未启用时邮箱恢复必须显式显示不可用', () => {
  assert.match(appSource, /邮箱恢复尚未启用/)
  assert.match(appSource, /!securityRecoveryEmail\.delivery_enabled/)
})

test('全局重验证弹窗存在且用 current-password 语义', () => {
  const dialog = appSource.slice(appSource.indexOf('class="reauth-overlay"'))
  assert.ok(dialog.length > 100, '必须存在全局重验证弹窗')
  assert.match(dialog.slice(0, 1200), /autocomplete="current-password"/)
  assert.match(appSource, /setReauthHandler\(openReauthDialog\)/)
  assert.match(appSource, /onUnmounted\(\(\) => setReauthHandler\(null\)\)/)
})

test('管理员登录页保留密码管理器语义并提供恢复码重置入口', () => {
  const login = appSource.slice(appSource.indexOf('管理员登录</h2>'))
  assert.match(login.slice(0, 900), /autocomplete="username"/)
  assert.match(login.slice(0, 900), /autocomplete="current-password"/)
  assert.match(appSource, /使用恢复码重置/)
  const recovery = appSource.slice(appSource.indexOf('恢复码重置密码</h2>'))
  assert.match(recovery.slice(0, 1600), /autocomplete="one-time-code"/, '恢复码输入使用 one-time-code')
  const newPwd = (recovery.slice(0, 1600).match(/autocomplete="new-password"/g) ?? []).length
  assert.equal(newPwd, 2, '恢复流程的新密码字段必须标记 new-password')
})
