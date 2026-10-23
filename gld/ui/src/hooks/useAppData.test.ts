// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const toastError = vi.fn()
const toastDismiss = vi.fn()
vi.mock("sonner", () => ({ toast: { error: (...a: unknown[]) => toastError(...a), dismiss: (...a: unknown[]) => toastDismiss(...a) } }))

const { useAppData } = await import("./useAppData")

const DATA = { home: "/h", root: "/h/.claude", groups: [], dirs: [], live: [], waiting: [], vgroups: [], favs: [], tags: {}, projects: [], providers: [] }
const SNIPS = { dir: "/h/s", files: [], scopes: [] }

// Faux serveur : /api, /snippet-list et /sessions-stamp ; down rend des 502.
function server() {
  const st = { down: false, stamp: 1, calls: { api: 0, stamp: 0 } }
  const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (st.down) return json({}, 502)
      if (url === "/api") return st.calls.api++, json(DATA)
      if (url === "/snippet-list") return json(SNIPS)
      if (url === "/sessions-stamp") return st.calls.stamp++, json({ stamp: st.stamp })
      return json({}, 404)
    })
  )
  return st
}

let visibility: DocumentVisibilityState = "visible"
beforeEach(() => {
  vi.useFakeTimers()
  visibility = "visible"
  vi.spyOn(document, "visibilityState", "get").mockImplementation(() => visibility)
  toastError.mockClear()
  toastDismiss.mockClear()
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

const flush = () => act(() => vi.advanceTimersByTimeAsync(0))

describe("useAppData", () => {
  it("charge sessions et snippets au montage", async () => {
    server()
    const { result } = renderHook(() => useAppData())
    expect(result.current.data).toBeNull()
    await flush()
    expect(result.current.data?.home).toBe("/h")
    expect(result.current.snips?.dir).toBe("/h/s")
    expect(result.current.error).toBeNull()
    expect(result.current.now).toBeGreaterThan(0)
  })

  it("signale un échec par error et un toast unique, puis l'efface au chargement suivant", async () => {
    const st = server()
    st.down = true
    const { result } = renderHook(() => useAppData())
    await flush()
    expect(result.current.data).toBeNull()
    expect(result.current.error).toBe("HTTP 502")
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("HTTP 502"), expect.objectContaining({ id: "server-down" }))
    st.down = false
    await act(() => result.current.reload())
    expect(result.current.error).toBeNull()
    expect(result.current.data).not.toBeNull()
    expect(toastDismiss).toHaveBeenCalledWith("server-down")
  })

  it("relit dès que le tampon des transcripts change", async () => {
    const st = server()
    renderHook(() => useAppData())
    await flush()
    const loads = st.calls.api
    await act(() => vi.advanceTimersByTimeAsync(3_000)) // premier relevé : il sert de référence
    expect(st.calls.api).toBe(loads)
    st.stamp = 2
    await act(() => vi.advanceTimersByTimeAsync(3_000))
    expect(st.calls.api).toBe(loads + 1)
  })

  it("ne relit rien quand l'onglet est caché, et relit à son retour", async () => {
    const st = server()
    renderHook(() => useAppData())
    await flush()
    const loads = st.calls.api
    visibility = "hidden"
    await act(() => vi.advanceTimersByTimeAsync(20_000))
    expect(st.calls.api).toBe(loads)
    expect(st.calls.stamp).toBe(0)
    visibility = "visible"
    await act(async () => {
      document.dispatchEvent(new Event("visibilitychange"))
      await vi.advanceTimersByTimeAsync(0)
    })
    expect(st.calls.api).toBe(loads + 1)
  })
})
