// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { useWatch } from "./useWatch"

class FakeES {
  static all: FakeES[] = []
  onopen: (() => void) | null = null
  onmessage: ((e: MessageEvent) => void) | null = null
  onerror: (() => void) | null = null
  readyState = 0
  closed = false
  url: string
  constructor(url: string) {
    this.url = url
    FakeES.all.push(this)
  }
  close() {
    this.closed = true
    this.readyState = 2
  }
  open() {
    this.readyState = 1
    act(() => this.onopen?.())
  }
  // Fermeture définitive : l'EventSource ne réessaiera plus de lui-même.
  die() {
    this.readyState = 2
    act(() => this.onerror?.())
  }
  // Erreur passagère : le navigateur réessaie seul.
  blip() {
    this.readyState = 0
    act(() => this.onerror?.())
  }
}
const live = () => FakeES.all.filter((x) => !x.closed)
const tick = (ms: number) => act(() => vi.advanceTimersByTime(ms))

beforeEach(() => {
  vi.useFakeTimers()
  FakeES.all = []
  vi.stubGlobal("EventSource", FakeES)
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe("useWatch : reconnexion", () => {
  it("fermeture définitive : nouvel essai à 5 s, 15 s, puis 30 s", () => {
    const { result } = renderHook(() => useWatch("/p", [""], () => {}))
    expect(result.current).toMatchObject({ connected: false, state: "connecting" })
    tick(300)
    expect(live()).toHaveLength(1)
    live()[0].open()
    expect(result.current).toMatchObject({ connected: true, state: "open" })
    for (const s of [5, 15, 30, 30]) {
      live()[0].die()
      expect(result.current).toMatchObject({ connected: false, state: "waiting", retryIn: s })
      expect(live()).toHaveLength(0)
      tick(s * 1000 - 1)
      expect(live()).toHaveLength(0)
      tick(1)
      expect(live()).toHaveLength(1)
    }
  })

  it("une ouverture réussie remet l'attente à zéro et vaut resynchronisation", () => {
    const ev = vi.fn()
    const { result } = renderHook(() => useWatch("/p", [""], ev))
    tick(300)
    live()[0].open()
    expect(ev).not.toHaveBeenCalled()
    live()[0].die()
    tick(5000)
    live()[0].die()
    tick(15000)
    live()[0].open()
    expect(ev).toHaveBeenCalledWith({ path: "", type: "resync" })
    expect(result.current.state).toBe("open")
    live()[0].die()
    expect(result.current.retryIn).toBe(5)
  })

  it("erreur passagère : pas de nouveau flux, état interrompu", () => {
    const { result } = renderHook(() => useWatch("/p", [""], () => {}))
    tick(300)
    live()[0].open()
    live()[0].blip()
    expect(result.current).toMatchObject({ connected: false, state: "browserRetry" })
    tick(60000)
    expect(FakeES.all).toHaveLength(1)
  })

  it("démontage : plus aucun minuteur ni flux", () => {
    const { unmount } = renderHook(() => useWatch("/p", [""], () => {}))
    tick(300)
    live()[0].die()
    unmount()
    expect(vi.getTimerCount()).toBe(0)
    tick(60000)
    expect(FakeES.all).toHaveLength(1)
    expect(live()).toHaveLength(0)
  })

  it("changement des dossiers pendant une attente : un seul flux, rouvert sur la nouvelle liste", () => {
    const { rerender } = renderHook(({ dirs }) => useWatch("/p", dirs, () => {}), { initialProps: { dirs: [""] } })
    tick(300)
    live()[0].die()
    rerender({ dirs: ["", "src"] })
    tick(300)
    expect(live()).toHaveLength(1)
    expect(live()[0].url).toContain("paths=src")
    tick(60000)
    expect(live()).toHaveLength(1)
  })
})
