// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "./api"
import { ApiError } from "./apiError"
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

describe("api /v1, fichiers", () => {
  it("lecture et écriture : dossier et chemin en requête, contenu en corps", async () => {
    await api.fsFile("/p", "a b/é.txt")
    expect(last()).toMatchObject({ url: "/v1/fs/entries/content?dir=%2Fp&path=a+b%2F%C3%A9.txt", method: "GET" })
    await api.fsSave("/p", "a.txt", "x", "e1", true)
    expect(last()).toMatchObject({ url: "/v1/fs/entries/content?dir=%2Fp&path=a.txt", method: "PUT", body: '{"content":"x","etag":"e1","force":true}' })
    await api.fsTree("/p", "")
    expect(last().url).toBe("/v1/fs/tree?dir=%2Fp")
  })
  it("création, renommage, suppression et corbeille", async () => {
    await api.fsNew("/p", "d", "dir")
    expect(last()).toMatchObject({ url: "/v1/fs/entries?dir=%2Fp", method: "POST", body: '{"path":"d","kind":"dir"}' })
    await api.fsRename("/p", "a", "b")
    expect(last()).toMatchObject({ url: "/v1/fs/entries?dir=%2Fp", method: "PATCH", body: '{"from":"a","to":"b"}' })
    await api.fsRm("/p", "a")
    expect(last()).toMatchObject({ url: "/v1/fs/entries?dir=%2Fp&item=a", method: "DELETE", body: undefined })
    await api.fsTrashRestore("/p", "12")
    expect(last()).toMatchObject({ url: "/v1/fs/trash/12?dir=%2Fp", method: "PATCH", body: '{"trashed":false}' })
    await api.fsTrashPurge("/p", "12")
    expect(last()).toMatchObject({ url: "/v1/fs/trash?dir=%2Fp&item=12", method: "DELETE" })
  })
  it("un 409 lève une ApiError portant le code", async () => {
    f.mockResolvedValueOnce(new Response('{"error":{"code":"deleted","message":"m"}}', { status: 409 }))
    const e = await api.fsSave("/p", "a", "x", "e").catch((x: unknown) => x)
    expect(e).toBeInstanceOf(ApiError)
    expect(e).toMatchObject({ status: 409, code: "deleted" })
  })
})
