// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api } from "@/lib/api"
import FullTerminal from "./FullTerminal"

const x = vi.hoisted(() => ({ focus: vi.fn() }))
vi.mock("@xterm/xterm/css/xterm.css", () => ({}))
vi.mock("@xterm/addon-fit", () => ({ FitAddon: class { fit() {} } }))
vi.mock("@xterm/xterm", () => ({
  Terminal: class {
    cols = 80
    rows = 24
    loadAddon() {}
    open() {}
    focus = x.focus
    write() {}
    onData() {}
    dispose() {}
  },
}))

class FakeWS {
  static OPEN = 1
  static last: FakeWS
  readyState = 0
  sent: string[] = []
  binaryType = ""
  onopen: (() => void) | null = null
  onmessage: (() => void) | null = null
  onclose: (() => void) | null = null
  constructor() {
    FakeWS.last = this
  }
  send(d: string) {
    this.sent.push(d)
  }
  close() {}
}
beforeEach(() => {
  setLang("fr")
  x.focus.mockReset()
  vi.stubGlobal("WebSocket", FakeWS)
  vi.stubGlobal("ResizeObserver", class { observe() {} disconnect() {} })
  vi.spyOn(api, "post").mockResolvedValue({ ok: true, out: "", token: "tk" } as never)
})
afterEach(() => (cleanup(), vi.unstubAllGlobals(), vi.restoreAllMocks()))

const opened = async () => {
  await waitFor(() => expect(FakeWS.last?.onopen).toBeTruthy())
  const ws = FakeWS.last
  ws.readyState = 1
  act(() => ws.onopen!())
  return ws
}

describe("FullTerminal", () => {
  it("par défaut : prend la main dès l'ouverture, focus et taille envoyée", async () => {
    render(<FullTerminal id="s1" />)
    const ws = await opened()
    expect(ws.sent).toContain(JSON.stringify({ write: true }))
    expect(ws.sent.some((s) => s.includes("resize"))).toBe(true)
    expect(x.focus).toHaveBeenCalled()
  })

  it("takeHand=false : lecture seule, ni main prise, ni focus, ni redimensionnement de l'hôte ; le crayon prend la main", async () => {
    render(<FullTerminal id="s1" takeHand={false} />)
    const ws = await opened()
    expect(ws.sent).toEqual([])
    expect(x.focus).not.toHaveBeenCalled()
    fireEvent.click(await screen.findByRole("button", { name: /Lecture seule/ }))
    expect(ws.sent).toContain(JSON.stringify({ write: true }))
    expect(ws.sent.some((s) => s.includes("resize"))).toBe(true)
    expect(x.focus).toHaveBeenCalled()
  })
})
