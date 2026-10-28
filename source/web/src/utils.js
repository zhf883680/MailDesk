export function fmtTime(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const now = new Date()
  const sameDay = d.toDateString() === now.toDateString()
  const yest = new Date(now)
  yest.setDate(now.getDate() - 1)
  if (sameDay) {
    return d.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
  }
  if (d.toDateString() === yest.toDateString()) return '昨天'
  if (d.getFullYear() === now.getFullYear()) {
    return `${d.getMonth() + 1}月${d.getDate()}日`
  }
  return `${d.getFullYear()}/${d.getMonth() + 1}/${d.getDate()}`
}

export function fmtFull(iso) {
  if (!iso) return ''
  const d = new Date(iso)
  const week = ['日', '一', '二', '三', '四', '五', '六'][d.getDay()]
  return `${d.getFullYear()}年${d.getMonth() + 1}月${d.getDate()}日 周${week} ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
}

export function dayGroup(iso) {
  const d = new Date(iso)
  const now = new Date()
  const start = new Date(now.getFullYear(), now.getMonth(), now.getDate())
  if (d >= start) return '今天'
  if (d >= new Date(start.getTime() - 86400000)) return '昨天'
  if (d >= new Date(start.getTime() - 6 * 86400000)) return '本周'
  return '更早'
}

export function initials(name, addr) {
  const src = (name || addr || '?').trim()
  if (!src) return '?'
  // 头像只放一个字：跳过【】、空格、英文等装饰，取首个汉字，
  // 例如「淘宝网」→「淘」、「【速界】Github日报」→「速」
  const cjk = src.replace(/[^\u4e00-\u9fa5]/g, '')
  if (cjk) return cjk.slice(0, 1)
  const parts = src.replace(/[^\w\s.@-]/g, '').split(/[\s.@_-]+/).filter(Boolean)
  if (parts.length) return parts[0][0].toUpperCase()
  return src.slice(0, 1).toUpperCase()
}

const palette = ['#0f6b5c', '#b0761c', '#5b5bd6', '#9c4221', '#2c6e9b', '#7a5c3e', '#a03d63', '#3f6b3f']
export function colorFor(seed) {
  let h = 0
  const s = seed || ''
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return palette[h % palette.length]
}

export function senderAvatar(name, addr) {
  const seed = String(addr || name || '').trim().toLowerCase()
  return {
    label: initials(name, addr),
    color: colorFor(seed),
  }
}

export function importanceLabel(v) {
  return { high: '重要', medium: '一般', low: '低' }[v] || ''
}

export function hasTranslation(mail) {
  return !!(mail && (mail.translated_md || mail.translated_html || mail.translated))
}

// 阅读页默认视图：有译文先看中文；没有译文时优先展示邮件原有的版式/正文。
export function initialBodyView(mail) {
  if (!mail) return 'zh'
  if (hasTranslation(mail)) return 'zh'
  if (mail.html) return 'html'
  if (mail.text) return 'text'
  return 'zh'
}

// 纯文本正文显示什么：请求原文时显示原文，否则显示译文；
// 两者都没有时留空，交给阅读页提示用户点击「翻译」。
export function plainBodyKind(mail, view) {
  if (!mail) return 'empty'
  if (view === 'text') return mail.text ? 'original' : 'empty'
  return mail.translated ? 'translation' : 'empty'
}

// 附件大小：18432 → "18 KB"，245760 → "240 KB"，2.4MB → "2.4 MB"
export function fmtSize(bytes) {
  const n = Number(bytes)
  if (!Number.isFinite(n) || n <= 0) return ''
  if (n < 1024) return `${Math.round(n)} B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)} KB`
  const mb = n / (1024 * 1024)
  return `${mb < 10 ? mb.toFixed(1) : Math.round(mb)} MB`
}

function sameFormValue(a, b) {
  if (Object.is(a, b)) return true
  if (Array.isArray(a) || Array.isArray(b)) {
    if (!Array.isArray(a) || !Array.isArray(b) || a.length !== b.length) return false
    return a.every((value, index) => sameFormValue(value, b[index]))
  }
  if (!a || !b || typeof a !== 'object' || typeof b !== 'object') return false
  const aKeys = Object.keys(a)
  const bKeys = Object.keys(b)
  if (aKeys.length !== bKeys.length) return false
  return aKeys.every((key) => Object.hasOwn(b, key) && sameFormValue(a[key], b[key]))
}

export function shouldDismissOnBackdrop(initial, current) {
  return sameFormValue(initial, current)
}

export function escapeHTML(s) {
  return String(s || '').replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]))
}

import { marked } from 'marked'
import DOMPurify from 'dompurify'

marked.setOptions({ gfm: true, breaks: true, headerIds: false, mangle: false })

// 渲染 AI 返回的 Markdown（支持标题、列表、表格、代码块），并做 XSS 清洗
export function renderMarkdownish(s) {
  const src = String(s || '').replace(/\r\n/g, '\n')
  if (!src.trim()) return ''
  let html
  try {
    html = marked.parse(src, { async: false })
  } catch (e) {
    return escapeHTML(src).replace(/\n/g, '<br>')
  }
  html = DOMPurify.sanitize(html, {
    USE_PROFILES: { html: true },
    ADD_ATTR: ['target', 'rel'],
  })
  return html.replace(/<a /g, '<a target="_blank" rel="noopener noreferrer" ')
}

