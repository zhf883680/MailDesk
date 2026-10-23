<script setup>
import { onMounted, onBeforeUnmount } from 'vue'
import LoginView from './components/LoginView.vue'
import Sidebar from './components/Sidebar.vue'
import MailList from './components/MailList.vue'
import MailReader from './components/MailReader.vue'
import DashboardView from './components/DashboardView.vue'
import SettingsView from './components/SettingsView.vue'
import ComposeModal from './components/ComposeModal.vue'
import Icon from './components/Icon.vue'
import { state, init, reloadBoot, loadEmails, refreshSelected, openCompose } from './store'

let timer = null

function openSettings(tab) {
  state.settingsTab = tab || 'mailboxes'
  state.view = 'settings'
  state.sidebarOpen = false
}

async function poll() {
  if (!state.authed) return
  const hadPending = state.stats.pending_ai > 0
  await reloadBoot()
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
})
onBeforeUnmount(() => { if (timer) clearInterval(timer) })
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

    <main class="main-area">
      <template v-if="state.view === 'mailbox'">
        <MailList />
        <MailReader />
      </template>
      <div v-else-if="state.view === 'dashboard'" class="content-pane">
        <DashboardView @open-settings="openSettings" />
      </div>
      <div v-else-if="state.view === 'settings'" class="content-pane">
        <SettingsView :initial-tab="state.settingsTab" />
      </div>
    </main>

    <button v-show="!(state.view === 'mailbox' && state.mobilePane === 'reader')" class="fab" @click="openCompose()" aria-label="写邮件">
      <Icon name="pencil" :size="19" />
    </button>

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
@media (max-width: 980px) {
  .fab {
    display: grid; place-items: center; position: fixed; right: 18px; bottom: 22px;
    width: 52px; height: 52px; border-radius: 18px; z-index: 40;
    background: linear-gradient(140deg, #2f8571, #1d6152); color: #f2fffa;
    box-shadow: 0 16px 30px -14px rgba(20, 50, 40, 0.85);
  }
}
</style>
