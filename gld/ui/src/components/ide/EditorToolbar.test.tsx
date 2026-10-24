// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import type { Right } from "@/lib/auth"
import { EditorToolbar } from "./EditorToolbar"
import type { CmdCtx, EditorHandle } from "./commands"
import type { Tab } from "./ideState"
import { DEFAULT_PREFS } from "./useViewPrefs"

const tab = (path: string, o: Partial<Tab> = {}): Tab => ({ path, content: "x", saved: "x", etag: "e", readOnly: false, ...o })
const handle = (): EditorHandle => ({ undo: vi.fn(), redo: vi.fn(), find: vi.fn(), replace: vi.fn(), gotoLine: vi.fn(), palette: vi.fn(), focus: vi.fn(), zoom: vi.fn(), subscribe: () => () => {} })
const ctx = (o: Partial<CmdCtx> = {}, rights: Right[] = ["fs_read", "fs_write", "exec"]): CmdCtx => {
  const a = tab("a.txt", { content: "y" })
  return {
    can: (r) => rights.includes(r),
    tab: a,
    tabs: [a, tab("b.txt")],
    readOnly: false,
    editor: handle(),
    prefs: { ...DEFAULT_PREFS, set: vi.fn() },
    terminal: "ready",
    treeShown: true,
    save: vi.fn(),
    saveAll: vi.fn(),
    newFile: vi.fn(),
    newFolder: vi.fn(),
    closeTab: vi.fn(),
    closeAll: vi.fn(),
    openQuick: vi.fn(),
    focusSession: vi.fn(),
    ...o,
  }
}
const btn = (name: string) => screen.getByRole("button", { name })
const reasonOf = (el: HTMLElement) => document.getElementById((el.getAttribute("aria-describedby") ?? "").split(" ")[0])?.textContent

beforeEach(() => setLang("fr"))
afterEach(cleanup)

