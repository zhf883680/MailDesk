<script setup>
import { computed, onMounted, ref } from 'vue'
import Icon from './Icon.vue'
import { state, openMailbox, openEmail, loadEmails, loadDigest, syncAll, openCompose } from '../store'
import { fmtTime, importanceLabel, renderMarkdownish } from '../utils'

const emit = defineEmits(['open-settings'])

const greeting = computed(() => {
  const h = new Date().getHours()
  if (h < 6) return '夜深了'
  if (h < 11) return '早上好'
  if (h < 14) return '中午好'
  if (h < 18) return '下午好'
  return '晚上好'
})

const today = new Date()
const dateLine = `${today.getMonth() + 1} 月 ${today.getDate()} 日 · 周${['日', '一', '二', '三', '四', '五', '六'][today.getDay()]}`

const stats = computed(() => state.stats)
const digestOpen = ref(false)
const hasSync = computed(() => {
  const t = state.stats.last_sync
  return t && new Date(t).getFullYear() > 2000
})
const headline = computed(() => stats.value.need_action > 0
  ? `今天有 ${stats.value.need_action} 封邮件等你处理`
  : '今天没有待处理邮件，一切从容')

const cards = computed(() => ([
  { key: 'unread', label: '未读邮件', value: stats.value.unread, hint: `共 ${stats.value.total} 封`, icon: 'mail' },
  { key: 'important', label: '重要邮件', value: stats.value.high, hint: 'AI 判定优先处理', icon: 'alert' },
  { key: 'action', label: '需要处理', value: stats.value.need_action, hint: '需要回复或操作', icon: 'check' },
]))

const todos = computed(() => state.emails
  .filter((e) => e.need_action && !e.is_ad)
  .slice(0, 5))

const recent = computed(() => state.emails.filter((e) => !e.is_ad).slice(0, 6))

onMounted(async () => {
  if (state.mailboxId !== 'all' || state.filter !== 'all') {
    state.mailboxId = 'all'
    state.filter = 'all'
  }
  await loadEmails()
  if (state.ai.enabled && !state.digest) loadDigest(false)
})

function goMailbox(id) { openMailbox(id) }

function lastSyncText(mb) {
  if (!mb.last_sync || mb.last_sync.startsWith('0001')) return '尚未同步'
  return '同步于 ' + fmtTime(mb.last_sync)
}

function importanceColor(v) {
  return v === 'high' ? 'var(--red)' : v === 'medium' ? 'var(--amber)' : 'var(--muted)'
}
function mbName(id) { return state.mailboxes.find((m) => m.id === id)?.name || '' }
function mbColor(id) { return state.mailboxes.find((m) => m.id === id)?.color || '#0f6b5c' }
</script>

