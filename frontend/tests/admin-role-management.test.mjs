import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 2H-R2：分级权限前端源码断言。
//
// 覆盖角色显示映射、首次登录强制改密门禁、用户列表任命入口、苏归专属管理员
// 管理页、一次性临时密码只显示一次、权限隔离与错误处理。测试解析源码文本，
// 不涉及任何真实临时密码明文。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (path) => readFileSync(resolve(frontendRoot, path), 'utf8')
const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const styleSource = read('src/style.css')

function routeTemplate(name, nextMarker) {
  const start = appSource.indexOf(`routeName === '${name}'`)
  assert.ok(start > 0, `找不到路由模板 ${name}`)
  const next = appSource.indexOf(nextMarker, start + 10)
  return appSource.slice(start, next > start ? next : undefined)
}

// ===== 1-2 角色显示映射 =====

test('roleDisplayName 是唯一映射：owner→苏归、admin→管理员、user→用户', () => {
  const fn = clientSource.slice(clientSource.indexOf('export function roleDisplayName'))
  assert.match(fn, /case 'owner':\s*return '苏归'/)
  assert.match(fn, /case 'admin':\s*return '管理员'/)
  assert.match(fn, /case 'user':\s*return '用户'/)
  // 兜底也是「用户」，绝不把技术值原样返回。
  assert.match(fn.slice(0, fn.indexOf('\n}')), /default:\s*return '用户'/)
})

test('前端引用 roleDisplayName 展示角色，不直接渲染裸 owner/admin 技术值', () => {
  assert.match(appSource, /import\s*\{[\s\S]*roleDisplayName[\s\S]*\}\s*from '\.\/api\/client'/)
  // 管理员管理表格用 roleDisplayName(entry.role)，不是 {{ entry.role }}。
  assert.match(appSource, /roleDisplayName\(entry\.role\)/)
  assert.doesNotMatch(appSource, /\{\{\s*entry\.role\s*\}\}/)
  assert.doesNotMatch(appSource, /\{\{\s*admin\.role\s*\}\}/)
  assert.doesNotMatch(appSource, /\{\{\s*admin\?\.role\s*\}\}/)
})

// ===== 3-4 管理员管理入口的权限隔离 =====

test('管理员管理入口卡片仅苏归可见（v-if=isOwner）', () => {
  assert.match(appSource, /<ModuleCard v-if="isOwner" title="管理员管理"[^>]*@enter="navigate\('\/admin\/admins'\)"/)
})

test('管理员管理路由已注册并接入路由加载', () => {
  assert.match(appSource, /if \(path === '\/admin\/admins'\) return 'admin-admins'/)
  assert.match(appSource, /if \(path\.startsWith\('\/admin\/admins\/'\)\) return 'admin-admin-detail'/)
  assert.match(appSource, /if \(routeName\.value === 'admin-admins' && isOwner\.value\) await loadOwnerAdmins\(\)/)
})

test('非苏归访问管理员管理路由时前端显示无权限（后端仍 403）', () => {
  const tpl = routeTemplate('admin-admins', "routeName === 'admin-admin-detail'")
  assert.match(tpl, /v-if="!isOwner"/)
  assert.match(tpl, /仅「苏归」可进入管理员管理/)
})

// ===== 5-7 首次登录强制改密 =====

test('强制改密门禁：must_change_password 时呈现专用改密视图', () => {
  assert.match(appSource, /const mustChangePassword = computed\(\(\) => Boolean\(admin\.value\?\.must_change_password\)\)/)
  assert.match(appSource, /v-if="admin && mustChangePassword && isAdminSurface"/)
  assert.match(appSource, /aria-label="首次登录修改密码"/)
})

test('强制改密门禁刷新后仍生效：标志来自 /api/admin/me，且改密期跳过数据加载', () => {
  // must_change_password 随 /api/admin/me 返回（ensureAdmin），刷新后重新计算。
  assert.match(clientSource, /must_change_password\?: boolean/)
  // 进入任意管理路由时，若仍需改密则直接 return，不触发会 403 的数据加载。
  assert.match(appSource, /if \(mustChangePassword\.value\) return/)
})

