<script setup>
import { ref, onMounted } from 'vue'
import Icon from './Icon.vue'
import Logo from './Logo.vue'
import { api } from '../api'
import { login, state } from '../store'

const key = ref('')
const show = ref(false)
const err = ref('')
const loading = ref(false)
const shake = ref(false)
const locked = ref(false)
const remaining = ref(3)
const inputRef = ref(null)

onMounted(async () => {
  try {
    const s = await api.lockStatus()
    locked.value = s.locked
    remaining.value = s.remaining
    if (s.locked) err.value = '该 IP 已被锁定，只能重启程序解锁'
  } catch (e) { /* ignore */ }
  inputRef.value?.focus()
})

async function submit() {
  if (loading.value || locked.value) return
  if (!key.value.trim()) {
    err.value = '请输入访问密钥'
    shake.value = true
    setTimeout(() => (shake.value = false), 420)
    return
  }
  loading.value = true
  err.value = ''
  inputRef.value?.blur()
  try {
    await login(key.value.trim())
  } catch (e) {
    err.value = e.message
    shake.value = true
    setTimeout(() => (shake.value = false), 420)
    try {
      const s = await api.lockStatus()
      locked.value = s.locked
      remaining.value = s.remaining
    } catch (e2) { /* ignore */ }
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="login-wrap">
    <section class="login-art">
      <div class="brand-line">
        <span class="brand-mark"><Logo :size="22" /></span>
        MailDesk
      </div>
      <div>
        <h1>让每一封邮件<br />先被<em>读懂</em>，再被阅读。</h1>
        <p>多邮箱统一收件、英文邮件自动译成中文、AI 判断轻重缓急，还能按你的规则提炼要点。全部数据留在本机。</p>
      </div>
      <div class="art-foot">
        <span><Icon name="lock" :size="13" /> 本地部署</span>
        <span><Icon name="bot" :size="13" /> 兼容 OpenAI 接口</span>
        <span><Icon name="languages" :size="13" /> 中英对照</span>
      </div>
    </section>

    <section class="login-form-side">
      <div class="login-card">
        <h2>登录邮件台</h2>
        <p>输入访问密钥以继续。这是一台私有服务，仅你本人使用。</p>

        <div class="key-field">
          <input
            ref="inputRef"
            v-model="key"
            :type="show ? 'text' : 'password'"
            :class="{ shake }"
            placeholder="访问密钥"
            autocomplete="current-password"
            :disabled="locked"
            @keyup.enter="submit"
          />
          <button class="icon-btn eye" type="button" @click="show = !show" :aria-label="show ? '隐藏密钥' : '显示密钥'">
            <Icon :name="show ? 'eyeOff' : 'eye'" :size="17" />
          </button>
        </div>

        <button class="btn btn-primary login-btn" :disabled="loading || locked" @click="submit">
          <Icon v-if="loading" name="refresh" :size="16" class="spin" />
          {{ locked ? '已锁定' : loading ? '验证中…' : '进入邮箱' }}
        </button>

        <div v-if="err" class="login-err">
          <Icon name="alert" :size="14" /><span>{{ err }}</span>
        </div>

        <div v-if="locked" class="lock-box">
          <Icon name="lock" :size="16" />
          <div>连续 3 次密钥错误，此 IP 已被锁定。锁定状态仅存于内存，重启程序即可解锁。</div>
        </div>

        <div v-else class="login-hint">
          <div>· 同一 IP 连续输错 3 次将锁定，重启程序才能解锁。</div>
          <div>· 首次使用默认密钥 <code>admin123</code>，登录后请到「设置 · 安全」修改。</div>
        </div>
      </div>
    </section>
  </div>
</template>
