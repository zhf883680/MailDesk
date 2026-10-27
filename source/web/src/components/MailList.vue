<script setup>
import { computed, ref } from 'vue'
import Icon from './Icon.vue'
import { state, setFilter, setSearch, selectEmail, syncAll, openMailbox, ignoreAds, toast } from '../store'
import { fmtTime, dayGroup, importanceLabel, senderAvatar } from '../utils'

const title = computed(() => {
  if (state.mailboxId === 'all') {
    if (state.filter === 'action') return '待处理'
    if (state.filter === 'important') return '重要邮件'
    return '全部邮件'
  }
  const mb = state.mailboxes.find((m) => m.id === state.mailboxId)
  return mb ? mb.name : '收件箱'
})

const subtitle = computed(() => {
  if (state.q) return `搜索「${state.q}」`
  if (state.mailboxId === 'all') return `${state.emails.length} 封邮件 · 已按时间排序`
  const mb = state.mailboxes.find((m) => m.id === state.mailboxId)
  return mb ? mb.email : ''
})

const filters = computed(() => {
  // 进入具体邮箱时使用该邮箱的计数，避免顶部数字与当前列表范围不一致
  const s = state.mailboxId === 'all'
    ? state.stats
    : (state.stats.by_mailbox?.[state.mailboxId] || state.stats)
  return [
    { key: 'all', label: '全部' },
    { key: 'unread', label: '未读', n: s.unread },
    { key: 'important', label: '重要', n: s.high },
    { key: 'action', label: '待处理', n: s.need_action },
    { key: 'ads', label: '广告', n: s.ads },
  ]
})

const groups = computed(() => {
  const map = new Map()
  for (const mail of state.emails) {
    const g = dayGroup(mail.date)
    if (!map.has(g)) map.set(g, [])
    map.get(g).push(mail)
  }
  const order = ['今天', '昨天', '本周', '更早']
  return order.filter((k) => map.has(k)).map((k) => ({ label: k, items: map.get(k) }))
})

function mailboxName(id) {
  return state.mailboxes.find((m) => m.id === id)?.name || ''
}
function mailboxColor(id) {
  return state.mailboxes.find((m) => m.id === id)?.color || '#0f6b5c'
}
function importanceClass(v) {
  return v === 'high' ? 'badge-high' : v === 'medium' ? 'badge-medium' : 'badge-low'
}
function avatar(mail) {
  return senderAvatar(mail.from_name, mail.from_addr)
}

const confirmIgnore = ref(false)
let confirmTimer = null

function askIgnoreAll() {
  if (!confirmIgnore.value) {
    confirmIgnore.value = true
    clearTimeout(confirmTimer)
    confirmTimer = setTimeout(() => { confirmIgnore.value = false }, 3000)
    return
  }
  clearTimeout(confirmTimer)
  confirmIgnore.value = false
  runIgnoreAll()
}

async function runIgnoreAll() {
  const ids = state.emails.map((e) => e.id)
  try {
    await ignoreAds(ids)
  } catch (e) {
    toast(e.message, 'err')
  }
}
</script>

