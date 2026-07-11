<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  ApiError,
  getJSON,
  postForm,
  postJSON,
  type Admin,
  type AuthResponse,
  type ConfigResponse,
  type HealthResponse,
  type ImportBatch,
  type ImportConfirmResponse,
  type ImportDetailResponse,
  type ImportHistoryItem,
  type ImportHistoryResponse,
  type ImportIssue,
  type ImportPreviewResponse,
} from './api/client'

const maxExcelSize = 20 * 1024 * 1024

type RouteName = 'home' | 'admin-imports' | 'admin-import-history' | 'admin-import-detail'
type IssueFilter = 'all' | 'error' | 'warning' | 'notice'

const fallbackConfig: ConfigResponse = {
  name: 'PJSK Goods Next',
  stage: 'local-shell',
  legacyAdminPort: '8512',
  legacyUserPort: '8513',
  frontendOrigins: ['http://localhost:5173', 'http://127.0.0.1:5173'],
  modules: [
    { key: 'frontend-shell', title: '前端工作台', status: 'ready', description: 'Vue 管理端已启动。' },
    { key: 'backend-core', title: 'Go 后端', status: 'queued', description: '等待 /health 与 /api/config。' },
    { key: 'excel-import', title: 'Excel 导入', status: 'queued', description: '管理员可预览、确认并查看历史。' },
  ],
}

const health = ref<HealthResponse | null>(null)
const config = ref<ConfigResponse>(fallbackConfig)
const errorMessage = ref('')
const loading = ref(true)
const checkedAt = ref('')
const activeView = ref<'overview' | 'ops' | 'legacy'>('overview')
const routeName = ref<RouteName>(routeFromPath(window.location.pathname))
const routeImportID = ref(importIDFromPath(window.location.pathname))

const admin = ref<Admin | null>(null)
const authChecked = ref(false)
const authMessage = ref('')
const loginUsername = ref('admin')
const loginPassword = ref('')
const loginLoading = ref(false)

const selectedFile = ref<File | null>(null)
const uploadLoading = ref(false)
const uploadMessage = ref('')
const preview = ref<ImportPreviewResponse | null>(null)
const confirmResult = ref<ImportConfirmResponse | null>(null)
const confirmLoading = ref(false)
const confirmMessage = ref('')
const allowWarnings = ref(false)
const expandedBatchIds = ref<Set<string>>(new Set())
const issueFilter = ref<IssueFilter>('all')

const historyLoading = ref(false)
const historyMessage = ref('')
const importHistory = ref<ImportHistoryItem[]>([])
const detailLoading = ref(false)
const detailMessage = ref('')
const importDetail = ref<ImportDetailResponse | null>(null)

const isBackendOnline = computed(() => health.value?.status === 'ok')
const readyCount = computed(() => config.value.modules.filter((item) => item.status === 'ready').length)
const queuedCount = computed(() => config.value.modules.filter((item) => item.status === 'queued').length)
const isAdminRoute = computed(() => routeName.value !== 'home')
const canUpload = computed(() => selectedFile.value !== null && !uploadLoading.value)
const canConfirm = computed(() => {
  if (!preview.value?.import_batch_id || confirmLoading.value || confirmResult.value) return false
  if ((preview.value.errors?.length ?? 0) > 0) return false
  if ((preview.value.warnings?.length ?? 0) > 0 && !allowWarnings.value) return false
  return true
})

const templateCounts = computed(() => countTemplates(preview.value?.batches ?? []))
const detailTemplateCounts = computed(() => countTemplates(importDetail.value?.preview?.batches ?? []))
const allIssues = computed(() => [
  ...(preview.value?.errors ?? []),
  ...(preview.value?.warnings ?? []),
  ...(preview.value?.notices ?? []),
])
const filteredIssues = computed(() => issueFilter.value === 'all' ? allIssues.value : allIssues.value.filter((item) => item.level === issueFilter.value))

function routeFromPath(path: string): RouteName {
  if (path === '/admin/imports/history') return 'admin-import-history'
  if (path.startsWith('/admin/imports/') && path !== '/admin/imports/preview') return 'admin-import-detail'
  if (path === '/admin/imports') return 'admin-imports'
  return 'home'
}

function importIDFromPath(path: string) {
  if (!path.startsWith('/admin/imports/')) return ''
  const id = decodeURIComponent(path.replace('/admin/imports/', '').replace(/\/$/, ''))
  return id === 'history' ? '' : id
}

