// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { authFetch, setAuth } from "./auth"

beforeEach(() => {
  setAuth({ state: "open", csrf: "", share: undefined })
  vi.stubGlobal("fetch", vi.fn(async () => new Response("{}")))
})
afterEach(() => vi.unstubAllGlobals())

const sent = () => new Headers((vi.mocked(fetch).mock.calls[0][1] as RequestInit | undefined)?.headers)

describe("authFetch X-Gld-Share", () => {
  it("absent sans partage", async () => {
    await authFetch("/v1/sessions/s/conversation")
    expect(sent().has("X-Gld-Share")).toBe(false)
  })
  it.each(["GET", "POST"])("présent sur %s quand share est posé", async (method) => {
    setAuth({ share: "0123456789abcdef", csrf: "c" })
    await authFetch("/v1/sessions/s/queue", { method })
    expect(sent().get("X-Gld-Share")).toBe("0123456789abcdef")
  })
})
