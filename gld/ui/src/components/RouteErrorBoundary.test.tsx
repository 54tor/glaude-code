// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { RouteErrorBoundary } from "./RouteErrorBoundary"

let boom: Error | null = null
const Child = () => {
  if (boom) throw boom
  return <p>route ok</p>
}
const page = (k: string) => (
  <div>
    <nav>barre latérale</nav>
    <RouteErrorBoundary key={k}>
      <Child />
    </RouteErrorBoundary>
  </div>
)
const reload = vi.fn()

beforeEach(() => {
  boom = new TypeError("Failed to fetch dynamically imported module: /assets/ide.js")
  reload.mockReset()
  vi.stubGlobal("location", { ...window.location, reload })
  vi.spyOn(console, "error").mockImplementation(() => {})
})
afterEach(() => (cleanup(), vi.unstubAllGlobals(), vi.restoreAllMocks()))

describe("RouteErrorBoundary", () => {
  it("une route qui lève : message, raison et bouton, le reste de l'application reste affiché", () => {
    render(page("a"))
    expect(screen.getByText("barre latérale")).toBeTruthy()
    expect(screen.getByRole("button", { name: "Recharger la page" })).toBeTruthy()
    expect(screen.getByText(/Failed to fetch dynamically imported module/)).toBeTruthy()
    expect(console.error).toHaveBeenCalledWith(expect.anything(), boom, expect.anything())
  })

  it("le bouton recharge la page", () => {
    render(page("a"))
    fireEvent.click(screen.getByRole("button", { name: "Recharger la page" }))
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it("une autre erreur ne montre pas son texte brut", () => {
    boom = new Error("secret interne")
    render(page("a"))
    expect(screen.queryByText(/secret interne/)).toBeNull()
  })

  it("changer de route efface l'erreur", () => {
    const { rerender } = render(page("a"))
    boom = null
    rerender(page("b"))
    expect(screen.getByText("route ok")).toBeTruthy()
  })
})
