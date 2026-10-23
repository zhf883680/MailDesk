<script setup>
import { computed } from 'vue'
import Icon from './Icon.vue'
import { state, openMailbox, openCompose, logout, syncAll } from '../store'

const emit = defineEmits(['open-settings'])

const unreadOf = (id) => (state.stats.by_mailbox?.[id]?.unread || 0)

const nav = computed(() => ([
  { key: 'dashboard', label: '汇总', icon: 'layers', count: state.stats.unread },
  { key: 'all', label: '全部邮件', icon: 'inbox', count: state.stats.total },
  { key: 'action', label: '待处理', icon: 'check', count: state.stats.need_action },
  { key: 'star', label: '重要', icon: 'star', count: state.stats.high },
]))

function go(key) {
  state.sidebarOpen = false
  if (key === 'dashboard') {
    state.view = 'dashboard'
    state.selected = null
    return
  }
  if (key === 'all') return openMailbox('all')
  if (key === 'action') return openMailbox('all', 'action')
  if (key === 'star') return openMailbox('all', 'important')
}

function mailbox(id) {
  state.sidebarOpen = false
  openMailbox(id)
}

function isActive(key) {
  if (key === 'dashboard') return state.view === 'dashboard'
  if (key === 'all') return state.view === 'mailbox' && state.mailboxId === 'all' && state.filter === 'all'
  if (key === 'action') return state.view === 'mailbox' && state.filter === 'action'
  if (key === 'star') return state.view === 'mailbox' && state.filter === 'important'
  return false
}

function compose() {
  state.sidebarOpen = false
  openCompose()
}
</script>

<template>
  <aside class="sidebar" :class="{ open: state.sidebarOpen }">
    <div class="side-brand">
      <span class="brand-mark"><Icon name="mail" :size="16" /></span>
      <div>
        <div class="name">MailDesk</div>
        <div class="sub">AI Mail Console</div>
      </div>
    </div>

    <button class="compose-btn" @click="compose">
      <Icon name="pencil" :size="15" /> 写邮件
    </button>

    <nav class="side-nav">
      <button
        v-for="item in nav" :key="item.key"
        class="nav-item" :class="{ active: isActive(item.key) }"
        @click="go(item.key)"
      >
        <Icon :name="item.icon" :size="16" :filled="item.key === 'star' && isActive(item.key)" />
        <span>{{ item.label }}</span>
        <span v-if="item.count" class="count">{{ item.count > 99 ? '99+' : item.count }}</span>
      </button>
    </nav>

    <div class="side-section">
      <span class="label">邮箱</span>
      <button title="添加邮箱" @click="emit('open-settings', 'mailboxes')"><Icon name="plus" :size="14" /></button>
    </div>

    <div class="mb-list">
      <button
        v-for="mb in state.mailboxes" :key="mb.id"
        class="mb-item" :class="{ active: state.view === 'mailbox' && state.mailboxId === mb.id && state.filter === 'all' }"
        @click="mailbox(mb.id)"
      >
        <span class="mb-dot" :style="{ background: mb.color || '#2f8571' }"></span>
        <span class="mb-name">{{ mb.name }}</span>
        <span v-if="unreadOf(mb.id)" class="mb-count">{{ unreadOf(mb.id) }}</span>
      </button>
      <button v-if="!state.mailboxes.length" class="mb-item" @click="emit('open-settings', 'mailboxes')">
        <Icon name="plus" :size="14" /><span class="mb-name">添加第一个邮箱</span>
      </button>
    </div>

    <div class="side-foot">
      <button class="icon-btn" title="设置" @click="emit('open-settings', state.mailboxes.length ? 'ai' : 'mailboxes')"><Icon name="settings" :size="17" /></button>
      <button class="icon-btn" title="同步邮件" @click="syncAll"><Icon name="refresh" :size="16" :class="{ spin: state.syncing }" /></button>
      <button class="icon-btn" title="退出登录" @click="logout"><Icon name="logout" :size="16" /></button>
      <span class="status">
        <template v-if="state.syncing">{{ state.syncLabel || '同步中…' }}</template>
        <template v-else-if="state.stats.pending_ai">{{ state.stats.pending_ai }} 封待 AI 处理</template>
        <template v-else>{{ state.mailboxes.length }} 个邮箱</template>
      </span>
    </div>
  </aside>
</template>