function navigate(path: string) {
  window.history.pushState(null, '', path)
  routeName.value = routeFromPath(path)
  routeImportID.value = importIDFromPath(path)
  void handleRouteEntered()
}

async function handleRouteEntered() {
  if (!isAdminRoute.value) return
  await ensureAdmin()
  if (!admin.value) return
  if (routeName.value === 'admin-import-history') await loadHistory()
  if (routeName.value === 'admin-import-detail' && routeImportID.value) await loadDetail(routeImportID.value)
}

async function load() {
  loading.value = true
  errorMessage.value = ''
  try {
    const [healthResponse, configResponse] = await Promise.all([
      getJSON<HealthResponse>('/health'),
      getJSON<ConfigResponse>('/api/config'),
    ])
    health.value = healthResponse
    config.value = configResponse
  } catch (error) {
    health.value = null
    config.value = fallbackConfig
    errorMessage.value = error instanceof Error ? error.message : 'Backend unreachable'
  } finally {
    checkedAt.value = new Date().toLocaleString('zh-CN', { hour12: false })
    loading.value = false
  }
}

async function ensureAdmin() {
  authMessage.value = ''
  try {
    const response = await getJSON<AuthResponse>('/api/admin/me')
    admin.value = response.admin
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '请先登录管理员账号。'
    } else {
      authMessage.value = error instanceof Error ? error.message : '管理员状态检查失败'
    }
  } finally {
    authChecked.value = true
  }
}

async function login() {
  loginLoading.value = true
  authMessage.value = ''
  try {
    const response = await postJSON<AuthResponse>('/api/admin/login', {
      username: loginUsername.value,
      password: loginPassword.value,
    })
    admin.value = response.admin
    loginPassword.value = ''
    await handleRouteEntered()
  } catch (error) {
    authMessage.value = error instanceof Error ? error.message : '登录失败'
  } finally {
    loginLoading.value = false
  }
}

async function logout() {
  try {
    await postJSON<void>('/api/admin/logout', {})
  } catch (error) {
    if (!(error instanceof ApiError && error.status === 401)) {
      authMessage.value = error instanceof Error ? error.message : '退出失败'
      return
    }
  }
  admin.value = null
  preview.value = null
  confirmResult.value = null
  importHistory.value = []
  importDetail.value = null
}

function onFileChange(event: Event) {
  const input = event.target as HTMLInputElement
  const file = input.files?.[0] ?? null
  uploadMessage.value = ''
  preview.value = null
  confirmResult.value = null
  confirmMessage.value = ''
  allowWarnings.value = false

  if (!file) {
    selectedFile.value = null
    return
  }
  if (!file.name.toLowerCase().endsWith('.xlsx')) {
    selectedFile.value = null
    input.value = ''
    uploadMessage.value = '请选择 .xlsx 文件。'
    return
  }
  if (file.size > maxExcelSize) {
    selectedFile.value = null
    input.value = ''
    uploadMessage.value = '文件不能超过 20MB。'
    return
  }
  selectedFile.value = file
}

async function uploadPreview() {
  if (!selectedFile.value || uploadLoading.value) return
  uploadLoading.value = true
  uploadMessage.value = ''
  const form = new FormData()
  form.append('file', selectedFile.value)
  try {
    preview.value = await postForm<ImportPreviewResponse>('/api/admin/imports/preview', form)
    confirmResult.value = null
    confirmMessage.value = ''
    allowWarnings.value = false
    expandedBatchIds.value = new Set()
    issueFilter.value = 'all'
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    uploadMessage.value = error instanceof Error ? error.message : '上传失败'
  } finally {
    uploadLoading.value = false
  }
}

async function confirmImport() {
  if (!preview.value?.import_batch_id || confirmLoading.value) return
  confirmLoading.value = true
  confirmMessage.value = ''
  try {
    confirmResult.value = await postJSON<ImportConfirmResponse>('/api/admin/imports/confirm', {
      import_batch_id: preview.value.import_batch_id,
      allow_warnings: allowWarnings.value,
    })
    await loadHistory()
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    confirmMessage.value = error instanceof Error ? error.message : '确认导入失败'
  } finally {
    confirmLoading.value = false
  }
}

