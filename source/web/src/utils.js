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
  if (/[\u4e00-\u9fa5]/.test(src)) return src.slice(-2)
  const parts = src.replace(/[^\w\s.@-]/g, '').split(/[\s.@_-]+/).filter(Boolean)
  if (parts.length >= 2) return (parts[0][0] + parts[1][0]).toUpperCase()
  return src.slice(0, 2).toUpperCase()
}

const palette = ['#0f6b5c', '#b0761c', '#5b5bd6', '#9c4221', '#2c6e9b', '#7a5c3e', '#a03d63', '#3f6b3f']
export function colorFor(seed) {
  let h = 0
  const s = seed || ''
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) >>> 0
  return palette[h % palette.length]
}

export function importanceLabel(v) {
  return { high: '重要', medium: '一般', low: '低' }[v] || ''
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

