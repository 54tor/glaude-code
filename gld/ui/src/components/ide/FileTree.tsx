import { cloneElement, useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, File, FilePlus, Folder, FolderPlus, Link2, TextCursorInput, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { cn } from "cn"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { api, type FsEntry } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { Confirm, type Ask } from "@/components/Confirm"
import { Prompt, type Ask as PromptAsk } from "@/components/Prompt"
import { tone } from "../GitChanges"
import type { useTree } from "./useTree"

type TreeAction = "newFile" | "newDir" | "rename" | "delete"
type TreeTarget = { path: string; name: string; type: FsEntry["type"] }
type Row = TreeTarget & { parent: string; depth: number }

const join = (parentPath: string, name: string) => (parentPath ? parentPath + "/" : "") + name
const rootTarget: TreeTarget = { path: "", name: "", type: "dir" }
const NAVIGATION_KEYS = ["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Enter"]
const BASE_INDENT_PX = 4
const INDENT_PER_LEVEL_PX = 14

// Lettre d'une seule colonne → code « XY » attendu par tone().
const toneCode = (mark: string) => {
  if (mark === "?") return "??"
  if (mark === "U") return "UU"
  return mark
}

const entryIcon = (type: FsEntry["type"]) => {
  if (type === "dir") return <Folder className="size-4 shrink-0 text-info" />
  if (type === "symlink") return <Link2 className="size-4 shrink-0" />
  return <File className="size-4 shrink-0 text-muted-foreground" />
}

// actionsRef : création à la racine pilotée de l'extérieur (barre de l'arbre), par le même flux que le menu contextuel.
export type TreeActions = { create(kind: "newFile" | "newDir"): void }
// filter : sous-chaîne du nom, sans casse, sur les seuls listings déjà chargés ; les ancêtres d'une entrée trouvée restent visibles.
type Props = { dir: string; tree: ReturnType<typeof useTree>; canWrite: boolean; selected: string; marks?: Map<string, string>; dirs?: Set<string>; filter?: string; actionsRef?: RefObject<TreeActions | null>; onOpen: (path: string) => void; onRenamed?: (from: string, to: string) => void }

export function FileTree({ dir, tree, canWrite, selected, marks, dirs, filter = "", actionsRef, onOpen, onRenamed }: Props) {
  const { t } = useTranslation()
  const [focus, setFocus] = useState("")
  const box = useRef<HTMLUListElement>(null)
  const fromKeyboard = useRef(false) // le focus n'est déplacé par code que si le changement vient du clavier

  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
  const [confirm, setConfirm] = useState<Ask | null>(null)

  // Échec : le message du serveur en toast ; le dialogue reste ouvert.
  const done = async (call: Promise<unknown>): Promise<boolean> => {
    try {
      await call
      return true
    } catch (error) {
      toast.error(error instanceof ApiError ? error.message : t("shell.act.failed"))
      return false
    }
  }
  const onAction = (action: TreeAction, target: TreeTarget) => {
    const parent = target.path.includes("/") ? target.path.slice(0, target.path.lastIndexOf("/")) : ""
    if (action === "newFile" || action === "newDir") {
      const kind = action === "newFile" ? "file" : "dir"
      return setPrompt({
        title: t(action === "newFile" ? "ide.ask.newFile" : "ide.ask.newDir"),
        value: "",
        placeholder: t("ide.ask.name"),
        confirm: t("ide.ask.confirm"),
        run: async (name) => {
          const created = await done(api.fsNew(dir, join(target.path, name.trim()), kind))
          if (!created) return false
          // La racine ("") est toujours affichée : on la relit sans l'ajouter aux dossiers dépliés.
          if (target.path === "" || tree.open.has(target.path)) tree.refresh(target.path)
          else tree.setOpen(target.path, true)
          return true
        },
      })
    }
    if (action === "rename")
      return setPrompt({
        title: t("ide.ask.rename", { name: target.name }),
        value: target.name,
        confirm: t("ide.ask.confirm"),
        run: async (name) => {
          const to = join(parent, name.trim())
          const renamed = await done(api.fsRename(dir, target.path, to))
          if (!renamed) return false
          tree.refresh(parent)
          onRenamed?.(target.path, to)
          return true
        },
      })
    setConfirm({
      title: t("ide.ask.deleteTitle", { name: target.name }),
      body: t("ide.ask.deleteBody"),
      confirm: t("ide.ask.deleteConfirm"),
      run: async () => {
        if (await done(api.fsRm(dir, target.path))) tree.refresh(parent)
      },
    })
  }

  useEffect(() => {
    if (!actionsRef) return
    actionsRef.current = { create: (kind) => onAction(kind, rootTarget) }
    return () => {
      actionsRef.current = null
    }
  })

  const query = filter.trim().toLowerCase()
  const expanded = (path: string) => (query ? typeof tree.lists[path] === "object" : tree.open.has(path))
  const rows: Row[] = []
  // Renvoie vrai si une entrée de ce dossier (ou de ses sous-dossiers chargés) correspond au filtre.
  const walk = (parent: string, depth: number): boolean => {
    const listing = tree.lists[parent]
    if (typeof listing !== "object") return false
    let anyMatch = false
    for (const entry of listing.entries) {
      const path = join(parent, entry.name)
      const rowCountBefore = rows.length
      rows.push({ path, parent, name: entry.name, type: entry.type, depth })
      const selfMatches = !query || entry.name.toLowerCase().includes(query)
      const descendantMatches = entry.type === "dir" && expanded(path) && walk(path, depth + 1)
      if (!selfMatches && !descendantMatches) rows.length = rowCountBefore
      anyMatch ||= selfMatches || descendantMatches
    }
    return anyMatch
  }
  walk("", 0)
  const rootList = tree.lists[""]
  const truncated = Object.values(tree.lists).some((listing) => typeof listing === "object" && listing.truncated)

  useEffect(() => {
    if (fromKeyboard.current) box.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(focus)}"]`)?.focus()
  }, [focus])

  // Filtre actif : l'affichage montre tout listing chargé déplié ; plier ou déplier n'aurait aucun effet visible
  // et réécrirait en douce les dossiers dépliés mémorisés, donc rien ne bascule tant qu'il filtre.
  const toggle = (path: string, expand: boolean) => !query && tree.setOpen(path, expand)
  const activate = (row: Row) => {
    if (row.type === "dir") toggle(row.path, !expanded(row.path))
    else if (row.type === "file") onOpen(row.path)
  }
  const onKeyDown = (event: KeyboardEvent) => {
    if (!NAVIGATION_KEYS.includes(event.key)) return
    event.preventDefault()
    fromKeyboard.current = true
    const focusedIndex = rows.findIndex((row) => row.path === focus)
    const current = rows[focusedIndex]
    const goTo = (targetIndex: number) => rows[targetIndex] && setFocus(rows[targetIndex].path)
    if (event.key === "ArrowDown") return goTo(focusedIndex + 1)
    if (event.key === "ArrowUp") return goTo(Math.max(0, focusedIndex - 1))
    if (!current) return
    if (event.key === "Enter") return activate(current)
    if (event.key === "ArrowRight") {
      if (current.type !== "dir") return
      if (!expanded(current.path)) toggle(current.path, true)
      else if (rows[focusedIndex + 1]?.parent === current.path) goTo(focusedIndex + 1)
      return
    }
    const canCollapse = current.type === "dir" && expanded(current.path) && !query
    if (canCollapse) return toggle(current.path, false)
    if (current.parent) setFocus(current.parent)
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto py-1 text-sm">
      {truncated && <p className="mx-2 mb-1 rounded-md border px-2 py-1 text-xs text-muted-foreground">{t("ide.tree.truncated")}</p>}
      {rootList === "loading" && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.loading")}</p>}
      {rootList === "failed" && <p className="px-3 py-1 text-xs text-destructive">{t("ide.tree.failed")}</p>}
      {typeof rootList === "object" && rootList.entries.length === 0 && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.empty")}</p>}
      {query && typeof rootList === "object" && rootList.entries.length > 0 && rows.length === 0 && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.noMatch")}</p>}
      <ul ref={box} role="tree" aria-label={t("ide.tree.label")} onKeyDown={onKeyDown} onPointerDown={() => (fromKeyboard.current = false)}>
        {rows.map((row, rowIndex) => {
          const isDir = row.type === "dir"
          const isOpen = isDir && expanded(row.path)
          const mark = isDir ? undefined : marks?.get(row.path)
          const isTabStop = row.path === focus || (!focus && rowIndex === 0)
          const hasGitChanges = isDir && dirs?.has(row.path)
          const activateRow = (event: { stopPropagation(): void }) => {
            event.stopPropagation()
            activate(row)
          }
          const item = (
            <li
              role="treeitem"
              aria-level={row.depth + 1}
              aria-expanded={isDir ? isOpen : undefined}
              aria-selected={row.path === selected}
              data-path={row.path}
              tabIndex={isTabStop ? 0 : -1}
              onFocus={() => setFocus(row.path)}
              onClick={activateRow}
              className={cn(
                "flex h-7 cursor-default items-center gap-1 rounded-md pr-2 outline-none focus-visible:ring-2 focus-visible:ring-ring/80",
                row.path === selected ? "bg-accent text-accent-foreground" : "hover:bg-muted",
                row.type === "symlink" && "text-muted-foreground"
              )}
              style={{ paddingLeft: BASE_INDENT_PX + row.depth * INDENT_PER_LEVEL_PX }}
            >
              {isDir ? (
                <button tabIndex={-1} className="grid size-5 shrink-0 place-items-center text-muted-foreground" aria-label={t(isOpen ? "ide.tree.collapse" : "ide.tree.expand", { name: row.name })} onClick={activateRow}>
                  <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
                </button>
              ) : (
                <span className="size-5 shrink-0" />
              )}
              {entryIcon(row.type)}
              <span className={cn("truncate", hasGitChanges && "text-warning/80")}>{row.name}</span>
              {hasGitChanges && <span aria-hidden title={t("ide.git.dir")} className="ml-auto size-1.5 shrink-0 rounded-full bg-warning/60" />}
              {mark && (
                <span className={cn("ml-auto w-4 shrink-0 text-center text-xs font-semibold", tone(toneCode(mark)))} title={t(`ide.git.${mark}`)}>
                  {mark}
                </span>
              )}
            </li>
          )
          if (!canWrite) return cloneElement(item, { key: row.path })
          return (
            <ContextMenu key={row.path}>
              <ContextMenuTrigger asChild>{item}</ContextMenuTrigger>
              <ContextMenuContent className="w-48">
                {isDir && (
                  <>
                    <ContextMenuItem onSelect={() => onAction("newFile", row)}>
                      <FilePlus /> {t("ide.menu.newFile")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => onAction("newDir", row)}>
                      <FolderPlus /> {t("ide.menu.newDir")}
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                  </>
                )}
                <ContextMenuItem onSelect={() => onAction("rename", row)}>
                  <TextCursorInput /> {t("ide.menu.rename")}
                </ContextMenuItem>
                <ContextMenuSeparator />
                <ContextMenuItem variant="destructive" onSelect={() => onAction("delete", row)}>
                  <Trash2 /> {t("ide.menu.delete")}
                </ContextMenuItem>
              </ContextMenuContent>
            </ContextMenu>
          )
        })}
      </ul>
      <Prompt ask={prompt} onClose={() => setPrompt(null)} />
      <Confirm ask={confirm} onClose={() => setConfirm(null)} />
    </div>
  )
}
