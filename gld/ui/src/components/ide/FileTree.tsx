import { cloneElement, useEffect, useRef, useState, type KeyboardEvent, type RefObject } from "react"
import { useTranslation } from "react-i18next"
import { ChevronRight, File, FilePlus, Folder, FolderPlus, Link2, Pencil, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { cn } from "cn"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { api, type FsEntry, type FsRes } from "@/lib/api"
import { Confirm, type Ask } from "@/components/Confirm"
import { Prompt, type Ask as PromptAsk } from "@/components/Prompt"
import { tone } from "../GitChanges"
import type { useTree } from "./useTree"

type TreeAction = "newFile" | "newDir" | "rename" | "delete"
type TreeTarget = { path: string; name: string; type: FsEntry["type"] }
type Row = TreeTarget & { parent: string; depth: number }

const join = (p: string, n: string) => (p ? p + "/" : "") + n
const rootTarget: TreeTarget = { path: "", name: "", type: "dir" }

// actionsRef : création à la racine pilotée de l'extérieur (barre de l'arbre), par le même flux que le menu contextuel.
export type TreeActions = { create(kind: "newFile" | "newDir"): void }
// filter : sous-chaîne du nom, sans casse, sur les seuls listings déjà chargés ; les ancêtres d'une entrée trouvée restent visibles.
type Props = { dir: string; tree: ReturnType<typeof useTree>; canWrite: boolean; selected: string; marks?: Map<string, string>; dirs?: Set<string>; filter?: string; actionsRef?: RefObject<TreeActions | null>; onOpen: (path: string) => void; onRenamed?: (from: string, to: string) => void }

export function FileTree({ dir, tree, canWrite, selected, marks, dirs, filter = "", actionsRef, onOpen, onRenamed }: Props) {
  const { t } = useTranslation()
  const [focus, setFocus] = useState("")
  const box = useRef<HTMLUListElement>(null)
  const kbd = useRef(false) // le focus n'est déplacé par code que si le changement vient du clavier

  const [prompt, setPrompt] = useState<PromptAsk | null>(null)
  const [confirm, setConfirm] = useState<Ask | null>(null)

  // Échec : le message du serveur en toast ; le dialogue reste ouvert.
  const done = (r: FsRes): boolean => (r.ok ? true : (toast.error(r.body.error || t("shell.act.failed")), false))
  const onAction = (a: TreeAction, r: TreeTarget) => {
    const parent = r.path.includes("/") ? r.path.slice(0, r.path.lastIndexOf("/")) : ""
    if (a === "newFile" || a === "newDir") {
      const kind = a === "newFile" ? "file" : "dir"
      return setPrompt({
        title: t(a === "newFile" ? "ide.ask.newFile" : "ide.ask.newDir"),
        value: "",
        placeholder: t("ide.ask.name"),
        confirm: t("ide.ask.confirm"),
        run: async (name) => {
          if (!done(await api.fsNew(dir, join(r.path, name.trim()), kind))) return false
          // La racine ("") est toujours affichée : on la relit sans l'ajouter aux dossiers dépliés.
          if (r.path === "" || tree.open.has(r.path)) tree.refresh(r.path)
          else tree.setOpen(r.path, true)
          return true
        },
      })
    }
    if (a === "rename")
      return setPrompt({
        title: t("ide.ask.rename", { name: r.name }),
        value: r.name,
        confirm: t("ide.ask.confirm"),
        run: async (name) => {
          const to = join(parent, name.trim())
          if (!done(await api.fsRename(dir, r.path, to))) return false
          tree.refresh(parent)
          onRenamed?.(r.path, to)
          return true
        },
      })
    setConfirm({
      title: t("ide.ask.deleteTitle", { name: r.name }),
      body: t("ide.ask.deleteBody"),
      confirm: t("ide.ask.deleteConfirm"),
      run: async () => {
        if (done(await api.fsRm(dir, r.path))) tree.refresh(parent)
      },
    })
  }

  useEffect(() => {
    if (!actionsRef) return
    actionsRef.current = { create: (kind) => onAction(kind, rootTarget) }
  })

  const q = filter.trim().toLowerCase()
  const expanded = (path: string) => (q ? typeof tree.lists[path] === "object" : tree.open.has(path))
  const rows: Row[] = []
  // Renvoie vrai si une entrée de ce dossier (ou de ses sous-dossiers chargés) correspond au filtre.
  const walk = (parent: string, depth: number): boolean => {
    const l = tree.lists[parent]
    if (typeof l !== "object") return false
    let any = false
    for (const e of l.entries) {
      const path = join(parent, e.name)
      const at = rows.length
      rows.push({ path, parent, name: e.name, type: e.type, depth })
      const self = !q || e.name.toLowerCase().includes(q)
      const sub = e.type === "dir" && expanded(path) && walk(path, depth + 1)
      if (!self && !sub) rows.length = at
      any ||= self || sub
    }
    return any
  }
  walk("", 0)
  const rootList = tree.lists[""]
  const truncated = Object.values(tree.lists).some((l) => typeof l === "object" && l.truncated)

  useEffect(() => {
    if (kbd.current) box.current?.querySelector<HTMLElement>(`[data-path="${CSS.escape(focus)}"]`)?.focus()
  }, [focus])

  const activate = (r: Row) => {
    if (r.type === "dir") tree.setOpen(r.path, !tree.open.has(r.path))
    else if (r.type === "file") onOpen(r.path)
  }
  const key = (e: KeyboardEvent) => {
    if (!["ArrowDown", "ArrowUp", "ArrowLeft", "ArrowRight", "Enter"].includes(e.key)) return
    e.preventDefault()
    kbd.current = true
    const i = rows.findIndex((r) => r.path === focus)
    const cur = rows[i]
    const go = (j: number) => rows[j] && setFocus(rows[j].path)
    if (e.key === "ArrowDown") go(i + 1)
    else if (e.key === "ArrowUp") go(Math.max(0, i - 1))
    else if (!cur) return
    else if (e.key === "Enter") activate(cur)
    else if (e.key === "ArrowRight") {
      if (cur.type !== "dir") return
      if (!tree.open.has(cur.path)) tree.setOpen(cur.path, true)
      else go(i + 1)
    } else if (cur.type === "dir" && tree.open.has(cur.path)) tree.setOpen(cur.path, false)
    else if (cur.parent) setFocus(cur.parent)
  }

  return (
    <div className="min-h-0 flex-1 overflow-auto py-1 text-sm">
      {truncated && <p className="mx-2 mb-1 rounded-md border px-2 py-1 text-xs text-muted-foreground">{t("ide.tree.truncated")}</p>}
      {rootList === "loading" && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.loading")}</p>}
      {rootList === "failed" && <p className="px-3 py-1 text-xs text-destructive">{t("ide.tree.failed")}</p>}
      {typeof rootList === "object" && rootList.entries.length === 0 && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.empty")}</p>}
      {q && typeof rootList === "object" && rootList.entries.length > 0 && rows.length === 0 && <p className="px-3 py-1 text-xs text-muted-foreground">{t("ide.tree.noMatch")}</p>}
      <ul ref={box} role="tree" aria-label={t("ide.tree.label")} onKeyDown={key} onPointerDown={() => (kbd.current = false)}>
        {rows.map((r, i) => {
          const isDir = r.type === "dir"
          const isOpen = isDir && expanded(r.path)
          const mark = isDir ? undefined : marks?.get(r.path)
          const item = (
            <li
              role="treeitem"
              aria-level={r.depth + 1}
              aria-expanded={isDir ? isOpen : undefined}
              aria-selected={r.path === selected}
              data-path={r.path}
              tabIndex={r.path === focus || (!focus && i === 0) ? 0 : -1}
              onFocus={() => setFocus(r.path)}
              onClick={(e) => (e.stopPropagation(), activate(r))}
              className={cn(
                "flex h-7 cursor-default items-center gap-1 rounded-md pr-2 outline-none focus-visible:ring-2 focus-visible:ring-ring/50",
                r.path === selected ? "bg-accent text-accent-foreground" : "hover:bg-muted",
                r.type === "symlink" && "text-muted-foreground"
              )}
              style={{ paddingLeft: 4 + r.depth * 14 }}
            >
              {isDir ? (
                <button tabIndex={-1} className="grid size-5 shrink-0 place-items-center text-muted-foreground" aria-label={t(isOpen ? "ide.tree.collapse" : "ide.tree.expand", { name: r.name })} onClick={(e) => (e.stopPropagation(), activate(r))}>
                  <ChevronRight className={cn("size-3.5 transition-transform", isOpen && "rotate-90")} />
                </button>
              ) : (
                <span className="size-5 shrink-0" />
              )}
              {isDir ? <Folder className="size-4 shrink-0 text-info" /> : r.type === "symlink" ? <Link2 className="size-4 shrink-0" /> : <File className="size-4 shrink-0 text-muted-foreground" />}
              <span className={cn("truncate", isDir && dirs?.has(r.path) && "text-warning/80")}>{r.name}</span>
              {isDir && dirs?.has(r.path) && <span aria-hidden title={t("ide.git.dir")} className="ml-auto size-1.5 shrink-0 rounded-full bg-warning/60" />}
              {mark && (
                <span className={cn("ml-auto w-4 shrink-0 text-center text-xs font-semibold", tone(mark === "?" ? "??" : mark === "U" ? "UU" : mark))} title={t(`ide.git.${mark}`)}>
                  {mark}
                </span>
              )}
            </li>
          )
          if (!canWrite) return cloneElement(item, { key: r.path })
          return (
            <ContextMenu key={r.path}>
              <ContextMenuTrigger asChild>{item}</ContextMenuTrigger>
              <ContextMenuContent className="w-48">
                {isDir && (
                  <>
                    <ContextMenuItem onSelect={() => onAction("newFile", r)}>
                      <FilePlus /> {t("ide.menu.newFile")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => onAction("newDir", r)}>
                      <FolderPlus /> {t("ide.menu.newDir")}
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                  </>
                )}
                <ContextMenuItem onSelect={() => onAction("rename", r)}>
                  <Pencil /> {t("ide.menu.rename")}
                </ContextMenuItem>
                <ContextMenuItem variant="destructive" onSelect={() => onAction("delete", r)}>
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
