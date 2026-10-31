import { i18n, loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { useEffect, useRef, useState } from "react"
import { BookOpen, Brain, ChevronRight, Copy, FileText, Globe, ExternalLink, Pencil, ScrollText, Tag, Trash2 } from "lucide-react"
import { useLocation } from "wouter"
import { toast } from "sonner"
import { api, type MemFile } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { href } from "@/lib/route"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { TYPES, typeOf } from "./MemoryTypes"
import { Markdown } from "./Message"
import { copy, movable, split } from "./memoryLib"
import { reportAction } from "@/lib/report"

const MEMORY_INDEX_SUFFIX = "/MEMORY.md"

function icon(key: string) {
  if (key.startsWith("m/")) return Brain
  if (key.startsWith("r/")) return ScrollText
  return FileText
}

// Édition d'un fichier sur place : le texte brut, enregistré tel quel (comme dans la page Mémoire).
function EditDialog({ file, edit, onClose, onSaved }: { file: MemFile; edit: boolean; onClose: () => void; onSaved: () => void }) {
  const { t } = useTranslation()
  const [text, setText] = useState<string | null>(null)
  const [editing, setEditing] = useState(edit) // d'abord l'aperçu mis en forme ; « Modifier » passe au texte brut
  const [busy, setBusy] = useState(false)
  const close = useRef(onClose) // le parent recrée onClose à chaque rendu : l'effet de lecture ne doit pas repartir (il écraserait la frappe)
  useEffect(() => {
    close.current = onClose
  })
  useEffect(() => {
    let isMounted = true
    api.memoryFile(file.key).then(
      (memoryFile) => isMounted && setText(memoryFile.text),
      () => {
        if (!isMounted) return
        toast.error(i18n.t("memory.main.unreadable"))
        close.current()
      }
    )
    return () => {
      isMounted = false
    }
  }, [file.key])
  const save = async () => {
    if (text === null) return
    setBusy(true)
    const ok = await reportAction(() => api.memorySave(file.key, text))
    setBusy(false)
    if (!ok) return
    onSaved()
    setEditing(false)
  }
  const [frontMatter, body] = split(text ?? "")
  return (
    <Dialog open onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-3 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="truncate">{file.name}</DialogTitle>
          <DialogDescription className="truncate font-mono text-xs">{file.path}</DialogDescription>
        </DialogHeader>
        {editing ? (
          <textarea
            value={text ?? ""}
            onChange={(event) => setText(event.target.value)}
            onKeyDown={(event) => {
              const isSaveShortcut = (event.ctrlKey || event.metaKey) && event.key === "Enter"
              if (!isSaveShortcut) return
              event.preventDefault()
              save()
            }}
            disabled={text === null || busy}
            spellCheck={false}
            autoFocus
            aria-label={t("memory.project.contentAria", { name: file.name })}
            className="field-sizing-content min-h-64 w-full flex-1 resize-none overflow-y-auto rounded-lg border border-input bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/80"
          />
        ) : (
          <div className="min-h-32 flex-1 overflow-y-auto rounded-lg border px-4 py-3 text-sm leading-relaxed">
            {text === null ? (
              <p className="text-muted-foreground">{t("common.loading")}</p>
            ) : (
              <>
                {frontMatter && <pre className="mb-3 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{frontMatter}</pre>}
                <Markdown t={body} />
              </>
            )}
          </div>
        )}
        <DialogFooter>
          {editing ? (
            <>
              <Button variant="ghost" onClick={() => setEditing(false)}>
                {t("memory.project.preview")}
              </Button>
              <Button onClick={save} disabled={text === null || busy} title={t("memory.project.shortcut")}>
                {t("common.save")}
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={onClose}>
                {t("common.close")}
              </Button>
              <Button onClick={() => setEditing(true)} disabled={text === null}>
                <Pencil /> {t("memory.project.edit")}
              </Button>
            </>
          )}
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}

// La mémoire locale d'un projet ouvert dans la liste des sessions : entrées de mémoire, règles .claude/rules, CLAUDE.md, AGENTS.md…
// Un clic ouvre le fichier dans une fenêtre d'édition (la page Mémoire reste à un clic : flèche ou menu) ; un clic droit permet aussi de changer son type,
// de le copier ou déplacer vers les règles globales, ou de le supprimer. Relue au retour sur l'onglet.
export function ProjectMemory({ dir }: { dir: string }) {
  const { t } = useTranslation()
  const [files, setFiles] = useState<MemFile[] | null>(null)
  const [open, setOpen] = useState(false)
  const [shown, setShown] = useState<{ file: MemFile; edit: boolean } | null>(null)
  const [removing, setRemoving] = useState<MemFile | null>(null)
  const [, go] = useLocation()
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let isMounted = true
    const load = () =>
      api.memory().then((memory) => {
        if (!isMounted) return
        setFiles(memory.projects.find((project) => project.dir === dir)?.files ?? [])
      }, () => {})
    load()
    const onVisibilityChange = () => document.visibilityState === "visible" && load()
    document.addEventListener("visibilitychange", onVisibilityChange)
    return () => {
      isMounted = false
      document.removeEventListener("visibilitychange", onVisibilityChange)
    }
  }, [dir, tick])
  const refresh = () => setTick((currentTick) => currentTick + 1)
  if (!files || files.length === 0) return null
  const tokens = files.filter((file) => file.always).reduce((total, file) => total + file.tokens, 0)
  const alwaysLoadedFirstThenByName = (left: MemFile, right: MemFile) => +!!right.always - +!!left.always || left.name.localeCompare(right.name, loc())
  const sorted = [...files].sort(alwaysLoadedFirstThenByName)

  const setType = async (file: MemFile, type: string) => {
    if (await reportAction(() => api.memoryType(file.key, type))) refresh()
  }
  const toGlobal = async (file: MemFile, copyIt: boolean) => {
    if (await reportAction(() => api.memoryMove(file.key, "global", copyIt))) refresh()
  }
  const copyContent = async (file: MemFile) => {
    try {
      copy((await api.memoryFile(file.key)).text, "memory.lib.copiedContent")
    } catch (copyError) {
      toast.error(errMsg(copyError))
    }
  }
  const remove = async () => {
    const fileToRemove = removing
    setRemoving(null)
    if (!fileToRemove) return
    if (await reportAction(() => api.memoryRemove([fileToRemove.key]))) refresh()
  }
  const isIndex = (file: MemFile) => file.key.endsWith(MEMORY_INDEX_SUFFIX)
  const canType = (file: MemFile) => file.key.startsWith("m/") && !isIndex(file)
  // l'index MEMORY.md reste dans son projet
  const canMoveToGlobal = (file: MemFile) => movable(file.key) && !isIndex(file)

  return (
    <section className="mb-4 rounded-lg border">
      <button onClick={() => setOpen(!open)} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm" aria-expanded={open}>
        <ChevronRight className={cn("size-4 shrink-0 transition-transform", open && "rotate-90")} />
        <Brain className="size-4 shrink-0 text-muted-foreground" />
        <span className="font-medium">{t("memory.project.title")}</span>
        <span className="text-xs text-muted-foreground">
          {t("memory.project.files", { count: files.length })}
          {tokens > 0 && t("memory.project.tokensLoaded", { tokens: tokens.toLocaleString(loc()) })}
        </span>
      </button>
      {open && (
        <ul className="grid gap-px border-t p-1 md:grid-cols-2">
          {sorted.map((file) => {
            const fileType = typeOf(file.type)
            const Icon = fileType?.icon ?? icon(file.key)
            return (
              <li key={file.key}>
                <ContextMenu>
                  <ContextMenuTrigger asChild>
                    <div className="group/row flex items-center rounded-md hover:bg-muted">
                      <button onClick={() => setShown({ file, edit: false })} className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1 text-left text-sm" title={file.path}>
                        <Icon className={cn("size-3.5 shrink-0", fileType?.color ?? "text-muted-foreground")} />
                        <span className={cn("min-w-0 flex-1 truncate", file.off && "line-through opacity-60")}>{file.name}</span>
                        {fileType && <span className={cn("shrink-0 rounded-sm bg-muted px-1.5 text-2xs", fileType.color)}>{fileType.label}</span>}
                        {file.paths && <span className="shrink-0 text-2xs text-muted-foreground">{t("memory.project.scoped")}</span>}
                        {file.always && <span className="shrink-0 text-2xs tabular-nums text-muted-foreground">{t("memory.project.tokens", { n: file.tokens })}</span>}
                      </button>
                      <Button size="icon-xs" variant="ghost" className="mr-1 shrink-0 md:opacity-0 md:group-focus-within/row:opacity-100 md:group-hover/row:opacity-100" onClick={() => go(href({ kind: "memory", key: file.key }))} aria-label={t("memory.project.openAria", { name: file.name })} title={t("memory.project.openTitle")}>
                        <ExternalLink />
                      </Button>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-60">
                    <ContextMenuLabel className="truncate">{file.name}</ContextMenuLabel>
                    <ContextMenuItem onSelect={() => setShown({ file, edit: false })}>
                      <BookOpen /> {t("memory.project.show")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => go(href({ kind: "memory", key: file.key }))}>
                      <ExternalLink /> {t("memory.project.openPage")}
                    </ContextMenuItem>
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => copyContent(file)}>
                      <Copy /> {t("memory.project.copyContent")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => copy(file.path, "memory.lib.copiedPath")}>
                      <Copy /> {t("memory.project.copyPath")}
                    </ContextMenuItem>
                    {canMoveToGlobal(file) && (
                      <ContextMenuItem onSelect={() => toGlobal(file, true)}>
                        <Copy /> {t("memory.project.copyGlobal")}
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem onSelect={() => setShown({ file, edit: true })}>
                      <Pencil /> {t("memory.project.editHere")}
                    </ContextMenuItem>
                    {canType(file) && (
                      <ContextMenuSub>
                        <ContextMenuSubTrigger>
                          <Tag /> {t("memory.project.type")}
                        </ContextMenuSubTrigger>
                        <ContextMenuSubContent>
                          {TYPES.map((memoryType) => (
                            <ContextMenuItem key={memoryType.key} onSelect={() => setType(file, memoryType.key)}>
                              <memoryType.icon /> {memoryType.key} <span className="text-xs text-muted-foreground">({memoryType.label}){file.type === memoryType.key && " ✓"}</span>
                            </ContextMenuItem>
                          ))}
                          <ContextMenuSeparator />
                          <ContextMenuItem onSelect={() => setType(file, "")}>{t("memory.project.noType")}{!file.type && " ✓"}</ContextMenuItem>
                        </ContextMenuSubContent>
                      </ContextMenuSub>
                    )}
                    {canMoveToGlobal(file) && (
                      <ContextMenuItem onSelect={() => toGlobal(file, false)}>
                        <Globe /> {t("memory.project.moveGlobal")}
                      </ContextMenuItem>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem variant="destructive" onSelect={() => setRemoving(file)}>
                      <Trash2 /> {t("memory.project.remove")}
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              </li>
            )
          })}
        </ul>
      )}
      {shown && <EditDialog key={shown.file.key} file={shown.file} edit={shown.edit} onClose={() => setShown(null)} onSaved={refresh} />}
      <AlertDialog open={!!removing} onOpenChange={(isOpen) => !isOpen && setRemoving(null)}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>{t("memory.project.deleteTitle", { name: removing?.name ?? "" })}</AlertDialogTitle>
            <AlertDialogDescription>
              {removing?.key.startsWith("m/") ? t("memory.project.deleteBodyMemory") : t("memory.project.deleteBodyFile")}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={remove}>
              {t("common.delete")}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
