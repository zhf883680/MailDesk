<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import Icon from './Icon.vue'
import { api } from '../api'
import { state, reloadBoot, toast } from '../store'
import {
  currentPushEnvironment,
  pushIssueFor,
  pushSupport,
  enableWebPush,
  getPushSubscription,
  disableWebPush,
} from '../notifications'

const props = defineProps({ initialTab: { type: String, default: 'mailboxes' } })
const tab = ref(props.initialTab)
watch(() => props.initialTab, (v) => { if (v) tab.value = v })

const tabs = [
  { key: 'mailboxes', label: '邮箱接入', icon: 'mail' },
  { key: 'ai', label: 'AI 引擎', icon: 'bot' },
  { key: 'rules', label: '智能规则', icon: 'tag' },
  { key: 'forward', label: '关键词转发', icon: 'bell' },
  { key: 'notifications', label: '浏览器通知', icon: 'bell' },
  { key: 'security', label: '安全与同步', icon: 'shield' },
]

/* ---------------- 邮箱 ---------------- */
const presets = [
  { label: 'Gmail', domain: 'gmail.com', imap: 'imap.gmail.com', iport: 993, smtp: 'smtp.gmail.com', sport: 465 },
  { label: 'Outlook / Hotmail', domain: 'outlook.com', imap: 'outlook.office365.com', iport: 993, smtp: 'smtp.office365.com', sport: 587, starttls: true },
  { label: 'QQ 邮箱', domain: 'qq.com', imap: 'imap.qq.com', iport: 993, smtp: 'smtp.qq.com', sport: 465 },
  { label: '网易 163', domain: '163.com', imap: 'imap.163.com', iport: 993, smtp: 'smtp.163.com', sport: 465 },
  { label: '网易 126', domain: '126.com', imap: 'imap.126.com', iport: 993, smtp: 'smtp.126.com', sport: 465 },
  { label: '阿里企业邮', domain: 'aliyun.com', imap: 'imap.aliyun.com', iport: 993, smtp: 'smtp.aliyun.com', sport: 465 },
  { label: 'iCloud', domain: 'icloud.com', imap: 'imap.mail.me.com', iport: 993, smtp: 'smtp.mail.me.com', sport: 587, starttls: true },
]

const showMbForm = ref(false)
const editingId = ref('')
const mbTesting = ref(false)
const mbSaving = ref(false)
const mbMsg = ref({ text: '', err: false })
const mbForm = reactive({
  name: '', email: '', username: '', password: '',
  imap_host: '', imap_port: 993, imap_tls: true,
  smtp_host: '', smtp_port: 465, smtp_tls: true,
  color: '#0f6b5c',
})
function applyPreset(domain) {
  const p = presets.find((x) => x.domain === domain)
  if (!p) return
  mbForm.imap_host = p.imap
  mbForm.imap_port = p.iport
  mbForm.imap_tls = true
  mbForm.smtp_host = p.smtp
  mbForm.smtp_port = p.sport
  mbForm.smtp_tls = !p.starttls
}

function onEmailBlur() {
  const d = (mbForm.email.split('@')[1] || '').toLowerCase()
  if (d && !mbForm.imap_host) applyPreset(d)
  if (!mbForm.name && mbForm.email) mbForm.name = mbForm.email.split('@')[0]
}

function openAdd() {
  Object.assign(mbForm, {
    name: '', email: '', username: '', password: '',
    imap_host: '', imap_port: 993, imap_tls: true,
    smtp_host: '', smtp_port: 465, smtp_tls: true,
    color: '#0f6b5c',
  })
  editingId.value = ''
  mbMsg.value = { text: '', err: false }
  showMbForm.value = true
}

function openEdit(mb) {
  Object.assign(mbForm, {
    name: mb.name, email: mb.email, username: mb.username || mb.email, password: '',
    imap_host: mb.imap_host, imap_port: mb.imap_port, imap_tls: mb.imap_tls,
    smtp_host: mb.smtp_host, smtp_port: mb.smtp_port, smtp_tls: mb.smtp_tls,
    color: mb.color || '#0f6b5c',
  })
  editingId.value = mb.id
  mbMsg.value = { text: '', err: false }
  showMbForm.value = true
}

async function testMb() {
  mbTesting.value = true
  mbMsg.value = { text: '', err: false }
  try {
    const r = await api.testMailbox(editingId.value || 'new', { ...mbForm })
    mbMsg.value = { text: r.message, err: false }
  } catch (e) {
    mbMsg.value = { text: e.message, err: true }
  } finally {
    mbTesting.value = false
  }
}

async function saveMb() {
  if (!mbForm.email || !mbForm.imap_host || !mbForm.smtp_host) {
    mbMsg.value = { text: '邮箱地址、IMAP 与 SMTP 服务器不能为空', err: true }
    return
  }
  if (!editingId.value && !mbForm.password) {
    mbMsg.value = { text: '首次添加需要填写密码或授权码', err: true }
    return
  }
  mbSaving.value = true
  try {
    await api.saveMailbox(editingId.value, { ...mbForm })
    await reloadBoot()
    showMbForm.value = false
    toast(editingId.value ? '邮箱已更新' : '邮箱已添加，开始同步')
  } catch (e) {
    mbMsg.value = { text: e.message, err: true }
  } finally {
    mbSaving.value = false
  }
}

const pendingDelete = ref('')
async function deleteMb(mb) {
  if (pendingDelete.value !== mb.id) {
    pendingDelete.value = mb.id
    setTimeout(() => { if (pendingDelete.value === mb.id) pendingDelete.value = '' }, 4000)
    return
  }
  try {
    await api.deleteMailbox(mb.id)
    await reloadBoot()
    toast('邮箱已移除')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    pendingDelete.value = ''
  }
}

async function syncOne(mb) {
  try {
    await api.syncMailbox(mb.id)
    toast('已开始同步 ' + mb.name)
    setTimeout(async () => { await reloadBoot() }, 2500)
  } catch (e) {
    toast(e.message, 'err')
  }
}