test('改密表单具备密码管理器 autocomplete 语义，默认隐藏输入', () => {
  const gate = appSource.slice(appSource.indexOf('aria-label="首次登录修改密码"'), appSource.indexOf('aria-label="首次登录修改密码"') + 1400)
  // 隐藏用户名 + 当前(临时)密码 current-password + 新/确认 new-password。
  assert.match(gate, /name="username"[^>]*autocomplete="username"/)
  assert.match(gate, /临时密码[\s\S]*?type="password"[^>]*autocomplete="current-password"/)
  const newPwd = [...gate.matchAll(/autocomplete="new-password"/g)]
  assert.equal(newPwd.length, 2, '新密码与确认新密码都应为 new-password')
  // 全部为 password 类型（默认隐藏）。
  assert.doesNotMatch(gate, /firstPwd[A-Za-z]*"[^>]*type="text"/)
})

test('改密成功后重新拉取身份并解除门禁再进入管理中心', () => {
  const fn = appSource.slice(appSource.indexOf('async function submitFirstPasswordChange'), appSource.indexOf('async function submitFirstPasswordChange') + 900)
  assert.match(fn, /await changeAdminPassword\(firstPwdCurrent\.value, firstPwdNew\.value\)/)
  assert.match(fn, /await ensureAdmin\(\)/)
  assert.match(fn, /if \(mustChangePassword\.value\)/)
  assert.match(fn, /navigate\('\/admin'\)/)
})

test('临时密码不写入 localStorage/sessionStorage/URL', () => {
  // 全站没有任何持久化存储写入调用（临时密码只存于内存 ref）。
  assert.doesNotMatch(appSource, /localStorage\.setItem/)
  assert.doesNotMatch(appSource, /sessionStorage\.setItem/)
  assert.doesNotMatch(appSource, /(local|session)Storage\[/)
  // 临时密码不进地址栏：不出现在任何 navigate() 参数里。
  assert.doesNotMatch(appSource, /navigate\([^)]*tempPassword/)
  // 登录后强制改密时丢弃深链、跳 /admin，不把密码放进地址。
  assert.match(appSource, /if \(response\.admin\.must_change_password\)/)
})

// ===== 8-11 任命与临时密码 =====

test('管理接口集中在 api/client，路径正确且经 execute（reauth 自动重试）', () => {
  for (const [fn, method, path] of [
    ['listOwnerAdmins', 'getJSON', '/api/admin/owner/admins'],
    ['appointOwnerAdmin', 'postJSON', '/api/admin/owner/admins'],
    ['enableOwnerAdmin', 'postJSON', '/enable'],
    ['disableOwnerAdmin', 'postJSON', '/disable'],
    ['revokeOwnerAdmin', 'postJSON', '/revoke'],
    ['resetOwnerAdminPassword', 'postJSON', '/reset-password'],
    ['getOwnerAdminAudit', 'getJSON', '/audit'],
  ]) {
    const body = clientSource.slice(clientSource.indexOf(`export function ${fn}`), clientSource.indexOf(`export function ${fn}`) + 300)
    assert.match(body, new RegExp(method), `${fn} 必须走 ${method}`)
    assert.ok(body.includes(path), `${fn} 路径应含 ${path}`)
  }
  // getJSON/postJSON 都经过带 reauth 重试的 execute（已由 admin-owner-security 覆盖）。
  assert.match(clientSource, /error\.status === 403 && error\.message === REAUTH_REQUIRED/)
})

test('任命弹窗显示目标 CN/显示名，苏归不能手动设置长期密码', () => {
  const modal = appSource.slice(appSource.indexOf('aria-label="设为管理员"'), appSource.indexOf('aria-label="一次性临时密码"'))
  assert.match(modal, /appointUser\.cn_code/)
  assert.match(modal, /appointUser\.display_name/)
  // 只输入登录用户名，没有任何密码输入框。
  assert.match(modal, /管理员登录用户名/)
  assert.doesNotMatch(modal, /type="password"/)
  assert.match(modal, /初始密码由系统随机生成/)
})

test('任命成功只显示一次临时密码，且提供手动复制（不自动复制）', () => {
  const fn = appSource.slice(appSource.indexOf('async function submitAppoint'), appSource.indexOf('async function submitAppoint') + 900)
  assert.match(fn, /showTempPassword\(result\)/)
  assert.match(fn, /await Promise\.all\(\[loadOwnerAdmins\(\), loadAdminUsers\(\)\]\)/)
  const modal = appSource.slice(appSource.indexOf('aria-label="一次性临时密码"'), appSource.indexOf('aria-label="一次性临时密码"') + 900)
  assert.match(modal, /只显示一次/)
  assert.match(modal, /temp-password-box/)
  assert.match(modal, /@click="copyTempPassword"/)
  // 复制只在用户点击时发生，绝非自动触发。
  assert.match(appSource, /async function copyTempPassword\(\)[\s\S]*?navigator\.clipboard\.writeText\(tempPasswordValue\.value\)/)
})

test('关闭弹窗后临时密码从内存抹除，无法再次取回', () => {
  const fn = appSource.slice(appSource.indexOf('function acknowledgeTempPassword'), appSource.indexOf('function acknowledgeTempPassword') + 300)
  assert.match(fn, /tempPasswordValue\.value = ''/)
  assert.match(fn, /tempPasswordFor\.value = ''/)
  // 明文只存于内存 ref，弹窗以 v-if="tempPasswordValue" 控制，值清空即消失。
  assert.match(appSource, /v-if="tempPasswordValue" class="app-modal-overlay"/)
})

// ===== 11 启停/撤销/重置写操作 =====

test('启停/撤销/重置均调用 owner-only 写接口（经 reauth 拦截）', () => {
  for (const [fn, api] of [
    ['disableAdmin', 'disableOwnerAdmin'],
    ['enableAdmin', 'enableOwnerAdmin'],
    ['revokeAdmin', 'revokeOwnerAdmin'],
    ['resetAdminPassword', 'resetOwnerAdminPassword'],
  ]) {
    const body = appSource.slice(appSource.indexOf(`async function ${fn}`), appSource.indexOf(`async function ${fn}`) + 700)
    assert.ok(body.includes(api), `${fn} 应调用 ${api}`)
    assert.match(body, /window\.prompt/, `${fn} 应要求填写原因`)
  }
  // 重置成功同样只展示一次临时密码。
  const reset = appSource.slice(appSource.indexOf('async function resetAdminPassword'), appSource.indexOf('async function resetAdminPassword') + 700)
  assert.match(reset, /showTempPassword\(result\)/)
})

// ===== 12 苏归本人无危险操作 =====

test('管理员管理页对苏归本人只显示「唯一苏归」，无停用/撤销/重置按钮', () => {
  const tpl = routeTemplate('admin-admins', "routeName === 'admin-admin-detail'")
  // owner 行走 v-if="entry.role === 'owner'" 分支，只有「唯一苏归」文案。
  assert.match(tpl, /v-if="entry\.role === 'owner'"[\s\S]*?唯一苏归/)
  // 危险操作按钮都在 v-else（非 owner）分支里。
  const ownerBranch = tpl.slice(tpl.indexOf("entry.role === 'owner'"), tpl.indexOf('<template v-else>', tpl.indexOf("entry.role === 'owner'")))
  assert.doesNotMatch(ownerBranch, /revokeAdmin|disableAdmin|resetAdminPassword/)
})

test('用户列表对苏归本人关联用户不显示任命/危险操作', () => {
  const tpl = routeTemplate('admin-users', "routeName === 'admin-user-detail'")
  assert.match(tpl, /v-if="user\.id === ownerLinkedUserId"/)
  // 该分支只渲染徽标，不含设为管理员/撤销等按钮。
  const selfBranch = tpl.slice(tpl.indexOf('user.id === ownerLinkedUserId'), tpl.indexOf('v-else-if', tpl.indexOf('user.id === ownerLinkedUserId')))
  assert.doesNotMatch(selfBranch, /openAppoint|revokeAdmin/)
})

// ===== 13 错误处理 =====

test('friendlyAdminError 覆盖 401/403/404/409/422/500 且不回显后端堆栈', () => {
  const fn = appSource.slice(appSource.indexOf('function friendlyAdminError'), appSource.indexOf('function friendlyAdminError') + 900)
  for (const code of ['401', '403', '404', '409', '400', '422', '500']) {
    assert.ok(fn.includes(`case ${code}:`), `缺少 ${code} 分支`)
  }
  assert.match(fn, /reauth_required/)
  // 不把 error 对象/堆栈塞进界面。
  assert.doesNotMatch(appSource, /\.value = String\(error\)/)
  assert.doesNotMatch(appSource, /error\.stack/)
})

// ===== 14-15 revoke 与复聘文案 =====

test('撤销文案说明普通用户身份不受影响', () => {
  assert.match(appSource, /async function revokeAdmin[\s\S]*?普通用户账号、订单、付款与查询码不受影响/)
  // 管理员管理页图例区同样区分停用与撤销。
  const tpl = routeTemplate('admin-admins', "routeName === 'admin-admin-detail'")
  assert.match(tpl, /停用：保留管理员身份/)
  assert.match(tpl, /撤销：收回管理员权限/)
})

test('已撤销用户显示复聘语义（复用原账号、签发新临时密码）', () => {
  const tpl = routeTemplate('admin-users', "routeName === 'admin-user-detail'")
  assert.match(tpl, /管理员（已撤销）/)
  assert.match(tpl, /@click="openAppoint\(user, true\)"/)
  // 复聘弹窗说明复用原账号、不新建。
  const modal = appSource.slice(appSource.indexOf('aria-label="设为管理员"'), appSource.indexOf('aria-label="一次性临时密码"'))
  assert.match(modal, /复用其原管理员账号[\s\S]*?不会新建账号/)
})

// ===== 16 布局 =====

test('新增样式支持桌面与移动端布局', () => {
  assert.match(styleSource, /\.first-pwd-overlay\s*\{/)
  assert.match(styleSource, /\.temp-password-box\s*\{/)
  assert.match(styleSource, /\.role-badge\s*\{/)
  // 移动端媒体查询下改密操作纵向排布。
  assert.match(styleSource, /@media \(max-width: 560px\)[\s\S]*?\.first-pwd-dialog__actions \{ flex-direction: column-reverse; \}/)
  // 管理员表格沿用可横向滚动的容器，不截断按钮。
  const tpl = routeTemplate('admin-admins', "routeName === 'admin-admin-detail'")
  assert.match(tpl, /class="table-scroll history-table admin-manage-table"/)
})

// ===== 审计标签 =====

// ===== 门禁 B 回归：表格列对齐 与 任命/reauth/临时密码弹窗层级 =====

test('回归·对齐：管理员身份/操作列不再把 td 变成 flex，改用行内 .cell-actions', () => {
  // td 保持普通表格单元格（享受全局 vertical-align:middle 与统一行高）。
  assert.doesNotMatch(appSource, /class="admin-role-cell"/)
  assert.doesNotMatch(appSource, /class="admin-actions-cell"/)
  assert.doesNotMatch(styleSource, /\.admin-role-cell|\.admin-actions-cell/)
  // 徽标 + 按钮包在一个行内 flex 容器里，且不换行。
  const cellActions = styleSource.slice(styleSource.indexOf('.cell-actions {'), styleSource.indexOf('.cell-actions {') + 260)
  assert.match(cellActions, /display: inline-flex/)
  assert.match(cellActions, /align-items: center/)
  assert.match(cellActions, /white-space: nowrap/)
  // 用户表与管理员表的操作内容都改用 .cell-actions 包裹。
  assert.ok((appSource.match(/class="cell-actions"/g) || []).length >= 2, '两张表都应使用 .cell-actions')
})

test('回归·行高一致：四种状态（未任命/active/disabled/revoked）走统一单元格结构', () => {
  const tpl = routeTemplate('admin-users', "routeName === 'admin-user-detail'")
  // 所有状态分支都在同一个 <td><span class="cell-actions"> 容器内，靠模板分支切换
  // 内容而不是切换容器结构，因此行高不随状态变化。
  const cell = tpl.slice(tpl.indexOf('v-if="isOwner"'), tpl.indexOf('<td><button class="secondary-button" type="button" @click="navigate(\'/admin/users/'))
  assert.match(cell, /<span class="cell-actions">/)
  for (const marker of ["user.id === ownerLinkedUserId", "=== 'none'", "=== 'revoked'", 'v-else']) {
    assert.ok(cell.includes(marker), `缺少状态分支 ${marker}`)
  }
  // 单元格内只有一个 cell-actions 容器（不是每个状态各建一个不同容器）。
  assert.equal((cell.match(/class="cell-actions"/g) || []).length, 1)
})

test('回归·窄屏不断行：管理员表设最小宽度，用户名/显示名单行省略', () => {
  assert.match(styleSource, /\.admin-manage-table table \{ min-width: \d+px; \}/)
  const nameCell = styleSource.slice(styleSource.indexOf('.admin-name-cell {'), styleSource.indexOf('.admin-name-cell {') + 200)
  assert.match(nameCell, /white-space: nowrap/)
  assert.match(nameCell, /text-overflow: ellipsis/)
  // 管理员表用户名/显示名单元格改用 .admin-name-cell，不再用会换行的 .cell-wrap。
  const tpl = routeTemplate('admin-admins', "routeName === 'admin-admin-detail'")
  assert.match(tpl, /<td class="admin-name-cell"><strong>\{\{ entry\.username \}\}<\/strong><\/td>/)
  assert.doesNotMatch(tpl, /class="cell-wrap"/)
})

test('回归·弹窗层级：reauth z-index 高于任命/临时密码弹窗，永不被遮挡', () => {
  // 任命与临时密码弹窗用 .app-modal-overlay（较低层级），reauth 用 .reauth-overlay（较高）。
  const appModal = styleSource.slice(styleSource.indexOf('.app-modal-overlay {'), styleSource.indexOf('.app-modal-overlay {') + 200)
  const reauth = styleSource.slice(styleSource.indexOf('.reauth-overlay {'), styleSource.indexOf('.reauth-overlay {') + 260)
  const appZ = Number(appModal.match(/z-index: (\d+)/)[1])
  const reauthZ = Number(reauth.match(/z-index: (\d+)/)[1])
  assert.ok(reauthZ > appZ, `reauth z-index(${reauthZ}) 必须高于 app 弹窗(${appZ})`)
  // 强制改密门禁是最顶层。
  const firstPwd = styleSource.slice(styleSource.indexOf('.first-pwd-overlay {'), styleSource.indexOf('.first-pwd-overlay {') + 200)
  const gateZ = Number(firstPwd.match(/z-index: (\d+)/)[1])
  assert.ok(gateZ > reauthZ, '强制改密门禁应在最顶层')
})

test('回归·任命流程：reauth 打开时任命弹窗隐藏，reauth 成功自动重试、成功后只留临时密码弹窗', () => {
  // 任命弹窗在 reauth 可见时隐藏（!reauthVisible），让 reauth 独占前台。
  assert.match(appSource, /v-if="appointVisible && appointUser && !reauthVisible" class="app-modal-overlay"/)
  // 自动重试由 api/client 的 execute() 承担：reauth 成功后重跑原请求一次。
  assert.match(clientSource, /if \(confirmed\) \{[\s\S]*?return parseResponse<T>\(await run\(\)\)/)
  // 任命成功后：关闭任命弹窗并展示一次临时密码。
  const fn = appSource.slice(appSource.indexOf('async function submitAppoint'), appSource.indexOf('async function submitAppoint') + 900)
  assert.match(fn, /appointVisible\.value = false/)
  assert.match(fn, /showTempPassword\(result\)/)
})

test('回归·reauth 取消：任命弹窗带原用户名重现，且不产生任命写入', () => {
  const start = appSource.indexOf('async function submitAppoint')
  const fn = appSource.slice(start, appSource.indexOf('function showTempPassword'))
  // 失败（含 reauth 取消抛出的 403）只设置提示并复位 busy；不清空 appointUsername，
  // 不置 appointVisible=false —— 因此 reauth 关闭后弹窗带原用户名重现。
  const catchBlock = fn.slice(fn.indexOf('} catch (error) {'))
  assert.match(catchBlock, /appointMessage\.value = friendlyAdminError/)
  assert.doesNotMatch(catchBlock, /appointUsername\.value = ''/)
  assert.doesNotMatch(catchBlock, /appointVisible\.value = false/)
  // 写入只发生在成功路径（try 内），reauth 取消时后端返回 403，不写库。
})

test('回归·防重复提交：任命进行中禁用确认按钮', () => {
  const modal = appSource.slice(appSource.indexOf('aria-label="设为管理员"'), appSource.indexOf('aria-label="一次性临时密码"'))
  assert.match(modal, /:disabled="appointBusy"/)
  // submitAppoint 开头即拦截重入。
  assert.match(appSource, /async function submitAppoint\(\) \{\s*\n\s*if \(appointBusy\.value/)
})

test('回归·不提前调用 owner-only 接口：非苏归不触发管理员数据加载', () => {
  // 路由分发处以 isOwner 为前置条件。
  assert.match(appSource, /routeName\.value === 'admin-admins' && isOwner\.value\) await loadOwnerAdmins/)
  assert.match(appSource, /routeName\.value === 'admin-admin-detail' && isOwner\.value/)
  // 两个加载函数自身也各带 isOwner 守卫。
  assert.match(appSource, /async function loadOwnerAdmins\(\) \{\s*\n\s*if \(!isOwner\.value\) return/)
  assert.match(appSource, /async function loadOwnerAdminDetail\(id: string\) \{[\s\S]*?if \(!isOwner\.value\) return/)
})

test('新增管理审计事件类型有中文标签，不展示技术值', () => {
  for (const [type, label] of [
    ['admin_appointed', '任命管理员'],
    ['admin_revoked', '撤销管理员'],
    ['admin_enabled', '启用管理员'],
    ['admin_disabled', '停用管理员'],
    ['admin_password_reset_by_owner', '重置管理员密码'],
  ]) {
    assert.ok(appSource.includes(`${type}: '${label}'`), `缺少审计标签 ${type}`)
  }
})
