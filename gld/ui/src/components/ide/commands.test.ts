import { describe, expect, it, vi } from "vitest"
import fr from "@/i18n/fr/ide.json"
import en from "@/i18n/en/ide.json"
import type { Right } from "@/lib/auth"
import type { Tab } from "./ideState"
import { commands, type CmdCtx, type EditorHandle } from "./commands"
import { DEFAULT_PREFS } from "./useViewPrefs"

const at = (o: unknown, key: string) =>
  key
    .split(".")
    .reduce<unknown>(
      (x, k) =>
        x && typeof x === "object"
          ? (x as Record<string, unknown>)[k]
          : undefined,
      o
    )
const has = (key: string) =>
  typeof at(fr, key) === "string" && typeof at(en, key) === "string"

const tab = (over: Partial<Tab> = {}): Tab => ({
  path: "a.ts",
  content: "x",
  saved: "x",
  etag: "e",
  ...over,
})
const handle = (): EditorHandle => ({
  undo: vi.fn(),
  redo: vi.fn(),
  find: vi.fn(),
  replace: vi.fn(),
  gotoLine: vi.fn(),
  palette: vi.fn(),
  focus: vi.fn(),
  zoom: vi.fn(),
  subscribe: () => () => {},
})
const ctx = (
  over: Partial<CmdCtx> = {},
  rights: Right[] = ["fs_read", "fs_write", "exec"]
): CmdCtx => {
  const t = tab()
  return {
    can: (r) => rights.includes(r),
    tab: t,
    tabs: [t],
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
    ...over,
  }
}
const byId = (id: string) => commands.find((c) => c.id === id)!
const WRITE = [
  "file.newFile",
  "file.newFolder",
  "file.save",
  "file.saveAll",
  "edit.undo",
  "edit.redo",
  "find.replace",
]
const REQUIRED = [
  "file.newFile",
  "file.newFolder",
  "file.save",
  "file.saveAll",
  "file.closeTab",
  "file.closeAll",
  "file.quickOpen",
  "edit.undo",
  "edit.redo",
  "find.find",
  "find.replace",
  "goto.line",
  "goto.palette",
  "view.wordWrap",
  "view.minimap",
  "view.fontPlus",
  "view.fontMinus",
  "view.fontReset",
  "view.toggleTree",
  "view.toggleTerminal",
  "view.toggleStatus",
  "terminal.toggle",
  "terminal.focusSession",
]
// Un contexte par façon d'être désactivé : chaque commande désactivée dans l'un d'eux doit dire pourquoi.
const blocked: CmdCtx[] = [
  ctx({
    readOnly: true,
    tab: tab({ readOnly: true }),
    tabs: [tab({ readOnly: true, content: "y" })],
  }),
  ctx({}, ["fs_read"]),
  ctx({ tab: undefined, tabs: [], editor: null }),
  ctx({ terminal: "disabled" }),
  ctx({ terminal: "loading" }),
  ctx({ terminal: "nosessions" }),
  ctx({ terminal: "unavailable" }),
  ctx({ treeShown: false }),
]

describe("registre de commandes", () => {
  it("contient les commandes attendues, chacune une seule fois", () => {
    const ids = commands.map((c) => c.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toEqual(expect.arrayContaining(REQUIRED))
  })

  it("libellé : clé ide.cmd.<id> présente en fr et en", () => {
    for (const c of commands) {
      expect(c.label).toBe(`ide.cmd.${c.id}`)
      expect(has(c.label), c.label).toBe(true)
    }
  })

  it("commandes d'écriture désactivées en lecture seule et sans fs_write", () => {
    for (const id of WRITE) {
      expect(byId(id).enabled(blocked[1]), id).toBe(false)
      if (id !== "file.newFile" && id !== "file.newFolder")
        expect(byId(id).enabled(blocked[0]), id).toBe(false)
      expect(byId(id).enabled(ctx({ tabs: [tab({ content: "y" })] })), id).toBe(
        true
      )
    }
  })

  it("création à la racine : désactivée quand l'arborescence est masquée", () => {
    for (const id of ["file.newFile", "file.newFolder"]) {
      expect(byId(id).enabled(ctx({ treeShown: false })), id).toBe(false)
      expect(byId(id).reason!(ctx({ treeShown: false })), id).toBe("ide.reason.noTree")
    }
  })

  it("terminal (groupe Terminal et Affichage › zone terminal) : désactivé sans exec ou terminal indisponible, raison propre à chaque cas", () => {
    const ids = ["terminal.toggle", "terminal.focusSession", "view.toggleTerminal"]
    for (const c of ids.map(byId)) {
      expect(c.enabled(ctx({}, ["fs_read", "fs_write"])), c.id).toBe(false)
      expect(c.reason!(ctx({}, ["fs_read", "fs_write"])), c.id).toBe("ide.reason.noExec")
      expect(c.reason!(ctx({ terminal: "disabled" })), c.id).toBe("ide.reason.termOff")
      expect(c.reason!(ctx({ terminal: "loading" })), c.id).toBe("ide.reason.termLoading")
      expect(c.reason!(ctx({ terminal: "nosessions" })), c.id).toBe("ide.reason.noSessionsRead")
      expect(c.reason!(ctx({ terminal: "unavailable" })), c.id).toBe("ide.reason.sessionsDown")
      expect(c.enabled(ctx()), c.id).toBe(true)
      expect(c.enabled(ctx({ terminal: "nosession" })), c.id).toBe(true)
    }
  })

  it("une commande désactivée donne une raison non vide, traduite en fr et en", () => {
    for (const k of blocked)
      for (const c of commands) {
        if (c.enabled(k)) continue
        const r = c.reason?.(k) ?? ""
        expect(r, c.id).not.toBe("")
        expect(has(r), r).toBe(true)
      }
  })

  it("run passe par la poignée, les préférences et les rappels de l'espace", () => {
    const k = ctx()
    byId("find.replace").run(k)
    byId("goto.palette").run(k)
    byId("file.save").run(k)
    byId("view.fontPlus").run(k)
    byId("view.wordWrap").run(k)
    byId("terminal.toggle").run(k)
    expect(k.editor!.replace).toHaveBeenCalled()
    expect(k.editor!.palette).toHaveBeenCalled()
    expect(k.save).toHaveBeenCalled()
    expect(k.prefs.set).toHaveBeenCalledWith("fontSize", 14)
    expect(k.prefs.set).toHaveBeenCalledWith("wordWrap", true)
    expect(k.prefs.set).toHaveBeenCalledWith("showTerminal", false)
  })

  it("raccourcis : simples chaînes d'affichage", () => {
    expect(byId("file.save").shortcut).toBe("Ctrl+S")
    expect(byId("file.quickOpen").shortcut).toBe("Ctrl+P")
    expect(byId("goto.palette").shortcut).toBe("F1")
    expect(byId("edit.redo").shortcut).toBe("Ctrl+Maj+Z")
  })

})
