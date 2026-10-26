export function urlBase64ToUint8Array(value) {
  const padding = '='.repeat((4 - (value.length % 4)) % 4)
  const base64 = (value + padding).replace(/-/g, '+').replace(/_/g, '/')
  const raw = atob(base64)
  const bytes = new Uint8Array(raw.length)
  for (let i = 0; i < raw.length; i++) bytes[i] = raw.charCodeAt(i)
  return bytes
}

export function isIOS() {
  if (typeof navigator === 'undefined') return false
  return /iPad|iPhone|iPod/.test(navigator.userAgent)
    || (navigator.platform === 'MacIntel' && navigator.maxTouchPoints > 1)
}

export function isStandalonePWA() {
  if (typeof window === 'undefined') return false
  return window.matchMedia?.('(display-mode: standalone)').matches || navigator.standalone === true
}

export function pushSupport(env) {
  if (!env.secureContext) return 'insecure'
  if (!env.notificationSupported || !env.serviceWorkerSupported || !env.pushManagerSupported) return 'unsupported'
  if (env.permission === 'denied') return 'denied'
  if (env.iOS && !env.standalone) return 'install-first'
  return 'ready'
}

const pushIssues = {
  insecure: 'Web Push 需要 HTTPS。请通过 HTTPS 反向代理访问 MailDesk；localhost 调试除外。',
  unsupported: '当前浏览器不支持 Web Push，请升级 Safari、Chrome 或 Edge。',
  denied: '通知权限已被拒绝。请到系统设置中允许 MailDesk 通知，然后重新打开页面。',
  'install-first': 'iPhone / iPad 请先用 Safari 打开本页，点「分享 → 添加到主屏幕」，再从主屏幕打开 MailDesk。',
  ready: '',
}

export function pushIssueFor(state) {
  return pushIssues[state] ?? '当前环境无法使用 Web Push。'
}

export function currentPushEnvironment() {
  return {
    secureContext: typeof window !== 'undefined' && window.isSecureContext,
    notificationSupported: typeof Notification !== 'undefined',
    serviceWorkerSupported: typeof navigator !== 'undefined' && 'serviceWorker' in navigator,
    pushManagerSupported: typeof window !== 'undefined' && 'PushManager' in window,
    iOS: isIOS(),
    standalone: isStandalonePWA(),
    permission: typeof Notification !== 'undefined' ? Notification.permission : 'default',
  }
}

export function applicationServerKeyMatches(subscription, publicKey) {
  const current = subscription?.options?.applicationServerKey
  if (!current || !publicKey) return false
  const actual = current instanceof ArrayBuffer
    ? new Uint8Array(current)
    : new Uint8Array(current.buffer, current.byteOffset, current.byteLength)
  const expected = urlBase64ToUint8Array(publicKey)
  return actual.length === expected.length && actual.every((value, index) => value === expected[index])
}

export async function getPushSubscription() {
  if (!('serviceWorker' in navigator)) return null
  const registration = await navigator.serviceWorker.getRegistration('/')
  return registration ? registration.pushManager.getSubscription() : null
}

export async function enableWebPush(publicKey) {
  if (!publicKey) throw new Error('服务端尚未生成 Web Push 公钥')
  const registration = await navigator.serviceWorker.register('/sw.js', {
    scope: '/',
    updateViaCache: 'none',
  })
  try { await registration.update() } catch (error) { /* 下次打开仍会检查更新 */ }
  await navigator.serviceWorker.ready
  let subscription = await registration.pushManager.getSubscription()
  if (subscription && !applicationServerKeyMatches(subscription, publicKey)) {
    await subscription.unsubscribe()
    subscription = null
  }
  if (!subscription) {
    subscription = await registration.pushManager.subscribe({
      userVisibleOnly: true,
      applicationServerKey: urlBase64ToUint8Array(publicKey),
    })
  }
  return subscription
}

export async function disableWebPush() {
  const subscription = await getPushSubscription()
  if (!subscription) return ''
  const endpoint = subscription.endpoint
  await subscription.unsubscribe()
  return endpoint
}
