// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { i18n } from "@/i18n"
import { GuestApp } from "./GuestApp"

const WHITE = new Set(["/conv", "/conv-text", "/conv-tools", "/conv-image", "/session-live", "/session-state", "/session-task-output", "/session-screen", "/queue", "/queue-add", "/queue-rm", "/guest-me", "/guest-leave"])
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } })
const live = { open: true, status: "", activity: "", thinking: false, since: 0, context: 0, out: 0, model: "", subs: [] }

let mode: "rw" | "ro"
let ended: number | "html" | null
let queue: unknown[]
let calls: { path: string; method: string; csrf: string | null; body: string }[]

beforeEach(() => {
  vi.useFakeTimers()
  mode = "rw"
  ended = null
  queue = []
  calls = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const path = new URL(url, "http://x").pathname
      calls.push({ path, method: init.method ?? "GET", csrf: new Headers(init.headers).get("X-Gld-CSRF"), body: String(init.body ?? "") })
      if (ended === "html") return new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html" } })
      if (ended) return json({ ok: false }, ended)
      if (path === "/guest-me") return json({ id: "s1", dir: "/d", mode, csrf: "tok" })
      if (path === "/conv") return json({ msgs: [{ r: "assistant", t: "salut invité" }, { r: "doc", t: "/d/a.md" }], start: 0 })
      if (path === "/session-live") return json(live)
      if (path === "/queue") return json({ items: queue })
      if (path === "/queue-add") {
        queue = [{ id: "q1", text: JSON.parse(String(init.body)).text, author: "guest", state: "queued", at: 1 }]
        return json({ ok: true, out: "" })
      }
      if (path === "/queue-rm") {
        queue = []
        return json({ ok: true, out: "" })
      }
      if (path === "/guest-leave") return json({ ok: true })
      return json({ ok: false }, 403)
    })
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.useRealTimers()
})

const tick = (ms: number) => act(async () => void (await vi.advanceTimersByTimeAsync(ms)))
const mount = async () => {
  render(<GuestApp />)
  await tick(50)
}
const box = () => screen.getByRole("textbox") as HTMLTextAreaElement
const send = async (text: string) => {
  fireEvent.change(box(), { target: { value: text } })
  fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.composer.send") }))
  await tick(50)
}

describe("GuestApp", () => {
  it("rw : ajoute à la file avec l'en-tête CSRF et montre l'élément avec son auteur", async () => {
    await mount()
    expect(screen.getByText("salut invité")).toBeTruthy()
    await send("coucou")
    const add = calls.find((c) => c.path === "/queue-add")!
    expect(add.csrf).toBe("tok")
    expect(JSON.parse(add.body).text).toBe("coucou")
    await tick(2100)
    expect(screen.getByText("coucou")).toBeTruthy()
    expect(screen.getByText(i18n.t("conversation.composer.guest"))).toBeTruthy()
  })

  it("ro : composeur désactivé avec bannière, et bascule rw→ro vue au sondage suivant", async () => {
    await mount()
    expect(box().disabled).toBe(false)
    expect(screen.queryByText(i18n.t("conversation.guest.readOnly"))).toBeNull()
    mode = "ro"
    await tick(5100)
    expect(box().disabled).toBe(true)
    expect(screen.getByText(i18n.t("conversation.guest.readOnly"))).toBeTruthy()
  })

  it("403 au sondage : écran « partage terminé » et sondage arrêté", async () => {
    await mount()
    ended = 403
    await tick(5100)
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    const n = calls.length
    await tick(15000)
    expect(calls.length).toBe(n)
  })

  it("réponse HTML (hôte sans cookie) : partage terminé", async () => {
    await mount()
    ended = "html"
    await tick(5100)
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
  })

  it("ne demande jamais de route hors liste blanche, ni lien, ni document", async () => {
    await mount()
    await send("x")
    await tick(12000)
    expect(calls.length).toBeGreaterThan(3)
    expect([...new Set(calls.map((c) => c.path))].filter((p) => !WHITE.has(p))).toEqual([])
    expect(document.querySelector("a[href]")).toBeNull()
    expect(document.querySelector("nav, aside")).toBeNull()
    expect(screen.queryByText("a.md")).toBeNull()
  })

  it("retirer : seulement sur les éléments de l'invité, pas ceux de l'hôte ni en cours d'envoi", async () => {
    queue = [
      { id: "h", text: "hôte", author: "host", state: "queued", at: 1 },
      { id: "g1", text: "invité un", author: "guest", state: "queued", at: 2 },
      { id: "g2", text: "invité deux", author: "guest", state: "sending", at: 3 },
      { id: "g3", text: "invité trois", author: "guest", state: "uncertain", at: 4 },
    ]
    await mount()
    expect(screen.getAllByRole("button", { name: i18n.t("conversation.composer.dequeue") })).toHaveLength(2)
    fireEvent.click(screen.getAllByRole("button", { name: i18n.t("conversation.composer.dequeue") })[0])
    await tick(50)
    expect(JSON.parse(calls.find((c) => c.path === "/queue-rm")!.body).item).toBe("g1")
  })

  it("Quitter : appelle /guest-leave, affiche l'au revoir et arrête les sondages", async () => {
    await mount()
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.guest.leave") }))
    await tick(50)
    expect(calls.some((c) => c.path === "/guest-leave" && c.method === "POST" && c.csrf === "tok")).toBe(true)
    expect(screen.getByText(i18n.t("conversation.guest.bye"))).toBeTruthy()
    const n = calls.length
    await tick(15000)
    expect(calls.length).toBe(n)
  })
})
