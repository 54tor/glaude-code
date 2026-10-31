// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { setLang } from "@/i18n"
import type { Right } from "@/lib/auth"
import { MenuBar } from "./MenuBar"
import { commands, type CmdCtx, type EditorHandle } from "./commands"
import type { Tab } from "./ideState"
import { DEFAULT_PREFS } from "./useViewPrefs"

const tab = (path: string, tab: Partial<Tab> = {}): Tab => ({ path, content: "x", saved: "x", etag: "e", readOnly: false, ...tab })
const handle = (): EditorHandle => ({ undo: vi.fn(), redo: vi.fn(), find: vi.fn(), replace: vi.fn(), gotoLine: vi.fn(), palette: vi.fn(), focus: vi.fn(), zoom: vi.fn(), subscribe: () => () => {} })
const ctx = (cmdCtx: Partial<CmdCtx> = {}, rights: Right[] = ["fs_read", "fs_write", "exec"]): CmdCtx => {
  const localTab = tab("a.txt", { content: "y" })
  return {
    can: (raw) => rights.includes(raw),
    tab: localTab,
    tabs: [localTab],
    readOnly: false,
    editor: handle(),
    prefs: { ...DEFAULT_PREFS, set: vi.fn() },
    treeShown: true,
    save: vi.fn(),
    saveAll: vi.fn(),
    newFile: vi.fn(),
    newFolder: vi.fn(),
    closeTab: vi.fn(),
    closeAll: vi.fn(),
    openQuick: vi.fn(),
    ...cmdCtx,
  }
}
const alt = (key: string, target: Element = document.body) => fireEvent.keyDown(target, { key, altKey: true })
const menu = () => screen.findByRole("menu")
const entries = (element: HTMLElement, name?: string) => [...within(element).queryAllByRole("menuitem", { name }), ...within(element).queryAllByRole("menuitemcheckbox", { name })]
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
    for (const localKey of ["f", "e", "r", "a", "l"]) {
      act(() => void alt(localKey))
      const element = await menu()
      for (const el of entries(element)) seen.push(el.dataset.cmd ?? "(sans commande)")
      fireEvent.keyDown(element, { key: "Escape" })
      await waitFor(() => expect(screen.queryByRole("menu")).toBeNull())
    }
    expect(seen.sort()).toEqual(commands.map((command) => command.id).sort())
  })

  it("menus dans l'ordre, raccourcis affichés", async () => {
    render(<MenuBar ctx={ctx()} />)
    const bar = screen.getByRole("menubar", { name: "Menu de l'espace de travail" })
    expect(within(bar).getAllByRole("menuitem").map((element) => element.textContent)).toEqual(["Fichier", "Édition", "Rechercher", "Affichage", "Aller à"])
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
    const cmdCtx = ctx({ readOnly: true })
    render(<MenuBar ctx={cmdCtx} />)
    alt("f")
    const save = await item("Enregistrer")
    expect(save.getAttribute("aria-disabled")).toBe("true")
    expect(reasonOf(save)).toBe("Fichier en lecture seule")
    fireEvent.click(save)
    expect(cmdCtx.save).not.toHaveBeenCalled()
    expect((await item("Ouverture rapide")).getAttribute("aria-disabled")).toBeNull()
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

  it("Alt+lettre ignoré sous une modale : aucun menu ne s'ouvre derrière", () => {
    render(
      <>
        <MenuBar ctx={ctx()} />
        <div role="dialog">
          <button>dans la modale</button>
        </div>
        <div role="alertdialog">
          <button>alerte</button>
        </div>
      </>
    )
    expect(alt("f", screen.getByText("dans la modale"))).toBe(true)
    expect(alt("f", screen.getByText("alerte"))).toBe(true)
    expect(screen.queryByRole("menu")).toBeNull()
  })

  it("bascules cochées selon les préférences ; une entrée active lance sa commande", async () => {
    const cmdCtx = ctx({ prefs: { ...DEFAULT_PREFS, wordWrap: true, set: vi.fn() } })
    render(<MenuBar ctx={cmdCtx} />)
    alt("a")
    const wrap = await item("Retour à la ligne automatique")
    expect(wrap.getAttribute("role")).toBe("menuitemcheckbox")
    expect(wrap.getAttribute("aria-checked")).toBe("true")
    expect((await item("Minimap")).getAttribute("aria-checked")).toBe("false")
    fireEvent.click(wrap)
    expect(cmdCtx.prefs.set).toHaveBeenCalledTimes(1)
    expect(cmdCtx.prefs.set).toHaveBeenCalledWith("wordWrap", false)
    alt("f")
    fireEvent.click(await item("Ouverture rapide"))
    expect(cmdCtx.openQuick).toHaveBeenCalledTimes(1)
  })
})

