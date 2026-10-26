import test from 'node:test'
import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import vm from 'node:vm'

test('service worker shows a push notification when the app is not visible', async () => {
  const source = await readFile(new URL('../public/sw.js', import.meta.url), 'utf8')
  const listeners = {}
  const shown = []
  let pending
  const self = {
    location: { origin: 'https://mail.example.com' },
    addEventListener(type, callback) {
      listeners[type] = callback
    },
    registration: {
      showNotification(title, options) {
        shown.push({ title, options })
        return Promise.resolve()
      },
    },
    clients: {
      matchAll() {
        return Promise.resolve([])
      },
      openWindow(url) {
        shown.push({ opened: url })
        return Promise.resolve()
      },
    },
  }

  vm.runInNewContext(source, { self, URL, Promise, console })
  listeners.push({
    data: {
      json() {
        return {
          title: '新邮件',
          body: 'Alex\n你好',
          url: '/?email=mb%3A1',
          tag: 'maildesk-mb:1',
          icon: '/icon-192.png',
        }
      },
    },
    waitUntil(promise) {
      pending = promise
    },
  })
  await pending

  assert.equal(shown[0].title, '新邮件')
  assert.equal(shown[0].options.body, 'Alex\n你好')
  assert.equal(shown[0].options.tag, 'maildesk-mb:1')

  let clickPending
  listeners.notificationclick({
    notification: { close() {}, data: shown[0].options.data },
    waitUntil(promise) {
      clickPending = promise
    },
  })
  await clickPending
  assert.deepEqual(shown[1], { opened: 'https://mail.example.com/?email=mb%3A1' })
})
