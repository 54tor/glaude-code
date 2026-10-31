import { describe, expect, it, vi } from "vitest"
import fr from "@/i18n/fr/ide.json"
import en from "@/i18n/en/ide.json"
import type { Right } from "@/lib/auth"
import type { Tab } from "./ideState"
import { ariaKeys, commands, showKeys, type CmdCtx, type EditorHandle } from "./commands"
import { DEFAULT_PREFS } from "./useViewPrefs"

const at = (value: unknown, key: string) =>
  key
    .split(".")
    .reduce<unknown>(
      (value, key) =>
        value && typeof value === "object"
          ? (value as Record<string, unknown>)[key]
          : undefined,
      value
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
    can: (raw) => rights.includes(raw),
    tab: t,
    tabs: [t],
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
    ...over,
  }
}
const byId = (id: string) => commands.find((command) => command.id === id)!
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
  "view.toggleStatus",
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
  ctx({ treeShown: false }),
]

describe("registre de commandes", () => {
  it("contient les commandes attendues, chacune une seule fois", () => {
    const ids = commands.map((command) => command.id)
    expect(new Set(ids).size).toBe(ids.length)
    expect(ids).toEqual(expect.arrayContaining(REQUIRED))
  })

  it("libellé : clé ide.cmd.<id> présente en fr et en", () => {
    for (const command of commands) {
      expect(command.label).toBe(`ide.cmd.${command.id}`)
      expect(has(command.label), command.label).toBe(true)
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

  it("aperçu Markdown : actif seulement sur un fichier .md", () => {
    const preview = byId("view.togglePreview")
    expect(preview.enabled(ctx({ tab: tab({ path: "/d/a.ts" }) }))).toBe(false)
    expect(preview.reason!(ctx({ tab: tab({ path: "/d/a.ts" }) }))).toBe("ide.reason.notMarkdown")
    expect(preview.enabled(ctx({ tab: tab({ path: "/d/README.MD" }) }))).toBe(true)
  })

  it("focus : une seule source ; fermer et basculer les zones ne visent pas l'éditeur", () => {
    const keep = ["file.saveAll", "file.closeTab", "file.closeAll", "view.toggleTree", "view.togglePreview", "view.toggleStatus"]
    for (const command of commands) expect(command.focus, command.id).toBe(keep.includes(command.id) ? "keep" : "editor")
  })

  it("une commande désactivée donne une raison non vide, traduite en fr et en", () => {
    for (const cmdCtx of blocked)
      for (const command of commands) {
        if (command.enabled(cmdCtx)) continue
        const raw = command.reason?.(cmdCtx) ?? ""
        expect(raw, command.id).not.toBe("")
        expect(has(raw), raw).toBe(true)
      }
  })

  it("run passe par la poignée, les préférences et les rappels de l'espace", () => {
    const cmdCtx = ctx()
    byId("find.replace").run(cmdCtx)
    byId("goto.palette").run(cmdCtx)
    byId("file.save").run(cmdCtx)
    byId("view.fontPlus").run(cmdCtx)
    byId("view.wordWrap").run(cmdCtx)
    byId("view.togglePreview").run(cmdCtx)
    expect(cmdCtx.editor!.replace).toHaveBeenCalled()
    expect(cmdCtx.editor!.palette).toHaveBeenCalled()
    expect(cmdCtx.save).toHaveBeenCalled()
    expect(cmdCtx.prefs.set).toHaveBeenCalledWith("fontSize", 14)
    expect(cmdCtx.prefs.set).toHaveBeenCalledWith("wordWrap", true)
    expect(cmdCtx.prefs.set).toHaveBeenCalledWith("showPreview", false)
  })

  it("raccourcis : simples chaînes d'affichage", () => {
    expect(byId("file.save").shortcut).toBe("Ctrl+S")
    expect(byId("file.quickOpen").shortcut).toBe("Ctrl+P")
    expect(byId("goto.palette").shortcut).toBe("F1")
    expect(byId("edit.redo").shortcut).toBe("Ctrl+Shift+Z")
  })

  it("raccourcis : touches nommées traduites à l'affichage, noms ARIA canoniques", () => {
    const fr = (key: string) => ({ "ide.keys.Shift": "Maj", "ide.keys.Enter": "Entrée", "ide.keys.Escape": "Échap", "ide.keys.Ctrl": "Ctrl", "ide.keys.Alt": "Alt" })[key] ?? "?"
    expect(showKeys("Ctrl+Shift+Z", fr)).toBe("Ctrl+Maj+Z")
    expect(showKeys("Alt+Enter", fr)).toBe("Alt+Entrée")
    expect(showKeys("Escape", fr)).toBe("Échap")
    expect(showKeys("F1", fr)).toBe("F1")
    expect(ariaKeys("Ctrl+Shift+Z")).toBe("Control+Shift+Z")
    expect(ariaKeys(undefined)).toBeUndefined()
    for (const key of ["Ctrl", "Shift", "Alt", "Enter", "Escape"]) expect(has(`ide.keys.${key}`), key).toBe(true)
  })
})
