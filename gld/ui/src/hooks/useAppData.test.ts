// @vitest-environment jsdom
import { act, cleanup, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

const toastError = vi.fn()
const toastDismiss = vi.fn()
vi.mock("sonner", () => ({ toast: { error: (...a: unknown[]) => toastError(...a), dismiss: (...a: unknown[]) => toastDismiss(...a) } }))

const { useAppData } = await import("./useAppData")
const { setAuth } = await import("@/lib/auth")

const DATA = { home: "/h", root: "/h/.claude", groups: [], dirs: [], live: [], waiting: [], vgroups: [], favs: [], projects: [], providers: [] }
const SNIPS = { dir: "/h/s", files: [], scopes: [] }

// Faux serveur : /v1/state, /v1/snippets et /v1/state/stamp ; down rend des 502.
function server() {
  const st = { down: false, stamp: 1, calls: { api: 0, stamp: 0 } }
  const json = (body: unknown, status = 200) => Promise.resolve(new Response(JSON.stringify(body), { status }))
  vi.stubGlobal(
    "fetch",
    vi.fn((url: string) => {
      if (st.down) return json({}, 502)
      if (url === "/v1/state") return st.calls.api++, json(DATA)
      if (url === "/v1/snippets") return json(SNIPS)
      if (url === "/v1/state/stamp") return st.calls.stamp++, json({ stamp: st.stamp })
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
    expect(result.current.error).toBe("Erreur : 502")
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("Erreur : 502"), expect.objectContaining({ id: "server-down" }))
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

  it("403 : « accès refusé » en session établie seulement ; en mode ouvert, une erreur comme avant", async () => {
    vi.stubGlobal("fetch", vi.fn(() => Promise.resolve(new Response("{}", { status: 403 }))))
    setAuth({ state: "open" })
    const open = renderHook(() => useAppData())
    await flush()
    expect(open.result.current.denied).toBe(false)
    expect(open.result.current.error).toBe("Erreur : 403")
    expect(toastError).toHaveBeenCalledWith(expect.stringContaining("Erreur : 403"), expect.objectContaining({ id: "server-down" }))
    open.unmount()
    toastError.mockClear()
    setAuth({ state: "ok" })
    const ok = renderHook(() => useAppData())
    await flush()
    expect(ok.result.current.denied).toBe(true)
    expect(toastError).not.toHaveBeenCalled()
    setAuth({ state: "loading" })
  })
})

describe("markClosing", () => {
  it("masque une session fermée tant que le serveur la liste encore ouverte", async () => {
    const { markClosing } = await import("./useAppData")
    const stillListed = { ...DATA, live: ["s1", "s2"], waiting: ["s1"] }
    vi.stubGlobal(
      "fetch",
      vi.fn((url: string) => Promise.resolve(new Response(JSON.stringify(url === "/v1/snippets" ? SNIPS : stillListed), { status: 200 })))
    )
    markClosing(["s1"])
    const { result } = renderHook(() => useAppData())
    await act(async () => {
      await result.current.reload()
    })
    expect(result.current.data?.live).toEqual(["s2"])
    expect(result.current.data?.waiting).toEqual([])
  })
})