/* ---------------- AI ---------------- */
const aiForm = reactive({ ...state.ai, api_key: '' })
const adWhiteText = ref((state.ai.ad_whitelist || []).join('\n'))
const aiSaving = ref(false)
const aiTesting = ref(false)
const aiMsg = ref({ text: '', err: false })

async function saveAI() {
  aiSaving.value = true
  aiMsg.value = { text: '', err: false }
  try {
    await api.saveAI({
      ...aiForm,
      ad_whitelist: adWhiteText.value.split(/[\n,，;；]+/).map((s) => s.trim()).filter(Boolean),
    })
    await reloadBoot()
    aiMsg.value = { text: '配置已保存', err: false }
    toast('AI 配置已保存')
  } catch (e) {
    aiMsg.value = { text: e.message, err: true }
  } finally {
    aiSaving.value = false
  }
}

async function testAI() {
  aiTesting.value = true
  aiMsg.value = { text: '', err: false }
  try {
    const r = await api.testAI({ ...aiForm })
    aiMsg.value = { text: r.message, err: false }
  } catch (e) {
    aiMsg.value = { text: e.message, err: true }
  } finally {
    aiTesting.value = false
  }
}

/* ---------------- 规则 ---------------- */
const rules = ref([])
const showRuleForm = ref(false)
const ruleIndex = ref(-1)
const ruleForm = reactive({ name: '', keywords: '', from_contains: '', to_contains: '', prompt: '', enabled: true, color: '#0f6b5c' })
const ruleSaving = ref(false)

onMounted(async () => {
  rules.value = JSON.parse(JSON.stringify(state.rules || []))
  fwdRules.value = JSON.parse(JSON.stringify(state.forwardRules || []))
  Object.assign(aiForm, state.ai, { api_key: '' })
  Object.assign(fwdForm, state.forward, { device_key: '' })
  await refreshPushState()
})

function openRule(i = -1) {
  ruleIndex.value = i
  if (i >= 0) {
    const r = rules.value[i]
    Object.assign(ruleForm, {
      name: r.name, keywords: (r.keywords || []).join('，'), from_contains: r.from_contains || '',
      to_contains: r.to_contains || '', prompt: r.prompt || '', enabled: r.enabled !== false,
      color: r.color || '#0f6b5c',
    })
  } else {
    Object.assign(ruleForm, { name: '', keywords: '', from_contains: '', to_contains: '', prompt: '', enabled: true, color: '#0f6b5c' })
  }
  showRuleForm.value = true
}

function applyRule() {
  if (!ruleForm.name.trim()) { toast('请填写规则名称', 'err'); return }
  if (!ruleForm.prompt.trim()) { toast('请填写提示词', 'err'); return }
  const item = {
    id: ruleIndex.value >= 0 ? rules.value[ruleIndex.value].id : '',
    name: ruleForm.name.trim(),
    keywords: ruleForm.keywords.split(/[,，;；]/).map((s) => s.trim()).filter(Boolean),
    from_contains: ruleForm.from_contains.trim(),
    to_contains: ruleForm.to_contains.trim(),
    prompt: ruleForm.prompt.trim(),
    enabled: ruleForm.enabled,
    color: ruleForm.color,
  }
  if (ruleIndex.value >= 0) rules.value.splice(ruleIndex.value, 1, item)
  else rules.value.push(item)
  showRuleForm.value = false
}

function removeRule(i) {
  rules.value.splice(i, 1)
}

async function saveRules() {
  ruleSaving.value = true
  try {
    await api.saveRules(rules.value)
    await reloadBoot()
    toast('规则已保存，新邮件将按规则处理')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    ruleSaving.value = false
  }
}


/* ---------------- Bark 关键词转发 ---------------- */
const fwdForm = reactive({
  enabled: false, base_url: 'https://api.day.app', device_key: '',
  group: 'MailDesk', sound: '', level: 'active',
})
const fwdSaving = ref(false)
const fwdTesting = ref(false)
const fwdMsg = ref({ text: '', err: false })
const fwdRules = ref([])
const showFwdForm = ref(false)
const fwdIndex = ref(-1)
const fwdRuleSaving = ref(false)
const fwdRuleForm = reactive({
  name: '', keywords: '', from_contains: '', mailbox_ids: [],
  title_template: '', body_template: '', enabled: true, color: '#b0761c',
})

const vCode = '{{code}}'
const vMailbox = '{{mailbox}}'
const vSubjectZh = '{{subject_zh}}'
const vSnippet = '{{snippet}}'

function openFwdRule(i = -1) {
  fwdIndex.value = i
  if (i >= 0) {
    const r = fwdRules.value[i]
    Object.assign(fwdRuleForm, {
      name: r.name,
      keywords: (r.keywords || []).join('，'),
      from_contains: r.from_contains || '',
      mailbox_ids: [...(r.mailbox_ids || [])],
      title_template: r.title_template || '',
      body_template: r.body_template || '',
      enabled: r.enabled !== false,
      color: r.color || '#b0761c',
    })
  } else {
    Object.assign(fwdRuleForm, {
      name: '', keywords: '', from_contains: '', mailbox_ids: [],
      title_template: '', body_template: '', enabled: true, color: '#b0761c',
    })
  }
  showFwdForm.value = true
}

function applyFwdRule() {
  if (!fwdRuleForm.name.trim()) { toast('请填写规则名称', 'err'); return }
  if (!fwdRuleForm.keywords.trim() && !fwdRuleForm.from_contains.trim()) {
    toast('请至少填写关键词或发件人', 'err'); return
  }
  const item = {
    id: fwdIndex.value >= 0 ? fwdRules.value[fwdIndex.value].id : '',
    name: fwdRuleForm.name.trim(),
    keywords: fwdRuleForm.keywords.split(/[,，;；]/).map((x) => x.trim()).filter(Boolean),
    from_contains: fwdRuleForm.from_contains.trim(),
    mailbox_ids: [...fwdRuleForm.mailbox_ids],
    title_template: fwdRuleForm.title_template.trim(),
    body_template: fwdRuleForm.body_template.trim(),
    enabled: fwdRuleForm.enabled,
    color: fwdRuleForm.color,
  }
  if (fwdIndex.value >= 0) fwdRules.value.splice(fwdIndex.value, 1, item)
  else fwdRules.value.push(item)
  showFwdForm.value = false
}

