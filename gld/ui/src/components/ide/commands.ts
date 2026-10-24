import type { Right } from "@/lib/auth"
import { isDirty, type Tab } from "./ideState"
import { zoomed, type ViewPrefs } from "./useViewPrefs"
import type { TermState } from "./useTerminalAvailability"

// indent : « spaces:N » ou « tabs:N », mis en forme par la barre d'état.
export type EditorStatus = {
  line: number
  col: number
  selected: number
  eol: "LF" | "CRLF"
  language: string
  indent: string
  readOnly: boolean
}
export type EditorHandle = {
  undo(): void
  redo(): void
  find(): void
  replace(): void
  gotoLine(): void
  palette(): void
  focus(): void
  zoom(delta: 1 | -1 | 0): void
  subscribe(l: (s: EditorStatus) => void): () => void
}

// Tout ce qu'une commande lit ou appelle : le registre reste sans Monaco ni React.
export type CmdCtx = {
  can(r: Right): boolean
  tab: Tab | undefined
  tabs: Tab[]
  // Onglet actif en lecture seule (fins de ligne mixtes, fichier refusé) ; le droit fs_write se lit à part via can.
  readOnly: boolean
  // null tant qu'aucun éditeur n'est monté pour l'onglet actif (chargement paresseux de Monaco, notice tooBig/binary).
  editor: EditorHandle | null
  prefs: ViewPrefs
  // État de la zone terminal (droit exec, GLD_WEB_TERMINAL=1, liste des sessions) : chaque refus a sa raison.
  terminal: TermState
  // Arborescence affichée : la création à la racine passe par son flux, sans elle la commande ne ferait rien.
  treeShown: boolean
  save(): void
  saveAll(): void
  newFile(): void
  newFolder(): void
  closeTab(): void
  closeAll(): void
  openQuick(): void
  focusSession(): void
}

export type Command = {
  id: string
  label: string
  shortcut?: string
  group: "file" | "edit" | "find" | "view" | "goto" | "terminal"
  // editor : menu et boutons donnent le focus à l'éditeur avant la commande (la saisie rapide de Monaco l'exige).
  // keep : le focus reste sur le déclencheur (fermeture d'onglets : l'éditeur va se démonter ; bascules de zones).
  focus: "editor" | "keep"
  enabled(ctx: CmdCtx): boolean
  reason?(ctx: CmdCtx): string
  run(ctx: CmdCtx): void
}

// Chaque garde renvoie la clé i18n de la raison du refus, ou "" si elle passe ; la première qui refuse l'emporte.
type Guard = (c: CmdCtx) => string
const write: Guard = (c) => (c.can("fs_write") ? "" : "ide.reason.noWrite")
const hasTab: Guard = (c) => (c.tab ? "" : "ide.reason.noTab")
const writable: Guard = (c) => (c.readOnly ? "ide.reason.readOnly" : "")
const hasEditor: Guard = (c) => (c.editor ? "" : "ide.reason.noEditor")
const exec: Guard = (c) => (c.can("exec") ? "" : "ide.reason.noExec")
const TERM_REASON: Partial<Record<TermState, string>> = {
  loading: "ide.reason.termLoading",
  disabled: "ide.reason.termOff",
  forbidden: "ide.reason.noExec",
  nosessions: "ide.reason.noSessionsRead",
  unavailable: "ide.reason.sessionsDown",
}
const term: Guard = (c) => TERM_REASON[c.terminal] ?? ""
const dirty: Guard = (c) =>
  c.tabs.some((t) => isDirty(t) && !t.readOnly) ? "" : "ide.reason.nothingDirty"
const treeOn: Guard = (c) => (c.treeShown ? "" : "ide.reason.noTree")
const anyTab: Guard = (c) => (c.tabs.length ? "" : "ide.reason.noTab")

const cmd = (
  id: string,
  group: Command["group"],
  guards: Guard[],
  run: (c: CmdCtx) => void,
  shortcut?: string,
  focus: Command["focus"] = "editor"
): Command => {
  const reason = (c: CmdCtx) => guards.map((g) => g(c)).find(Boolean) ?? ""
  return {
    id,
    label: `ide.cmd.${id}`,
    group,
    shortcut,
    focus,
    run,
    enabled: (c) => !reason(c),
    ...(guards.length && { reason }),
  }
}
const toggle =
  (k: "wordWrap" | "minimap" | "showTree" | "showTerminal" | "showStatus") =>
  (c: CmdCtx) =>
    c.prefs.set(k, !c.prefs[k])
const font = (d: 1 | -1 | 0) => (c: CmdCtx) =>
  c.prefs.set("fontSize", zoomed(c.prefs.fontSize, d))

export const commands: Command[] = [
  cmd("file.newFile", "file", [write, treeOn], (c) => c.newFile()),
  cmd("file.newFolder", "file", [write, treeOn], (c) => c.newFolder()),
  cmd(
    "file.save",
    "file",
    [hasTab, writable, write],
    (c) => c.save(),
    "Ctrl+S"
  ),
  cmd("file.saveAll", "file", [write, dirty], (c) => c.saveAll(), undefined, "keep"),
  cmd("file.closeTab", "file", [hasTab], (c) => c.closeTab(), undefined, "keep"),
  cmd("file.closeAll", "file", [anyTab], (c) => c.closeAll(), undefined, "keep"),
  cmd("file.quickOpen", "file", [], (c) => c.openQuick(), "Ctrl+P"),
  cmd(
    "edit.undo",
    "edit",
    [hasEditor, writable, write],
    (c) => c.editor?.undo(),
    "Ctrl+Z"
  ),
  cmd(
    "edit.redo",
    "edit",
    [hasEditor, writable, write],
    (c) => c.editor?.redo(),
    "Ctrl+Shift+Z"
  ),
  cmd("find.find", "find", [hasEditor], (c) => c.editor?.find(), "Ctrl+F"),
  cmd(
    "find.replace",
    "find",
    [hasEditor, writable, write],
    (c) => c.editor?.replace(),
    "Ctrl+H"
  ),
  cmd("goto.line", "goto", [hasEditor], (c) => c.editor?.gotoLine(), "Ctrl+G"),
  cmd("goto.palette", "goto", [hasEditor], (c) => c.editor?.palette(), "F1"),
  cmd("view.wordWrap", "view", [], toggle("wordWrap")),
  cmd("view.minimap", "view", [], toggle("minimap")),
  cmd("view.fontPlus", "view", [], font(1)),
  cmd("view.fontMinus", "view", [], font(-1)),
  cmd("view.fontReset", "view", [], font(0)),
  cmd("view.toggleTree", "view", [], toggle("showTree"), undefined, "keep"),
  cmd("view.toggleTerminal", "view", [exec, term], toggle("showTerminal"), undefined, "keep"),
  cmd("view.toggleStatus", "view", [], toggle("showStatus"), undefined, "keep"),
  cmd("terminal.toggle", "terminal", [exec, term], toggle("showTerminal"), undefined, "keep"),
  cmd("terminal.focusSession", "terminal", [exec, term], (c) => c.focusSession(), undefined, "keep"),
]

// Raccourcis écrits en anglais dans le registre (noms ARIA) ; seules les touches nommées changent à l'affichage.
const NAMED = ["Ctrl", "Shift", "Alt", "Enter", "Escape"]
export const showKeys = (s: string, t: (k: string) => string) =>
  s
    .split("+")
    .map((k) => (NAMED.includes(k) ? t(`ide.keys.${k}`) : k))
    .join("+")
export const ariaKeys = (s?: string) => s?.replace("Ctrl", "Control")
