import { i18n, loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { useEffect, useRef, useState } from "react"
import { BookOpen, Brain, ChevronRight, ClipboardCopy, Copy, FileText, Globe, Link as LinkIcon, ExternalLink, Pencil, ScrollText, Tag, Trash2 } from "lucide-react"
import { useLocation } from "wouter"
import { toast } from "sonner"
import { api, type MemFile } from "@/lib/api"
import { href } from "@/lib/route"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { TYPES, typeOf } from "./MemoryTypes"
import { Markdown } from "./Message"
import { copy, movable, split } from "./memoryLib"
import { reportResult } from "@/lib/report"

const icon = (key: string) => (key.startsWith("m/") ? Brain : key.startsWith("r/") ? ScrollText : FileText)


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
    let live = true
    api.memoryFile(file.key).then(
      (r) => {
        if (!live) return
        if (r.text === undefined) {
          toast.error(r.error ?? i18n.t("memory.main.unreadable"))
          close.current()
        } else setText(r.text)
      },
      () => live && (toast.error(i18n.t("memory.main.unreadable")), close.current())
    )
    return () => {
      live = false
    }
  }, [file.key])
  const save = async () => {
    if (text === null) return
    setBusy(true)
    const ok = reportResult(await api.post("/memory-save", { key: file.key, text }))
    setBusy(false)
    if (ok) {
      onSaved()
      setEditing(false)
    }
  }
  const [fm, body] = split(text ?? "")
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[90vh] flex-col gap-3 sm:max-w-3xl">
        <DialogHeader>
          <DialogTitle className="truncate">{file.name}</DialogTitle>
          <DialogDescription className="truncate font-mono text-xs">{file.path}</DialogDescription>
        </DialogHeader>
        {editing ? (
          <textarea
            value={text ?? ""}
            onChange={(e) => setText(e.target.value)}
            onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), save())}
            disabled={text === null || busy}
            spellCheck={false}
            autoFocus
            aria-label={t("memory.project.contentAria", { name: file.name })}
            className="field-sizing-content min-h-64 w-full flex-1 resize-none overflow-y-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        ) : (
          <div className="min-h-32 flex-1 overflow-y-auto rounded-lg border px-4 py-3 text-sm leading-relaxed">
            {text === null ? (
              <p className="text-muted-foreground">{t("common.loading")}</p>
            ) : (
              <>
                {fm && <pre className="mb-3 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>}
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
  const [shown, setShown] = useState<{ f: MemFile; edit: boolean } | null>(null)
  const [removing, setRemoving] = useState<MemFile | null>(null)
  const [, go] = useLocation()
  const [tick, setTick] = useState(0)
  useEffect(() => {
    let live = true
    const load = () => api.memory().then((r) => live && setFiles(r.projects.find((p) => p.dir === dir)?.files ?? []), () => {})
    load()
    const on = () => document.visibilityState === "visible" && load()
    document.addEventListener("visibilitychange", on)
    return () => {
      live = false
      document.removeEventListener("visibilitychange", on)
    }
  }, [dir, tick])
  const refresh = () => setTick((t) => t + 1)
  if (!files || files.length === 0) return null
  const tokens = files.filter((f) => f.always).reduce((n, f) => n + f.tokens, 0)
  const sorted = [...files].sort((a, b) => +!!b.always - +!!a.always || a.name.localeCompare(b.name, loc()))

  const setType = async (f: MemFile, type: string) => reportResult(await api.post("/memory-type", { key: f.key, type })) && refresh()
  const toGlobal = async (f: MemFile, copyIt: boolean) => reportResult((await api.post("/memory-move", { key: f.key, to: "global", copy: copyIt })) as { ok: boolean; out: string }) && refresh()
  const copyContent = async (f: MemFile) => {
    const r = await api.memoryFile(f.key)
    if (r.text === undefined) return void toast.error(r.error ?? t("memory.main.unreadable"))
    copy(r.text, "memory.lib.copiedContent")
  }
  const remove = async () => {
    const f = removing
    setRemoving(null)
    if (f && reportResult(await api.post("/memory-rm", { key: f.key }))) refresh()
  }
  const canType = (f: MemFile) => f.key.startsWith("m/") && !f.key.endsWith("/MEMORY.md")

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
          {sorted.map((f) => {
            const ty = typeOf(f.type)
            const Icon = ty?.icon ?? icon(f.key)
            return (
              <li key={f.key}>
                <ContextMenu>
                  <ContextMenuTrigger asChild>
                    <div className="group/row flex items-center rounded-md hover:bg-muted">
                      <button onClick={() => setShown({ f, edit: false })} className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1 text-left text-sm" title={f.path}>
                        <Icon className={cn("size-3.5 shrink-0", ty?.color ?? "text-muted-foreground")} />
                        <span className={cn("min-w-0 flex-1 truncate", f.off && "line-through opacity-60")}>{f.name}</span>
                        {ty && <span className={cn("shrink-0 rounded-sm bg-muted px-1.5 text-2xs", ty.color)}>{ty.label}</span>}
                        {f.paths && <span className="shrink-0 text-2xs text-muted-foreground">{t("memory.project.scoped")}</span>}
                        {f.always && <span className="shrink-0 text-2xs tabular-nums text-muted-foreground">{t("memory.project.tokens", { n: f.tokens })}</span>}
                      </button>
                      <Button size="icon-xs" variant="ghost" className="mr-1 shrink-0 md:opacity-0 md:group-focus-within/row:opacity-100 md:group-hover/row:opacity-100" onClick={() => go(href({ kind: "memory", key: f.key }))} aria-label={t("memory.project.openAria", { name: f.name })} title={t("memory.project.openTitle")}>
                        <ExternalLink />
                      </Button>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-60">
                    <ContextMenuLabel className="truncate">{f.name}</ContextMenuLabel>
                    <ContextMenuItem onSelect={() => setShown({ f, edit: false })}>
                      <BookOpen /> {t("memory.project.show")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => go(href({ kind: "memory", key: f.key }))}>
                      <ExternalLink /> {t("memory.project.openPage")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => setShown({ f, edit: true })}>
                      <Pencil /> {t("memory.project.editHere")}
                    </ContextMenuItem>
                    {canType(f) && (
                      <ContextMenuSub>
                        <ContextMenuSubTrigger>
                          <Tag /> {t("memory.project.type")}
                        </ContextMenuSubTrigger>
                        <ContextMenuSubContent>
                          {TYPES.map((ty) => (
                            <ContextMenuItem key={ty.key} onSelect={() => setType(f, ty.key)}>
                              <ty.icon /> {ty.key} <span className="text-xs text-muted-foreground">({ty.label}){f.type === ty.key && " ✓"}</span>
                            </ContextMenuItem>
                          ))}
                          <ContextMenuSeparator />
                          <ContextMenuItem onSelect={() => setType(f, "")}>{t("memory.project.noType")}{!f.type && " ✓"}</ContextMenuItem>
                        </ContextMenuSubContent>
                      </ContextMenuSub>
                    )}
                    <ContextMenuItem onSelect={() => copyContent(f)}>
                      <ClipboardCopy /> {t("memory.project.copyContent")}
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => copy(f.path, "memory.lib.copiedPath")}>
                      <LinkIcon /> {t("memory.project.copyPath")}
                    </ContextMenuItem>
                    {movable(f.key) && !f.key.endsWith("/MEMORY.md") && ( // l'index MEMORY.md reste dans son projet
                      <>
                        <ContextMenuSeparator />
                        <ContextMenuItem onSelect={() => toGlobal(f, false)}>
                          <Globe /> {t("memory.project.moveGlobal")}
                        </ContextMenuItem>
                        <ContextMenuItem onSelect={() => toGlobal(f, true)}>
                          <Copy /> {t("memory.project.copyGlobal")}
                        </ContextMenuItem>
                      </>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem variant="destructive" onSelect={() => setRemoving(f)}>
                      <Trash2 /> {t("memory.project.remove")}
                    </ContextMenuItem>
                  </ContextMenuContent>
                </ContextMenu>
              </li>
            )
          })}
        </ul>
      )}
      {shown && <EditDialog key={shown.f.key} file={shown.f} edit={shown.edit} onClose={() => setShown(null)} onSaved={refresh} />}
      <AlertDialog open={!!removing} onOpenChange={(o) => !o && setRemoving(null)}>
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
