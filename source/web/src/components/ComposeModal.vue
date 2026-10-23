<script setup>
import { reactive, ref, onMounted, onBeforeUnmount, computed } from 'vue'
import Icon from './Icon.vue'
import { api } from '../api'
import { state, toast } from '../store'

const draft = reactive({ ...state.composeDraft })
const sending = ref(false)
const polishing = ref(false)
const err = ref('')
const showCc = ref(!!(draft.cc || '').trim())

const fromMailbox = computed(() => state.mailboxes.find((m) => m.id === draft.mailbox_id))

const emailRe = /^[^\s@,;]+@[^\s@,;]+\.[^\s@,;]+$/

function parseList(s) {
  return String(s || '')
    .split(/[,;，；\s]+/)
    .map((x) => x.trim())
    .filter(Boolean)
}

function close() {
  state.composing = false
}

async function polish() {
  if (!draft.body.trim()) { toast('先写点内容再润色吧', 'err'); return }
  polishing.value = true
  try {
    const r = await api.polish({ text: draft.body })
    draft.body = r.text
    toast('AI 已润色正文')
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    polishing.value = false
  }
}

async function send() {
  err.value = ''
  const to = parseList(draft.to)
  const cc = parseList(draft.cc)
  if (!draft.mailbox_id) { err.value = '请选择发件邮箱'; return }
  if (!to.length) { err.value = '请填写收件人'; return }
  const bad = [...to, ...cc].filter((a) => !emailRe.test(a))
  if (bad.length) { err.value = `邮箱格式有问题：${bad.join('、')}`; return }
  sending.value = true
  try {
    await api.send({
      mailbox_id: draft.mailbox_id,
      to,
      cc,
      subject: draft.subject,
      body: draft.body,
    })
    toast('邮件已发送')
    state.composing = false
  } catch (e) {
    err.value = e.message
  } finally {
    sending.value = false
  }
}

function onKey(e) {
  if (e.key === 'Escape') close()
  if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') send()
}

onMounted(() => {
  window.addEventListener('keydown', onKey)
  document.body.style.overflow = 'hidden'
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  document.body.style.overflow = ''
})
</script>

<template>
  <div class="modal-backdrop" @click.self="close">
    <div class="modal wide">
      <div class="modal-head">
        <h3>写邮件</h3>
        <button class="icon-btn" @click="close" aria-label="关闭"><Icon name="close" :size="18" /></button>
      </div>

      <div class="modal-body">
        <div class="compose-row">
          <label>发件</label>
          <select v-model="draft.mailbox_id" class="select" style="height: 32px; border: none; background: transparent; padding-left: 0">
            <option v-for="mb in state.mailboxes" :key="mb.id" :value="mb.id">{{ mb.name }} · {{ mb.email }}</option>
          </select>
          <span v-if="fromMailbox" class="tag-dot" :style="{ background: fromMailbox.color }"></span>
        </div>

        <div class="compose-row">
          <label>收件</label>
          <input v-model="draft.to" placeholder="alex@example.com，多个地址用逗号分隔" />
          <button class="btn btn-sm btn-ghost" @click="showCc = !showCc">抄送</button>
        </div>

        <div v-if="showCc" class="compose-row">
          <label>抄送</label>
          <input v-model="draft.cc" placeholder="可选" />
        </div>

        <div class="compose-row">
          <label>主题</label>
          <input v-model="draft.subject" placeholder="邮件主题" />
        </div>

        <textarea v-model="draft.body" class="compose-body" placeholder="写点什么…"></textarea>

        <div v-if="err" class="ai-error" style="margin-bottom: 8px">
          <Icon name="alert" :size="14" /> {{ err }}
        </div>

        <div class="compose-tools">
          <button class="btn btn-sm" :disabled="polishing" @click="polish">
            <Icon name="sparkles" :size="13" :class="{ spin: polishing }" /> {{ polishing ? '润色中…' : 'AI 润色' }}
          </button>
          <span style="font-size: 11.5px; color: var(--muted)">Ctrl / ⌘ + Enter 发送</span>
        </div>
      </div>

      <div class="modal-foot">
        <button class="btn btn-primary" :disabled="sending" @click="send">
          <Icon v-if="!sending" name="send" :size="14" />
          <Icon v-else name="refresh" :size="14" class="spin" />
          {{ sending ? '发送中…' : '发送邮件' }}
        </button>
        <div class="grow"></div>
        <button class="btn btn-ghost" @click="close">取消</button>
      </div>
    </div>
  </div>
</template>
