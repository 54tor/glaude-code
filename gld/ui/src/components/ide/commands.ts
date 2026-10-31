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
  subscribe(listener: (status: EditorStatus) => void): () => void
}

// Tout ce qu'une commande lit ou appelle : le registre reste sans Monaco ni React.
export type CmdCtx = {
  can(right: Right): boolean
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
type Guard = (context: CmdCtx) => string
const write: Guard = (context) => (context.can("fs_write") ? "" : "ide.reason.noWrite")
const hasTab: Guard = (context) => (context.tab ? "" : "ide.reason.noTab")
const writable: Guard = (context) => (context.readOnly ? "ide.reason.readOnly" : "")
const hasEditor: Guard = (context) => (context.editor ? "" : "ide.reason.noEditor")
const exec: Guard = (context) => (context.can("exec") ? "" : "ide.reason.noExec")
const TERM_REASON: Partial<Record<TermState, string>> = {
  loading: "ide.reason.termLoading",
  disabled: "ide.reason.termOff",
  forbidden: "ide.reason.noExec",
  nosessions: "ide.reason.noSessionsRead",
  unavailable: "ide.reason.sessionsDown",
}
const term: Guard = (context) => TERM_REASON[context.terminal] ?? ""
const dirty: Guard = (context) =>
  context.tabs.some((tab) => isDirty(tab) && !tab.readOnly) ? "" : "ide.reason.nothingDirty"
const treeOn: Guard = (context) => (context.treeShown ? "" : "ide.reason.noTree")
const anyTab: Guard = (context) => (context.tabs.length ? "" : "ide.reason.noTab")

const cmd = (
  id: string,
  group: Command["group"],
  guards: Guard[],
  run: (context: CmdCtx) => void,
  shortcut?: string,
  focus: Command["focus"] = "editor"
): Command => {
  const reason = (context: CmdCtx) => guards.map((guard) => guard(context)).find(Boolean) ?? ""
  return {
    id,
    label: `ide.cmd.${id}`,
    group,
    shortcut,
    focus,
    run,
    enabled: (context) => !reason(context),
    ...(guards.length && { reason }),
  }
}
const toggle =
  (prefName: "wordWrap" | "minimap" | "showTree" | "showTerminal" | "showStatus") =>
  (context: CmdCtx) =>
    context.prefs.set(prefName, !context.prefs[prefName])
const font = (delta: 1 | -1 | 0) => (context: CmdCtx) =>
  context.prefs.set("fontSize", zoomed(context.prefs.fontSize, delta))

export const commands: Command[] = [
  cmd("file.newFile", "file", [write, treeOn], (context) => context.newFile()),
  cmd("file.newFolder", "file", [write, treeOn], (context) => context.newFolder()),
  cmd(
    "file.save",
    "file",
    [hasTab, writable, write],
    (context) => context.save(),
    "Ctrl+S"
  ),
  cmd("file.saveAll", "file", [write, dirty], (context) => context.saveAll(), undefined, "keep"),
  cmd("file.closeTab", "file", [hasTab], (context) => context.closeTab(), undefined, "keep"),
  cmd("file.closeAll", "file", [anyTab], (context) => context.closeAll(), undefined, "keep"),
  cmd("file.quickOpen", "file", [], (context) => context.openQuick(), "Ctrl+P"),
  cmd(
    "edit.undo",
    "edit",
    [hasEditor, writable, write],
    (context) => context.editor?.undo(),
    "Ctrl+Z"
  ),
  cmd(
    "edit.redo",
    "edit",
    [hasEditor, writable, write],
    (context) => context.editor?.redo(),
    "Ctrl+Shift+Z"
  ),
  cmd("find.find", "find", [hasEditor], (context) => context.editor?.find(), "Ctrl+F"),
  cmd(
    "find.replace",
    "find",
    [hasEditor, writable, write],
    (context) => context.editor?.replace(),
    "Ctrl+H"
  ),
  cmd("goto.line", "goto", [hasEditor], (context) => context.editor?.gotoLine(), "Ctrl+G"),
  cmd("goto.palette", "goto", [hasEditor], (context) => context.editor?.palette(), "F1"),
  cmd("view.wordWrap", "view", [], toggle("wordWrap")),
  cmd("view.minimap", "view", [], toggle("minimap")),
  cmd("view.fontPlus", "view", [], font(1)),
  cmd("view.fontMinus", "view", [], font(-1)),
  cmd("view.fontReset", "view", [], font(0)),
  cmd("view.toggleTree", "view", [], toggle("showTree"), undefined, "keep"),
  cmd("view.toggleTerminal", "view", [exec, term], toggle("showTerminal"), undefined, "keep"),
  cmd("view.toggleStatus", "view", [], toggle("showStatus"), undefined, "keep"),
  cmd("terminal.toggle", "terminal", [exec, term], toggle("showTerminal"), undefined, "keep"),
  cmd("terminal.focusSession", "terminal", [exec, term], (context) => context.focusSession(), undefined, "keep"),
]

// Raccourcis écrits en anglais dans le registre (noms ARIA) ; seules les touches nommées changent à l'affichage.
const NAMED = ["Ctrl", "Shift", "Alt", "Enter", "Escape"]
export const showKeys = (shortcut: string, t: (key: string) => string) =>
  shortcut
    .split("+")
    .map((keyName) => (NAMED.includes(keyName) ? t(`ide.keys.${keyName}`) : keyName))
    .join("+")
export const ariaKeys = (shortcut?: string) => shortcut?.replace("Ctrl", "Control")
