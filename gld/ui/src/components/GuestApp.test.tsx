// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { i18n } from "@/i18n"
import type { Msg } from "@/lib/api"
import { GuestApp } from "./GuestApp"

const WHITE = new Set(["GET /conv", "GET /conv-text", "GET /conv-tools", "GET /conv-image", "GET /session-live", "GET /session-state", "GET /session-task-output", "GET /session-screen", "GET /queue", "POST /queue-add", "POST /queue-rm", "GET /guest-me", "POST /guest-leave"])
const key = (c: { method: string; path: string }) => `${c.method} ${c.path}`
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } })
const live = { open: true, status: "", activity: "", thinking: false, since: 0, context: 0, out: 0, model: "", subs: [] }

let mode: "rw" | "ro"
let ended: number | "html" | null
let queue: unknown[]
let calls: { path: string; method: string; csrf: string | null; body: string; search: string }[]
let all: Msg[]
let rich: boolean
const msg = (i: number): Msg => ({ r: "assistant", t: `m${i}` })

beforeEach(() => {
  vi.useFakeTimers()
  mode = "rw"
  ended = null
  queue = []
  rich = false
  all = [{ r: "assistant", t: "salut invité" }, { r: "doc", t: "/d/a.md" }]
  calls = []
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const path = new URL(url, "http://x").pathname
      const search = new URL(url, "http://x").search
      calls.push({ path, search, method: init.method ?? "GET", csrf: new Headers(init.headers).get("X-Gld-CSRF"), body: String(init.body ?? "") })
      if (ended === "html") return new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html" } })
      if (ended) return json({ ok: false }, ended)
      if (path === "/guest-me") return json({ id: "s1", dir: "/d", mode, csrf: "tok" })
      if (path === "/conv") {
        const q = new URL(url, "http://x").searchParams
        const limit = Number(q.get("limit") ?? 1000)
        const end = q.has("before") && Number(q.get("before")) > 0 ? Number(q.get("before")) : all.length
        const start = Math.max(0, end - limit)
        const base = rich ? [{ r: "assistant", t: "[lien](https://ex.org/x)" }, { r: "user", t: "", img: "1", o: 1, k: 0 }, { r: "assistant", t: "coupé", cut: 5, len: 9000, o: 2, n: 1 }, { r: "tool", t: "Bash  ls" }] : []
        return json({ msgs: [...base, ...all.slice(start, end)], start, more: start > 0 })
      }
      if (path === "/conv-text") return json({ t: "suite", cut: 0 })
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
    expect([...new Set(calls.map(key))].filter((p) => !WHITE.has(p))).toEqual([])
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

  it("contenu riche : image, message coupé, outil, lien : requêtes autorisées et lien sûr", async () => {
    rich = true
    await mount()
    fireEvent.click(screen.getByRole("button", { name: /suite|more/i }))
    await tick(50)
    await tick(2100)
    expect(document.querySelector("img")!.getAttribute("src")).toMatch(/^\/conv-image\?/)
    const link = document.querySelector("a[href='https://ex.org/x']")!
    expect(link.getAttribute("rel")).toContain("noopener")
    expect(link.getAttribute("rel")).toContain("noreferrer")
    expect(screen.getByText(/Bash/)).toBeTruthy()
    expect(calls.some((c) => c.path === "/conv-text")).toBe(true)
    expect([...new Set(calls.map(key))].filter((p) => !WHITE.has(p))).toEqual([])
  })

  it("historique : plus de 200 messages, bouton, préfixe conservé par le sondage suivant", async () => {
    all = Array.from({ length: 450 }, (_, i) => msg(i))
    await mount()
    expect(screen.queryByText("m249")).toBeNull()
    expect(screen.getByText("m449")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.view.loadOlder") }))
    await tick(50)
    expect(screen.getByText("m249")).toBeTruthy()
    all = [...all, msg(450)]
    await tick(2100)
    expect(screen.getByText("m450")).toBeTruthy()
    expect(screen.getByText("m249")).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.view.loadOlder") }))
    await tick(50)
    expect(screen.getByText("m0")).toBeTruthy()
    expect(screen.queryByRole("button", { name: i18n.t("conversation.view.loadOlder") })).toBeNull()
  })

  it("bascule des outils : redemande /conv avec tools=1", async () => {
    await mount()
    expect(calls.filter((c) => c.path === "/conv").every((c) => !c.search.includes("tools=1"))).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.view.toolsShown") }))
    await tick(50)
    expect(calls.some((c) => c.path === "/conv" && c.search.includes("tools=1"))).toBe(true)
  })

  it("l'hôte met fin au partage pendant la saisie : écran terminé après le POST refusé", async () => {
    await mount()
    fireEvent.change(box(), { target: { value: "x" } })
    ended = 403
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.composer.send") }))
    await tick(50)
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    const n = calls.length
    await tick(15000)
    expect(calls.length).toBe(n)
  })

  it("double Entrée rapide : un seul envoi", async () => {
    await mount()
    fireEvent.change(box(), { target: { value: "une fois" } })
    fireEvent.keyDown(box(), { key: "Enter" })
    fireEvent.keyDown(box(), { key: "Enter" })
    await tick(50)
    expect(calls.filter((c) => c.path === "/queue-add")).toHaveLength(1)
  })
})