async function loadHistory() {
  historyLoading.value = true
  historyMessage.value = ''
  try {
    const response = await getJSON<ImportHistoryResponse>('/api/admin/imports')
    importHistory.value = response.items ?? []
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    historyMessage.value = error instanceof Error ? error.message : '导入历史加载失败'
  } finally {
    historyLoading.value = false
  }
}

async function loadDetail(id: string) {
  detailLoading.value = true
  detailMessage.value = ''
  importDetail.value = null
  try {
    importDetail.value = await getJSON<ImportDetailResponse>(`/api/admin/imports/${encodeURIComponent(id)}`)
  } catch (error) {
    if (error instanceof ApiError && error.status === 401) {
      admin.value = null
      authMessage.value = '登录已过期，请重新登录。'
      return
    }
    detailMessage.value = error instanceof Error ? error.message : '导入详情加载失败'
  } finally {
    detailLoading.value = false
  }
}

function toggleBatch(batchId: string) {
  const next = new Set(expandedBatchIds.value)
  if (next.has(batchId)) next.delete(batchId)
  else next.add(batchId)
  expandedBatchIds.value = next
}

function isExpanded(batchId: string) {
  return expandedBatchIds.value.has(batchId)
}

function formatMoney(value: number | null | undefined) {
  return Number(value ?? 0).toFixed(2)
}

