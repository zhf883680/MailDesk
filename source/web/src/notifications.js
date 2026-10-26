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

export async function getPushSubscription() {
  if (!('serviceWorker' in navigator)) return null
  const registration = await navigator.serviceWorker.getRegistration('/')
  return registration ? registration.pushManager.getSubscription() : null
}

export async function enableWebPush(publicKey) {
  if (!publicKey) throw new Error('服务端尚未生成 Web Push 公钥')
  const registration = await navigator.serviceWorker.register('/sw.js', { scope: '/' })
  await navigator.serviceWorker.ready
  let subscription = await registration.pushManager.getSubscription()
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
