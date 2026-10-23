<script setup>
import { ref, computed, watch } from 'vue'
import Icon from './Icon.vue'
import {
  state, toggleStar, setSeen, removeSelected, reanalyze, replyToSelected, toast,
} from '../store'
import { api } from '../api'
import { fmtFull, fmtTime, initials, colorFor, importanceLabel, renderMarkdownish } from '../utils'

const bodyLang = ref('raw') // raw=原文, zh=译文
const translating = ref(false)
const confirmDelete = ref(false)

const mail = computed(() => state.selected)
const hasTrans = computed(() => !!(mail.value && (mail.value.translated_html || mail.value.translated)))
const mailbox = computed(() => state.mailboxes.find((m) => m.id === mail.value?.mailbox_id))

watch(() => state.selected?.id, () => {
  bodyLang.value = 'raw'
  translating.value = false
  confirmDelete.value = false
})

async function showTranslated() {
  bodyLang.value = 'zh'
  if (hasTrans.value || translating.value || !mail.value) return
  translating.value = true
  try {
    const e = await api.translate(mail.value.id)
    if (state.selected?.id === e.id) state.selected = e
  } catch (err) {
    toast(err.message, 'err')
    bodyLang.value = 'raw'
  } finally {
    translating.value = false
  }
}

const aiDone = computed(() => mail.value && mail.value.ai_status === 'done')
const aiPending = computed(() => mail.value && (mail.value.ai_status === 'pending' || mail.value.ai_status === 'running'))
const aiOff = computed(() => !state.ai.enabled && !mail.value?.summary && !mail.value?.translated)
const lastForward = computed(() => {
  const logs = mail.value?.forward_log || []
  return logs.length ? logs[logs.length - 1] : null
})
const forwarding = ref(false)

async function retryForward() {
  if (!mail.value) return
  forwarding.value = true
  try {
    await api.forwardEmail(mail.value.id)
    toast('已重新推送到 Bark')
    setTimeout(async () => {
      try {
        const e = await api.email(mail.value.id)
        if (state.selected?.id === e.id) state.selected = e
      } catch (err) { /* ignore */ }
    }, 1800)
  } catch (e) {
    toast(e.message, 'err')
  } finally {
    forwarding.value = false
  }
}

function avatarColor() {
  return colorFor(mail.value?.from_addr || mail.value?.from_name)
}

function backToList() {
  state.mobilePane = 'list'
}

async function askDelete() {
  if (!confirmDelete.value) {
    confirmDelete.value = true
    setTimeout(() => { confirmDelete.value = false }, 4000)
    return
  }
  await removeSelected()
  confirmDelete.value = false
}

function toList() {
  const list = (mail.value?.to || []).map((a) => a.name || a.addr)
  return list.join('、')
}
</script>

