// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

type D = typeof import("./useDraft") & typeof import("./importQueues") & { api: typeof import("@/lib/api").api }
const ok = { ok: true, out: "" }

// Le magasin lit le stockage à l'import : chaque test repart d'un module neuf.
async function fresh(stored: unknown): Promise<D> {
  localStorage.clear()
  if (stored) localStorage.setItem("gld.drafts", JSON.stringify(stored))
  vi.resetModules()
  const api = (await import("@/lib/api")).api
  const u = await import("./useDraft")
  u.guardDrafts(() => true) // auth.ts le ferme tant qu'aucun compte n'est ouvert
  return { ...u, ...(await import("./importQueues")), api }
}
afterEach(() => vi.restoreAllMocks())
beforeEach(() => localStorage.clear())

describe("useDraft", () => {
  it("ne garde que le texte", async () => {
    const d = await fresh(null)
    d.setDraft("s1", { text: "a" })
    expect(d.draftOf("s1")).toEqual({ text: "a" })
  })
})

describe("importLocalQueues", () => {
  const stored = { s1: { text: "x", queue: ["a", "b"] }, s2: { text: "", queue: ["c"] } }
  it("poste chaque message en ordre, vide les files, ne rejoue pas", async () => {
    const d = await fresh(stored)
    const add = vi.spyOn(d.api, "queueAdd").mockResolvedValue(ok)
    await d.importLocalQueues()
    expect(add.mock.calls).toEqual([["s1", "a"], ["s1", "b"], ["s2", "c"]])
    const raw = JSON.parse(localStorage.getItem("gld.drafts") ?? "{}")
    expect(raw.s1.queue).toBeUndefined()
    expect(raw.s1.text).toBe("x")
    expect(localStorage.getItem("gld.queueImported")).toBe("1")
    await d.importLocalQueues()
    expect(add).toHaveBeenCalledTimes(3)
  })
  it("laisse tout en place et sans garde si le réseau échoue", async () => {
    const d = await fresh(stored)
    vi.spyOn(d.api, "queueAdd").mockResolvedValue({ ok: false, out: "", net: true })
    await d.importLocalQueues()
    expect(localStorage.getItem("gld.queueImported")).toBeNull()
    d.setDraft("s1", { text: "y" }) // une sauvegarde ne doit pas effacer la file locale
    expect(JSON.parse(localStorage.getItem("gld.drafts") ?? "{}").s1.queue).toEqual(["a", "b"])
  })
  it("saute un envoi incertain trop ancien", async () => {
    const d = await fresh({ s1: { text: "", queue: ["a", "b"], sending: Date.now() - 60_000 } })
    const add = vi.spyOn(d.api, "queueAdd").mockResolvedValue(ok)
    await d.importLocalQueues()
    expect(add.mock.calls).toEqual([["s1", "b"]])
  })
})