describe("EditorToolbar", () => {
  it("chaque bouton lance sa commande sur la poignée ou le contexte", () => {
    const c = ctx()
    render(<EditorToolbar ctx={c} />)
    const h = c.editor!
    fireEvent.click(btn("Enregistrer"))
    expect(c.save).toHaveBeenCalledTimes(1)
    fireEvent.click(btn("Tout enregistrer"))
    expect(c.saveAll).toHaveBeenCalledTimes(1)
    fireEvent.click(btn("Annuler"))
    expect(h.undo).toHaveBeenCalled()
    fireEvent.click(btn("Rétablir"))
    expect(h.redo).toHaveBeenCalled()
    fireEvent.click(btn("Rechercher"))
    expect(h.find).toHaveBeenCalled()
    fireEvent.click(btn("Remplacer"))
    expect(h.replace).toHaveBeenCalled()
    fireEvent.click(btn("Aller à la ligne"))
    expect(h.gotoLine).toHaveBeenCalled()
    fireEvent.click(btn("Palette de commandes"))
    expect(h.palette).toHaveBeenCalled()
    fireEvent.click(btn("Retour à la ligne automatique"))
    expect(c.prefs.set).toHaveBeenCalledWith("wordWrap", true)
    fireEvent.click(btn("Agrandir le texte"))
    expect(c.prefs.set).toHaveBeenCalledWith("fontSize", 14)
    fireEvent.click(btn("Réduire le texte"))
    expect(c.prefs.set).toHaveBeenCalledWith("fontSize", 12)
    fireEvent.click(btn("Fermer tous les onglets"))
    expect(c.closeAll).toHaveBeenCalled()
    fireEvent.click(btn("Aller au terminal de session"))
    expect(c.focusSession).toHaveBeenCalled()
    fireEvent.click(btn("Ouvrir ou replier le terminal"))
    expect(c.prefs.set).toHaveBeenCalledWith("showTerminal", false)
  })

  it("les commandes de l'éditeur lui donnent le focus avant de s'exécuter : la saisie rapide de Monaco l'exige", () => {
    for (const [name, fn] of [["Aller à la ligne", "gotoLine"], ["Palette de commandes", "palette"], ["Rechercher", "find"]] as const) {
      const c = ctx()
      const h = c.editor!
      const order: string[] = []
      vi.mocked(h.focus).mockImplementation(() => void order.push("focus"))
      vi.mocked(h[fn]).mockImplementation(() => void order.push(fn))
      const { unmount } = render(<EditorToolbar ctx={c} />)
      fireEvent.click(btn(name))
      expect(order).toEqual(["focus", fn])
      unmount()
    }
  })

  it("une commande sans rapport avec l'éditeur ne lui prend pas le focus", () => {
    const c = ctx()
    render(<EditorToolbar ctx={c} />)
    fireEvent.click(btn("Tout enregistrer"))
    fireEvent.click(btn("Ouvrir ou replier le terminal"))
    expect(c.editor!.focus).not.toHaveBeenCalled()
  })

  it("sans éditeur ou bouton désactivé : ni focus ni commande", () => {
    const c = ctx({ editor: null })
    render(<EditorToolbar ctx={c} />)
    fireEvent.click(btn("Retour à la ligne automatique"))
    expect(c.prefs.set).toHaveBeenCalledWith("wordWrap", true)
    cleanup()
    const r = ctx({ readOnly: true })
    render(<EditorToolbar ctx={r} />)
    fireEvent.click(btn("Remplacer"))
    fireEvent.click(btn("Annuler"))
    expect(r.editor!.focus).not.toHaveBeenCalled()
    expect(r.editor!.replace).not.toHaveBeenCalled()
    expect(r.editor!.undo).not.toHaveBeenCalled()
  })

  it("le retour à la ligne est un bouton bascule qui reflète la préférence", () => {
    render(<EditorToolbar ctx={ctx({ prefs: { ...DEFAULT_PREFS, wordWrap: true, set: vi.fn() } })} />)
    expect(btn("Retour à la ligne automatique").getAttribute("aria-pressed")).toBe("true")
  })

  it("lecture seule : boutons d'écriture désactivés, jamais masqués, avec la raison en description", () => {
    const c = ctx({ readOnly: true })
    render(<EditorToolbar ctx={c} />)
    for (const n of ["Enregistrer", "Annuler", "Rétablir", "Remplacer"]) {
      const b = btn(n)
      expect(b.getAttribute("aria-disabled")).toBe("true")
      expect(reasonOf(b)).toBe("Fichier en lecture seule")
    }
    fireEvent.click(btn("Annuler"))
    expect(c.editor!.undo).not.toHaveBeenCalled()
    expect(btn("Rechercher").getAttribute("aria-disabled")).toBeNull()
  })

  it("sans fs_write : enregistrer désactivé avec la raison", () => {
    const c = ctx({}, ["fs_read"])
    render(<EditorToolbar ctx={c} />)
    fireEvent.click(btn("Enregistrer"))
    fireEvent.click(btn("Tout enregistrer"))
    expect(c.save).not.toHaveBeenCalled()
    expect(c.saveAll).not.toHaveBeenCalled()
    expect(reasonOf(btn("Tout enregistrer"))).toBe("Vous n'avez pas le droit de modifier les fichiers de ce dossier")
  })

  it("sans éditeur monté : commandes de la poignée désactivées avec la raison", () => {
    render(<EditorToolbar ctx={ctx({ editor: null })} />)
    expect(reasonOf(btn("Aller à la ligne"))).toBe("Aucun éditeur actif")
  })

  it("navigation au clavier par flèches dans la barre", async () => {
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    render(<EditorToolbar ctx={ctx()} />)
    const bar = screen.getByRole("toolbar", { name: "Actions de l'éditeur" })
    const first = btn("Enregistrer")
    first.focus()
    fireEvent.keyDown(first, { key: "ArrowRight" })
    await waitFor(() => expect(document.activeElement).toBe(btn("Tout enregistrer")))
    expect(bar.contains(document.activeElement)).toBe(true)
    vi.unstubAllGlobals()
  })
  it("infobulle : libellé et raccourci ; désactivé : la raison en plus, et la description pointe sur la raison", async () => {
    vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
    render(<EditorToolbar ctx={ctx({ readOnly: true })} />)
    const tip = () => document.querySelector<HTMLElement>("[data-slot=tooltip-content]")
    fireEvent.focus(btn("Rechercher"))
    await waitFor(() => expect(tip()).toBeTruthy())
    expect(tip()!.textContent).toContain("Rechercher")
    expect(tip()!.querySelector("kbd")?.textContent).toBe("Ctrl+F")
    expect(tip()!.textContent).not.toContain("lecture seule")
    const on = (btn("Rechercher").getAttribute("aria-describedby") ?? "").split(" ")
    expect(on.map((id) => document.getElementById(id)?.textContent ?? "").join()).not.toContain("lecture seule")
    fireEvent.blur(btn("Rechercher"))
    await waitFor(() => expect(tip()).toBeNull())
    const off = btn("Annuler")
    fireEvent.focus(off)
    await waitFor(() => expect(tip()?.textContent).toContain("Annuler"))
    expect(tip()!.querySelector("kbd")?.textContent).toBe("Ctrl+Z")
    expect(tip()!.textContent).toContain("Fichier en lecture seule")
    const ids = off.getAttribute("aria-describedby")!.split(" ")
    expect(ids.map((id) => document.getElementById(id)?.textContent)).toContain("Fichier en lecture seule")
    vi.unstubAllGlobals()
  })

})
