import { reactive } from 'vue'
import { api } from './api'

const emptyStats = () => ({
  total: 0, unread: 0, high: 0, need_action: 0, ads: 0, pending_ai: 0,
  by_mailbox: {}, last_sync: null,
})

export const state = reactive({
  booting: true,
  authed: false,
  demo: false,
  version: '',
  mailboxes: [],
  ai: { enabled: false, base_url: '', model: '', translate: true, classify: true, max_chars: 6000 },
  rules: [],
  forward: { enabled: false, base_url: 'https://api.day.app', device_key: '', group: 'MailDesk', sound: '', level: 'active' },
  forwardRules: [],
  general: { sync_minutes: 5, fetch_limit: 40 },
  keyIsDefault: false,
  stats: emptyStats(),

  view: 'dashboard',
  settingsTab: 'mailboxes',
  mailboxId: 'all',
  filter: 'all',
  q: '',

  emails: [],
  listLoading: false,
  selectedId: null,
  selected: null,
  readerLoading: false,
  mobilePane: 'list',

  composing: false,
  composeDraft: null,
  sidebarOpen: false,

  syncing: false,
  syncLabel: '',
  digest: null,
  digestLoading: false,
  digestError: '',

  toasts: [],
})

let toastSeq = 0
export function toast(message, type = 'ok', ms = 2600) {
  const id = ++toastSeq
  state.toasts.push({ id, message, type })
  setTimeout(() => {
    const i = state.toasts.findIndex((t) => t.id === id)
    if (i >= 0) state.toasts.splice(i, 1)
  }, ms)
}

function applyBoot(d) {
  state.mailboxes = d.mailboxes || []
  state.ai = d.ai || state.ai
  state.rules = d.rules || []
  state.forward = d.forward || state.forward
  state.forwardRules = d.forward_rules || []
  state.general = { sync_minutes: d.sync_minutes, fetch_limit: d.fetch_limit }
  state.stats = d.stats || emptyStats()
  state.keyIsDefault = !!d.key_is_default
  state.demo = !!d.demo
  state.version = d.version || ''
  state.authed = true
}

export async function init() {
  state.booting = true
  try {
    await reloadBoot()
    if (!state.authed) return
    await loadEmails()
  } finally {
    state.booting = false
  }
}

export async function login(key) {
  await api.login(key)
  await init()
  state.selectedId = null
  state.selected = null
  state.view = 'dashboard'
}

export async function logout() {
  try { await api.logout() } catch (e) { /* ignore */ }
  state.authed = false
  state.selected = null
  state.selectedId = null
  state.emails = []
  state.digest = null
}

let bootSeq = 0
export async function reloadBoot() {
  const seq = ++bootSeq
  try {
    const d = await api.bootstrap()
    // 丢弃过期响应，避免旧统计覆盖刚保存的白名单等配置
    if (seq === bootSeq) applyBoot(d)
  } catch (e) {
    if (e.status === 401) state.authed = false
  }
}

let listSeq = 0
export async function loadEmails() {
  const seq = ++listSeq
  state.listLoading = true
  try {
    const rows = await api.emails({ mailbox: state.mailboxId, q: state.q, filter: state.filter, limit: 200 })
    if (seq !== listSeq) return
    state.emails = rows
    // 同步刷新统计，确保计数与当前列表使用同一份配置
    await reloadStats()
  } catch (e) {
    if (seq !== listSeq) return
    if (e.status === 401) { state.authed = false; return }
    toast(e.message, 'err')
  } finally {
    if (seq === listSeq) state.listLoading = false
  }
}

let statsSeq = 0
export async function reloadStats() {
  const seq = ++statsSeq
  try {
    const s = await api.stats()
    if (seq === statsSeq) state.stats = s
  } catch (e) {
    if (e.status === 401) state.authed = false
  }
}

// 重新拉取当前页面所需的数据（下拉刷新 / 回到前台时使用）
export async function refreshData() {
  await reloadBoot()
  if (!state.authed) return
  if (state.view === 'mailbox') {
    await loadEmails()
    if (state.selected) await refreshSelected()
  } else if (state.view === 'dashboard') {
    await loadEmails()
    if (state.ai.enabled) await loadDigest(false)
  }
}

let searchTimer = null
export function setSearch(q) {
  state.q = q
  if (searchTimer) clearTimeout(searchTimer)
  searchTimer = setTimeout(() => { loadEmails() }, 260)
}

export async function openMailbox(id, filter = 'all') {
  state.view = 'mailbox'
  state.mailboxId = id
  state.filter = filter
  state.q = ''
  state.mobilePane = 'list'
  state.selected = null
  state.selectedId = null
  await loadEmails()
}

export function setFilter(f) {
  state.filter = f
  loadEmails()
}

export async function openEmail(email) {
  if (!email) return
  state.view = 'mailbox'
  state.mailboxId = email.mailbox_id
  state.filter = 'all'
  state.q = ''
  await loadEmails()
  await selectEmail(email.id)
}

export async function selectEmail(id) {
  state.selectedId = id
  state.readerLoading = true
  state.mobilePane = 'reader'
  try {
    const e = await api.email(id)
    if (state.selectedId !== id) return
    state.selected = e
    const item = state.emails.find((x) => x.id === id)
    if (item && !item.seen) {
      item.seen = true
      state.stats.unread = Math.max(0, state.stats.unread - 1)
      const ms = state.stats.by_mailbox[e.mailbox_id]
      if (ms && ms.unread > 0) ms.unread--
    }
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    state.readerLoading = false
  }
}