<template>
  <section class="reader-pane" :class="{ mobile: state.mobilePane === 'reader' }">
    <div v-if="!mail && !state.readerLoading" class="reader-empty">
      <div class="glyph"><Icon name="mail" :size="30" :stroke="1.3" /></div>
      <h3>选择一封邮件</h3>
      <p>AI 会在这里给出中文摘要、重要程度与是否需要处理，英文邮件同时给出中文译文与原文对照。</p>
    </div>

    <div v-else-if="state.readerLoading && !mail" class="reader-inner">
      <div class="skeleton" style="height: 26px; width: 70%; margin-bottom: 14px"></div>
      <div class="skeleton" style="height: 13px; width: 40%; margin-bottom: 26px"></div>
      <div class="skeleton" style="height: 84px; margin-bottom: 18px"></div>
      <div v-for="i in 5" :key="i" class="skeleton" style="height: 12px; margin-bottom: 11px"></div>
    </div>

    <div v-else class="reader-inner">
      <div class="reader-top">
        <button class="icon-btn mobile-only" @click="backToList" aria-label="返回列表">
          <Icon name="arrowLeft" :size="19" />
        </button>
        <div class="grow"></div>
        <button class="icon-btn" :class="{ active: mail.flagged }" title="标记星标" @click="toggleStar">
          <Icon name="star" :size="17" :filled="mail.flagged" />
        </button>
        <button class="icon-btn" :title="mail.seen ? '标为未读' : '标为已读'" @click="setSeen(!mail.seen)">
          <Icon :name="mail.seen ? 'mail' : 'check'" :size="17" />
        </button>
        <button class="icon-btn" title="AI 重新分析" @click="reanalyze">
          <Icon name="sparkles" :size="17" />
        </button>
        <button class="icon-btn" title="回复" @click="replyToSelected">
          <Icon name="reply" :size="17" />
        </button>
        <button class="icon-btn" :class="{ active: confirmDelete }" :title="confirmDelete ? '再次点击确认删除' : '删除'" @click="askDelete">
          <Icon name="trash" :size="16" />
        </button>
      </div>

      <div v-if="confirmDelete" class="ai-error" style="margin-bottom: 12px">
        <Icon name="alert" :size="14" /> 再次点击删除按钮确认，邮件将移入垃圾箱。
      </div>

      <h1 class="reader-subject">{{ mail.subject_zh || mail.subject }}</h1>
      <p v-if="mail.subject_zh && mail.subject_zh !== mail.subject" class="reader-subject-zh">{{ mail.subject }}</p>

      <div class="sender-row">
        <div class="avatar" :style="{ background: avatarColor() }">{{ initials(mail.from_name, mail.from_addr) }}</div>
        <div class="sender-info">
          <div class="sender-name">
            {{ mail.from_name || mail.from_addr }}
            <span v-if="mailbox" class="badge" :style="{ background: 'var(--paper-2)', color: 'var(--muted)' }">
              <span class="tag-dot" :style="{ background: mailbox.color }"></span>{{ mailbox.name }}
            </span>
          </div>
          <div class="sender-addr">{{ mail.from_addr }}</div>
          <div class="sender-addr" style="margin-top: 4px" v-if="toList()">发给 {{ toList() }}</div>
        </div>
        <div class="sender-date">{{ fmtFull(mail.date) }}</div>
      </div>

      <!-- AI 解读 -->
      <div v-if="aiOff" class="ai-panel" style="background: var(--surface-2); border-color: var(--line)">
        <div class="ai-head" style="color: var(--muted)"><Icon name="sparkles" :size="13" /> AI 未启用</div>
        <div class="ai-summary" style="color: var(--muted)">
          在「设置 · AI 引擎」填入 OpenAI 兼容接口后，这里会自动生成中文摘要、重要等级与待办判断。
        </div>
      </div>

      <div v-else class="ai-panel">
        <div class="ai-head">
          <Icon name="sparkles" :size="13" />
          <span>AI 解读</span>
          <span class="hint" style="margin-left: auto; letter-spacing: 0; text-transform: none; font-weight: 400; color: var(--muted)">
            {{ state.ai.model }}
          </span>
        </div>

        <div v-if="aiPending" class="ai-wait">
          <Icon name="refresh" :size="14" class="spin" />
          <span>正在分析邮件：翻译、判断重要程度与待办事项…</span>
        </div>

        <div v-else-if="mail.ai_status === 'error'" class="ai-error">
          <Icon name="alert" :size="14" />
          <div>
            AI 处理失败：{{ mail.ai_error || '未知错误' }}
            <button class="btn btn-sm" style="margin-left: 8px" @click="reanalyze">重试</button>
          </div>
        </div>

        <template v-else>
          <div class="ai-summary">{{ mail.summary || '这封邮件没有生成摘要。' }}</div>
          <div class="ai-meta">
            <span class="label">判断</span>
            <span v-if="mail.importance" class="badge" :class="'badge-' + mail.importance">
              {{ mail.importance === 'high' ? '重要 · 优先' : importanceLabel(mail.importance) }}
            </span>
            <span class="badge" :class="mail.need_action ? 'badge-action' : 'badge-low'">
              {{ mail.need_action ? '需要处理' : '无需处理' }}
            </span>
            <span v-if="mail.is_ad" class="badge badge-ad">广告 / 推广</span>
            <span v-if="mail.category" class="badge badge-low">{{ mail.category }}</span>
            <span v-if="hasTrans" class="badge badge-ai"><Icon name="languages" :size="10" /> 已译成中文</span>
          </div>
        </template>
      </div>

      <div v-if="lastForward" class="forward-note" :class="{ err: !lastForward.ok }">
        <Icon name="bell" :size="12" />
        <template v-if="lastForward.ok">
          已通过「{{ lastForward.rule_name }}」推送到 Bark
        </template>
        <template v-else>
          Bark 推送失败：{{ lastForward.error }}
          <button class="btn btn-sm" :disabled="forwarding" @click="retryForward">重新推送</button>
        </template>
      </div>

      <!-- 自定义规则结果 -->
      <div v-for="r in (mail.rule_results || [])" :key="r.rule_id" class="rule-card" :style="{ borderLeftColor: r.color || 'var(--accent)' }">
        <div class="rule-head">
          <Icon name="tag" :size="13" :style="{ color: r.color || 'var(--accent)' }" />
          {{ r.rule_name }}
          <span class="tag">自定义规则</span>
        </div>
        <div v-if="r.error" class="ai-error"><Icon name="alert" :size="13" />{{ r.error }}</div>
        <div v-else class="rule-body" v-html="renderMarkdownish(r.content)"></div>
      </div>

      <!-- 正文：原文 / 整页译文 切换 -->
      <div class="mail-body-wrap">
        <div class="body-toolbar">
          <div class="seg">
            <button :class="{ on: bodyLang === 'raw' }" @click="bodyLang = 'raw'">原文</button>
            <button :class="{ on: bodyLang === 'zh' }" @click="showTranslated">
              <Icon v-if="translating" name="refresh" :size="11" class="spin" />
              译文
            </button>
          </div>
          <span v-if="hasTrans && mail.subject_zh" class="trans-note" style="margin-bottom: 0"><Icon name="languages" :size="11" /> {{ mail.subject_zh }}</span>
          <div class="grow" style="flex: 1"></div>
        </div>

        <template v-if="mail.html">
          <div v-if="translating" class="html-translating">
            <Icon name="refresh" :size="15" class="spin" />
            正在翻译整页内容，保持原网页排版…（约需十几秒）
          </div>
          <iframe
            v-else
            class="html-frame"
            :srcdoc="bodyLang === 'zh' && mail.translated_html ? mail.translated_html : mail.html"
            sandbox="allow-popups allow-popups-to-escape-sandbox"
            referrerpolicy="no-referrer"
          ></iframe>
          <div v-if="bodyLang === 'zh' && !mail.translated_html && hasTrans" class="mail-body translated" style="margin-top: 4px">{{ mail.translated }}</div>
        </template>

        <template v-else>
          <div v-if="translating" class="html-translating">
            <Icon name="refresh" :size="15" class="spin" /> 正在翻译…
          </div>
          <div v-else-if="bodyLang === 'zh' && mail.translated" class="mail-body translated">{{ mail.translated }}</div>
          <div v-else class="mail-body">{{ mail.text || '（无文本内容）' }}</div>
        </template>
      </div>
    </div>
  </section>
</template>
