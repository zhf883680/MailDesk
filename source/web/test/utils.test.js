import test from 'node:test'
import assert from 'node:assert/strict'

import {
  senderAvatar,
  initialBodyView,
  plainBodyKind,
  fmtSize,
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

test('fmtSize formats attachment sizes for humans', () => {
  assert.equal(fmtSize(0), '')
  assert.equal(fmtSize(512), '512 B')
  assert.equal(fmtSize(18432), '18 KB')
  assert.equal(fmtSize(245760), '240 KB')
  assert.equal(fmtSize(2.4 * 1024 * 1024), '2.4 MB')
  assert.equal(fmtSize(15 * 1024 * 1024), '15 MB')
})