function formatBytes(value: number) {
  if (value >= 1024 * 1024) return `${(value / 1024 / 1024).toFixed(2)} MB`
  if (value >= 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${value} B`
}

function formatDate(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('zh-CN', { hour12: false })
}

function countTemplates(batches: ImportBatch[]) {
  const counts = new Map<string, number>()
  for (const batch of batches) counts.set(batch.template_type, (counts.get(batch.template_type) ?? 0) + 1)
  return Array.from(counts.entries()).map(([name, count]) => ({ name, count }))
}

function issueContext(issue: ImportIssue) {
  const parts = [
    issue.sheet_name ? `工作表 ${issue.sheet_name}` : '',
    issue.batch_id ? `批次 ${issue.batch_id}` : '',
    issue.row_number ? `第 ${issue.row_number} 行` : '',
    issue.column ? `列 ${issue.column}` : '',
  ].filter(Boolean)
  return parts.join(' / ') || '无位置上下文'
}

function priceTypeLabel(batch: ImportBatch) {
  return batch.calculation_price_type || (batch.price_types ?? []).map((item) => item.type).join(', ') || '无'
}

function historyTotalAmount(item: ImportHistoryItem) {
  return item.confirm_result?.total_amount ?? 0
}

window.addEventListener('popstate', () => {
  routeName.value = routeFromPath(window.location.pathname)
  routeImportID.value = importIDFromPath(window.location.pathname)
  void handleRouteEntered()
})

onMounted(() => {
  void load()
  if (isAdminRoute.value) void handleRouteEntered()
  else authChecked.value = true
})
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <div>
        <p class="product-label">PJSK Goods Next</p>
        <h1>谷子管理工作台</h1>
      </div>
      <div class="topbar__actions">
        <span v-if="admin" class="admin-chip">{{ admin.display_name ?? admin.username }}</span>
        <span class="connection-pill" :data-online="isBackendOnline">
          <span class="connection-dot" />
          {{ isBackendOnline ? '后端在线' : '本地前端模式' }}
        </span>
        <button class="icon-button" type="button" title="重新检查后端" @click="load" :disabled="loading">↻</button>
      </div>
    </header>

    <nav class="tabs" aria-label="工作台导航">
      <button :class="{ active: routeName === 'home' }" type="button" @click="navigate('/')">总览</button>
      <button :class="{ active: routeName === 'admin-imports' }" type="button" @click="navigate('/admin/imports')">Excel 导入预览</button>
      <button :class="{ active: routeName === 'admin-import-history' || routeName === 'admin-import-detail' }" type="button" @click="navigate('/admin/imports/history')">导入历史</button>
    </nav>

    <main v-if="isAdminRoute" class="workspace">
      <section v-if="!authChecked" class="panel">
        <div class="panel__header"><h2>管理员状态</h2><span>checking</span></div>
        <p class="muted">正在检查登录状态。</p>
      </section>

      <section v-else-if="!admin" class="panel auth-panel">
        <div class="panel__header"><h2>管理员登录</h2><span>HttpOnly Cookie</span></div>
        <form class="login-form" @submit.prevent="login">
          <label><span>用户名</span><input v-model="loginUsername" autocomplete="username" required /></label>
          <label><span>密码</span><input v-model="loginPassword" type="password" autocomplete="current-password" required /></label>
          <button class="primary-button" type="submit" :disabled="loginLoading">{{ loginLoading ? '登录中' : '登录' }}</button>
        </form>
        <div v-if="authMessage" class="inline-alert">{{ authMessage }}</div>
      </section>

      <template v-else>
        <section class="panel admin-actions">
          <div class="panel__header">
            <div>
              <h2>管理员导入中心</h2>
              <p class="muted">当前只提供 Excel 预览、确认导入、历史与详情查询。</p>
            </div>
            <button class="secondary-button" type="button" @click="logout">退出</button>
          </div>
          <div class="action-row">
            <button class="secondary-button" type="button" @click="navigate('/admin/imports')">导入预览</button>
            <button class="secondary-button" type="button" @click="navigate('/admin/imports/history')">导入历史</button>
          </div>
        </section>

        <template v-if="routeName === 'admin-imports'">
          <section class="panel upload-panel">
            <div class="panel__header">
              <div>
                <h2>Excel 导入预览</h2>
                <p class="muted">仅解析预览；确认前不会写入正式订单或付款数据。</p>
              </div>
            </div>
            <div class="upload-row">
              <label class="file-picker">
                <span>选择 .xlsx 文件</span>
                <input type="file" accept=".xlsx" :disabled="uploadLoading" @change="onFileChange" />
              </label>
              <button class="primary-button" type="button" :disabled="!canUpload" @click="uploadPreview">{{ uploadLoading ? '解析中' : '上传并预览' }}</button>
            </div>
            <p class="muted">文件大小限制 20MB；上传字段为 <code class="inline-code">file</code>。</p>
            <div v-if="selectedFile" class="file-line">{{ selectedFile.name }} / {{ formatBytes(selectedFile.size) }}</div>
            <div v-if="uploadMessage" class="inline-alert">{{ uploadMessage }}</div>
          </section>

          <section v-if="preview" class="summary-grid" aria-label="导入预览摘要">
            <article class="metric-tile wide-metric"><span>文件名</span><strong>{{ preview.file.original_filename }}</strong></article>
            <article class="metric-tile wide-metric"><span>SHA-256</span><strong>{{ preview.file.sha256 }}</strong></article>
            <article class="metric-tile"><span>工作表</span><strong>{{ preview.file.sheet_count }}</strong></article>
            <article class="metric-tile"><span>批次</span><strong>{{ preview.batches.length }}</strong></article>
            <article class="metric-tile"><span>Errors</span><strong>{{ preview.errors?.length ?? 0 }}</strong></article>
            <article class="metric-tile"><span>Warnings</span><strong>{{ preview.warnings?.length ?? 0 }}</strong></article>
            <article class="metric-tile"><span>Notices</span><strong>{{ preview.notices?.length ?? 0 }}</strong></article>
            <article class="metric-tile wide-metric"><span>模板类型</span><strong>{{ templateCounts.map((item) => `${item.name} ${item.count}`).join(' / ') }}</strong></article>
          </section>

          <section v-if="preview" class="panel confirm-panel">
            <div class="panel__header"><div><h2>确认导入</h2><p class="muted">确认时使用服务器保存的预览结果，不信任前端明细。</p></div><span>{{ preview.import_batch_id }}</span></div>
            <div v-if="(preview.errors?.length ?? 0) > 0" class="inline-alert">存在 error，当前预览禁止确认导入。</div>
            <label v-if="(preview.warnings?.length ?? 0) > 0" class="confirm-check"><input v-model="allowWarnings" type="checkbox" /><span>我已人工检查 warnings，允许继续确认导入。</span></label>
            <div class="confirm-actions">
              <button class="primary-button" type="button" :disabled="!canConfirm" @click="confirmImport">{{ confirmLoading ? '确认中' : '确认导入' }}</button>
              <span class="muted">不会写入 payments / payment_items。</span>
            </div>
            <div v-if="confirmMessage" class="inline-alert">{{ confirmMessage }}</div>
            <div v-if="confirmResult" class="confirm-result">
              <strong>导入已确认</strong>
              <span>CN {{ confirmResult.cn_count }}</span>
              <span>商品 {{ confirmResult.product_count }}</span>
              <span>订单 {{ confirmResult.order_count }}</span>
              <span>明细 {{ confirmResult.order_item_count }}</span>
              <span>总件数 {{ confirmResult.total_quantity }}</span>
              <span>总金额 {{ formatMoney(confirmResult.total_amount) }}</span>
              <span>接受 warnings {{ confirmResult.warnings_accepted ? '是' : '否' }}</span>
              <span>{{ formatDate(confirmResult.confirmed_at) }}</span>
            </div>
          </section>

          <section v-if="preview" class="panel">
            <div class="panel__header"><h2>批次列表</h2><span>{{ preview.batches.length }} batches</span></div>
            <div class="batch-list">
              <article v-for="batch in preview.batches" :key="batch.id" class="batch-card">
                <button class="batch-card__summary" type="button" @click="toggleBatch(batch.id)">
                  <span>{{ isExpanded(batch.id) ? '▾' : '▸' }}</span><strong>{{ batch.sheet_name }} / {{ batch.batch_name }}</strong><span class="status-chip" data-state="draft">{{ batch.template_type }}</span><span v-if="batch.template_type === 'simple_cn_amount'" class="simple-note">仅预览，不转换为订单项</span>
                </button>
                <div class="batch-metrics"><span>CN {{ batch.cn_count }}</span><span>种类 {{ batch.item_type_count }}</span><span>总件数 {{ batch.total_quantity }}</span><span>表格 {{ formatMoney(batch.table_amount) }}</span><span>程序 {{ formatMoney(batch.calculated_amount) }}</span><span :class="{ danger: Math.abs(batch.difference) > 0.01 }">差额 {{ formatMoney(batch.difference) }}</span><span>价格 {{ priceTypeLabel(batch) }}</span></div>
                <div v-if="isExpanded(batch.id)" class="batch-detail">
                  <div class="table-scroll detail-table"><table><thead><tr><th>原始 CN</th><th>规范 CN</th><th>种类</th><th>分类</th><th>数量</th><th>价格</th><th>小计</th><th>来源</th></tr></thead><tbody><tr v-if="!(batch.details?.length)"><td colspan="8">无订单项明细。</td></tr><tr v-for="detail in batch.details ?? []" :key="`${batch.id}-${detail.row_number}-${detail.column_name}-${detail.original_cn}`"><td>{{ detail.original_cn }}</td><td>{{ detail.normalized_cn }}</td><td>{{ detail.item_name }}</td><td>{{ detail.category || '-' }}</td><td>{{ detail.quantity }}</td><td>{{ formatMoney(detail.unit_price) }}</td><td>{{ formatMoney(detail.amount) }}</td><td>{{ detail.sheet_name }}!{{ detail.column_name }}{{ detail.row_number }}</td></tr></tbody></table></div>
                </div>
              </article>
            </div>
          </section>

          <section v-if="preview" class="panel">
            <div class="panel__header"><h2>问题列表</h2><div class="filter-buttons"><button :class="{ active: issueFilter === 'all' }" type="button" @click="issueFilter = 'all'">全部</button><button :class="{ active: issueFilter === 'error' }" type="button" @click="issueFilter = 'error'">error</button><button :class="{ active: issueFilter === 'warning' }" type="button" @click="issueFilter = 'warning'">warning</button><button :class="{ active: issueFilter === 'notice' }" type="button" @click="issueFilter = 'notice'">notice</button></div></div>
            <div class="issue-list"><article v-if="filteredIssues.length === 0" class="issue-row">当前筛选下没有问题。</article><article v-for="issue in filteredIssues" :key="`${issue.level}-${issue.code}-${issue.sheet_name}-${issue.batch_id}-${issue.row_number}-${issue.column}`" class="issue-row" :data-level="issue.level"><strong>{{ issue.level }} / {{ issue.code }}</strong><span>{{ issue.message }}</span><small>{{ issue.code === 'image_formula_ignored' ? '图片公式已忽略 / ' : '' }}{{ issueContext(issue) }}</small></article></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-import-history'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入历史</h2><p class="muted">只读查看，不提供撤销或修改订单。</p></div><button class="secondary-button" type="button" :disabled="historyLoading" @click="loadHistory">刷新</button></div>
            <div v-if="historyMessage" class="inline-alert">{{ historyMessage }}</div>
            <div class="table-scroll history-table"><table><thead><tr><th>文件</th><th>SHA-256</th><th>状态</th><th>上传</th><th>确认</th><th>工作表/批次</th><th>问题</th><th>写入结果</th><th>总金额</th><th></th></tr></thead><tbody><tr v-if="!historyLoading && importHistory.length === 0"><td colspan="10">暂无导入记录。</td></tr><tr v-for="item in importHistory" :key="item.id"><td><strong>{{ item.original_filename }}</strong><small>{{ formatBytes(item.file_size) }}</small></td><td class="hash-cell">{{ item.file_hash }}</td><td><span class="status-chip" data-state="draft">{{ item.status }}</span></td><td>{{ item.uploaded_by || '-' }}<small>{{ formatDate(item.created_at) }}</small></td><td>{{ item.confirmed_by || '-' }}<small>{{ formatDate(item.confirmed_at) }}</small></td><td>{{ item.sheet_count }} / {{ item.batch_count }}</td><td>E {{ item.error_count }} / W {{ item.warning_count }} / N {{ item.notice_count }}</td><td>{{ item.confirm_result ? `${item.confirm_result.order_count} 单 / ${item.confirm_result.order_item_count} 明细` : '-' }}</td><td>{{ formatMoney(historyTotalAmount(item)) }}</td><td><button class="secondary-button" type="button" @click="navigate(`/admin/imports/${item.id}`)">详情</button></td></tr></tbody></table></div>
          </section>
        </template>

        <template v-else-if="routeName === 'admin-import-detail'">
          <section class="panel">
            <div class="panel__header"><div><h2>导入详情</h2><p class="muted">{{ routeImportID }}</p></div><button class="secondary-button" type="button" @click="navigate('/admin/imports/history')">返回历史</button></div>
            <div v-if="detailMessage" class="inline-alert">{{ detailMessage }}</div>
            <p v-if="detailLoading" class="muted">正在加载详情。</p>
            <template v-if="importDetail">
              <div class="summary-grid">
                <article class="metric-tile wide-metric"><span>文件名</span><strong>{{ importDetail.import.original_filename }}</strong></article>
                <article class="metric-tile wide-metric"><span>SHA-256</span><strong>{{ importDetail.import.file_hash }}</strong></article>
                <article class="metric-tile"><span>状态</span><strong>{{ importDetail.import.status }}</strong></article>
                <article class="metric-tile"><span>工作表</span><strong>{{ importDetail.import.sheet_count }}</strong></article>
                <article class="metric-tile"><span>批次</span><strong>{{ importDetail.import.batch_count }}</strong></article>
                <article class="metric-tile"><span>问题</span><strong>E {{ importDetail.import.error_count }} / W {{ importDetail.import.warning_count }} / N {{ importDetail.import.notice_count }}</strong></article>
                <article class="metric-tile"><span>接受 warnings</span><strong>{{ importDetail.import.warnings_accepted ? '是' : '否' }}</strong></article>
                <article class="metric-tile"><span>确认管理员</span><strong>{{ importDetail.import.confirmed_by || '-' }}</strong></article>
                <article class="metric-tile"><span>确认时间</span><strong>{{ formatDate(importDetail.import.confirmed_at) }}</strong></article>
              </div>

              <section v-if="importDetail.import.confirm_result" class="confirm-result detail-result">
                <strong>写入结果</strong><span>CN {{ importDetail.import.confirm_result.cn_count }}</span><span>商品 {{ importDetail.import.confirm_result.product_count }}</span><span>订单 {{ importDetail.import.confirm_result.order_count }}</span><span>明细 {{ importDetail.import.confirm_result.order_item_count }}</span><span>总件数 {{ importDetail.import.confirm_result.total_quantity }}</span><span>总金额 {{ formatMoney(importDetail.import.confirm_result.total_amount) }}</span>
              </section>

              <section v-if="importDetail.preview" class="panel nested-panel">
                <div class="panel__header"><h2>解析摘要</h2><span>{{ detailTemplateCounts.map((item) => `${item.name} ${item.count}`).join(' / ') }}</span></div>
                <div class="table-scroll compact-table"><table><thead><tr><th>工作表</th><th>模板</th><th>批次</th><th>表格金额</th><th>程序金额</th><th>差额</th></tr></thead><tbody><tr v-for="sheet in importDetail.preview.sheets" :key="sheet.name"><td>{{ sheet.name }}</td><td>{{ sheet.template_type }}</td><td>{{ sheet.batch_count }}</td><td>{{ formatMoney(sheet.table_amount) }}</td><td>{{ formatMoney(sheet.calculated_amount) }}</td><td :class="{ danger: Math.abs(sheet.difference) > 0.01 }">{{ formatMoney(sheet.difference) }}</td></tr></tbody></table></div>
              </section>

              <section v-if="importDetail.preview" class="panel nested-panel">
                <div class="panel__header"><h2>问题</h2><span>{{ (importDetail.preview.errors?.length ?? 0) + (importDetail.preview.warnings?.length ?? 0) + (importDetail.preview.notices?.length ?? 0) }}</span></div>
                <div class="issue-list"><article v-for="issue in [...(importDetail.preview.errors ?? []), ...(importDetail.preview.warnings ?? []), ...(importDetail.preview.notices ?? [])]" :key="`${issue.level}-${issue.code}-${issue.sheet_name}-${issue.batch_id}-${issue.row_number}-${issue.column}`" class="issue-row" :data-level="issue.level"><strong>{{ issue.level }} / {{ issue.code }}</strong><span>{{ issue.message }}</span><small>{{ issueContext(issue) }}</small></article><article v-if="!((importDetail.preview.errors?.length ?? 0) + (importDetail.preview.warnings?.length ?? 0) + (importDetail.preview.notices?.length ?? 0))" class="issue-row">无问题。</article></div>
              </section>
            </template>
          </section>
        </template>
      </template>
    </main>

    <main v-else class="workspace">
      <section class="metrics" aria-label="运行指标">
        <article class="metric-tile"><span>可用模块</span><strong>{{ readyCount }}</strong></article>
        <article class="metric-tile"><span>待接入模块</span><strong>{{ queuedCount }}</strong></article>
        <article class="metric-tile"><span>后端服务</span><strong>{{ health?.service ?? '未连接' }}</strong></article>
        <article class="metric-tile"><span>检查时间</span><strong>{{ checkedAt || '待检查' }}</strong></article>
      </section>
      <nav class="subtabs" aria-label="首页信息"><button :class="{ active: activeView === 'overview' }" type="button" @click="activeView = 'overview'">总览</button><button :class="{ active: activeView === 'ops' }" type="button" @click="activeView = 'ops'">接口</button><button :class="{ active: activeView === 'legacy' }" type="button" @click="activeView = 'legacy'">旧版</button></nav>
      <section v-if="activeView === 'overview'" class="panel"><div class="panel__header"><h2>模块状态</h2><span>{{ config.stage }}</span></div><div v-if="errorMessage" class="inline-alert">{{ errorMessage }}</div><div class="module-table"><div class="module-row module-row--head"><span>模块</span><span>状态</span><span>说明</span></div><div v-for="item in config.modules" :key="item.key" class="module-row"><strong>{{ item.title }}</strong><span class="status-chip" :data-state="item.status">{{ item.status }}</span><span>{{ item.description }}</span></div></div></section>
      <section v-else-if="activeView === 'ops'" class="workspace workspace--two"><div class="panel"><div class="panel__header"><h2>后端接口</h2><span>{{ isBackendOnline ? 'online' : 'offline' }}</span></div><div class="endpoint-list"><div><code>GET /health</code><span>{{ health?.status ?? 'waiting' }}</span></div><div><code>GET /api/config</code><span>{{ isBackendOnline ? 'ready' : 'waiting' }}</span></div><div><code>POST /api/admin/imports/preview</code><span>admin only</span></div><div><code>POST /api/admin/imports/confirm</code><span>admin only</span></div><div><code>GET /api/admin/imports</code><span>admin only</span></div></div></div><div class="panel"><div class="panel__header"><h2>下一步</h2><span>history first</span></div><ol class="task-list"><li>验收确认导入幂等保护。</li><li>查看导入历史和详情。</li><li>再进入订单只读管理。</li></ol></div></section>
      <section v-else class="workspace workspace--two"><div class="panel"><div class="panel__header"><h2>Streamlit 管理端</h2><span>port {{ config.legacyAdminPort }}</span></div><code>cd legacy-streamlit && python -m streamlit run main.py --server.port {{ config.legacyAdminPort }}</code></div><div class="panel"><div class="panel__header"><h2>Streamlit 用户端</h2><span>port {{ config.legacyUserPort }}</span></div><code>cd legacy-streamlit && python -m streamlit run user.py --server.port {{ config.legacyUserPort }}</code></div></section>
    </main>
  </div>
</template>