describe("MenuBar : focus après une commande", () => {
  const setup = (cmdCtx: Partial<CmdCtx> = {}) => {
    const order: string[] = []
    const editorHandle = handle()
    const localCmdCtx = ctx({ editor: editorHandle, ...cmdCtx })
    render(
      <>
        <MenuBar ctx={localCmdCtx} />
        <div className="monaco-editor" data-testid="box">
          <textarea aria-label="ed" />
          <input aria-label="find-widget" />
        </div>
      </>
    )
    const edEl = screen.getByLabelText("ed")
    vi.mocked(editorHandle.focus).mockImplementation(() => (order.push("focus"), edEl.focus()))
    vi.mocked(editorHandle.find).mockImplementation(() => (order.push("find"), screen.getByLabelText("find-widget").focus()))
    vi.mocked(editorHandle.undo).mockImplementation(() => void order.push("undo"))
    return { c: localCmdCtx, h: editorHandle, order, edEl }
  }
  const trigger = (name: string) => within(screen.getByRole("menubar")).getByRole("menuitem", { name })
  const closed = () => waitFor(() => expect(screen.queryByRole("menu")).toBeNull())

  it("Rechercher : l'éditeur reçoit le focus avant la commande, le widget de Monaco le garde", async () => {
    const { h: editorHandle, order } = setup()
    alt("r")
    fireEvent.click(await item("Rechercher"))
    await closed()
    expect(order).toEqual(["focus", "find"])
    expect(editorHandle.find).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(screen.getByLabelText("find-widget"))
  })

  it("Annuler et Retour à la ligne : focus dans l'éditeur, une seule fois", async () => {
    const { c: cmdCtx, h: editorHandle, order, edEl } = setup()
    alt("e")
    fireEvent.click(await item("Annuler"))
    await closed()
    expect(order).toEqual(["focus", "undo"])
    expect(document.activeElement).toBe(edEl)
    alt("a")
    fireEvent.click(await item("Retour à la ligne automatique"))
    await closed()
    expect(editorHandle.focus).toHaveBeenCalledTimes(2)
    expect(cmdCtx.prefs.set).toHaveBeenCalledTimes(1)
    expect(document.activeElement).toBe(edEl)
  })

  it("bascule de l'arborescence : pas de focus dans l'éditeur, retour au menu", async () => {
    const { h: editorHandle } = setup()
    alt("a")
    fireEvent.click(await item("Afficher l'arborescence"))
    await closed()
    expect(editorHandle.focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger("Affichage"))
  })

  it("Fermer l'onglet et Fermer tous les onglets : pas de focus dans un éditeur qui se démonte, retour au menu", async () => {
    const { c: cmdCtx, h: editorHandle } = setup()
    for (const name of ["Fermer l'onglet", "Fermer tous les onglets"]) {
      alt("f")
      fireEvent.click(await item(name))
      await closed()
      await waitFor(() => expect(document.activeElement).toBe(trigger("Fichier")))
    }
    expect(cmdCtx.closeTab).toHaveBeenCalledTimes(1)
    expect(cmdCtx.closeAll).toHaveBeenCalledTimes(1)
    expect(editorHandle.focus).not.toHaveBeenCalled()
  })

  it("Échap : le focus revient au menu, rien ne s'exécute", async () => {
    const { h: editorHandle } = setup()
    alt("f")
    fireEvent.keyDown(await menu(), { key: "Escape" })
    await closed()
    expect(editorHandle.focus).not.toHaveBeenCalled()
    expect(document.activeElement).toBe(trigger("Fichier"))
  })
})
