// @vitest-environment jsdom
import { useState } from "react"
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import { api } from "@/lib/api"
import type { Right } from "@/lib/auth"
import { TreeToolbar } from "./TreeToolbar"
import type { CmdCtx } from "./commands"
import { DEFAULT_PREFS } from "./useViewPrefs"

const ctx = (rights: Right[] = ["fs_read", "fs_write"]): CmdCtx => ({
  can: (r) => rights.includes(r),
  tab: undefined,
  tabs: [],
  readOnly: false,
  editor: null,
  prefs: { ...DEFAULT_PREFS, set: vi.fn() },
  treeShown: true,
  save: vi.fn(),
  saveAll: vi.fn(),
  newFile: vi.fn(),
  newFolder: vi.fn(),
  closeTab: vi.fn(),
  closeAll: vi.fn(),
  openQuick: vi.fn(),
})
const tree = () => ({ refreshAll: vi.fn(), collapseAll: vi.fn() })
const btn = (name: string) => screen.getByRole("button", { name })

beforeEach(() => setLang("fr"))
afterEach(() => (cleanup(), vi.restoreAllMocks()))

describe("TreeToolbar", () => {
  it("nouveau fichier, nouveau dossier, actualiser, tout replier, ouverture rapide", () => {
    const c = ctx()
    const t = tree()
    render(<TreeToolbar dir="/p" tree={t} ctx={c} filter="" onFilter={() => {}} onRestored={() => {}} />)
    fireEvent.click(btn("Nouveau fichier"))
    expect(c.newFile).toHaveBeenCalled()
    fireEvent.click(btn("Nouveau dossier"))
    expect(c.newFolder).toHaveBeenCalled()
    fireEvent.click(btn("Actualiser"))
    expect(t.refreshAll).toHaveBeenCalled()
    fireEvent.click(btn("Tout replier"))
    expect(t.collapseAll).toHaveBeenCalled()
    fireEvent.click(btn("Ouverture rapide"))
    expect(c.openQuick).toHaveBeenCalled()
  })

  it("sans fs_write : création désactivée (pas masquée) avec la raison", () => {
    const c = ctx(["fs_read"])
    render(<TreeToolbar dir="/p" tree={tree()} ctx={c} filter="" onFilter={() => {}} onRestored={() => {}} />)
    const b = btn("Nouveau fichier")
    expect(b.getAttribute("aria-disabled")).toBe("true")
    fireEvent.click(b)
    expect(c.newFile).not.toHaveBeenCalled()
    expect(document.getElementById(b.getAttribute("aria-describedby")!)?.textContent).toMatch(/pas le droit/)
  })

  it("le filtre remonte la saisie, vider remet tout", () => {
    const f = vi.fn()
    const { rerender } = render(<TreeToolbar dir="/p" tree={tree()} ctx={ctx()} filter="" onFilter={f} onRestored={() => {}} />)
    fireEvent.change(screen.getByRole("searchbox", { name: "Filtrer par nom" }), { target: { value: "Src" } })
    expect(f).toHaveBeenLastCalledWith("Src")
    rerender(<TreeToolbar dir="/p" tree={tree()} ctx={ctx()} filter="Src" onFilter={f} onRestored={() => {}} />)
    fireEvent.click(btn("Effacer le filtre"))
    expect(f).toHaveBeenLastCalledWith("")
  })

  it("effacer le filtre par le bouton rend le focus au champ ; Échap vide aussi", () => {
    const Host = () => {
      const [f, setF] = useState("Src")
      return <TreeToolbar dir="/p" tree={tree()} ctx={ctx()} filter={f} onFilter={setF} onRestored={() => {}} />
    }
    render(<Host />)
    const input = screen.getByRole("searchbox", { name: "Filtrer par nom" }) as HTMLInputElement
    btn("Effacer le filtre").focus()
    fireEvent.click(btn("Effacer le filtre"))
    expect(input.value).toBe("")
    expect(screen.queryByRole("button", { name: "Effacer le filtre" })).toBeNull()
    expect(document.activeElement).toBe(input)
    fireEvent.change(input, { target: { value: "x" } })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(input.value).toBe("")
    expect(document.activeElement).toBe(input)
  })

  it("Corbeille ouvre le dialogue de la corbeille", async () => {
    const trash = vi.spyOn(api, "fsTrash").mockResolvedValue({ items: [] })
    render(<TreeToolbar dir="/p" tree={tree()} ctx={ctx()} filter="" onFilter={() => {}} onRestored={() => {}} />)
    fireEvent.click(btn("Corbeille"))
    expect(await screen.findByText("La corbeille est vide.")).toBeTruthy()
    expect(trash).toHaveBeenCalledWith("/p")
  })
})
