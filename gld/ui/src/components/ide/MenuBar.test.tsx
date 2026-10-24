// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import type { Right } from "@/lib/auth"
import { MenuBar } from "./MenuBar"
import { commands, type CmdCtx, type EditorHandle } from "./commands"
import type { Tab } from "./ideState"
import { DEFAULT_PREFS } from "./useViewPrefs"

const tab = (path: string, o: Partial<Tab> = {}): Tab => ({ path, content: "x", saved: "x", etag: "e", readOnly: false, ...o })
const handle = (): EditorHandle => ({ undo: vi.fn(), redo: vi.fn(), find: vi.fn(), replace: vi.fn(), gotoLine: vi.fn(), palette: vi.fn(), focus: vi.fn(), zoom: vi.fn(), subscribe: () => () => {} })
const ctx = (o: Partial<CmdCtx> = {}, rights: Right[] = ["fs_read", "fs_write", "exec"]): CmdCtx => {
  const a = tab("a.txt", { content: "y" })
  return {
    can: (r) => rights.includes(r),
    tab: a,
    tabs: [a],
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
const alt = (key: string, target: Element = document.body) => fireEvent.keyDown(target, { key, altKey: true })
const menu = () => screen.findByRole("menu")
const entries = (m: HTMLElement, name?: string) => [...within(m).queryAllByRole("menuitem", { name }), ...within(m).queryAllByRole("menuitemcheckbox", { name })]
const item = async (name: string) => {
  const [el] = entries(await menu(), name)
  if (!el) throw new Error(`entrée absente : ${name}`)
  return el
}
const reasonOf = (el: HTMLElement) => document.getElementById(el.getAttribute("aria-describedby") ?? "")?.textContent

beforeEach(() => {
  setLang("fr")
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} })
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
})

describe("MenuBar", () => {
  // Pas de liste d'exception : toutes les commandes du registre ont une place dans un menu.
  it("chaque commande du registre est dans un seul menu, chaque entrée vient du registre", async () => {
    render(<MenuBar ctx={ctx()} />)
    const seen: string[] = []
    for (const k of ["f", "e", "r", "a", "l", "t"]) {
      act(() => void alt(k))
      const m = await menu()
      for (const el of entries(m)) seen.push(el.dataset.cmd ?? "(sans commande)")
      fireEvent.keyDown(m, { key: "Escape" })
      await waitFor(() => expect(screen.queryByRole("menu")).toBeNull())
    }
    expect(seen.sort()).toEqual(commands.map((c) => c.id).sort())
  })

  it("menus dans l'ordre, raccourcis affichés", async () => {
    render(<MenuBar ctx={ctx()} />)
    const bar = screen.getByRole("menubar", { name: "Menu de l'espace de travail" })
    expect(within(bar).getAllByRole("menuitem").map((x) => x.textContent)).toEqual(["Fichier", "Édition", "Rechercher", "Affichage", "Aller à", "Terminal"])
    alt("f")
    const save = await item("Enregistrer")
    expect(save.textContent).toContain("Ctrl+S")
    expect(save.getAttribute("aria-keyshortcuts")).toBe("Control+S")
  })

  it("navigation au clavier : flèches entre les menus puis dans les entrées", async () => {
    render(<MenuBar ctx={ctx()} />)
    const [file, edit] = within(screen.getByRole("menubar")).getAllByRole("menuitem")
    act(() => file.focus())
    fireEvent.keyDown(file, { key: "ArrowRight" })
    await waitFor(() => expect(document.activeElement).toBe(edit))
    fireEvent.keyDown(edit, { key: "ArrowDown" })
    const undo = await item("Annuler")
    await waitFor(() => expect(document.activeElement).toBe(undo))
    fireEvent.keyDown(undo, { key: "ArrowDown" })
    const redo = await item("Rétablir")
    await waitFor(() => expect(document.activeElement).toBe(redo))
  })

  it("Alt+lettre et F1 laissés à Monaco, xterm et aux champs", () => {
    render(
      <>
        <MenuBar ctx={ctx()} />
        <div className="monaco-editor">
          <div tabIndex={0} data-testid="mon" />
        </div>
        <textarea aria-label="xterm" />
      </>
    )
    expect(alt("f", screen.getByTestId("mon"))).toBe(true)
    expect(alt("f", screen.getByLabelText("xterm"))).toBe(true)
    expect(fireEvent.keyDown(screen.getByTestId("mon"), { key: "F1" })).toBe(true)
    expect(screen.queryByRole("menu")).toBeNull()
    expect(alt("f")).toBe(false)
  })

  it("lecture seule : entrées d'écriture désactivées avec la raison, rien ne s'exécute", async () => {
    const c = ctx({ readOnly: true })
    render(<MenuBar ctx={c} />)
    alt("f")
    const save = await item("Enregistrer")
    expect(save.getAttribute("aria-disabled")).toBe("true")
    expect(reasonOf(save)).toBe("Fichier en lecture seule")
    fireEvent.click(save)
    expect(c.save).not.toHaveBeenCalled()
    expect((await item("Ouverture rapide")).getAttribute("aria-disabled")).toBeNull()
  })

  it("sans exec ou terminal désactivé : entrées Terminal désactivées avec la raison", async () => {
    const { unmount } = render(<MenuBar ctx={ctx({}, ["fs_read", "fs_write"])} />)
    alt("t")
    expect(reasonOf(await item("Ouvrir ou replier le terminal"))).toMatch(/exec/)
    unmount()
    render(<MenuBar ctx={ctx({ terminal: "disabled" })} />)
    alt("t")
    const go = await item("Aller au terminal de session")
    expect(go.getAttribute("aria-disabled")).toBe("true")
    expect(reasonOf(go)).toMatch(/GLD_WEB_TERMINAL/)
  })

  it("Affichage › zone terminal : désactivée sans exec, pendant le chargement ou terminal coupé, chacun avec sa raison ; rien ne s'exécute", async () => {
    const c = ctx({}, ["fs_read", "fs_write"])
    const { unmount } = render(<MenuBar ctx={c} />)
    alt("a")
    const off = await item("Afficher la zone terminal")
    expect(off.getAttribute("aria-disabled")).toBe("true")
    expect(reasonOf(off)).toBe("Le terminal demande le droit « exec »")
    fireEvent.click(off)
    expect(c.prefs.set).not.toHaveBeenCalled()
    unmount()
    for (const [terminal, r] of [["loading", /chargement/i], ["disabled", /GLD_WEB_TERMINAL/], ["nosessions", /sessions_read/]] as const) {
      const { unmount: u } = render(<MenuBar ctx={ctx({ terminal })} />)
      alt("a")
      const el = await item("Afficher la zone terminal")
      expect(reasonOf(el), terminal).toMatch(r)
      u()
    }
  })

  it("raccourcis traduits : Maj en français, Shift en anglais, nom ARIA canonique", async () => {
    const { unmount } = render(<MenuBar ctx={ctx()} />)
    alt("e")
    const redo = await item("Rétablir")
    expect(redo.textContent).toContain("Ctrl+Maj+Z")
    expect(redo.getAttribute("aria-keyshortcuts")).toBe("Control+Shift+Z")
    unmount()
    setLang("en")
    render(<MenuBar ctx={ctx()} />)
    alt("e")
    expect((await item("Redo")).textContent).toContain("Ctrl+Shift+Z")
  })

  it("bascules cochées selon les préférences ; une entrée active lance sa commande", async () => {
    const c = ctx({ prefs: { ...DEFAULT_PREFS, wordWrap: true, set: vi.fn() } })
    render(<MenuBar ctx={c} />)
    alt("a")
    const wrap = await item("Retour à la ligne automatique")
    expect(wrap.getAttribute("role")).toBe("menuitemcheckbox")
    expect(wrap.getAttribute("aria-checked")).toBe("true")
    expect((await item("Minimap")).getAttribute("aria-checked")).toBe("false")
    fireEvent.click(wrap)
    expect(c.prefs.set).toHaveBeenCalledTimes(1)
    expect(c.prefs.set).toHaveBeenCalledWith("wordWrap", false)
    alt("f")
    fireEvent.click(await item("Ouverture rapide"))
    expect(c.openQuick).toHaveBeenCalledTimes(1)
  })
})

