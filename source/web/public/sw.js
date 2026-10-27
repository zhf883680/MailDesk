const SW_VERSION = '1.4.5'

self.addEventListener('install', (event) => {
  console.info(`[MailDesk SW] v${SW_VERSION} installing`)
  event.waitUntil(self.skipWaiting())
})

self.addEventListener('activate', (event) => {
  console.info(`[MailDesk SW] v${SW_VERSION} activated`)
  event.waitUntil(self.clients.claim())
})

self.addEventListener('push', (event) => {
  let payload = {}
  try {
    payload = event.data ? event.data.json() : {}
  } catch (error) {
    payload = { body: event.data ? event.data.text() : '收到一封新邮件' }
  }

  const title = payload.title || 'MailDesk'
  const target = payload.url || '/'
  const force = payload.data?.force === true
  const options = {
    body: payload.body || '收到一封新邮件',
    tag: payload.tag || 'maildesk',
    icon: payload.icon || '/icon-192.png',
    badge: '/icon-192.png',
    data: { ...(payload.data || {}), url: target },
  }
  console.info('[MailDesk SW] push received', { tag: options.tag, force })

  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    if (!force && windows.some((client) => client.visibilityState === 'visible')) {
      console.info('[MailDesk SW] notification skipped because app is visible', { tag: options.tag })
      return
    }
    await self.registration.showNotification(title, options)
    console.info('[MailDesk SW] notification displayed', { tag: options.tag })
  })())
})

self.addEventListener('notificationclick', (event) => {
  event.notification.close()
  const target = new URL(event.notification.data?.url || '/', self.location.origin).href
  console.info('[MailDesk SW] notification clicked', { target })
  event.waitUntil((async () => {
    const windows = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
    for (const client of windows) {
      if (new URL(client.url).origin !== self.location.origin) continue
      if ('navigate' in client) await client.navigate(target)
      if ('focus' in client) return client.focus()
    }
    return self.clients.openWindow(target)
  })())
})
