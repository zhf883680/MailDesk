import test from 'node:test'
import assert from 'node:assert/strict'

import {
  senderAvatar,
  shouldDismissOnBackdrop,
  initialBodyView,
  plainBodyKind,
} from '../src/utils.js'

test('senderAvatar uses initials and a stable sender color', () => {
  const first = senderAvatar('Marta Lindqvist', 'marta@brightside.example')
  const second = senderAvatar('Marta Lindqvist', 'MARTA@brightside.example')

  assert.equal(first.label, 'M')
  assert.equal(first.color, second.color)
  assert.match(first.color, /^#[0-9a-f]{6}$/)
})

test('senderAvatar uses a single character', () => {
  assert.equal(senderAvatar('淘宝网', 'notice@taobao.example').label, '淘')
  assert.equal(senderAvatar('【速界】Github日报', 'digest@example.com').label, '速')
  assert.equal(senderAvatar('张三丰', 'zhang@example.com').label, '张')
  assert.equal(senderAvatar('Marta Lindqvist', 'marta@brightside.example').label, 'M')
})

test('backdrop dismissal is blocked while a form contains unsaved changes', () => {
  const initial = { to: '', subject: '', mailbox_ids: [] }
  const current = { to: 'alex@example.com', subject: '', mailbox_ids: [] }

  assert.equal(shouldDismissOnBackdrop(initial, current), false)
})

test('backdrop dismissal is allowed when a form is unchanged', () => {
  const initial = { to: 'alex@example.com', subject: '', mailbox_ids: ['work'] }
  const current = { to: 'alex@example.com', subject: '', mailbox_ids: ['work'] }

  assert.equal(shouldDismissOnBackdrop(initial, current), true)
})

test('plain-text emails show their original body instead of an empty pane', () => {
  const mail = { text: '尊敬的客户，您好！', html: '' }

  assert.equal(initialBodyView(mail), 'text')
  assert.equal(plainBodyKind(mail, initialBodyView(mail)), 'original')
})

test('translated emails still default to the Chinese body view', () => {
  const mail = { text: 'Hello', html: '<p>Hello</p>', translated: '你好' }

  assert.equal(initialBodyView(mail), 'zh')
  assert.equal(plainBodyKind(mail, 'zh'), 'translation')
})

test('body view falls back to the original HTML when there is no translation', () => {
  const mail = { text: 'Hello', html: '<p>Hello</p>' }

  assert.equal(initialBodyView(mail), 'html')
})
