import test from 'node:test'
import assert from 'node:assert/strict'

import { senderAvatar } from '../src/utils.js'

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
