<script setup>
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import Icon from './Icon.vue'
import { api } from '../api'
import { state, openEmail, toast, reloadStats } from '../store'
import { fmtTime } from '../utils'

const loading = ref(false)
const items = ref([])
const counts = ref({})
const paused = ref(false)
const enabled = ref(true)
const tab = ref('error')
const retryingAll = ref(false)
const retryBusy = ref({})
const expanded = ref({})
let timer = null

const errorCount = computed(() => counts.value.error || 0)
const queueCount = computed(() => (counts.value.pending || 0) + (counts.value.running || 0))
const doneCount = computed(() => items.value.filter((e) => e.ai_status === 'done').length)

const tabs = computed(() => ([
  { key: 'error', label: '失败', n: errorCount.value },
  { key: 'queue', label: '排队中', n: queueCount.value },
  { key: 'done', label: '最近完成', n: doneCount.value },
]))

const visible = computed(() => {
  if (tab.value === 'error') return items.value.filter((e) => e.ai_status === 'error')
  if (tab.value === 'queue') return items.value.filter((e) => e.ai_status === 'pending' || e.ai_status === 'running')
  return items.value.filter((e) => e.ai_status === 'done')
})

const retryLabel = computed(() => {
  if (tab.value === 'queue') return '重新排队'
  if (tab.value === 'done') return '全部重新分析'
  return '重试全部失败'
})

async function load(silent = false) {
  if (!silent) loading.value = true
  try {
    const d = await api.aiQueue()
    items.value = d.items || []
    counts.value = d.counts || {}
    paused.value = !!d.paused
    enabled.value = !!d.enabled
  } catch (e) {
    if (!silent) toast(e.message, 'err')
  } finally {
    if (!silent) loading.value = false
  }
}

function startPolling() {
  if (timer) return
  timer = setInterval(() => {
    if (document.visibilityState === 'visible') load(true)
  }, 5000)
}

function stopPolling() {
  if (timer) clearInterval(timer)
  timer = null
}

watch(() => queueCount.value, (n) => {
  if (n > 0) startPolling()
  else stopPolling()
})

async function retryOne(mail) {
  if (retryBusy.value[mail.id]) return
  retryBusy.value = { ...retryBusy.value, [mail.id]: true }
  try {
    await api.aiRetry({ ids: [mail.id] })
    mail.ai_status = 'pending'
    mail.ai_error = ''
    counts.value = {
      ...counts.value,
      error: Math.max(0, errorCount.value - 1),
      pending: (counts.value.pending || 0) + 1,
    }
    tab.value = 'queue'
    paused.value = false
    await reloadStats()
    toast('已重新加入 AI 队列')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    const next = { ...retryBusy.value }
    delete next[mail.id]
    retryBusy.value = next
  }
}

async function retryAll() {
  if (retryingAll.value || !visible.value.length) return
  retryingAll.value = true
  try {
    const r = await api.aiRetry({ ids: visible.value.map((e) => e.id) })
    toast(`已重新加入 ${r.queued} 封邮件`)
    paused.value = false
    tab.value = 'queue'
    await Promise.all([load(true), reloadStats()])
    startPolling()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    retryingAll.value = false
  }
}

function toggle(mail) {
  expanded.value = { ...expanded.value, [mail.id]: !expanded.value[mail.id] }
}

function open(mail) {
  openEmail(mail)
}

function mailboxName(id) {
  return state.mailboxes.find((m) => m.id === id)?.name || ''
}

function statusIcon(status) {
  if (status === 'error') return 'alert'
  if (status === 'running') return 'refresh'
  if (status === 'pending') return 'clock'
  return 'check'
}

function statusText(status) {
  if (status === 'error') return '失败'
  if (status === 'running') return '分析中'
  if (status === 'pending') return '排队中'
  return '已完成'
}

function onExternalRefresh() {
  load(true)
}

onMounted(async () => {
  await load()
  if (queueCount.value > 0) startPolling()
  window.addEventListener('maildesk:ai-refresh', onExternalRefresh)
})

onBeforeUnmount(() => {
  stopPolling()
  window.removeEventListener('maildesk:ai-refresh', onExternalRefresh)
})
</script>

