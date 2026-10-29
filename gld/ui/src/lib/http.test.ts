// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setAuth } from "./auth"
import { ApiError, http } from "./http"
import { qs, seg } from "./apiError"

const reply = (status: number, body?: string, h?: Record<string, string>) => vi.fn(async () => new Response(body, { status, headers: h }))
beforeEach(() => setAuth({ state: "open", csrf: "c", share: undefined }))
afterEach(() => vi.unstubAllGlobals())

describe("http", () => {
  it("préfixe /v1, envoie le corps JSON et le CSRF", async () => {
    const f = reply(200, '{"a":1}')
    vi.stubGlobal("fetch", f)
    expect(await http.put<{ a: number }>("/keepwarm", { x: 1 })).toEqual({ a: 1 })
    const [url, init] = f.mock.calls[0] as unknown as [string, RequestInit]
    expect(url).toBe("/v1/keepwarm")
    expect(init.method).toBe("PUT")
    expect(init.body).toBe('{"x":1}')
    expect(new Headers(init.headers).get("X-Gld-CSRF")).toBe("c")
  })
  it("204 rend undefined", async () => {
    vi.stubGlobal("fetch", reply(204))
    expect(await http.del("/queue/x")).toBeUndefined()
  })
  it("erreur JSON uniforme", async () => {
    vi.stubGlobal("fetch", reply(409, '{"error":{"code":"conflict","message":"boom"}}', { "Retry-After": "7" }))
    const e = await http.get("/x").catch((x) => x)
    expect(e).toBeInstanceOf(ApiError)
    expect(e).toMatchObject({ status: 409, code: "conflict", message: "boom", retry: 7, net: false })
  })
  it("corps non JSON toléré", async () => {
    vi.stubGlobal("fetch", reply(502, "bad gateway"))
    expect(await http.get("/x").catch((x) => x)).toMatchObject({ status: 502, code: "http_502" })
  })
  it("2xx non JSON : bad_response", async () => {
    vi.stubGlobal("fetch", reply(200, "<html></html>"))
    expect(await http.get("/x").catch((x) => x)).toMatchObject({ status: 200, code: "bad_response" })
  })
  it("réseau : status 0 et net", async () => {
    vi.stubGlobal("fetch", vi.fn(async () => Promise.reject(new TypeError("down"))))
    expect(await http.get("/x").catch((x) => x)).toMatchObject({ status: 0, net: true })
  })
})

describe("qs / seg", () => {
  it("répète item, ignore le vide", () => {
    expect(qs({ dir: "/a b", item: ["d:1", "d:2"], q: "", n: undefined, force: true })).toBe("?dir=%2Fa+b&item=d%3A1&item=d%3A2&force=1")
    expect(qs({})).toBe("")
  })
  it("seg encode le slash", () => expect(seg("p/x/y.md")).toBe("p%2Fx%2Fy.md"))
})