function removeFwdRule(i) { fwdRules.value.splice(i, 1) }

async function saveFwdRules() {
  fwdRuleSaving.value = true
  try {
    await api.saveForwardRules(fwdRules.value)
    await reloadBoot()
    toast('转发规则已保存')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    fwdRuleSaving.value = false
  }
}

async function saveForward() {
  fwdSaving.value = true
  fwdMsg.value = { text: '', err: false }
  try {
    await api.saveForward({ ...fwdForm })
    await reloadBoot()
    fwdMsg.value = { text: '配置已保存', err: false }
    toast('Bark 配置已保存')
  } catch (e) {
    fwdMsg.value = { text: e.message, err: true }
  } finally {
    fwdSaving.value = false
  }
}

async function testForward() {
  fwdTesting.value = true
  fwdMsg.value = { text: '', err: false }
  try {
    await api.saveForward({ ...fwdForm })
    await reloadBoot()
    const r = await api.testForward()
    fwdMsg.value = { text: r.message, err: false }
  } catch (e) {
    fwdMsg.value = { text: e.message, err: true }
  } finally {
    fwdTesting.value = false
  }
}

function fwdScope(r) {
  if (!r.mailbox_ids || !r.mailbox_ids.length) return '全部邮箱'
  return r.mailbox_ids.map((id) => state.mailboxes.find((m) => m.id === id)?.name || id).join('、')
}

/* ---------------- 浏览器通知 ---------------- */
const pushEnv = ref(currentPushEnvironment())
const pushSub = ref(null)
const pushBusy = ref(false)
const pushTesting = ref(false)
const pushMsg = ref({ text: '', err: false })
const pushState = computed(() => pushSupport(pushEnv.value))
const pushIssue = computed(() => pushIssueFor(pushState.value))

async function refreshPushState() {
  pushEnv.value = currentPushEnvironment()
  try {
    pushSub.value = await getPushSubscription()
  } catch (e) {
    pushSub.value = null
  }
}

async function enableNotifications() {
  if (pushState.value !== 'ready' || pushBusy.value) return
  pushBusy.value = true
  pushMsg.value = { text: '', err: false }
  try {
    // 必须在用户点击的同步调用栈里申请权限，iOS 才会弹出系统确认框。
    const permission = await Notification.requestPermission()
    pushEnv.value = currentPushEnvironment()
    if (permission !== 'granted') {
      throw new Error(permission === 'denied' ? '通知权限已被拒绝' : '未获得通知权限')
    }
    await syncCurrentPushSubscription()
    pushMsg.value = { text: '本设备已开启 Web Push', err: false }
    toast('浏览器通知已开启')
  } catch (e) {
    pushMsg.value = { text: e.message || '开启通知失败', err: true }
    await refreshPushState()
  } finally {
    pushBusy.value = false
  }
}

async function syncCurrentPushSubscription() {
  const subscription = await enableWebPush(state.notifications.public_key)
  await api.subscribePush(subscription)
  await reloadBoot()
  pushSub.value = subscription
  return subscription
}

async function disableNotifications() {
  if (pushBusy.value) return
  pushBusy.value = true
  pushMsg.value = { text: '', err: false }
  try {
    const endpoint = pushSub.value?.endpoint
    if (endpoint) await api.unsubscribePush(endpoint)
    await disableWebPush()
    await reloadBoot()
    pushSub.value = null
    pushMsg.value = { text: '本设备通知已关闭', err: false }
    toast('已关闭本设备通知')
  } catch (e) {
    pushMsg.value = { text: e.message || '关闭通知失败', err: true }
  } finally {
    pushBusy.value = false
  }
}

async function testPushNotification() {
  if (pushTesting.value) return
  pushTesting.value = true
  pushMsg.value = { text: '', err: false }
  try {
    if (pushState.value === 'ready' && Notification.permission === 'granted') {
      await syncCurrentPushSubscription()
    } else if (!pushSub.value) {
      throw new Error('当前设备尚未订阅，请先点击「开启 Web Push」')
    }
    const result = await api.testPush()
    pushMsg.value = { text: result.message, err: false }
    toast('测试通知已发送')
  } catch (e) {
    pushMsg.value = { text: e.message || '测试通知发送失败', err: true }
  } finally {
    pushTesting.value = false
  }
}

/* ---------------- 安全 ---------------- */
const sec = reactive({ current: '', next: '', confirm: '' })
const secSaving = ref(false)

async function saveKey() {
  if (sec.next.length < 6) { toast('新密钥至少 6 位', 'err'); return }
  if (sec.next !== sec.confirm) { toast('两次输入的新密钥不一致', 'err'); return }
  secSaving.value = true
  try {
    await api.saveSecurity({ current_key: sec.current, new_key: sec.next })
    toast('访问密钥已更新')
    sec.current = ''; sec.next = ''; sec.confirm = ''
    await reloadBoot()
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    secSaving.value = false
  }
}

const gen = reactive({ sync_minutes: state.general.sync_minutes || 5, fetch_limit: state.general.fetch_limit || 40 })
const genSaving = ref(false)
async function saveGeneral() {
  genSaving.value = true
  try {
    await api.saveGeneral({ ...gen })
    await reloadBoot()
    toast('同步设置已保存')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    genSaving.value = false
  }
}

const mbStats = computed(() => state.stats.by_mailbox || {})
const vSubject = '{{subject}}'
const vFrom = '{{from}}'
const vTo = '{{to}}'
const vBody = '{{body}}'
const vTrans = '{{translation}}'
</script>