export async function refreshSelected() {
  if (!state.selectedId) return
  try {
    const e = await api.email(state.selectedId)
    state.selected = e
    const s = state.emails.find((x) => x.id === e.id)
    if (s) Object.assign(s, {
      ai_status: e.ai_status, summary: e.summary, importance: e.importance,
      need_action: e.need_action, is_ad: e.is_ad, category: e.category,
      has_trans: !!e.translated, rule_count: (e.rule_results || []).length,
    })
  } catch (e) { /* ignore */ }
}

// 标记 / 取消「已处理」
export async function setHandled(handled) {
  if (!state.selected) return
  const id = state.selected.id
  const e = await api.flags(id, { handled })
  if (state.selected?.id === id) state.selected = e
  const item = state.emails.find((x) => x.id === id)
  if (item) {
    item.handled = e.handled
    item.need_action = e.need_action
  }
  await reloadStats()
  toast(handled ? '已标记为处理完成' : '已恢复为待处理')
}

// 纠正 AI 的广告误判
export async function setAdOverride(value) {
  if (!state.selected) return
  const id = state.selected.id
  const e = await api.flags(id, { ad_override: value })
  if (state.selected?.id === id) state.selected = e
  const item = state.emails.find((x) => x.id === id)
  if (item) item.is_ad = e.is_ad
  await reloadStats()
  toast(value === 'not_ad' ? '已标记为「不是广告」' : '已标记为广告')
}

// 忽略 / 取消忽略广告：忽略后不再计入广告统计与广告列表
export async function setAdIgnored(ignore) {
  if (!state.selected) return
  const id = state.selected.id
  const e = await api.flags(id, { ad_ignored: ignore })
  if (state.selected?.id === id) state.selected = e
  const item = state.emails.find((x) => x.id === id)
  if (item) item.ad_ignored = e.ad_ignored
  await reloadStats()
  toast(ignore ? '已忽略这封广告' : '已取消忽略')
}

export async function ignoreAds(ids) {
  if (!ids.length) return
  const r = await api.ignoreAds(ids)
  const set = new Set(ids)
  state.emails = state.emails.filter((e) => !set.has(e.id))
  await reloadStats()
  toast(`已忽略 ${r.updated} 封广告`)
}

export async function toggleStar() {
  if (!state.selected) return
  const next = !state.selected.flagged
  state.selected.flagged = next
  const item = state.emails.find((x) => x.id === state.selected.id)
  if (item) item.flagged = next
  try {
    await api.flags(state.selected.id, { flagged: next })
  } catch (e) {
    toast(e.message, 'err')
  }
}

export async function setSeen(seen) {
  if (!state.selected) return
  state.selected.seen = seen
  const item = state.emails.find((x) => x.id === state.selected.id)
  if (item) item.seen = seen
  try {
    await api.flags(state.selected.id, { seen })
    await reloadStats()
  } catch (e) {
    toast(e.message, 'err')
  }
}

export async function removeSelected() {
  if (!state.selected) return
  const id = state.selected.id
  try {
    await api.removeEmail(id)
    state.emails = state.emails.filter((x) => x.id !== id)
    state.selected = null
    state.selectedId = null
    state.mobilePane = 'list'
    toast('邮件已移入垃圾箱')
    await reloadStats()
  } catch (e) {
    toast(e.message, 'err')
  }
}

export async function reanalyze() {
  if (!state.selected) return
  try {
    await api.analyze(state.selected.id)
    state.selected.ai_status = 'pending'
    const item = state.emails.find((x) => x.id === state.selected.id)
    if (item) item.ai_status = 'pending'
    toast('已加入 AI 分析队列')
  } catch (e) {
    toast(e.message, 'err')
  }
}

export async function syncAll() {
  if (state.syncing) return
  state.syncing = true
  state.syncLabel = '正在同步邮箱…'
  try {
    await api.syncAll()
    let rounds = 0
    while (rounds < 12) {
      await new Promise((r) => setTimeout(r, 1200))
      await reloadBoot()
      await loadEmails()
      if (state.stats.pending_ai === 0) break
      rounds++
    }
    toast('同步完成')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    state.syncing = false
    state.syncLabel = ''
  }
}

export async function loadDigest(force = false) {
  state.digestLoading = true
  state.digestError = ''
  try {
    const d = await api.digest(force)
    state.digest = d.text
  } catch (e) {
    state.digestError = e.message
  } finally {
    state.digestLoading = false
  }
}

export function openCompose(draft = null) {
  state.composeDraft = draft || {
    mailbox_id: state.mailboxes[0] ? state.mailboxes[0].id : '',
    to: '', cc: '', subject: '', body: '', showCc: false,
  }
  state.composing = true
}

export function replyToSelected() {
  if (!state.selected) return
  const e = state.selected
  const quoted = `\n\n—— 原邮件 ——\n发件人：${e.from_name || ''} <${e.from_addr}>\n时间：${new Date(e.date).toLocaleString('zh-CN')}\n\n${e.text || ''}`
  openCompose({
    mailbox_id: e.mailbox_id,
    to: e.from_addr,
    cc: '',
    subject: e.subject.startsWith('Re:') ? e.subject : 'Re: ' + e.subject,
    body: quoted,
    showCc: false,
  })
}
