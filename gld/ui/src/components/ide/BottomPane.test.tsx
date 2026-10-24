// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type Data } from "@/lib/api"
import { BottomPane } from "./BottomPane"
import { DEFAULT_PREFS, type ViewPrefs } from "./useViewPrefs"

vi.mock("../FullTerminal", () => ({
  default: ({ id, takeHand }: { id: string; takeHand?: boolean }) => <div data-testid="term" data-take={String(takeHand)}>{id}</div>,
}))

const s = (id: string, cwd: string, mtime: number) => ({ id, cwd, mtime }) as never
const data = (live: string[], sessions: unknown[]) => ({ live, groups: [{ project: "p", missing: false, sessions }] }) as unknown as Data
const setup = (enabled: boolean, d: Data) => {
  vi.spyOn(api, "termInfo").mockResolvedValue({ enabled })
  vi.spyOn(api, "data").mockResolvedValue(d)
}
const prefs = (o: Partial<ViewPrefs> = {}): ViewPrefs => ({ ...DEFAULT_PREFS, set: vi.fn(), ...o })
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  setAuth({ state: "open" } as never)
})
afterEach(() => (cleanup(), vi.restoreAllMocks()))

describe("BottomPane", () => {
  it("terminal désactivé par le serveur : barre, état, aucun autre appel", async () => {
    setup(false, data([], []))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    expect(screen.getByText("Terminal")).toBeTruthy()
    expect(await screen.findByText(/désactivé \(GLD_WEB_TERMINAL=1\)/)).toBeTruthy()
    expect(api.data).not.toHaveBeenCalled()
    expect(screen.queryByTestId("term")).toBeNull()
  })
  it("sans le droit exec : bouton désactivé avec la raison, aucun appel", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } } as never)
    render(<BottomPane dir="/p" prefs={prefs()} />)
    const b = screen.getByRole("button") as HTMLButtonElement
    expect(b.disabled).toBe(true)
    expect(b.title).toMatch(/exec/)
    await new Promise((r) => setTimeout(r, 20))
    expect(api.termInfo).not.toHaveBeenCalled()
    expect(api.data).not.toHaveBeenCalled()
  })
  it("aucune session vivante dans le dossier : aide, zone repliée", async () => {
    setup(true, data(["b"], [s("a", "/p", 1), s("b", "/autre", 2)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    expect(await screen.findByText(/lancez `claude` dans ce dossier/)).toBeTruthy()
    expect(screen.queryByTestId("term")).toBeNull()
  })
  it("une session vivante, aucun choix mémorisé : ouverte d'emblée en lecture seule", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    const t = await screen.findByTestId("term")
    expect(t.textContent).toBe("a")
    expect(t.dataset.take).toBe("false")
  })
  it("choix mémorisé replié : prioritaire, rien de monté", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs({ showTerminalChosen: true, showTerminal: false })} />)
    await waitFor(() => expect(api.data).toHaveBeenCalled())
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByTestId("term")).toBeNull()
  })
  it("choix mémorisé ouvert : monté", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs({ showTerminalChosen: true, showTerminal: true })} />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
  })
  it("le bouton enregistre un choix explicite", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    const p = prefs()
    render(<BottomPane dir="/p" prefs={p} />)
    await screen.findByTestId("term")
    fireEvent.click(screen.getByRole("button", { name: "Masquer le panneau du bas" }))
    expect(p.set).toHaveBeenCalledWith("showTerminal", false)
  })
  it("plusieurs sessions : sélecteur collant quand api.data réordonne", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    setup(true, data(["a", "b"], [s("a", "/p", 1), s("b", "/p", 5)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    const el = await screen.findByTestId("term")
    expect(el.textContent).toBe("b")
    vi.spyOn(api, "data").mockResolvedValue(data(["a", "b"], [s("a", "/p", 9), s("b", "/p", 5)]))
    vi.advanceTimersByTime(11000)
    await waitFor(() => expect((screen.getByRole("combobox") as HTMLSelectElement).options[0].value).toBe("a"))
    expect(screen.getByTestId("term").textContent).toBe("b")
    vi.useRealTimers()
  })
  it("replié : rien n'est monté", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs({ showTerminalChosen: true, showTerminal: false })} />)
    await new Promise((r) => setTimeout(r, 20))
    expect(screen.queryByTestId("term")).toBeNull()
  })
  it("authentification en cours : aucun appel term-info ni data, même si l'utilisateur s'avère sans exec", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    setAuth({ state: "loading", rights: undefined, home: undefined } as never)
    render(<BottomPane dir="/p" prefs={prefs()} />)
    await new Promise((r) => setTimeout(r, 20))
    act(() => setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } } as never))
    await new Promise((r) => setTimeout(r, 20))
    expect(api.termInfo).not.toHaveBeenCalled()
    expect(api.data).not.toHaveBeenCalled()
  })
  it("onglet masqué : le sondage saute, reprend au retour de visibilité", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    await screen.findByTestId("term")
    const n = vi.mocked(api.data).mock.calls.length
    const hidden = vi.spyOn(document, "hidden", "get").mockReturnValue(true)
    vi.advanceTimersByTime(31000)
    expect(vi.mocked(api.data).mock.calls.length).toBe(n)
    hidden.mockReturnValue(false)
    act(() => void document.dispatchEvent(new Event("visibilitychange")))
    expect(vi.mocked(api.data).mock.calls.length).toBe(n + 1)
    vi.useRealTimers()
  })
  it("bouton de repli : aria-expanded, pas aria-pressed", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    await screen.findByTestId("term")
    const b = screen.getByRole("button", { name: "Masquer le panneau du bas" })
    expect(b.getAttribute("aria-expanded")).toBe("true")
    expect(b.hasAttribute("aria-pressed")).toBe(false)
  })
  it("pluriel : une session, deux sessions", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    const { unmount } = render(<BottomPane dir="/p" prefs={prefs()} />)
    expect(await screen.findByText("1 session en direct")).toBeTruthy()
    unmount()
    setup(true, data(["a", "b"], [s("a", "/p", 1), s("b", "/p", 2)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    expect(await screen.findByText("2 sessions en direct")).toBeTruthy()
  })
  it("compare les dossiers sans le séparateur final", async () => {
    setup(true, data(["a"], [s("a", "/p/", 1)]))
    render(<BottomPane dir="/p" prefs={prefs()} />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
  })
  it("demande de focus : ouvre la zone repliée, focus une seule fois", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    const p = prefs({ showTerminalChosen: true, showTerminal: false })
    const { rerender } = render(<BottomPane dir="/p" prefs={p} focusN={0} />)
    await waitFor(() => expect(api.data).toHaveBeenCalled())
    rerender(<BottomPane dir="/p" prefs={p} focusN={1} />)
    await waitFor(() => expect(p.set).toHaveBeenCalledWith("showTerminal", true))
    const open = prefs({ showTerminalChosen: true, showTerminal: true })
    rerender(<BottomPane dir="/p" prefs={open} focusN={1} />)
    await screen.findByTestId("term")
    await waitFor(() => expect(document.activeElement).toBe(screen.getByLabelText("Terminal de session")))
    ;(document.activeElement as HTMLElement).blur()
    rerender(<BottomPane dir="/p" prefs={p} focusN={1} />)
    rerender(<BottomPane dir="/p" prefs={open} focusN={1} />)
    await screen.findByTestId("term")
    await new Promise((r) => setTimeout(r, 20))
    expect(document.activeElement).toBe(document.body)
  })
})