describe("MenuBar : focus après une commande", () => {
  const setup = (o: Partial<CmdCtx> = {}) => {
    const order: string[] = []
    const h = handle()
    const c = ctx({ editor: h, ...o })
    render(
      <>
        <MenuBar ctx={c} />
        <div className="monaco-editor" data-testid="box">
          <textarea aria-label="ed" />
          <input aria-label="find-widget" />
        </div>
      </>
    )
    const edEl = screen.getByLabelText("ed")
    vi.mocked(h.focus).mockImplementation(() => (order.push("focus"), edEl.focus()))
    vi.mocked(h.find).mockImplementation(() => (order.push("find"), screen.getByLabelText("find-widget").focus()))
    vi.mocked(h.undo).mockImplementation(() => void order.push("undo"))
    return { c, h, order, edEl }
  }
  const trigger = (name: string) => within(screen.getByRole("menubar")).getByRole("menuitem", { name })
  const closed = () => waitFor(() => expect(screen.queryByRole("menu")).toBeNull())

  it("Rechercher : l'éditeur reçoit le focus avant la commande, le widget de Monaco le garde", async () => {
    const { h, order } = setup()
    alt("r")
    fireEvent.click(await item("Rechercher"))
    await closed()
    expect(order).toEqual(["focus", "find"])
    expect(h.find).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(screen.getByLabelText("find-widget"))
  })

  it("Annuler et Retour à la ligne : focus dans l'éditeur, une seule fois", async () => {
    const { c, h, order, edEl } = setup()
    alt("e")
    fireEvent.click(await item("Annuler"))
    await closed()
    expect(order).toEqual(["focus", "undo"])
    expect(document.activeElement).toBe(edEl)
    alt("a")
    fireEvent.click(await item("Retour à la ligne automatique"))
    await closed()
    expect(h.focus).toHaveBeenCalledTimes(2)
    expect(c.prefs.set).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(edEl)
  })

  it("bascule de l'arborescence : pas de focus dans l'éditeur, retour au menu", async () => {
    const { h } = setup()
    alt("a")
    fireEvent.click(await item("Afficher l'arborescence"))
    await closed()
    expect(h.focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger("Affichage"))
  })

  it("Fermer l'onglet et Fermer tous les onglets : pas de focus dans un éditeur qui se démonte, retour au menu", async () => {
    const { c, h } = setup()
    for (const name of ["Fermer l'onglet", "Fermer tous les onglets"]) {
      alt("f")
      fireEvent.click(await item(name))
      await closed()
      await waitFor(() => expect(document.activeElement).toBe(trigger("Fichier")))
    }
    expect(c.closeTab).toHaveBeenCalledTimes(1)
    expect(c.closeAll).toHaveBeenCalledTimes(1)
    expect(h.focus).not.toHaveBeenCalled()
  })

  it("Échap : le focus revient au menu, rien ne s'exécute", async () => {
    const { h } = setup()
    alt("f")
    fireEvent.keyDown(await menu(), { key: "Escape" })
    await closed()
    expect(h.focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger("Fichier"))
  })
})