<template>
  <div class="page aiq-page">
    <header class="page-head">
      <div>
        <div class="eyebrow">AI Pipeline</div>
        <h1>AI 分析队列</h1>
        <p>
          {{ queueCount }} 封排队中 · {{ errorCount }} 封失败 · {{ doneCount }} 封最近完成
        </p>
      </div>
      <div class="aiq-head-actions">
        <button class="btn" :disabled="loading" @click="load()">
          <Icon name="refresh" :size="14" :class="{ spin: loading }" /> 刷新
        </button>
        <button
          class="btn btn-primary"
          :disabled="retryingAll || !visible.length"
          @click="retryAll"
        >
          <Icon name="refresh" :size="14" /> {{ retryLabel }}
        </button>
      </div>
    </header>

    <div v-if="!enabled" class="aiq-note">
      <Icon name="alert" :size="14" />
      AI 引擎未启用，队列不会自动分析。可在「设置 · AI 引擎」中开启。
    </div>
    <div v-else-if="paused" class="aiq-note warn">
      <Icon name="alert" :size="14" />
      队列已暂停：上一次调用 AI 接口连续失败。重试任意邮件或更新配置后会恢复。
    </div>

    <div class="aiq-tabs">
      <button
        v-for="t in tabs" :key="t.key"
        class="chip" :class="{ active: tab === t.key }"
        @click="tab = t.key"
      >
        {{ t.label }}<span v-if="t.n" class="n">{{ t.n }}</span>
      </button>
    </div>

    <div v-if="loading && !items.length" class="panel">
      <div v-for="i in 5" :key="i" style="padding: 14px 0; border-bottom: 1px solid var(--line)">
        <div class="skeleton" style="height: 13px; width: 60%; margin-bottom: 9px"></div>
        <div class="skeleton" style="height: 11px; width: 34%"></div>
      </div>
    </div>

    <div v-else-if="!visible.length" class="panel aiq-empty">
      <Icon :name="tab === 'error' ? 'check' : tab === 'queue' ? 'clock' : 'sparkles'" :size="34" :stroke="1.3" />
      <h3>{{ tab === 'error' ? '没有失败的邮件' : tab === 'queue' ? '队列是空的' : '还没有完成的记录' }}</h3>
      <p>
        {{ tab === 'error'
          ? 'AI 处理失败的邮件会出现在这里，并附带错误原因。'
          : tab === 'queue'
            ? '新邮件进入后会自动排队分析，这里可以看到实时进度。'
            : '最近 60 封完成 AI 分析的邮件会显示在这里。' }}
      </p>
    </div>

    <div v-else class="panel aiq-list">
      <div
        v-for="mail in visible" :key="mail.id"
        class="aiq-row" :class="'st-' + mail.ai_status"
        @click="open(mail)"
      >
        <span class="aiq-status">
          <Icon :name="statusIcon(mail.ai_status)" :size="15" :class="{ spin: mail.ai_status === 'running' }" />
        </span>
        <div class="aiq-main">
          <div class="aiq-top">
            <span class="aiq-subject">{{ mail.subject_zh || mail.subject || '（无主题）' }}</span>
            <span class="aiq-time">{{ fmtTime(mail.date) }}</span>
          </div>
          <div class="aiq-meta">
            <span class="aiq-from">{{ mail.from_name || mail.from_addr }}</span>
            <span v-if="mailboxName(mail.mailbox_id)" class="aiq-dot">·</span>
            <span v-if="mailboxName(mail.mailbox_id)">{{ mailboxName(mail.mailbox_id) }}</span>
            <span class="aiq-state">{{ statusText(mail.ai_status) }}</span>
            <span v-if="mail.summary" class="aiq-summary">{{ mail.summary }}</span>
          </div>
          <div
            v-if="mail.ai_status === 'error'"
            class="aiq-error" :class="{ open: expanded[mail.id] }"
          >
            <Icon name="alert" :size="12" />
            <span class="aiq-error-text">{{ mail.ai_error || '未知错误' }}</span>
            <button
              v-if="(mail.ai_error || '').length > 56"
              class="aiq-more"
              @click.stop="toggle(mail)"
            >
              {{ expanded[mail.id] ? '收起' : '展开' }}
            </button>
          </div>
        </div>
        <div class="aiq-actions">
          <button
            class="btn btn-sm"
            :disabled="retryBusy[mail.id]"
            @click.stop="retryOne(mail)"
          >
            <Icon name="refresh" :size="12" :class="{ spin: retryBusy[mail.id] }" />
            {{ mail.ai_status === 'error' ? '重试' : '重新分析' }}
          </button>
          <button class="icon-btn" title="打开邮件" @click.stop="open(mail)">
            <Icon name="chevronRight" :size="16" />
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
