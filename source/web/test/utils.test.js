import test from 'node:test'
import assert from 'node:assert/strict'

import { senderAvatar, shouldDismissOnBackdrop } from '../src/utils.js'

test('senderAvatar uses initials and a stable sender color', () => {
  const first = senderAvatar('Marta Lindqvist', 'marta@brightside.example')
  const second = senderAvatar('Marta Lindqvist', 'MARTA@brightside.example')

  assert.equal(first.label, 'ML')
  assert.equal(first.color, second.color)
  assert.match(first.color, /^#[0-9a-f]{6}$/)
})

test('senderAvatar uses the last two Chinese characters', () => {
  assert.equal(senderAvatar('淘宝网', 'notice@taobao.example').label, '宝网')
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
