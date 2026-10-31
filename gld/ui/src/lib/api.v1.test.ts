// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "./api"
import { setAuth } from "./auth"

let f: ReturnType<typeof vi.fn>
beforeEach(() => {
  setAuth({ state: "open", csrf: "c", share: undefined })
  f = vi.fn(async () => new Response("{}", { status: 200 }))
  vi.stubGlobal("fetch", f)
})
afterEach(() => vi.unstubAllGlobals())

const last = () => {
  const [url, init] = f.mock.calls.at(-1) as unknown as [string, RequestInit]
  return { url, method: init.method, body: init.body }
}

describe("api /v1, sessions", () => {
  it("l'identifiant de session est un segment, le dossier part en requête", async () => {
    await api.conv({ dir: "-p", id: "s1" }, true, { limit: 50, before: 0, anchors: true })
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/conversation?dir=-p&tools=1&limit=50&before=0", method: "GET" })
    await api.sessionLive("-p", "s1")
    expect(last().url).toBe("/v1/sessions/s1/live?dir=-p")
    await api.convRewind("-p", "s1", 0)
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/conversation?dir=-p&after=0", method: "DELETE" })
  })
  it("un fichier chargé passe en segment encodé", async () => {
    await api.sessionLoadedFile("s1", "/h/.claude/CLAUDE.md")
    expect(last().url).toBe("/v1/sessions/s1/loaded/files/%2Fh%2F.claude%2FCLAUDE.md")
  })
  it("ouvrir : session ou récentes du projet", async () => {
    await api.open({ dir: "-p", id: "s1", args: "--x", prompt: "go" })
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/process?dir=-p", method: "PUT", body: '{"args":"--x","prompt":"go"}' })
    await api.open({ dir: "-p" })
    expect(last()).toMatchObject({ url: "/v1/projects/-p/process", method: "PUT" })
  })
  it("une grande liste part en lots de 100 au plus, URL sous 8 Ko même avec un long nom de dossier", async () => {
    const dir = "-" + "d".repeat(180)
    const items = Array.from({ length: 250 }, (_, index) => ({ dir, id: `00000000-0000-4000-8000-${String(index).padStart(12, "0")}` }))
    f.mockImplementation(async () => new Response('{"deleted":1,"spared":0,"message":"x"}', { status: 200 }))
    const said = await api.rmSessions(items)
    const urls = f.mock.calls.map((values) => values[0] as string)
    expect(urls.length).toBeGreaterThanOrEqual(3)
    for (const rawUrl of urls) {
      expect(rawUrl.length).toBeLessThan(8000)
      expect(rawUrl.split("item=").length - 1).toBeLessThanOrEqual(100)
    }
    expect(urls.join("&").split("item=").length - 1).toBe(250)
    expect(said.deleted).toBe(urls.length)
  })
  it("suppression et corbeille : ?item répété, sans corps", async () => {
    await api.rmSessions([{ dir: "-p", id: "a" }, { dir: "-p", id: "b" }])
    expect(last()).toMatchObject({ url: "/v1/sessions?item=-p%3Aa&item=-p%3Ab", method: "DELETE", body: undefined })
    await api.trashSessionsPurge(["a", "b"])
    expect(last().url).toBe("/v1/trash/sessions?item=a&item=b")
    await api.trashSessionsRestore(["a"])
    expect(last()).toMatchObject({ url: "/v1/trash/sessions?item=a", method: "PATCH", body: '{"trashed":false}' })
  })
  it("file d'attente : ajout, retrait, relance", async () => {
    await api.queueAdd("s1", "x")
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/queue", method: "POST", body: '{"text":"x"}' })
    await api.queueRm("s1", "q1")
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/queue/q1", method: "DELETE" })
    await api.queueRetry("s1", "q1")
    expect(last()).toMatchObject({ url: "/v1/sessions/s1/queue/q1", method: "PATCH", body: '{"state":"queued"}' })
  })
  it("dossiers et sauvegardes d'onglets", async () => {
    await api.dirsAdd("/p", true)
    expect(last()).toMatchObject({ url: "/v1/directories", method: "POST", body: '{"dir":"/p","children":true}' })
    await api.dirsRemove("/p")
    expect(last()).toMatchObject({ url: "/v1/directories?dir=%2Fp", method: "DELETE" })
    await api.tabsRm("2026-01-01T00:00:00+01:00")
    expect(last().url).toBe("/v1/tabs/2026-01-01T00%3A00%3A00%2B01%3A00")
  })
})