<template>
  <section class="list-pane">
    <div class="list-head">
      <div class="list-title">
        <button class="icon-btn mobile-only" @click="state.sidebarOpen = true" aria-label="菜单">
          <Icon name="menu" :size="19" />
        </button>
        <h2>{{ title }}<span class="count-txt">{{ state.emails.length }}</span></h2>
        <button class="icon-btn" title="立即同步" :disabled="state.syncing" @click="syncAll">
          <Icon name="refresh" :size="16" :class="{ spin: state.syncing }" />
        </button>
      </div>

      <div class="search-box">
        <Icon name="search" :size="15" />
        <input
          :value="state.q"
          type="search"
          placeholder="搜索主题、发件人、正文…"
          @input="setSearch($event.target.value)"
        />
        <button v-if="state.q" class="icon-btn clear" @click="setSearch('')">
          <Icon name="close" :size="13" />
        </button>
      </div>

      <div class="filter-chips">
        <button
          v-for="f in filters" :key="f.key"
          class="chip" :class="{ active: state.filter === f.key }"
          @click="setFilter(f.key)"
        >
          {{ f.label }}<span v-if="f.n" class="n">{{ f.n }}</span>
        </button>
      </div>
      <div v-if="state.filter === 'ads' && state.emails.length" class="ads-bar">
        <span>这些广告已取消默认忽略；重新忽略后仍可在全部邮件中找回</span>
        <button class="btn btn-sm" @click="askIgnoreAll">
          <Icon name="eyeOff" :size="12" />
          {{ confirmIgnore ? `确认忽略 ${state.emails.length} 封` : '全部忽略' }}
        </button>
      </div>
    </div>

    <div class="mail-scroll">
      <div v-if="state.listLoading && !state.emails.length" style="padding: 20px">
        <div v-for="i in 6" :key="i" style="padding: 12px 0; border-bottom: 1px solid var(--line)">
          <div class="skeleton" style="height: 11px; width: 32%; margin-bottom: 10px"></div>
          <div class="skeleton" style="height: 13px; width: 78%; margin-bottom: 8px"></div>
          <div class="skeleton" style="height: 11px; width: 92%; margin-bottom: 5px"></div>
          <div class="skeleton" style="height: 11px; width: 55%"></div>
        </div>
      </div>

      <div v-else-if="!state.emails.length" class="empty-state">
        <Icon name="inbox" :size="42" :stroke="1.2" />
        <h3>{{ state.q ? '没有找到相关邮件' : state.filter === 'ads' ? '没有广告邮件' : '这里还很安静' }}</h3>
        <p>
          {{ state.q ? '试试更短的关键词，或切换邮箱与筛选条件。'
            : state.filter === 'ads' ? '广告默认已忽略，不计入未读。需要查看时，可在全部邮件中取消忽略。'
            : state.mailboxes.length ? '同步后新的邮件会出现在这里。也可以先调整筛选条件。' : '先到设置里添加一个邮箱，开始接收邮件。' }}
        </p>
        <button v-if="!state.q && !state.mailboxes.length" class="btn btn-sm" @click="state.settingsTab = 'mailboxes'; state.view = 'settings'">添加邮箱</button>
        <button v-else-if="!state.q && state.filter === 'ads'" class="btn btn-sm" @click="state.settingsTab = 'ai'; state.view = 'settings'">管理广告白名单</button>
        <button v-else-if="!state.q" class="btn btn-sm" @click="openMailbox('all')">查看全部邮件</button>
      </div>

      <template v-for="group in groups" :key="group.label">
        <div class="mail-group-label">{{ group.label }}</div>
        <button
          v-for="mail in group.items" :key="mail.id"
          class="mail-item"
          :class="{ unread: !mail.seen, active: state.selectedId === mail.id, 'is-promo': mail.is_ad }"
          @click="selectEmail(mail.id)"
        >
          <div class="mi-body">
            <span class="mi-avatar-wrap" aria-hidden="true">
              <span class="mi-avatar" :style="{ background: avatar(mail).color }">{{ avatar(mail).label }}</span>
              <span class="mi-dot"></span>
            </span>
            <div class="mi-main">
              <div class="mi-top">
                <span class="mi-sender">{{ mail.from_name || mail.from_addr }}</span>
                <span class="mi-time">{{ fmtTime(mail.date) }}</span>
              </div>
              <div class="mi-subject">{{ mail.subject_zh || mail.subject }}</div>
              <div class="mi-snippet">{{ mail.summary || mail.snippet }}</div>
              <div class="mi-badges">
                <span v-if="mail.importance === 'high'" class="badge" :class="importanceClass(mail.importance)">
                  <Icon name="alert" :size="10" /> 重要
                </span>
                <span v-else-if="mail.importance === 'medium'" class="badge badge-medium">{{ importanceLabel(mail.importance) }}</span>
                <span v-if="mail.need_action" class="badge badge-action">待处理</span>
                <span v-if="mail.is_ad" class="badge badge-promo">广告</span>
                <span v-if="mail.ad_ignored" class="badge badge-muted">已忽略</span>
                <span v-if="mail.handled" class="badge badge-low"><Icon name="check" :size="10" /> 已处理</span>
                <span v-if="mail.has_trans" class="badge badge-ai mi-extra"><Icon name="languages" :size="10" /> 译</span>
                <span v-if="mail.ai_status === 'pending' || mail.ai_status === 'running'" class="badge badge-ai">
                  <Icon name="sparkles" :size="10" /> AI 分析中
                </span>
                <span v-if="mail.rule_count" class="badge badge-action mi-extra">{{ mail.rule_count }} 条规则结果</span>
                <span v-if="mail.forwarded" class="badge badge-muted mi-extra"><Icon name="bell" :size="10" /> 已推送</span>
                <span v-if="state.mailboxId === 'all'" class="mi-mb">
                  <span class="tag-dot" :style="{ background: mailboxColor(mail.mailbox_id) }"></span>{{ mailboxName(mail.mailbox_id) }}
                </span>
              </div>
            </div>
          </div>
        </button>
      </template>
    </div>
  </section>
</template>
