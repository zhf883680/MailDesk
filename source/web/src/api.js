const BASE = '/api/'

async function request(path, { method = 'GET', body } = {}) {
  let res
  try {
    res = await fetch(BASE + path, {
      method,
      headers: body ? { 'Content-Type': 'application/json' } : undefined,
      body: body ? JSON.stringify(body) : undefined,
      credentials: 'same-origin',
    })
  } catch (e) {
    throw new Error('无法连接服务，请确认程序正在运行')
  }
  let data = null
  try {
    data = await res.json()
  } catch (e) {
    throw new Error('服务返回异常')
  }
  if (!res.ok || !data.ok) {
    const err = new Error(data && data.error ? data.error : `请求失败 (${res.status})`)
    err.status = res.status
    throw err
  }
  return data.data
}

export const api = {
  login: (key) => request('login', { method: 'POST', body: { key } }),
  logout: () => request('logout', { method: 'POST' }),
  lockStatus: () => request('lock-status'),
  bootstrap: () => request('bootstrap'),
  stats: () => request('stats'),
  emails: (params) => {
    const qs = new URLSearchParams()
    Object.entries(params || {}).forEach(([k, v]) => {
      if (v !== undefined && v !== null && v !== '') qs.set(k, v)
    })
    return request('emails?' + qs.toString())
  },
  email: (id) => request('emails/' + encodeURIComponent(id)),
  flags: (id, data) => request(`emails/${encodeURIComponent(id)}/flags`, { method: 'POST', body: data }),
  ignoreAds: (ids) => request('emails/ignore-ads', { method: 'POST', body: { ids } }),
  removeEmail: (id) => request('emails/' + encodeURIComponent(id), { method: 'DELETE' }),
  analyze: (id) => request(`emails/${encodeURIComponent(id)}/analyze`, { method: 'POST' }),
  translate: (id) => request(`emails/${encodeURIComponent(id)}/translate`, { method: 'POST' }),
  syncAll: () => request('sync', { method: 'POST' }),
  syncMailbox: (id) => request(`mailboxes/${id}/sync`, { method: 'POST' }),
  saveMailbox: (id, data) => request(id ? `mailboxes/${id}` : 'mailboxes', { method: id ? 'PUT' : 'POST', body: data }),
  deleteMailbox: (id) => request('mailboxes/' + id, { method: 'DELETE' }),
  testMailbox: (id, data) => request(`mailboxes/${id}/test`, { method: 'POST', body: data }),
  send: (data) => request('send', { method: 'POST', body: data }),
  saveAI: (data) => request('settings/ai', { method: 'PUT', body: data }),
  saveRules: (data) => request('settings/rules', { method: 'PUT', body: data }),
  saveForward: (data) => request('settings/forward', { method: 'PUT', body: data }),
  saveForwardRules: (data) => request('settings/forward-rules', { method: 'PUT', body: data }),
  testForward: () => request('forward/test', { method: 'POST' }),
  forwardEmail: (id) => request(`emails/${encodeURIComponent(id)}/forward`, { method: 'POST' }),
  pushStatus: () => request('push/status'),
  subscribePush: (subscription) => {
    const json = subscription.toJSON()
    return request('push/subscribe', {
      method: 'POST',
      body: { endpoint: json.endpoint, keys: json.keys },
    })
  },
  unsubscribePush: (endpoint) => request('push/unsubscribe', { method: 'POST', body: { endpoint } }),
  testPush: () => request('push/test', { method: 'POST' }),
  saveGeneral: (data) => request('settings/general', { method: 'PUT', body: data }),
  saveSecurity: (data) => request('settings/security', { method: 'PUT', body: data }),
  testAI: (data) => request('ai/test', { method: 'POST', body: data }),
  polish: (data) => request('ai/polish', { method: 'POST', body: data }),
  digest: (force) => request('digest' + (force ? '?force=1' : ''), { method: force ? 'POST' : 'GET' }),

  // 附件下载：request() 只会解析 JSON，这里单独取二进制，
  // 失败时仍按后端返回的 JSON 错误信息抛出，便于页面弹提示而不是跳空白页
  download: async (id, index, filename) => {
    let res
    try {
      res = await fetch(BASE + `emails/${encodeURIComponent(id)}/attachments/${index}`, {
        credentials: 'same-origin',
      })
    } catch (e) {
      throw new Error('无法连接服务，请确认程序正在运行')
    }
    if (!res.ok) {
      let message = `下载失败 (${res.status})`
      try {
        const data = await res.json()
        if (data && data.error) message = data.error
      } catch (e) { /* 非 JSON 响应，保留默认提示 */ }
      throw new Error(message)
    }
    const blob = await res.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename || 'attachment'
    document.body.appendChild(a)
    a.click()
    a.remove()
    setTimeout(() => URL.revokeObjectURL(url), 1000)
  },
}
