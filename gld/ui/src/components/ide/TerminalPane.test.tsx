// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { setAuth } from "@/lib/auth"
import { api, type Data } from "@/lib/api"
import { TerminalPane } from "./TerminalPane"

vi.mock("../FullTerminal", () => ({
  default: ({ id }: { id: string }) => <div data-testid="term">{id}</div>,
}))

const s = (id: string, cwd: string, mtime: number) =>
  ({ id, cwd, mtime }) as never
const data = (live: string[], sessions: unknown[]) =>
  ({
    live,
    groups: [{ project: "p", missing: false, sessions }],
  }) as unknown as Data
const setup = (enabled: boolean, d: Data) => {
  vi.spyOn(api, "termInfo").mockResolvedValue({ enabled })
  vi.spyOn(api, "data").mockResolvedValue(d)
}
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
  setAuth({ state: "open" } as never)
})
afterEach(() => (cleanup(), vi.restoreAllMocks()))

describe("TerminalPane", () => {
  it("terminal désactivé : message", async () => {
    setup(false, data([], []))
    render(<TerminalPane dir="/p" />)
    expect(await screen.findByText(/GLD_WEB_TERMINAL=1/)).toBeTruthy()
  })
  it("aucune session vivante dans ce dossier : explication", async () => {
    setup(true, data(["b"], [s("a", "/p", 1), s("b", "/autre", 2)]))
    render(<TerminalPane dir="/p" />)
    expect(
      await screen.findByText(/se branche sur une session Claude ouverte/)
    ).toBeTruthy()
    expect(screen.queryByTestId("term")).toBeNull()
  })
  it("une session vivante : monte le terminal avec son identifiant", async () => {
    setup(true, data(["a"], [s("a", "/p", 1), s("z", "/p", 9)]))
    render(<TerminalPane dir="/p" />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
    expect(screen.queryByRole("combobox")).toBeNull()
  })
  it("plusieurs : sélecteur, la plus récente d'abord, choix mémorisé", async () => {
    setup(true, data(["a", "b"], [s("a", "/p", 1), s("b", "/p", 5)]))
    render(<TerminalPane dir="/p" />)
    const sel = (await screen.findByRole("combobox")) as HTMLSelectElement
    expect([...sel.options].map((o) => o.value)).toEqual(["b", "a"])
    expect(screen.getByTestId("term").textContent).toBe("b")
    fireEvent.change(sel, { target: { value: "a" } })
    expect(screen.getByTestId("term").textContent).toBe("a")
    expect(localStorage.getItem("gld.ide./p.term")).toBe("a")
  })
  it("la session se termine : message, pas d'exception", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<TerminalPane dir="/p" />)
    await screen.findByTestId("term")
    vi.spyOn(api, "data").mockResolvedValue(data([], [s("a", "/p", 1)]))
    vi.advanceTimersByTime(11000)
    await waitFor(() => expect(screen.queryByTestId("term")).toBeNull())
    expect(screen.getByText("La session est terminée.")).toBeTruthy()
    vi.useRealTimers()
  })
  it("la session montée qui se termine donne le message de fin", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    setup(true, data(["a"], [s("a", "/p", 1)]))
    render(<TerminalPane dir="/p" />)
    await screen.findByTestId("term")
    vi.spyOn(api, "data").mockResolvedValue(data([], [s("a", "/p", 1)]))
    vi.advanceTimersByTime(11000)
    expect(await screen.findByText("La session est terminée.")).toBeTruthy()
    vi.useRealTimers()
  })
  it("compare les dossiers sans le séparateur final", async () => {
    setup(true, data(["a"], [s("a", "/p/", 1)]))
    render(<TerminalPane dir="/p" />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
  })
  it("sans le droit exec : rien n'est rendu et rien n'est appelé", async () => {
    setup(true, data(["a"], [s("a", "/p", 1)]))
    setAuth({ state: "ok", home: "h", rights: { h: ["fs_read"] } } as never)
    const { container } = render(<TerminalPane dir="/p" />)
    await new Promise((r) => setTimeout(r, 20))
    expect(container.innerHTML).toBe("")
    expect(api.termInfo).not.toHaveBeenCalled()
  })
})
