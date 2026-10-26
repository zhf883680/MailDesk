<script setup>
import { onMounted, onBeforeUnmount, ref, computed, watch } from 'vue'
import LoginView from './components/LoginView.vue'
import Sidebar from './components/Sidebar.vue'
import MailList from './components/MailList.vue'
import MailReader from './components/MailReader.vue'
import DashboardView from './components/DashboardView.vue'
import SettingsView from './components/SettingsView.vue'
import ComposeModal from './components/ComposeModal.vue'
import Icon from './components/Icon.vue'
import Logo from './components/Logo.vue'
import { state, init, reloadBoot, reloadStats, loadEmails, refreshSelected, openCompose, selectEmail, refreshData, toast } from './store'

let timer = null

function openSettings(tab) {
  state.settingsTab = tab || 'mailboxes'
  state.view = 'settings'
  state.sidebarOpen = false
}

let pollCount = 0
async function poll() {
  if (!state.authed) return
  const hadPending = state.stats.pending_ai > 0
  pollCount++
  // 轻量统计每 7 秒刷新；约每分钟再拉一次完整配置，同步邮箱状态与错误信息
  if (pollCount % 9 === 0) await reloadBoot()
  else await reloadStats()
  if (state.view === 'mailbox') {
    if (hadPending || state.stats.pending_ai > 0) {
      await loadEmails()
      if (state.selected && ['pending', 'running'].includes(state.selected.ai_status)) {
        await refreshSelected()
      }
    }
  }
}

onMounted(async () => {
  await init()
  timer = setInterval(poll, 7000)
  window.addEventListener('touchstart', onTouchStart, { passive: true })
  window.addEventListener('touchmove', onTouchMove, { passive: false })
  window.addEventListener('touchend', onTouchEnd)
  window.addEventListener('touchcancel', onTouchEnd)
  document.addEventListener('visibilitychange', onVisible)
  window.addEventListener('pageshow', onPageShow)
})

let notificationTarget = ''
watch(() => state.authed, async (authed) => {
  if (authed) await openNotificationTarget()
})

async function openNotificationTarget() {
  if (!state.authed) return
  const params = new URLSearchParams(window.location.search)
  const id = params.get('email')
  if (!id || notificationTarget === id) return
  notificationTarget = id
  try {
    state.view = 'mailbox'
    state.mailboxId = 'all'
    state.filter = 'all'
    await selectEmail(id)
    await loadEmails()
    window.history.replaceState({}, '', window.location.pathname)
  } finally {
    notificationTarget = ''
  }
}
onBeforeUnmount(() => {
  if (timer) clearInterval(timer)
  window.removeEventListener('touchstart', onTouchStart)
  window.removeEventListener('touchmove', onTouchMove)
  window.removeEventListener('touchend', onTouchEnd)
  window.removeEventListener('touchcancel', onTouchEnd)
  document.removeEventListener('visibilitychange', onVisible)
  window.removeEventListener('pageshow', onPageShow)
})

/* ---------- 下拉刷新 ---------- */
const pullY = ref(0)
const refreshing = ref(false)
const pullState = ref('idle') // idle | pulling | ready | loading
const PULL_TRIGGER = 62
const pullStyle = computed(() => ({
  transform: pullY.value > 0 ? `translateY(${pullY.value}px)` : 'translateY(0)',
  transition: pullState.value === 'pulling' || pullState.value === 'ready' ? 'none' : 'transform 0.22s cubic-bezier(0.22, 1, 0.36, 1)',
}))

let tracking = false
let startY = 0
let scroller = null
let lastResumeRefresh = 0

function findScroller(el) {
  while (el && el !== document.body && el !== document.documentElement) {
    const style = getComputedStyle(el)
    if (/(auto|scroll)/.test(style.overflowY) && el.scrollHeight > el.clientHeight + 4) return el
    el = el.parentElement
  }
  return null
}

function onTouchStart(e) {
  if (e.touches.length !== 1 || pullState.value === 'loading') return
  scroller = findScroller(e.target)
  if (!scroller || scroller.scrollTop > 0) return
  startY = e.touches[0].clientY
  tracking = true
}

function onTouchMove(e) {
  if (!tracking) return
  const dy = e.touches[0].clientY - startY
  if (dy <= 0) {
    if (pullY.value) { pullY.value = 0; pullState.value = 'idle' }
    return
  }
  if (scroller && scroller.scrollTop > 0) { tracking = false; pullY.value = 0; pullState.value = 'idle'; return }
  const d = Math.min(84, dy * 0.45)
  if (d <= 3) return
  pullY.value = d
  pullState.value = d >= PULL_TRIGGER ? 'ready' : 'pulling'
  if (e.cancelable) e.preventDefault()
}

async function onTouchEnd() {
  if (!tracking) return
  tracking = false
  if (pullState.value === 'ready') {
    pullState.value = 'loading'
    pullY.value = PULL_TRIGGER
    try {
      await refreshData()
      toast('已刷新')
    } catch (err) {
      toast(err.message || '刷新失败', 'err')
    } finally {
      pullState.value = 'idle'
      pullY.value = 0
    }
  } else {
    pullState.value = 'idle'
    pullY.value = 0
  }
}

async function manualRefresh() {
  if (refreshing.value) return
  refreshing.value = true
  try {
    await refreshData()
    toast('已刷新')
  } catch (err) {
    toast(err.message || '刷新失败', 'err')
  } finally {
    refreshing.value = false
  }
}

