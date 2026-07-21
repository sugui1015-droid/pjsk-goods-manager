import assert from 'node:assert/strict'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import test from 'node:test'
import { fileURLToPath } from 'node:url'

// 阶段 2H-R3 / P1 修复的前端源码断言：
//  1) 身份区显示角色身份（苏归/管理员）而非登录用户名；
//  2) 统一 v-synced-scroll 顶部+底部双同步滚动条；
//  3) reauth 前置：点击后本地立即打开弹窗，成功只执行一次，取消不写入，
//     保留服务端 403 兜底；页面接口安全并行。

const frontendRoot = resolve(dirname(fileURLToPath(import.meta.url)), '..')
const read = (p) => readFileSync(resolve(frontendRoot, p), 'utf8')
const appSource = read('src/App.vue')
const clientSource = read('src/api/client.ts')
const styleSource = read('src/style.css')
const directiveSource = read('src/directives/syncedScroll.ts')

// ===== 身份显示 =====

test('身份区 computed 用 roleDisplayName(role)，不再绑定 display_name ?? username', () => {
  // 定义处：身份来自角色，附带账号名。
  const c = appSource.slice(appSource.indexOf('const adminIdentityLabel'), appSource.indexOf('const adminIdentityLabel') + 300)
  assert.match(c, /roleDisplayName\(account\.role\)/)
  assert.match(c, /账号：\$\{account\.username\}/)
  assert.match(c, /身份：/)
  // 绑定处：两个管理面板都用 adminIdentityLabel。
  assert.equal((appSource.match(/:identity="adminIdentityLabel"/g) || []).length, 2)
  // 不再有旧的 display_name ?? username 身份绑定。
  assert.doesNotMatch(appSource, /:identity="admin\.display_name \?\? admin\.username"/)
  assert.doesNotMatch(appSource, /:identity="admin \? \(admin\.display_name \?\? admin\.username\)/)
})

test('owner（display_name=NULL）仍显示苏归：身份只依赖 role', () => {
  // adminIdentityLabel 的角色部分完全来自 roleDisplayName(role)，与 display_name 无关，
  // 因此 display_name 为 NULL 时 owner 依旧 → 苏归、admin → 管理员。
  const c = appSource.slice(appSource.indexOf('const adminIdentityLabel'), appSource.indexOf('const adminIdentityLabel') + 300)
  assert.doesNotMatch(c, /display_name/)
  // roleDisplayName 本身：owner→苏归、admin→管理员（唯一映射，client.ts）。
  const fn = clientSource.slice(clientSource.indexOf('export function roleDisplayName'), clientSource.indexOf('export function roleDisplayName') + 400)
  assert.match(fn, /case 'owner':\s*return '苏归'/)
  assert.match(fn, /case 'admin':\s*return '管理员'/)
})

test('账号名与角色身份分离展示，不显示裸 owner', () => {
  const c = appSource.slice(appSource.indexOf('const adminIdentityLabel'), appSource.indexOf('const adminIdentityLabel') + 300)
  assert.match(c, /身份：.*账号：/s)
  // 用户端 CN 身份未被破坏。
  assert.match(appSource, /identity="queryUser \? \('CN：' \+ queryUser\.cn_code\)/)
})

// ===== 统一滚动组件 =====

test('存在统一的 v-synced-scroll 指令（单一实现，非逐页复制）', () => {
  assert.match(appSource, /import \{ vSyncedScroll \} from '\.\/directives\/syncedScroll'/)
  // 双向同步：顶部与容器互相设置 scrollLeft。
  assert.match(directiveSource, /onTopScroll[\s\S]*?container\.scrollLeft = topBar\.scrollLeft/)
  assert.match(directiveSource, /onContainerScroll[\s\S]*?topBar\.scrollLeft = container\.scrollLeft/)
  // compare-before-set 防循环/抖动。
  assert.match(directiveSource, /if \(container\.scrollLeft !== topBar\.scrollLeft\)/)
  assert.match(directiveSource, /if \(topBar\.scrollLeft !== container\.scrollLeft\)/)
})

test('无横向溢出时顶部滚动条隐藏', () => {
  assert.match(directiveSource, /const overflow = container\.scrollWidth - container\.clientWidth/)
  assert.match(directiveSource, /state\.topBar\.style\.display = overflow > 1 \? 'block' : 'none'/)
})

test('用 ResizeObserver 在尺寸变化时同步轨道宽度', () => {
  assert.match(directiveSource, /new ResizeObserver\(\(\) => refresh\(container, state\)\)/)
  assert.match(directiveSource, /state\.observer\.observe\(container\)/)
  assert.match(directiveSource, /if \(table\) state\.observer\.observe\(table\)/)
  // 轨道宽度跟随表格 scrollWidth。
  assert.match(directiveSource, /state\.spacer\.style\.width = `\$\{container\.scrollWidth\}px`/)
})

test('卸载时清理监听器与注入的轨道（无泄漏）', () => {
  const un = directiveSource.slice(directiveSource.indexOf('unmounted('))
  assert.match(un, /state\.observer\.disconnect\(\)/)
  assert.match(un, /removeEventListener\('scroll', state\.onTopScroll\)/)
  assert.match(un, /removeEventListener\('scroll', state\.onContainerScroll\)/)
  assert.match(un, /state\.topBar\.remove\(\)/)
})

test('所有目标宽表复用统一组件：每个 .table-scroll 都挂 v-synced-scroll', () => {
  const withDirective = (appSource.match(/<div v-synced-scroll class="table-scroll/g) || []).length
  const without = (appSource.match(/<div class="table-scroll/g) || []).length
  assert.ok(withDirective >= 18, `期望多数宽表接入，实际 ${withDirective}`)
  assert.equal(without, 0, '不应残留未接入统一滚动的 .table-scroll 容器')
})

test('顶部滚动条样式存在', () => {
  assert.match(styleSource, /\.table-scroll-topbar \{/)
  assert.match(styleSource, /\.table-scroll-topbar__spacer \{/)
  assert.match(styleSource, /overflow-x: auto/)
})

// ===== reauth 前置流程 =====

test('reauth 前置：ensureReauth 本地即时打开弹窗，不依赖网络响应', () => {
  const fn = appSource.slice(appSource.indexOf('function ensureReauth'), appSource.indexOf('function ensureReauth') + 400)
  // 新鲜窗口内直接放行；否则本地 openReauthDialog（同步置 reauthVisible=true）。
  assert.match(fn, /Date\.now\(\) - lastReauthAt\.value < CLIENT_REAUTH_WINDOW_MS/)
  assert.match(fn, /return openReauthDialog\(\)/)
  // openReauthDialog 立即置 reauthVisible=true（纯本地状态）。
  const open = appSource.slice(appSource.indexOf('function openReauthDialog'), appSource.indexOf('function openReauthDialog') + 250)
  assert.match(open, /reauthVisible\.value = true/)
  // 成功 reauth 记录时间戳。
  assert.match(appSource, /lastReauthAt\.value = Date\.now\(\)/)
})

test('任命/启停/撤销/重置：写操作前先 ensureReauth，取消则不写入', () => {
  const appoint = appSource.slice(appSource.indexOf('async function submitAppoint'), appSource.indexOf('function showTempPassword'))
  // ensureReauth 在 appointOwnerAdmin 之前，且取消（false）时 return，不发写请求。
  assert.ok(appoint.indexOf('if (!(await ensureReauth())) return') < appoint.indexOf('await appointOwnerAdmin'), 'ensureReauth 必须在任命写请求之前')
  // 管理操作统一入口 runAdminAction 与 resetAdminPassword 同样前置。
  const run = appSource.slice(appSource.indexOf('async function runAdminAction'), appSource.indexOf('async function runAdminAction') + 500)
  assert.ok(run.indexOf('if (!(await ensureReauth())) return') < run.indexOf('await action()'), 'runAdminAction 必须先 ensureReauth')
  const reset = appSource.slice(appSource.indexOf('async function resetAdminPassword'), appSource.indexOf('async function runAdminAction'))
  assert.ok(reset.indexOf('if (!(await ensureReauth())) return') < reset.indexOf('await resetOwnerAdminPassword'), 'resetAdminPassword 必须先 ensureReauth')
})

test('reauth 成功后原操作只执行一次；取消保留输入不写入', () => {
  const appoint = appSource.slice(appSource.indexOf('async function submitAppoint'), appSource.indexOf('function showTempPassword'))
  // 只有一次 appointOwnerAdmin 调用。
  assert.equal((appoint.match(/await appointOwnerAdmin\(/g) || []).length, 1)
  // 失败/取消分支不清空 appointUsername、不置 appointVisible=false → 弹窗带原用户名重现。
  const catchBlock = appoint.slice(appoint.indexOf('} catch (error) {'))
  assert.doesNotMatch(catchBlock, /appointUsername\.value = ''/)
  assert.doesNotMatch(catchBlock, /appointVisible\.value = false/)
})

test('保留服务端 403 reauth_required 兜底（execute 拦截重试一次）', () => {
  assert.match(clientSource, /error\.status === 403 && error\.message === REAUTH_REQUIRED && reauthHandler/)
  assert.match(clientSource, /const confirmed = await reauthHandler\(\)/)
  assert.match(clientSource, /if \(confirmed\) \{\s*return parseResponse<T>\(await run\(\)\)/)
})

test('降低体感延迟：用户页 users 与 owner-admins 并行加载', () => {
  const fn = appSource.slice(appSource.indexOf('async function loadAdminUsers'), appSource.indexOf('async function loadAdminUsers') + 900)
  assert.match(fn, /const usersPromise = getJSON/)
  assert.match(fn, /const ownerAdminsPromise = isOwner\.value \? loadOwnerAdmins\(\) : Promise\.resolve\(\)/)
  assert.match(fn, /await Promise\.all\(\[usersPromise, ownerAdminsPromise\]\)/)
})

test('普通管理员不请求 owner-only 接口（并行分支与守卫都以 isOwner 为条件）', () => {
  // 并行分支：非 owner 用 Promise.resolve()，不发 owner-only 请求。
  assert.match(appSource, /isOwner\.value \? loadOwnerAdmins\(\) : Promise\.resolve\(\)/)
  // 两个加载函数自带 isOwner 守卫。
  assert.match(appSource, /async function loadOwnerAdmins\(\) \{\s*\n\s*if \(!isOwner\.value\) return/)
  assert.match(appSource, /async function loadOwnerAdminDetail\(id: string\) \{[\s\S]*?if \(!isOwner\.value\) return/)
  // 路由分发也以 isOwner 为前置。
  assert.match(appSource, /routeName\.value === 'admin-admins' && isOwner\.value\) await loadOwnerAdmins/)
})
