// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { i18n } from "@/i18n"
import type { Msg } from "@/lib/api"
import { setAuth } from "@/lib/auth"
import { GuestApp } from "./GuestApp"
import { Message } from "./Message"

const S = "/v1/sessions/{id}"
const WHITE = new Set([`GET ${S}/conversation`, `GET ${S}/conversation/text`, `GET ${S}/conversation/tools`, `GET ${S}/conversation/images/{img}`, `GET ${S}/live`, `GET ${S}/state`, `GET ${S}/tasks/{task}/output`, `GET ${S}/queue`, `POST ${S}/queue`, `DELETE ${S}/queue/{qid}`, "GET /guest-me", "GET /guest-list", "POST /guest-leave"])
// Les segments variables d'une route, comme dans les motifs du serveur.
const norm = (p: string) => p.replace(/^\/v1\/sessions\/[^/]+/, S).replace(/\/images\/[^/]+$/, "/images/{img}").replace(/\/queue\/[^/]+$/, "/queue/{qid}").replace(/\/tasks\/[^/]+\/output$/, "/tasks/{task}/output")
const key = (c: { method: string; path: string }) => `${c.method} ${c.path}`
const json = (b: unknown, status = 200) => new Response(JSON.stringify(b), { status, headers: { "Content-Type": "application/json" } })
const live = { open: true, status: "", activity: "", thinking: false, since: 0, context: 0, out: 0, model: "", subs: [] }

