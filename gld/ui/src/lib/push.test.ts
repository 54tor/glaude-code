// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { PushPermissionError, pushSupported, subscribePush, unsubscribePush } from "./push"

const ENDPOINT = "https://web.push.apple.com/abc"

function installerNavigateurPush(permission: NotificationPermission, abonnementExistant: unknown = null) {
  const abonnementCree = { endpoint: ENDPOINT, toJSON: () => ({ endpoint: ENDPOINT, keys: { p256dh: "cle", auth: "secret" } }), unsubscribe: vi.fn().mockResolvedValue(true) }
  const gestionnairePush = {
    getSubscription: vi.fn().mockResolvedValue(abonnementExistant),
    subscribe: vi.fn().mockResolvedValue(abonnementCree),
  }
  vi.stubGlobal("PushManager", class {})
  vi.stubGlobal("Notification", { permission, requestPermission: vi.fn().mockResolvedValue(permission) })
  Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: { ready: Promise.resolve({ pushManager: gestionnairePush }) } })
  return { gestionnairePush, abonnementCree }
}

beforeEach(() => {
  vi.spyOn(api, "pushKey").mockResolvedValue({ key: "AQAB" })
  vi.spyOn(api, "pushPut").mockResolvedValue(undefined)
  vi.spyOn(api, "pushDel").mockResolvedValue(undefined)
})
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  Reflect.deleteProperty(navigator, "serviceWorker")
})

describe("pushSupported", () => {
  it("est faux sans PushManager", () => {
    Object.defineProperty(navigator, "serviceWorker", { configurable: true, value: {} })
    vi.stubGlobal("Notification", {})
    expect(pushSupported()).toBe(false)
  })

  it("est vrai quand service worker, PushManager et Notification existent", () => {
    installerNavigateurPush("default")
    expect(pushSupported()).toBe(true)
  })
})

describe("subscribePush", () => {
  it("s'abonne avec userVisibleOnly puis enregistre les préférences", async () => {
    const { gestionnairePush } = installerNavigateurPush("granted")

    await subscribePush({ asking: true, waiting: false })

    expect(gestionnairePush.subscribe).toHaveBeenCalledWith(expect.objectContaining({ userVisibleOnly: true }))
    expect(api.pushPut).toHaveBeenCalledWith(expect.objectContaining({ endpoint: ENDPOINT, asking: true, waiting: false, keys: { p256dh: "cle", auth: "secret" } }))
  })

  it("réutilise l'abonnement existant sans en créer un second", async () => {
    const abonnementExistant = { endpoint: ENDPOINT, toJSON: () => ({ endpoint: ENDPOINT, keys: { p256dh: "cle", auth: "secret" } }) }
    const { gestionnairePush } = installerNavigateurPush("granted", abonnementExistant)

    await subscribePush({ asking: true, waiting: true })

    expect(gestionnairePush.subscribe).not.toHaveBeenCalled()
    expect(api.pushPut).toHaveBeenCalledTimes(1)
  })

  it("rejette sans rien enregistrer quand la permission est refusée", async () => {
    installerNavigateurPush("denied")

    await expect(subscribePush({ asking: true, waiting: true })).rejects.toBeInstanceOf(PushPermissionError)

    expect(api.pushPut).not.toHaveBeenCalled()
  })
})

describe("unsubscribePush", () => {
  it("supprime l'abonnement côté serveur puis dans le navigateur", async () => {
    const { abonnementCree } = installerNavigateurPush("granted")
    const gestionnaire = (await navigator.serviceWorker.ready).pushManager as unknown as { getSubscription: ReturnType<typeof vi.fn> }
    gestionnaire.getSubscription.mockResolvedValue(abonnementCree)

    await unsubscribePush()

    expect(api.pushDel).toHaveBeenCalledWith(ENDPOINT)
    expect(abonnementCree.unsubscribe).toHaveBeenCalled()
  })
})
