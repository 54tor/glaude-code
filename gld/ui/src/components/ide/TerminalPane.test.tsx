// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { TerminalContent } from "./TerminalPane"
import type { Session } from "./useTerminalAvailability"

vi.mock("../FullTerminal", () => ({
  default: ({ id, takeHand }: { id: string; takeHand?: boolean }) => <div data-testid="term" data-take={String(takeHand)}>{id}</div>,
}))

const s = (id: string): Session => ({ id, cwd: "/p", mtime: 1 }) as never
beforeEach(() => {
  setLang("fr")
  localStorage.clear()
})
afterEach(cleanup)

describe("TerminalContent", () => {
  it("aucune session : explication", async () => {
    render(<TerminalContent dir="/p" sessions={[]} />)
    expect(screen.getByText(/se branche sur une session Claude ouverte/)).toBeTruthy()
  })
  it("une session : montée sans prendre la main, pas de sélecteur", async () => {
    render(<TerminalContent dir="/p" sessions={[s("a")]} />)
    expect((await screen.findByTestId("term")).dataset.take).toBe("false")
    expect(screen.queryByRole("combobox")).toBeNull()
  })
  it("plusieurs : sélecteur, choix mémorisé", async () => {
    render(<TerminalContent dir="/p" sessions={[s("b"), s("a")]} />)
    const sel = screen.getByRole("combobox") as HTMLSelectElement
    expect([...sel.options].map((o) => o.value)).toEqual(["b", "a"])
    fireEvent.change(sel, { target: { value: "a" } })
    expect((await screen.findByTestId("term")).textContent).toBe("a")
    expect(localStorage.getItem("gld.ide./p.term")).toBe("a")
  })
  it("la session montée se termine, seule : message de fin", async () => {
    const { rerender } = render(<TerminalContent dir="/p" sessions={[s("a")]} />)
    rerender(<TerminalContent dir="/p" sessions={[]} />)
    expect(screen.queryByTestId("term")).toBeNull()
    expect(screen.getByText("La session est terminée.")).toBeTruthy()
  })
  it("la session montée se termine alors que d'autres existent : message et sélecteur, aucun remontage silencieux", async () => {
    const { rerender } = render(<TerminalContent dir="/p" sessions={[s("a"), s("b")]} />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
    rerender(<TerminalContent dir="/p" sessions={[s("b")]} />)
    expect(screen.queryByTestId("term")).toBeNull()
    expect(screen.getByText("La session est terminée.")).toBeTruthy()
    fireEvent.change(screen.getByRole("combobox", { name: "Session à afficher" }), { target: { value: "b" } })
    expect((await screen.findByTestId("term")).textContent).toBe("b")
  })
  it("demande de focus à usage unique : consommée une fois, pas rejouée au remontage", async () => {
    const done = vi.fn()
    const { unmount } = render(<TerminalContent dir="/p" sessions={[s("a")]} focus onFocused={done} />)
    expect(document.activeElement).toBe(screen.getByLabelText("Terminal de session"))
    expect(done).toHaveBeenCalledTimes(1)
    unmount()
    render(<TerminalContent dir="/p" sessions={[s("a")]} focus={false} onFocused={done} />)
    expect(document.activeElement).toBe(document.body)
    expect(done).toHaveBeenCalledTimes(1)
  })
  it("session montée collante : un choix mémorisé qui revient en vie ne la remplace pas", async () => {
    localStorage.setItem("gld.ide./p.term", "b")
    const { rerender } = render(<TerminalContent dir="/p" sessions={[s("a")]} />)
    expect((await screen.findByTestId("term")).textContent).toBe("a")
    rerender(<TerminalContent dir="/p" sessions={[s("a"), s("b")]} />)
    expect(screen.getByTestId("term").textContent).toBe("a")
    expect((screen.getByRole("combobox") as HTMLSelectElement).value).toBe("a")
  })
})