let mode: "rw" | "ro"
let ended: number | "html" | null
let queue: unknown[]
let calls: { path: string; raw: string; method: string; csrf: string | null; body: string; search: string; share: string | null }[]
let shares: { handle: string; title: string; mode: "rw" | "ro" }[] | 403
let title: string
const H = "0123456789abcdef"
const H2 = "fedcba9876543210"
const go = (p: string) => window.history.pushState({}, "", p)
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
  shares = [{ handle: H, title: "Ma session", mode: "rw" }]
  title = "Ma session"
  setAuth({ share: undefined, csrf: "" })
  go(`/guest/${H}`)
  vi.stubGlobal(
    "fetch",
    vi.fn(async (url: string, init: RequestInit = {}) => {
      const raw = new URL(url, "http://x").pathname
      const path = norm(raw)
      const search = new URL(url, "http://x").search
      const method = init.method ?? "GET"
      calls.push({ path, raw, search, method: init.method ?? "GET", csrf: new Headers(init.headers).get("X-Gld-CSRF"), share: new Headers(init.headers).get("X-Gld-Share"), body: String(init.body ?? "") })
      if (path === "/guest-list") return shares === 403 ? json({ ok: false }, 403) : json(shares)
      if (ended === "html") return new Response("<html></html>", { status: 200, headers: { "Content-Type": "text/html" } })
      if (ended) return json({ ok: false }, ended)
      if (path === "/guest-list") return shares === 403 ? json({ ok: false }, 403) : json(shares)
      if (path === "/guest-me") return json({ id: "s1", dir: "/d", mode, csrf: "tok", handle: H, title, invite: "inv1", label: "Léa" })
      if (path === `${S}/conversation`) {
        const q = new URL(url, "http://x").searchParams
        const limit = Number(q.get("limit") ?? 1000)
        const end = q.has("before") && Number(q.get("before")) > 0 ? Number(q.get("before")) : all.length
        const start = Math.max(0, end - limit)
        const base = rich ? [{ r: "assistant", t: "[lien](https://ex.org/x)" }, { r: "user", t: "", img: "1", o: 1, k: 0 }, { r: "assistant", t: "coupé", cut: 5, len: 9000, o: 2, n: 1 }, { r: "tool", t: "Bash  ls" }] : []
        return json({ msgs: [...base, ...all.slice(start, end)], start, more: start > 0 })
      }
      if (path === `${S}/conversation/text`) return json({ t: "suite", cut: 0 })
      if (path === `${S}/live`) return json(live)
      if (path === `${S}/queue` && method === "GET") return json({ items: queue })
      if (path === `${S}/queue` && method === "POST") {
        queue = [{ id: "q1", text: JSON.parse(String(init.body)).text, author: "guest", guest: "inv1", label: "Léa", state: "queued", at: 1 }]
        return json({ item: queue[0] }, 201)
      }
      if (path === `${S}/queue/{qid}` && method === "DELETE") {
        const text = (queue[0] as { text?: string } | undefined)?.text ?? ""
        queue = []
        return json({ text })
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
    const add = calls.find((c) => c.path === `${S}/queue` && c.method === "POST")!
    expect(add.csrf).toBe("tok")
    expect(JSON.parse(add.body).text).toBe("coucou")
    await tick(2100)
    expect(screen.getByText("coucou")).toBeTruthy()
    expect(screen.getByText("Léa")).toBeTruthy()
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

  it("retirer : seulement sur les éléments de cette invitation, pas ceux de l'hôte, d'un autre invité ni en cours d'envoi", async () => {
    queue = [
      { id: "h", text: "hôte", author: "host", state: "queued", at: 1 },
      { id: "g1", text: "invité un", author: "guest", guest: "inv1", label: "Léa", state: "queued", at: 2 },
      { id: "g2", text: "invité deux", author: "guest", guest: "inv1", label: "Léa", state: "sending", at: 3 },
      { id: "g3", text: "invité trois", author: "guest", guest: "inv1", label: "Léa", state: "uncertain", at: 4 },
      { id: "g4", text: "autre invité", author: "guest", guest: "inv2", label: "Bob", state: "queued", at: 5 },
    ]
    await mount()
    expect(screen.getAllByRole("button", { name: i18n.t("conversation.composer.dequeue") })).toHaveLength(2)
    fireEvent.click(screen.getAllByRole("button", { name: i18n.t("conversation.composer.dequeue") })[0])
    await tick(50)
    expect(calls.find((c) => c.method === "DELETE")!.raw.split("/").pop()).toBe("g1")
  })

  it("la file nomme l'auteur : libellé de l'invité, Hôte pour l'hôte", async () => {
    queue = [
      { id: "h", text: "hôte", author: "host", state: "queued", at: 1 },
      { id: "g4", text: "autre invité", author: "guest", guest: "inv2", label: "Bob", state: "queued", at: 5 },
    ]
    await mount()
    expect(screen.getByText("Bob")).toBeTruthy()
    expect(screen.getByText(i18n.t("conversation.guest.host"))).toBeTruthy()
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
    expect(document.querySelector("img")!.getAttribute("src")).toMatch(/^\/v1\/sessions\/s1\/conversation\/images\/1-0\?/)
    const link = document.querySelector("a[href='https://ex.org/x']")!
    expect(link.getAttribute("rel")).toContain("noopener")
    expect(link.getAttribute("rel")).toContain("noreferrer")
    expect(screen.getByText(/Bash/)).toBeTruthy()
    expect(calls.some((c) => c.path === `${S}/conversation/text`)).toBe(true)
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
    expect(calls.filter((c) => c.path === `${S}/conversation`).every((c) => !c.search.includes("tools=1"))).toBe(true)
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.view.toolsHidden") }))
    await tick(50)
    expect(calls.some((c) => c.path === `${S}/conversation` && c.search.includes("tools=1"))).toBe(true)
    expect(screen.getByRole("button", { name: i18n.t("conversation.view.toolsShown") }).getAttribute("aria-pressed")).toBe("true")
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

  it("Chrome : scrollIntoView renvoie une Promise ; un nouveau message ne fait pas disparaître la transcription", async () => {
    const proto = Element.prototype as { scrollIntoView?: unknown }
    proto.scrollIntoView = vi.fn(() => Promise.resolve())
    try {
      await mount()
      expect(screen.getByText("salut invité")).toBeTruthy()
      all = [...all, msg(1)]
      await tick(2100)
      expect(screen.getByText("m1")).toBeTruthy()
      expect(screen.getByText("salut invité")).toBeTruthy()
    } finally {
      delete proto.scrollIntoView
    }
  })

  it("double Entrée rapide : un seul envoi", async () => {
    await mount()
    fireEvent.change(box(), { target: { value: "une fois" } })
    fireEvent.keyDown(box(), { key: "Enter" })
    fireEvent.keyDown(box(), { key: "Enter" })
    await tick(50)
    expect(calls.filter((c) => c.path === `${S}/queue` && c.method === "POST")).toHaveLength(1)
  })

  it("chaque requête porte X-Gld-Share (sauf /guest-list) et rien ne part avant", async () => {
    await mount()
    await send("x")
    await tick(12000)
    const own = calls.filter((c) => c.path !== "/guest-list")
    expect(own.length).toBeGreaterThan(3)
    expect(own.filter((c) => c.share !== H)).toEqual([])
    expect(calls.filter((c) => c.path === "/guest-list").every((c) => c.share === null)).toBe(true)
  })

  it("poignée invalide dans l'URL : traitée comme /guest, aucune requête avec en-tête", async () => {
    go("/guest/ZZ")
    shares = 403
    await mount()
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    expect(calls.every((c) => c.share === null && c.path === "/guest-list")).toBe(true)
  })

  it("titre du partage dans l'en-tête", async () => {
    title = "Refonte API"
    await mount()
    expect(screen.getByText("Refonte API")).toBeTruthy()
  })

  describe("/guest sans poignée", () => {
    beforeEach(() => go("/guest"))
    const loc = () => {
      const replace = vi.fn()
      vi.stubGlobal("location", { ...window.location, pathname: window.location.pathname, replace, assign: replace })
      return replace
    }
    it("0 partage ou 403 : partage terminé", async () => {
      shares = 403
      await mount()
      expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    })
    it("0 entrée : partage terminé", async () => {
      shares = []
      await mount()
      expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    })
    it("1 partage : location.replace vers /guest/<handle>", async () => {
      const replace = loc()
      await mount()
      expect(replace).toHaveBeenCalledWith(`/guest/${H}`)
    })
    it("plusieurs partages : sélecteur avec titres, mode et repli de titre", async () => {
      shares = [{ handle: H, title: "Alpha", mode: "rw" }, { handle: H2, title: "", mode: "ro" }]
      await mount()
      expect(screen.getByText("Alpha")).toBeTruthy()
      expect(screen.getByText(i18n.t("conversation.guest.title"))).toBeTruthy()
      expect(screen.getByRole("link", { name: /Alpha/ }).getAttribute("href")).toBe(`/guest/${H}`)
      expect(screen.getByText(i18n.t("conversation.guest.modeRo"))).toBeTruthy()
    })
  })

  describe("autres sessions", () => {
    const menu = () => screen.queryByRole("button", { name: i18n.t("conversation.guest.others") })
    it("pas de menu avec un seul partage", async () => {
      await mount()
      expect(menu()).toBeNull()
    })
    it("menu avec plusieurs partages, navigue vers la poignée choisie", async () => {
      shares = [{ handle: H, title: "Ma session", mode: "rw" }, { handle: H2, title: "Autre", mode: "ro" }]
      const assign = vi.fn()
      vi.stubGlobal("location", { ...window.location, pathname: window.location.pathname, assign })
      await mount()
      fireEvent.pointerDown(menu()!, { button: 0, ctrlKey: false })
      await tick(50)
      fireEvent.click(screen.getByRole("menuitem", { name: /Autre/ }))
      expect(assign).toHaveBeenCalledWith(`/guest/${H2}`)
      expect(screen.queryByRole("menuitem", { name: /Ma session/ })).toBeNull()
    })
    it("Quitter propose les autres partages", async () => {
      shares = [{ handle: H, title: "Ma session", mode: "rw" }, { handle: H2, title: "Autre", mode: "ro" }]
      await mount()
      fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.guest.leave") }))
      await tick(50)
      expect(screen.getByText(i18n.t("conversation.guest.bye"))).toBeTruthy()
      expect(screen.getByRole("link", { name: /Autre/ }).getAttribute("href")).toBe(`/guest/${H2}`)
    })
  })

  it("images : src et lien portent share=<poignée> en mode invité, rien côté hôte", async () => {
    rich = true
    await mount()
    await tick(2100)
    const img = document.querySelector("img")!
    expect(img.getAttribute("src")).toContain(`share=${H}`)
    expect(document.querySelector("a[href*='/conversation/images/']")!.getAttribute("href")).toContain(`share=${H}`)
    cleanup()
    setAuth({ share: undefined })
    render(<Message m={{ r: "user", t: "", img: "1", o: 1, k: 0 }} conv={{ dir: "/d", id: "s1", tools: false }} />)
    expect(document.querySelector("img")!.getAttribute("src")).not.toContain("share=")
  })

  it("poignée avec barre finale : retombe sur le sélecteur", async () => {
    go(`/guest/${H}/`)
    shares = 403
    await mount()
    expect(calls.every((c) => c.share === null)).toBe(true)
  })

  it("sélecteur : panne non 403 de /guest-list : message et Réessayer", async () => {
    go("/guest")
    const base = vi.mocked(fetch).getMockImplementation()!
    let down = true
    vi.mocked(fetch).mockImplementation(async (u, i) => (down && String(u).startsWith("/guest-list") ? new Response("x", { status: 500 }) : base(u, i)))
    await mount()
    expect(screen.getByText(i18n.t("conversation.guest.listFailed"))).toBeTruthy()
    down = false
    shares = [{ handle: H, title: "A", mode: "rw" }, { handle: H2, title: "B", mode: "rw" }]
    fireEvent.click(screen.getByRole("button", { name: i18n.t("conversation.guest.retry") }))
    await tick(50)
    expect(screen.getByRole("link", { name: /B/ })).toBeTruthy()
  })

  it("sondage /guest-list : ignoré onglet caché, arrêté en phase terminée", async () => {
    shares = [{ handle: H, title: "A", mode: "rw" }, { handle: H2, title: "B", mode: "rw" }]
    await mount()
    const n = () => calls.filter((c) => c.path === "/guest-list").length
    const vis = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
    const n0 = n()
    await tick(31000)
    expect(n()).toBe(n0)
    vis.mockReturnValue("visible")
    await tick(30000)
    expect(n()).toBeGreaterThan(n0)
    ended = 403
    await tick(5100)
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    await tick(100)
    const n1 = n()
    await tick(95000)
    expect(n()).toBe(n1)
    vis.mockRestore()
  })

  it("partage terminé : l'écran propose les autres partages valides", async () => {
    shares = [{ handle: H, title: "A", mode: "rw" }, { handle: H2, title: "Autre B", mode: "rw" }]
    await mount()
    ended = 403
    shares = [{ handle: H2, title: "Autre B", mode: "rw" }]
    await tick(5100)
    expect(screen.getByText(i18n.t("conversation.guest.ended"))).toBeTruthy()
    expect(screen.getByRole("link", { name: /Autre B/ }).getAttribute("href")).toBe(`/guest/${H2}`)
  })
})