<template>
  <div class="page">
    <header class="page-head" style="margin-bottom: 20px">
      <div style="display: flex; align-items: center; gap: 10px">
        <button class="icon-btn mobile-only" @click="state.sidebarOpen = true"><Icon name="menu" :size="19" /></button>
        <div>
          <div class="eyebrow">Settings</div>
          <h1>设置</h1>
        </div>
      </div>
      <div style="font-size: 11.5px; color: var(--muted); text-align: right; line-height: 1.7">
        MailDesk {{ state.version }}<br />数据保存在本机，不上传第三方
      </div>
    </header>

    <div class="settings-shell">
      <nav class="settings-nav">
        <button v-for="t in tabs" :key="t.key" :class="{ active: tab === t.key }" @click="tab = t.key">
          <Icon :name="t.icon" :size="15" /> {{ t.label }}
        </button>
      </nav>

      <div class="settings-body">
        <!-- 邮箱接入 -->
        <template v-if="tab === 'mailboxes'">
          <section class="settings-section">
            <h3>已接入的邮箱</h3>
            <p class="desc">支持任何提供 IMAP / SMTP 的邮箱。QQ、163 等需要使用「授权码」而非登录密码。</p>

            <div v-for="mb in state.mailboxes" :key="mb.id" class="mb-row">
              <span class="mb-dot" :style="{ background: mb.color, width: '10px', height: '10px', borderRadius: '4px' }"></span>
              <div class="mb-row-info">
                <div class="mb-row-name">
                  {{ mb.name }}
                  <span class="badge badge-low">{{ mbStats[mb.id]?.total || 0 }} 封</span>
                  <span v-if="mb.last_error" class="badge badge-high">同步失败</span>
                </div>
                <div class="mb-row-mail">{{ mb.email }} · {{ mb.imap_host }}</div>
                <div v-if="mb.last_error" class="mb-row-err">{{ mb.last_error }}</div>
              </div>
              <button class="icon-btn" title="立即同步" @click="syncOne(mb)"><Icon name="refresh" :size="15" /></button>
              <button class="icon-btn" title="编辑" @click="openEdit(mb)"><Icon name="pencil" :size="15" /></button>
              <button class="icon-btn" :style="pendingDelete === mb.id ? 'color:var(--red)' : ''" :title="pendingDelete === mb.id ? '再次点击确认移除' : '移除'" @click="deleteMb(mb)">
                <Icon :name="pendingDelete === mb.id ? 'alert' : 'trash'" :size="15" />
              </button>
            </div>

            <div v-if="!state.mailboxes.length" class="mb-empty" style="margin-top: 10px">还没有邮箱，点击下方按钮添加。</div>

            <div class="form-actions">
              <button class="btn btn-primary" @click="openAdd"><Icon name="plus" :size="14" /> 添加邮箱</button>
            </div>
          </section>

          <section class="settings-section">
            <h3>常见邮箱服务器</h3>
            <p class="desc">添加邮箱时填写完整邮箱地址，系统会自动套用下列服务器配置。</p>
            <div class="grid-3">
              <div v-for="p in presets" :key="p.domain" style="font-size: 12px; line-height: 1.8; color: var(--muted)">
                <strong style="color: var(--ink-2)">{{ p.label }}</strong><br />
                IMAP {{ p.imap }}:{{ p.iport }}<br />SMTP {{ p.smtp }}:{{ p.sport }}
              </div>
            </div>
          </section>
        </template>

        <!-- AI -->
        <template v-if="tab === 'ai'">
          <section class="settings-section">
            <h3>AI 引擎</h3>
            <p class="desc">兼容 OpenAI 接口规范（/chat/completions）。可填 OpenAI、DeepSeek、Moonshot、one-api 或本地 Ollama 的自定义地址。</p>

            <div class="field" style="margin-bottom: 18px">
              <label class="switch">
                <input type="checkbox" v-model="aiForm.enabled" />
                <span class="track"></span>
                启用 AI 处理（翻译、重要度判断、自定义规则）
              </label>
            </div>

            <div class="field">
              <label>接口地址 Base URL</label>
              <input v-model="aiForm.base_url" class="input" placeholder="https://api.openai.com/v1" />
              <div class="hint">填写到 /v1 即可，程序会自动补全 /chat/completions；也可直接填入完整地址。</div>
            </div>

            <div class="grid-2">
              <div class="field">
                <label>API Key</label>
                <input v-model="aiForm.api_key" type="password" class="input" :placeholder="state.ai.api_key ? '已保存：' + state.ai.api_key : 'sk-...'" />
                <div class="hint">留空表示不修改已保存的 Key。</div>
              </div>
              <div class="field">
                <label>模型</label>
                <input v-model="aiForm.model" class="input" placeholder="gpt-4o-mini" />
                <div class="hint">如 gpt-4o-mini、deepseek-chat、qwen-plus。</div>
              </div>
            </div>

            <div class="grid-2">
              <div class="field">
                <label>温度（0–1）</label>
                <input v-model.number="aiForm.temperature" type="number" min="0" max="1" step="0.1" class="input" />
              </div>
              <div class="field">
                <label>正文截断长度（字符）</label>
                <input v-model.number="aiForm.max_chars" type="number" min="1000" max="20000" step="500" class="input" />
              </div>
            </div>

            <div class="divider"></div>

            <div style="display: flex; gap: 26px; flex-wrap: wrap">
              <label class="switch"><input type="checkbox" v-model="aiForm.translate" /><span class="track"></span>英文邮件翻译成中文</label>
              <label class="switch"><input type="checkbox" v-model="aiForm.classify" /><span class="track"></span>AI 判断重要度 / 待办 / 广告</label>
            </div>

            <div v-if="aiForm.translate" class="field" style="margin-top: 16px; max-width: 380px">
              <label>翻译方式</label>
              <select v-model="aiForm.translate_mode" class="input">
                <option value="auto">默认翻译 —— 收到邮件后自动翻译</option>
                <option value="manual">点击时翻译 —— 阅读时点「译文」才翻译</option>
              </select>
            </div>

            <div class="field" style="margin-top: 16px; max-width: 520px">
              <label>广告白名单（发件人或域名，每行一个）</label>
              <textarea v-model="adWhiteText" class="input" rows="3" placeholder="github.com&#10;notifications@stripe.com"></textarea>
              <div class="hint" style="margin-top: 4px">整域匹配（github.com 同时匹配 mail.github.com），也可填完整邮箱精确匹配；命中的发件人不计入广告，也不会出现在「广告」分类。单封误判可在阅读页点标签图标纠正。</div>
            </div>

            <div class="form-actions">
              <button class="btn btn-primary" :disabled="aiSaving" @click="saveAI"><Icon name="check" :size="14" /> 保存配置</button>
              <button class="btn" :disabled="aiTesting" @click="testAI">
                <Icon name="refresh" :size="14" :class="{ spin: aiTesting }" /> 测试连接
              </button>
              <span class="msg" :class="{ err: aiMsg.err }">{{ aiMsg.text }}</span>
            </div>
          </section>

          <section class="settings-section">
            <h3>处理流程</h3>
            <p class="desc">每封新邮件按以下顺序处理，结果会展示在邮件详情页。</p>
            <div style="display: grid; gap: 12px">
              <div style="display: flex; gap: 12px; font-size: 13px; line-height: 1.7">
                <span class="badge badge-ai">1</span><div><strong>中文翻译</strong> —— 判断为英文邮件时，将主题与正文译成中文，原文保留在下方对照。</div>
              </div>
              <div style="display: flex; gap: 12px; font-size: 13px; line-height: 1.7">
                <span class="badge badge-ai">2</span><div><strong>智能分级</strong> —— 输出一句话摘要、重要度（高/中/低）、是否需要处理、是否广告、分类。</div>
              </div>
              <div style="display: flex; gap: 12px; font-size: 13px; line-height: 1.7">
                <span class="badge badge-ai">3</span><div><strong>自定义规则</strong> —— 命中关键词或收发件人时，用你的提示词让 AI 提炼信息，并以卡片形式展示。</div>
              </div>
            </div>
          </section>
        </template>

        <!-- 规则 -->
        <template v-if="tab === 'rules'">
          <section class="settings-section">
            <h3>智能规则</h3>
            <p class="desc">
              当邮件主题、正文或发件人包含关键词（或收件人匹配）时，用自定义提示词让 AI 处理，并把结果展示在邮件页面上。
              提示词支持变量：<code style="font-size: 11px; margin-right: 5px">{{ vSubject }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vFrom }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vTo }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vBody }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vTrans }}</code>
            </p>

            <div v-for="(r, i) in rules" :key="r.id || i" class="rule-item">
              <span class="tag-dot" :style="{ background: r.color, marginTop: '6px', width: '8px', height: '8px' }"></span>
              <div class="r-main">
                <div class="r-name">
                  {{ r.name }}
                  <span v-if="!r.enabled" class="badge badge-low">已停用</span>
                </div>
                <div class="r-kw">
                  关键词：{{ (r.keywords || []).join('、') || '（未设置）' }}
                  <template v-if="r.from_contains"> · 发件人包含 {{ r.from_contains }}</template>
                  <template v-if="r.to_contains"> · 收件人包含 {{ r.to_contains }}</template>
                </div>
                <div class="r-prompt">{{ r.prompt }}</div>
              </div>
              <button class="icon-btn" @click="openRule(i)"><Icon name="pencil" :size="15" /></button>
              <button class="icon-btn" @click="removeRule(i)"><Icon name="trash" :size="15" /></button>
            </div>

            <div v-if="!rules.length" class="mb-empty" style="margin-top: 8px">
              还没有规则。例如：关键词填「invoice, 发票」，提示词写「提取金额、到期日与付款方式」。
            </div>

            <div class="form-actions">
              <button class="btn" @click="openRule(-1)"><Icon name="plus" :size="14" /> 新建规则</button>
              <button class="btn btn-primary" :disabled="ruleSaving" @click="saveRules"><Icon name="check" :size="14" /> 保存全部规则</button>
            </div>
          </section>
        </template>


        <!-- Bark 关键词转发 -->
        <template v-if="tab === 'forward'">
          <section class="settings-section">
            <h3>Bark 推送</h3>
            <p class="desc">
              命中关键词的<strong>新邮件</strong>会实时推送到 iPhone 的 Bark，适合验证码、安全提醒、客户来信。
              在 App Store 安装 Bark，打开首页复制设备码填入即可。
            </p>

            <div class="field" style="margin-bottom: 18px">
              <label class="switch">
                <input type="checkbox" v-model="fwdForm.enabled" />
                <span class="track"></span>
                启用关键词转发
              </label>
            </div>

            <div class="grid-2">
              <div class="field">
                <label>Bark 服务器</label>
                <input v-model="fwdForm.base_url" class="input" placeholder="https://api.day.app" />
                <div class="hint">官方服务器保持默认；自建 Bark 填自己的地址，也可直接粘贴完整推送地址。</div>
              </div>
              <div class="field">
                <label>设备码 Device Key</label>
                <input
                  v-model="fwdForm.device_key" class="input"
                  :placeholder="state.forward.device_key ? '已保存：' + state.forward.device_key : 'Bark App 首页复制的那串字符'"
                />
                <div class="hint">留空表示不修改已保存的设备码。</div>
              </div>
            </div>

            <div class="grid-3">
              <div class="field">
                <label>通知分组</label>
                <input v-model="fwdForm.group" class="input" placeholder="MailDesk" />
              </div>
              <div class="field">
                <label>提示音（可选）</label>
                <input v-model="fwdForm.sound" class="input" placeholder="例如 alarm" />
              </div>
              <div class="field">
                <label>中断级别</label>
                <select v-model="fwdForm.level" class="select">
                  <option value="active">active · 默认</option>
                  <option value="timeSensitive">timeSensitive · 时效性</option>
                  <option value="critical">critical · 重要警告</option>
                  <option value="passive">passive · 静默</option>
                </select>
              </div>
            </div>

            <div class="form-actions">
              <button class="btn btn-primary" :disabled="fwdSaving" @click="saveForward">
                <Icon name="check" :size="14" /> 保存配置
              </button>
              <button class="btn" :disabled="fwdTesting" @click="testForward">
                <Icon name="bell" :size="14" :class="{ spin: fwdTesting }" /> 发送测试通知
              </button>
              <span class="msg" :class="{ err: fwdMsg.err }">{{ fwdMsg.text }}</span>
            </div>
          </section>

          <section class="settings-section">
            <h3>转发规则</h3>
            <p class="desc">
              关键词命中主题、正文或发件人即转发；可限定只对某些邮箱生效。标题与正文支持变量
              <code style="font-size: 11px; margin-right: 5px">{{ vCode }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vSubjectZh }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vFrom }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vMailbox }}</code>
              <code style="font-size: 11px; margin-right: 5px">{{ vSnippet }}</code>。
            </p>

            <div v-for="(r, i) in fwdRules" :key="r.id || i" class="rule-item">
              <span class="tag-dot" :style="{ background: r.color, marginTop: '6px', width: '8px', height: '8px' }"></span>
              <div class="r-main">
                <div class="r-name">
                  {{ r.name }}
                  <span v-if="!r.enabled" class="badge badge-low">已停用</span>
                  <span class="badge badge-low">{{ fwdScope(r) }}</span>
                </div>
                <div class="r-kw">
                  关键词：{{ (r.keywords || []).join('、') || '（未设置）' }}
                  <template v-if="r.from_contains"> · 发件人包含 {{ r.from_contains }}</template>
                </div>
                <div class="r-prompt" v-if="r.title_template || r.body_template">
                  标题：{{ r.title_template || '（默认：邮件主题）' }}　正文：{{ r.body_template || '（默认：验证码 / 摘要）' }}
                </div>
              </div>
              <button class="icon-btn" @click="openFwdRule(i)"><Icon name="pencil" :size="15" /></button>
              <button class="icon-btn" @click="removeFwdRule(i)"><Icon name="trash" :size="15" /></button>
            </div>

            <div v-if="!fwdRules.length" class="mb-empty" style="margin-top: 8px">
              还没有转发规则。例如：关键词填「验证码，verification code」，正文模板写「{{ vCode }}」。
            </div>

            <div class="form-actions">
              <button class="btn" @click="openFwdRule(-1)"><Icon name="plus" :size="14" /> 新建转发规则</button>
              <button class="btn btn-primary" :disabled="fwdRuleSaving" @click="saveFwdRules">
                <Icon name="check" :size="14" /> 保存全部规则
              </button>
            </div>
          </section>

          <section class="settings-section">
            <h3>转发说明</h3>
            <p class="desc">几个需要知道的细节：</p>
            <div style="display: grid; gap: 10px; font-size: 12.5px; line-height: 1.7; color: var(--ink-2)">
              <div>· 每封邮件最多推送一次；同一个规则命中多封邮件会分别推送。</div>
              <div>· 首次添加邮箱时拉取的历史邮件<strong>不会</strong>推送，避免一次性收到几十条通知。</div>
              <div>· AI 判定为广告的邮件不会转发，规则命中关键词也一样。</div>
              <div>· 启用转发后后台至少每分钟同步一次，验证码通常在 1 分钟内到达手机。</div>
              <div>· 正文模板留空时：检测到验证码会自动发送「验证码：xxxxxx」，否则发送发件人与摘要。</div>
              <div>· 推送失败会在邮件详情页显示原因，可点「重新推送」重试。</div>
            </div>
          </section>

          <!-- 转发规则编辑弹窗 -->
          <div v-if="showFwdForm" class="modal-backdrop">
            <div class="modal">
              <div class="modal-head">
                <h3>{{ fwdIndex >= 0 ? '编辑转发规则' : '新建转发规则' }}</h3>
                <button class="icon-btn" @click="showFwdForm = false"><Icon name="close" :size="18" /></button>
              </div>
              <div class="modal-body">
                <div class="grid-2">
                  <div class="field">
                    <label>规则名称</label>
                    <input v-model="fwdRuleForm.name" class="input" placeholder="验证码即时转发" />
                  </div>
                  <div class="field">
                    <label>标识色</label>
                    <input v-model="fwdRuleForm.color" type="color" style="width: 100%; height: 38px; border: 1px solid var(--line-strong); border-radius: 9px; background: #fff" />
                  </div>
                </div>

                <div class="field">
                  <label>关键词（任一命中即可，逗号分隔）</label>
                  <input v-model="fwdRuleForm.keywords" class="input" placeholder="验证码, verification code, 动态码" />
                  <div class="hint">匹配主题、正文与发件人，不区分大小写。</div>
                </div>

                <div class="field">
                  <label>发件人包含（可选）</label>
                  <input v-model="fwdRuleForm.from_contains" class="input" placeholder="例如 @stripe.com" />
                </div>

                <div class="field">
                  <label>生效邮箱（不选表示全部）</label>
                  <div style="display: flex; flex-wrap: wrap; gap: 12px; padding-top: 2px">
                    <label v-for="mb in state.mailboxes" :key="mb.id" style="display: inline-flex; align-items: center; gap: 6px; font-size: 12.5px; color: var(--ink-2)">
                      <input type="checkbox" :value="mb.id" v-model="fwdRuleForm.mailbox_ids" />
                      <span class="tag-dot" :style="{ background: mb.color }"></span>{{ mb.name }}
                    </label>
                  </div>
                </div>

                <div class="field">
                  <label>推送标题模板（留空用邮件主题）</label>
                  <input v-model="fwdRuleForm.title_template" class="input" :placeholder="'验证码 · ' + vSubjectZh" />
                </div>

                <div class="field">
                  <label>推送正文模板（留空自动识别验证码）</label>
                  <textarea v-model="fwdRuleForm.body_template" class="textarea" style="min-height: 92px" :placeholder="vCode + '\n来自 ' + vFrom + ' · ' + vMailbox"></textarea>
                  <div class="hint">可用变量：{{ vCode }}、{{ vSubjectZh }}、{{ vFrom }}、{{ vMailbox }}、{{ vSnippet }}；留空时自动提取验证码。</div>
                </div>

                <label class="switch"><input type="checkbox" v-model="fwdRuleForm.enabled" /><span class="track"></span>启用此规则</label>
              </div>
              <div class="modal-foot">
                <div class="grow"></div>
                <button class="btn btn-ghost" @click="showFwdForm = false">取消</button>
                <button class="btn btn-primary" @click="applyFwdRule">确定</button>
              </div>
            </div>
          </div>
        </template>

        <!-- 浏览器 Web Push -->
        <template v-if="tab === 'notifications'">
          <section class="settings-section">
            <h3>浏览器通知 · Web Push</h3>
            <p class="desc">
              新邮件到达时发送系统通知，PWA 完全关闭后也能收到。iPhone / iPad 需要 iOS 16.4+、使用 HTTPS，并先添加到主屏幕。
            </p>

            <div style="display: flex; gap: 8px; flex-wrap: wrap; align-items: center; margin-bottom: 18px">
              <span class="badge" :class="pushSub ? 'badge-ai' : 'badge-low'">
                {{ pushSub ? '本设备已订阅' : '本设备未订阅' }}
              </span>
              <span class="badge badge-low">服务端 {{ state.notifications.subscription_count || 0 }} 台设备</span>
              <span v-if="pushEnv.iOS" class="badge" :class="pushEnv.standalone ? 'badge-low' : 'badge-high'">
                {{ pushEnv.standalone ? '已从主屏幕运行' : '尚未添加到主屏幕' }}
              </span>
            </div>

            <div v-if="pushIssue" class="ai-error" style="margin-bottom: 18px">
              <Icon name="alert" :size="14" />
              <div>{{ pushIssue }}</div>
            </div>

            <div class="form-actions">
              <button
                v-if="!pushSub"
                class="btn btn-primary"
                :disabled="pushBusy || pushState !== 'ready'"
                @click="enableNotifications"
              >
                <Icon name="bell" :size="14" :class="{ spin: pushBusy }" />
                {{ pushBusy ? '正在申请权限…' : '开启 Web Push' }}
              </button>
              <button v-else class="btn btn-primary" :disabled="pushBusy" @click="enableNotifications">
                <Icon name="refresh" :size="14" :class="{ spin: pushBusy }" /> 重新同步订阅
              </button>
              <button v-if="state.notifications.subscription_count" class="btn" :disabled="pushTesting" @click="testPushNotification">
                <Icon name="bell" :size="14" /> {{ pushTesting ? '正在发送…' : pushSub ? '发送测试通知' : '绑定本机并测试' }}
              </button>
              <button v-if="pushSub" class="btn btn-danger" :disabled="pushBusy" @click="disableNotifications">关闭本设备通知</button>
              <span class="msg" :class="{ err: pushMsg.err }">{{ pushMsg.text }}</span>
            </div>
          </section>

          <section class="settings-section">
            <h3>通知规则</h3>
            <p class="desc">通知与「关键词转发」相互独立，可以同时开启。</p>
            <div style="display: grid; gap: 10px; font-size: 12.5px; line-height: 1.7; color: var(--ink-2)">
              <div>· 默认通知每封新邮件，标题优先使用中文主题，正文只显示 AI 一句话摘要。</div>
              <div>· AI 判定为广告的邮件不会通知；等分类结果通常需要几秒。</div>
              <div>· 首次添加邮箱时同步到的历史邮件不会通知。</div>
              <div>· MailDesk 正显示在前台时静默，避免重复打扰；点击系统通知会直接打开对应邮件。</div>
              <div>· 同一封邮件只推送一次；失效的浏览器订阅会自动清理。</div>
              <div>· Web Push 订阅保存在本机，通知内容经加密后由浏览器推送服务转发。</div>
            </div>
          </section>
        </template>

        <!-- 安全 -->
        <template v-if="tab === 'security'">
          <section class="settings-section">
            <h3>访问密钥</h3>
            <p class="desc">登录此邮件台所需的唯一密钥。修改后请立即记下，遗忘只能通过命令行重置。</p>

            <div class="field">
              <label>当前密钥</label>
              <input v-model="sec.current" type="password" class="input" placeholder="请输入当前密钥" />
            </div>
            <div class="grid-2">
              <div class="field">
                <label>新密钥（至少 6 位）</label>
                <input v-model="sec.next" type="password" class="input" placeholder="新密钥" />
              </div>
              <div class="field">
                <label>确认新密钥</label>
                <input v-model="sec.confirm" type="password" class="input" placeholder="再次输入" />
              </div>
            </div>
            <div class="form-actions">
              <button class="btn btn-primary" :disabled="secSaving" @click="saveKey"><Icon name="shield" :size="14" /> 更新密钥</button>
              <span v-if="state.keyIsDefault" class="msg err">当前仍在使用默认密钥 admin123，建议立即修改</span>
            </div>
          </section>

          <section class="settings-section">
            <h3>登录保护</h3>
            <p class="desc">防暴力破解策略，规则如下：</p>
            <div style="display: grid; gap: 10px; font-size: 12.5px; line-height: 1.7; color: var(--ink-2)">
              <div>· 同一 IP 连续 <strong>3 次</strong> 输入错误密钥后，该 IP 立即锁定。</div>
              <div>· 锁定记录仅保存在内存中，<strong>重启程序</strong>才会解除，界面不会提供解锁入口。</div>
              <div>· 锁定只针对来源 IP，不影响其他设备登录。</div>
              <div>· 邮箱密码使用 AES-256-GCM 加密后存储在本地 <code>data/config.json</code> 中。</div>
            </div>
          </section>

          <section class="settings-section">
            <h3>同步设置</h3>
            <p class="desc">程序会按间隔自动拉取各邮箱收件箱的最新邮件。</p>
            <div class="grid-2">
              <div class="field">
                <label>自动同步间隔（分钟）</label>
                <input v-model.number="gen.sync_minutes" type="number" min="1" max="1440" class="input" />
              </div>
              <div class="field">
                <label>每个邮箱拉取封数</label>
                <input v-model.number="gen.fetch_limit" type="number" min="5" max="200" class="input" />
              </div>
            </div>
            <div class="form-actions">
              <button class="btn btn-primary" :disabled="genSaving" @click="saveGeneral"><Icon name="check" :size="14" /> 保存</button>
            </div>
          </section>
        </template>
      </div>
    </div>

    <!-- 邮箱编辑弹窗 -->
    <div v-if="showMbForm" class="modal-backdrop">
      <div class="modal">
        <div class="modal-head">
          <h3>{{ editingId ? '编辑邮箱' : '添加邮箱' }}</h3>
          <button class="icon-btn" @click="showMbForm = false"><Icon name="close" :size="18" /></button>
        </div>
        <div class="modal-body">
          <div class="grid-2">
            <div class="field">
              <label>显示名称</label>
              <input v-model="mbForm.name" class="input" placeholder="工作邮箱" />
            </div>
            <div class="field">
              <label>邮箱地址</label>
              <input v-model="mbForm.email" class="input" placeholder="you@example.com" @blur="onEmailBlur" />
            </div>
          </div>

          <div class="field">
            <label>快速套用服务器配置</label>
            <select class="select" @change="applyPreset($event.target.value)">
              <option value="">选择邮箱服务商…</option>
              <option v-for="p in presets" :key="p.domain" :value="p.domain">{{ p.label }}（{{ p.domain }}）</option>
            </select>
          </div>

          <div class="grid-2">
            <div class="field">
              <label>登录账号 / 用户名</label>
              <input v-model="mbForm.username" class="input" placeholder="默认同邮箱地址" />
            </div>
            <div class="field">
              <label>密码 / 授权码</label>
              <input v-model="mbForm.password" type="password" class="input" :placeholder="editingId ? '留空则不修改' : 'QQ、163 请填写授权码'" />
            </div>
          </div>

          <div class="divider"></div>

          <div class="grid-3">
            <div class="field">
              <label>IMAP 服务器</label>
              <input v-model="mbForm.imap_host" class="input" placeholder="imap.example.com" />
            </div>
            <div class="field">
              <label>端口</label>
              <input v-model.number="mbForm.imap_port" type="number" class="input" />
            </div>
            <div class="field">
              <label>加密方式</label>
              <select v-model="mbForm.imap_tls" class="select">
                <option :value="true">SSL / TLS (993)</option>
                <option :value="false">STARTTLS (143)</option>
              </select>
            </div>
          </div>

          <div class="grid-3">
            <div class="field">
              <label>SMTP 服务器</label>
              <input v-model="mbForm.smtp_host" class="input" placeholder="smtp.example.com" />
            </div>
            <div class="field">
              <label>端口</label>
              <input v-model.number="mbForm.smtp_port" type="number" class="input" />
            </div>
            <div class="field">
              <label>加密方式</label>
              <select v-model="mbForm.smtp_tls" class="select">
                <option :value="true">SSL / TLS (465)</option>
                <option :value="false">STARTTLS (587)</option>
              </select>
            </div>
          </div>

          <div class="field">
            <label>标识色</label>
            <div style="display: flex; gap: 8px; align-items: center">
              <input v-model="mbForm.color" type="color" style="width: 40px; height: 32px; border: 1px solid var(--line-strong); border-radius: 8px; background: #fff" />
              <span style="font-size: 12px; color: var(--muted)">用于区分不同邮箱</span>
            </div>
          </div>

          <div v-if="mbMsg.text" class="msg" :class="{ err: mbMsg.err }" style="font-size: 12px; color: var(--accent)" :style="mbMsg.err ? 'color:var(--red)' : ''">
            {{ mbMsg.text }}
          </div>
        </div>
        <div class="modal-foot">
          <button class="btn" :disabled="mbTesting" @click="testMb">
            <Icon name="refresh" :size="14" :class="{ spin: mbTesting }" /> 测试连接
          </button>
          <div class="grow"></div>
          <button class="btn btn-ghost" @click="showMbForm = false">取消</button>
          <button class="btn btn-primary" :disabled="mbSaving" @click="saveMb">
            <Icon name="check" :size="14" /> {{ mbSaving ? '保存中…' : '保存' }}
          </button>
        </div>
      </div>
    </div>

    <!-- 规则编辑弹窗 -->
    <div v-if="showRuleForm" class="modal-backdrop">
      <div class="modal">
        <div class="modal-head">
          <h3>{{ ruleIndex >= 0 ? '编辑规则' : '新建规则' }}</h3>
          <button class="icon-btn" @click="showRuleForm = false"><Icon name="close" :size="18" /></button>
        </div>
        <div class="modal-body">
          <div class="grid-2">
            <div class="field">
              <label>规则名称</label>
              <input v-model="ruleForm.name" class="input" placeholder="客户询价提取" />
            </div>
            <div class="field">
              <label>标识色</label>
              <input v-model="ruleForm.color" type="color" style="width: 100%; height: 38px; border: 1px solid var(--line-strong); border-radius: 9px; background: #fff" />
            </div>
          </div>

          <div class="field">
            <label>关键词（任一命中即可，用逗号分隔）</label>
            <input v-model="ruleForm.keywords" class="input" placeholder="invoice, 报价, quote, 合同" />
          </div>

          <div class="grid-2">
            <div class="field">
              <label>发件人包含（可选）</label>
              <input v-model="ruleForm.from_contains" class="input" placeholder="例如 @client.com" />
            </div>
            <div class="field">
              <label>收件人包含（可选）</label>
              <input v-model="ruleForm.to_contains" class="input" placeholder="例如 billing@me.com" />
            </div>
          </div>

          <div class="field">
            <label>自定义提示词</label>
            <textarea v-model="ruleForm.prompt" class="textarea" style="min-height: 130px" placeholder="例如：请提取邮件中的金额、到期日、账单周期与付款方式，用 4 个条目输出。"></textarea>
            <div class="hint">可使用变量 {{ vSubject }}、{{ vFrom }}、{{ vBody }}、{{ vTrans }}；不写变量时会自动附上邮件内容。</div>
          </div>

          <label class="switch"><input type="checkbox" v-model="ruleForm.enabled" /><span class="track"></span>启用此规则</label>
        </div>
        <div class="modal-foot">
          <div class="grow"></div>
          <button class="btn btn-ghost" @click="showRuleForm = false">取消</button>
          <button class="btn btn-primary" @click="applyRule">确定</button>
        </div>
      </div>
    </div>
  </div>
</template>
