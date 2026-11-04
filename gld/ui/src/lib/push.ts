import { i18n } from "@/i18n"
import { api, type PushPreferences, type PushState } from "@/lib/api"

export class PushPermissionError extends Error {}

const NOT_SUBSCRIBED: PushState = { subscribed: false, asking: false, waiting: false }

export const pushSupported = () => "serviceWorker" in navigator && "PushManager" in window && "Notification" in window

function base64UrlToBytes(base64Url: string) {
  const padding = "=".repeat((4 - (base64Url.length % 4)) % 4)
  const decoded = atob((base64Url + padding).replace(/-/g, "+").replace(/_/g, "/"))
  return Uint8Array.from(decoded, (character) => character.charCodeAt(0))
}

const pushManager = async () => (await navigator.serviceWorker.ready).pushManager

export async function currentPushState(): Promise<PushState> {
  const subscription = await (await pushManager()).getSubscription()
  return subscription ? api.pushGet(subscription.endpoint) : NOT_SUBSCRIBED
}

export async function subscribePush(preferences: PushPreferences) {
  if (Notification.permission === "default") await Notification.requestPermission()
  if (Notification.permission !== "granted") throw new PushPermissionError()
  const manager = await pushManager()
  const subscription =
    (await manager.getSubscription()) ??
    (await manager.subscribe({ userVisibleOnly: true, applicationServerKey: base64UrlToBytes((await api.pushKey()).key) }))
  const { endpoint, keys } = subscription.toJSON()
  await api.pushPut({ endpoint: endpoint ?? subscription.endpoint, keys: { p256dh: keys?.p256dh ?? "", auth: keys?.auth ?? "" }, ...preferences, lang: i18n.language })
}

export async function unsubscribePush() {
  const subscription = await (await pushManager()).getSubscription()
  if (!subscription) return
  await api.pushDel(subscription.endpoint)
  await subscription.unsubscribe()
}
