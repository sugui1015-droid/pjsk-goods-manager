<script setup lang="ts">
// The top identity/status bar shown on the role portals and module pages.
// Presentational: the parent supplies identity text and online state and
// handles the refresh/logout/back actions.
defineProps<{
  identity?: string
  online?: boolean
  onlineText?: string
  notice?: {
    text: string
    action?: string
  }
  showRefresh?: boolean
  backLabel?: string
}>()
defineEmits<{ (e: 'refresh'): void; (e: 'logout'): void; (e: 'back'): void; (e: 'notice'): void }>()
</script>

<template>
  <header class="portal-bar">
    <div class="portal-bar__brand">
      <span class="portal-bar__name">PJSK 谷子系统</span>
    </div>
    <div class="portal-bar__actions">
      <button v-if="backLabel" class="portal-bar__back" type="button" @click="$emit('back')">{{ backLabel }}</button>
      <span v-if="identity" class="portal-bar__identity">{{ identity }}</span>
      <span v-if="online !== undefined" class="portal-bar__status" :data-online="online">
        <span class="portal-bar__dot" />{{ online ? (onlineText ?? '后端在线') : (onlineText ?? '后端离线') }}
      </span>
      <button v-if="notice" class="portal-bar__notice" type="button" @click="$emit('notice')">
        <span aria-hidden="true">🔔</span>
        <span>{{ notice.text }}</span>
        <span v-if="notice.action" class="portal-bar__notice-action">{{ notice.action }}</span>
      </button>
      <button v-if="showRefresh" class="portal-bar__icon" type="button" title="刷新状态" @click="$emit('refresh')">↻</button>
      <button class="portal-bar__logout" type="button" @click="$emit('logout')">退出登录</button>
    </div>
  </header>
</template>
