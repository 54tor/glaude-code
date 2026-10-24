// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { QuickOpen } from "./QuickOpen"

beforeEach(() => setLang("fr"))
afterEach(cleanup)

const files = ["README.md", "src/app.ts", "src/lib/api.ts"]
const mount = (o: Partial<Parameters<typeof QuickOpen>[0]> = {}) => {
  const pick = vi.fn()
  const close = vi.fn()
  render(<QuickOpen open files={files} state="ready" onPick={pick} onClose={close} {...o} />)
  return { pick, close }
}

describe("QuickOpen", () => {
  it("filtre, flèches et Entrée ouvrent le fichier choisi", () => {
    const { pick } = mount()
    const input = screen.getByRole("combobox")
    fireEvent.change(input, { target: { value: "ts" } })
    expect(screen.getAllByRole("option").map((o) => o.getAttribute("title"))).toEqual(["src/app.ts", "src/lib/api.ts"])
    fireEvent.keyDown(input, { key: "ArrowDown" })
    fireEvent.keyDown(input, { key: "ArrowDown" })
    fireEvent.keyDown(input, { key: "ArrowUp" })
    fireEvent.keyDown(input, { key: "ArrowDown" })
    expect(screen.getAllByRole("option")[1].getAttribute("aria-selected")).toBe("true")
    fireEvent.keyDown(input, { key: "Enter" })
    expect(pick).toHaveBeenCalledWith("src/lib/api.ts")
  })

  it("Échap ferme", () => {
    const { close } = mount()
    fireEvent.keyDown(screen.getByRole("combobox"), { key: "Escape" })
    expect(close).toHaveBeenCalled()
  })

  it("bandeaux : index partiel, indexation en cours, aucun résultat", () => {
    mount({ state: "partial" })
    expect(screen.getByText(/Index partiel/)).toBeTruthy()
    cleanup()
    mount({ state: "building", files: [] })
    expect(screen.getByText(/Indexation en cours/)).toBeTruthy()
    cleanup()
    mount()
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "zzz" } })
    expect(screen.getByText("Aucun fichier ne correspond")).toBeTruthy()
  })
})
