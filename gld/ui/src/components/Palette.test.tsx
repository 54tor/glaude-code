// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type Session } from "@/lib/api"
import { Palette } from "./Palette"

// cmdk mesure sa liste ; jsdom n'a ni ResizeObserver ni scrollIntoView.
class RO { observe() {} unobserve() {} disconnect() {} }
globalThis.ResizeObserver = RO as unknown as typeof ResizeObserver
Element.prototype.scrollIntoView = () => {}

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const sess = (id: string, title: string, mtime: number): Session => ({ id, dir: "d", title, named: true, cwd: "/home/u/p", mtime, kb: 1, tok: { in: 0, out: 0, cr: 0, cw: 0 } })
const g = { project: "/home/u/p", missing: false, sessions: [] }

describe("Palette", () => {
  it("filtre les sessions et ouvre celle choisie", async () => {
    vi.spyOn(api, "skills").mockResolvedValue({ skills: [] } as never)
    const read = vi.fn()
    const rows = [
      { s: sess("1", "déploiement caddy", 2), g },
      { s: sess("2", "audit sécurité", 1), g },
    ]
    await act(async () => void render(<Palette open onOpenChange={() => {}} rows={rows} home="/home/u" go={() => {}} read={read} />))
    fireEvent.change(screen.getByRole("combobox"), { target: { value: "caddy" } })
    expect(screen.queryByText("audit sécurité")).toBeNull()
    fireEvent.click(screen.getByText("déploiement caddy"))
    expect(read).toHaveBeenCalledWith(rows[0].s)
  })
})
