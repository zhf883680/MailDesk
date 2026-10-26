import test from 'node:test'
import assert from 'node:assert/strict'

import {
  applicationServerKeyMatches,
  pushIssueFor,
  pushSupport,
  urlBase64ToUint8Array,
} from '../src/notifications.js'

const readyEnv = {
  secureContext: true,
  notificationSupported: true,
  serviceWorkerSupported: true,
  pushManagerSupported: true,
  iOS: false,
  standalone: false,
  permission: 'default',
}

test('pushSupport requires HTTPS or localhost', () => {
  assert.equal(pushSupport({ ...readyEnv, secureContext: false }), 'insecure')
})

test('pushSupport requires the iOS PWA to be installed on the home screen', () => {
  assert.equal(pushSupport({ ...readyEnv, iOS: true, standalone: false }), 'install-first')
})

test('pushSupport reports blocked permission', () => {
  assert.equal(pushSupport({ ...readyEnv, permission: 'denied' }), 'denied')
})

test('pushSupport is ready on supported iOS home-screen PWA', () => {
  assert.equal(pushSupport({ ...readyEnv, iOS: true, standalone: true }), 'ready')
})

test('urlBase64ToUint8Array decodes a VAPID key', () => {
  assert.deepEqual([...urlBase64ToUint8Array('AQIDBA')], [1, 2, 3, 4])
})

test('pushIssueFor leaves the ready state without an error message', () => {
  assert.equal(pushIssueFor('ready'), '')
})

test('pushIssueFor explains an unsupported browser', () => {
  assert.match(pushIssueFor('unsupported'), /不支持 Web Push/)
})

test('applicationServerKeyMatches rejects a subscription created with another VAPID key', () => {
  const subscription = {
    options: { applicationServerKey: new Uint8Array([1, 2, 3, 4]).buffer },
  }
  assert.equal(applicationServerKeyMatches(subscription, 'AQIDBA'), true)
  assert.equal(applicationServerKeyMatches(subscription, 'AQIDBQ'), false)
})
