<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'

type HealthResponse = {
  service: string
  status: string
  time: string
}

type ModuleInfo = {
  key: string
  title: string
  status: 'ready' | 'queued' | 'draft'
  description: string
}

type ConfigResponse = {
  name: string
  stage: string
  legacyAdminPort: string
  legacyUserPort: string
  frontendOrigins: string[]
  modules: ModuleInfo[]
}

const fallbackConfig: ConfigResponse = {
  name: 'PJSK Goods Next',
  stage: 'local-shell',
  legacyAdminPort: '8512',
  legacyUserPort: '8513',
  frontendOrigins: ['http://localhost:5173', 'http://127.0.0.1:5173'],
  modules: [
    {
      key: 'frontend-shell',
      title: '前端工作台',
      status: 'ready',
      description: 'Vue 3 应用已启动，可继续接入业务页面。',
    },
    {
      key: 'backend-core',
      title: 'Go 后端',
      status: 'queued',
      description: '等待 Go 服务启动后自动连通 /health 与 /api/config。',
    },
    {
      key: 'database',
      title: '数据库迁移',
      status: 'queued',
      description: 'PostgreSQL 表结构下一步落成 migration。',
    },
    {
      key: 'payment-workflow',
      title: '付款审核',
      status: 'queued',
      description: '先保留流程入口，接口在数据模型稳定后接入。',
    },
  ],
}

const apiBase = (import.meta.env.VITE_API_BASE_URL as string | undefined)?.replace(/\/$/, '') ?? ''

const health = ref<HealthResponse | null>(null)
const config = ref<ConfigResponse>(fallbackConfig)
const errorMessage = ref('')
const loading = ref(true)
const checkedAt = ref('')
const activeView = ref<'overview' | 'ops' | 'legacy'>('overview')

const isBackendOnline = computed(() => health.value?.status === 'ok')
const readyCount = computed(() => config.value.modules.filter((item) => item.status === 'ready').length)
const queuedCount = computed(() => config.value.modules.filter((item) => item.status === 'queued').length)

function endpoint(path: string) {
  return apiBase ? `${apiBase}${path}` : path
}

async function fetchJSON<T>(path: string): Promise<T> {
  const response = await fetch(endpoint(path))
  if (!response.ok) {
    throw new Error(`${path} returned ${response.status}`)
  }
  return (await response.json()) as T
}

async function load() {
  loading.value = true
  errorMessage.value = ''

  try {
    const [healthResponse, configResponse] = await Promise.all([
      fetchJSON<HealthResponse>('/health'),
      fetchJSON<ConfigResponse>('/api/config'),
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

onMounted(() => {
  void load()
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
        <span class="connection-pill" :data-online="isBackendOnline">
          <span class="connection-dot" />
          {{ isBackendOnline ? '后端在线' : '本地前端模式' }}
        </span>
        <button class="icon-button" type="button" title="重新检查后端" @click="load" :disabled="loading">
          ↻
        </button>
      </div>
    </header>

    <nav class="tabs" aria-label="工作台视图">
      <button :class="{ active: activeView === 'overview' }" type="button" @click="activeView = 'overview'">
        总览
      </button>
      <button :class="{ active: activeView === 'ops' }" type="button" @click="activeView = 'ops'">
        接口
      </button>
      <button :class="{ active: activeView === 'legacy' }" type="button" @click="activeView = 'legacy'">
        旧版
      </button>
    </nav>

    <main v-if="activeView === 'overview'" class="workspace">
      <section class="metrics" aria-label="运行指标">
        <article class="metric-tile">
          <span>可用模块</span>
          <strong>{{ readyCount }}</strong>
        </article>
        <article class="metric-tile">
          <span>待接入模块</span>
          <strong>{{ queuedCount }}</strong>
        </article>
        <article class="metric-tile">
          <span>后端服务</span>
          <strong>{{ health?.service ?? '未连接' }}</strong>
        </article>
        <article class="metric-tile">
          <span>检查时间</span>
          <strong>{{ checkedAt || '待检查' }}</strong>
        </article>
      </section>

      <section class="panel">
        <div class="panel__header">
          <h2>模块状态</h2>
          <span>{{ config.stage }}</span>
        </div>

        <div v-if="errorMessage" class="inline-alert">
          {{ errorMessage }}
        </div>

        <div class="module-table">
          <div class="module-row module-row--head">
            <span>模块</span>
            <span>状态</span>
            <span>说明</span>
          </div>
          <div v-for="item in config.modules" :key="item.key" class="module-row">
            <strong>{{ item.title }}</strong>
            <span class="status-chip" :data-state="item.status">{{ item.status }}</span>
            <span>{{ item.description }}</span>
          </div>
        </div>
      </section>
    </main>

    <main v-else-if="activeView === 'ops'" class="workspace workspace--two">
      <section class="panel">
        <div class="panel__header">
          <h2>后端接口</h2>
          <span>{{ isBackendOnline ? 'online' : 'offline' }}</span>
        </div>
        <div class="endpoint-list">
          <div>
            <code>GET /health</code>
            <span>{{ health?.status ?? 'waiting' }}</span>
          </div>
          <div>
            <code>GET /api/config</code>
            <span>{{ isBackendOnline ? 'ready' : 'waiting' }}</span>
          </div>
        </div>
      </section>

      <section class="panel">
        <div class="panel__header">
          <h2>下一步</h2>
          <span>backend first</span>
        </div>
        <ol class="task-list">
          <li>落 PostgreSQL migration。</li>
          <li>接 CN 查询码会话。</li>
          <li>接订单与付款草稿。</li>
        </ol>
      </section>
    </main>

    <main v-else class="workspace workspace--two">
      <section class="panel">
        <div class="panel__header">
          <h2>Streamlit 管理端</h2>
          <span>port {{ config.legacyAdminPort }}</span>
        </div>
        <code>cd legacy-streamlit && python -m streamlit run main.py --server.port {{ config.legacyAdminPort }}</code>
      </section>

      <section class="panel">
        <div class="panel__header">
          <h2>Streamlit 用户端</h2>
          <span>port {{ config.legacyUserPort }}</span>
        </div>
        <code>cd legacy-streamlit && python -m streamlit run user.py --server.port {{ config.legacyUserPort }}</code>
      </section>
    </main>
  </div>
</template>