<template>
  <div class="page">
    <header class="page-head">
      <div>
        <div class="eyebrow">{{ dateLine }}</div>
        <h1>{{ greeting }}，{{ headline }}</h1>
        <p v-if="hasSync">最近同步 {{ fmtTime(stats.last_sync) }} · {{ state.mailboxes.length }} 个邮箱已接入</p>
        <p v-else>还没有同步记录，添加邮箱后点击同步即可开始。</p>
      </div>
      <div style="display: flex; gap: 8px">
        <button class="btn" :disabled="state.syncing" @click="syncAll">
          <Icon name="refresh" :size="15" :class="{ spin: state.syncing }" /> {{ state.syncing ? '同步中' : '同步邮件' }}
        </button>
        <button class="btn btn-primary" @click="openCompose()"><Icon name="pencil" :size="14" /> 写邮件</button>
      </div>
    </header>

    <div class="stat-grid">
      <button
        v-for="c in cards" :key="c.key" class="stat-card" :class="c.key"
        @click="openMailbox('all', c.key)"
      >
        <div class="accent-bar" :style="{ background: c.key === 'important' ? 'var(--red)' : c.key === 'action' ? 'var(--accent)' : 'var(--ink)' }"></div>
        <div class="k"><Icon :name="c.icon" :size="13" /> {{ c.label }}</div>
        <div class="v">{{ c.value }}</div>
        <div class="stat-hint">{{ c.hint }}</div>
      </button>
    </div>

    <div class="panel digest-panel">
      <div class="panel-head">
        <Icon name="sparkles" :size="15" />
        <h3>AI 今日简报</h3>
        <span class="hint">
          <template v-if="stats.pending_ai">{{ stats.pending_ai }} 封分析中…</template>
          <template v-else>基于今日邮件</template>
        </span>
      </div>

      <div v-if="state.digestLoading" class="ai-wait" style="color: rgba(236,244,238,.7)">
        <Icon name="refresh" :size="15" class="spin" /> 正在阅读你的邮件并撰写简报…
      </div>
      <div v-else-if="state.digestError" class="digest-empty">
        {{ state.digestError }}
      </div>
      <div v-else-if="state.digest" class="digest-body md-body" :class="{ clamp: !digestOpen }" v-html="renderMarkdownish(state.digest)"></div>
      <div v-else class="digest-empty">
        {{ state.ai.enabled ? '点击下方按钮，让 AI 汇总今天最值得关注的事。' : '配置 AI 引擎后，这里会自动汇总每日重点邮件。' }}
      </div>

      <div class="digest-actions">
        <button v-if="state.digest" class="digest-toggle" @click="digestOpen = !digestOpen">
          {{ digestOpen ? '收起' : '展开全文' }}
          <Icon :name="digestOpen ? 'chevronDown' : 'chevronRight'" :size="12" />
        </button>
        <button v-if="state.ai.enabled" class="btn btn-sm" :disabled="state.digestLoading" @click="loadDigest(true)">
          <Icon name="sparkles" :size="13" /> {{ state.digest ? '重新生成' : '生成简报' }}
        </button>
        <button v-else class="btn btn-sm" @click="emit('open-settings', 'ai')">
          <Icon name="settings" :size="13" /> 配置 AI 引擎
        </button>
      </div>
    </div>

    <div style="display: grid; grid-template-columns: 1.35fr 1fr; gap: 16px; margin-top: 16px" class="dash-grid">
      <div class="panel">
        <div class="panel-head">
          <Icon name="check" :size="15" />
          <h3>需要你处理</h3>
          <button class="hint" style="margin-left: auto" @click="openMailbox('all', 'action')">查看全部 →</button>
        </div>
        <div v-if="!todos.length" class="empty-state" style="padding: 26px 10px">
          <Icon name="check" :size="30" :stroke="1.2" />
          <p>暂时没有需要处理的邮件，享受这份清净。</p>
        </div>
        <div v-else class="todo-list">
          <button v-for="e in todos" :key="e.id" class="todo-item" @click="openEmail(e)">
            <span class="dot" :style="{ background: importanceColor(e.importance) }"></span>
            <div class="todo-main">
              <div class="todo-sub">{{ e.subject_zh || e.subject }}</div>
              <div class="todo-sum">{{ e.summary || e.snippet }}</div>
              <div class="todo-meta">
                <span>{{ e.from_name || e.from_addr }}</span>
                <span>·</span>
                <span>{{ fmtTime(e.date) }}</span>
                <span>·</span>
                <span class="tag-dot" :style="{ background: mbColor(e.mailbox_id) }"></span>
                <span>{{ mbName(e.mailbox_id) }}</span>
                <span v-if="e.importance" class="badge" :class="'badge-' + e.importance">{{ importanceLabel(e.importance) }}</span>
              </div>
            </div>
          </button>
        </div>
      </div>

      <div class="panel recent-panel">
        <div class="panel-head">
          <Icon name="mail" :size="15" />
          <h3>最近邮件</h3>
          <span class="hint">AI 已分级</span>
        </div>
        <div v-if="!recent.length" class="empty-state" style="padding: 20px">
          <p>暂无邮件</p>
        </div>
        <div v-else class="todo-list">
          <button v-for="e in recent" :key="e.id" class="todo-item" @click="openEmail(e)">
            <span class="dot" :style="{ background: mbColor(e.mailbox_id) }"></span>
            <div class="todo-main">
              <div class="todo-sub" :style="{ fontWeight: e.seen ? 400 : 600 }">{{ e.subject_zh || e.subject }}</div>
              <div class="todo-meta">
                <span>{{ e.from_name || e.from_addr }}</span>
                <span>·</span>
                <span>{{ fmtTime(e.date) }}</span>
              </div>
            </div>
          </button>
        </div>
      </div>
    </div>

    <div class="panel" style="margin-top: 16px">
      <div class="panel-head">
        <Icon name="layers" :size="15" />
        <h3>按邮箱查看</h3>
        <button class="hint" style="margin-left: auto" @click="emit('open-settings', 'mailboxes')">管理邮箱 →</button>
      </div>
      <div v-if="!state.mailboxes.length" class="mb-empty">
        还没有接入邮箱。<button style="color: var(--accent)" @click="emit('open-settings', 'mailboxes')">去添加第一个邮箱</button>
      </div>
      <div v-else class="mb-grid">
        <button v-for="mb in state.mailboxes" :key="mb.id" class="mb-card" @click="goMailbox(mb.id)">
          <div class="mb-card-top">
            <span class="mb-dot" :style="{ background: mb.color, width: '10px', height: '10px', borderRadius: '4px' }"></span>
            <div style="min-width: 0">
              <div class="mb-card-name">{{ mb.name }}</div>
              <div class="mb-card-mail">{{ mb.email }}</div>
            </div>
            <span v-if="state.stats.by_mailbox?.[mb.id]?.unread" class="badge badge-action" style="margin-left: auto">
              {{ state.stats.by_mailbox[mb.id].unread }} 未读
            </span>
          </div>
          <div class="mb-card-stats">
            <span><b>{{ state.stats.by_mailbox?.[mb.id]?.total || 0 }}</b>邮件</span>
            <span><b>{{ state.stats.by_mailbox?.[mb.id]?.high || 0 }}</b>重要</span>
            <span><b>{{ state.stats.by_mailbox?.[mb.id]?.need_action || 0 }}</b>待处理</span>
          </div>
          <div class="mb-row-err" v-if="mb.last_error">{{ mb.last_error }}</div>
          <div style="font-size: 11px; color: var(--muted)">{{ lastSyncText(mb) }}</div>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.dash-grid {
  align-items: stretch;
}

.dash-grid > .panel + .panel {
  margin-top: 0;
}

@media (max-width: 900px) {
  .dash-grid { grid-template-columns: 1fr !important; }
}
</style>