/* ---------- 回到前台自动刷新（iOS 主屏 / 切回标签页） ---------- */async function resumeRefresh() {
  if (!state.authed) return
  if (Date.now() - lastResumeRefresh < 15000) return
  lastResumeRefresh = Date.now()
  try { await refreshData() } catch (e) { /* 静默失败，保留已渲染内容 */ }
}

function onVisible() {
  if (document.visibilityState === 'visible') resumeRefresh()
}

function onPageShow(e) {
  if (e.persisted) resumeRefresh()
}
</script>

<template>
  <div v-if="state.booting" class="boot">
    <div class="boot-mark"><Icon name="mail" :size="22" /></div>
    <div class="boot-name">MailDesk</div>
    <div class="boot-line"><span></span></div>
  </div>

  <LoginView v-else-if="!state.authed" />

  <div v-else class="app-shell">
    <Sidebar @open-settings="openSettings" />
    <div v-if="state.sidebarOpen" class="sidebar-scrim" @click="state.sidebarOpen = false"></div>

    <main class="main-area" :style="pullStyle">
      <template v-if="state.view === 'mailbox'">
        <MailList />
        <MailReader />
      </template>
      <div v-else-if="state.view === 'dashboard'" class="content-pane">
        <div class="m-topbar">
          <button class="icon-btn" @click="state.sidebarOpen = true" aria-label="菜单"><Icon name="menu" :size="21" /></button>
          <div class="m-brand"><Logo :size="24" /><span>MailDesk</span></div>
          <div class="grow"></div>
          <button class="icon-btn" aria-label="刷新" @click="manualRefresh"><Icon name="refresh" :size="19" :class="{ spin: refreshing }" /></button>
          <button class="icon-btn" aria-label="写邮件" @click="openCompose()"><Icon name="pencil" :size="19" /></button>
          <button class="icon-btn" aria-label="设置" @click="openSettings('ai')"><Icon name="settings" :size="20" /></button>
        </div>
        <DashboardView @open-settings="openSettings" />
      </div>
      <div v-else-if="state.view === 'settings'" class="content-pane">
        <SettingsView :initial-tab="state.settingsTab" />
      </div>
    </main>

    <button v-show="state.view !== 'settings' && !(state.view === 'mailbox' && state.mobilePane === 'reader')" class="fab" @click="openCompose()" aria-label="写邮件">
      <Icon name="pencil" :size="19" />
    </button>

    <div v-if="state.authed" class="pull-hint" :class="pullState" :style="{ opacity: pullY > 0 ? 1 : 0 }">
      <Icon name="refresh" :size="15" :class="{ spin: pullState === 'loading' }" />
      <span>{{ pullState === 'loading' ? '正在刷新…' : pullState === 'ready' ? '松开刷新' : '下拉刷新' }}</span>
    </div>

    <ComposeModal v-if="state.composing" />

    <div class="toasts">
      <div v-for="t in state.toasts" :key="t.id" class="toast" :class="{ err: t.type === 'err' }">
        <Icon :name="t.type === 'err' ? 'alert' : 'check'" :size="14" />
        {{ t.message }}
      </div>
    </div>
  </div>
</template>

<style>
.boot {
  height: 100dvh; display: flex; flex-direction: column; align-items: center; justify-content: center;
  gap: 12px; background: var(--paper);
}
.boot-mark {
  width: 46px; height: 46px; border-radius: 14px; display: grid; place-items: center;
  background: linear-gradient(145deg, #2f6f5f, #1a4438); color: #eafff7;
  box-shadow: 0 14px 30px -16px rgba(20, 50, 40, 0.9);
}
.boot-name { font-family: var(--serif); font-size: 19px; letter-spacing: 1px; color: var(--ink-2); }
.boot-line { width: 90px; height: 2px; background: var(--line); border-radius: 2px; overflow: hidden; margin-top: 6px; }
.boot-line span { display: block; width: 40%; height: 100%; background: var(--accent); animation: bootmove 1.1s ease-in-out infinite; }
@keyframes bootmove { 0% { transform: translateX(-100%); } 100% { transform: translateX(250%); } }
.fab { display: none; }
.pull-hint {
  position: fixed; top: 10px; left: 50%; transform: translateX(-50%); z-index: 60;
  display: none; align-items: center; gap: 6px;
  height: 34px; padding: 0 14px; border-radius: 99px;
  background: rgba(20, 32, 28, 0.92); color: #e9f5ef; font-size: 12.5px;
  box-shadow: var(--shadow-lg); pointer-events: none;
  transition: opacity 0.15s ease;
}
.pull-hint.ready { background: #0f6b5c; }
.pull-hint svg { transition: transform 0.2s ease; }
.pull-hint.pulling svg { transform: rotate(0deg); }
.pull-hint.ready svg { transform: rotate(180deg); }
@media (max-width: 980px) {
  .pull-hint { display: flex; }
}
@media (max-width: 980px) {
  .fab {
    display: grid; place-items: center; position: fixed; right: 16px; bottom: calc(20px + env(safe-area-inset-bottom));
    width: 54px; height: 54px; border-radius: 18px; z-index: 40;
    background: linear-gradient(140deg, #2f8571, #1d6152); color: #f2fffa;
    box-shadow: 0 16px 30px -14px rgba(20, 50, 40, 0.85);
  }
  .fab svg { width: 22px; height: 22px; }
}
</style>
