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
import { TYPES } from "./MemoryTypes"
import { Markdown } from "./Message"
import { copy, movable, split } from "./memoryLib"

const icon = (key: string) => (key.startsWith("m/") ? Brain : key.startsWith("r/") ? ScrollText : FileText)

const report = (r: { ok: boolean; out: string }) => (r.ok ? toast.success(r.out) : toast.error(r.out), r.ok)

// Édition d'un fichier sur place : le texte brut, enregistré tel quel (comme dans la page Mémoire).
function EditDialog({ file, edit, onClose, onSaved }: { file: MemFile; edit: boolean; onClose: () => void; onSaved: () => void }) {
  const [text, setText] = useState<string | null>(null)
  const [editing, setEditing] = useState(edit) // d'abord l'aperçu mis en forme ; « Modifier » passe au texte brut
  const [busy, setBusy] = useState(false)
  const close = useRef(onClose) // le parent recrée onClose à chaque rendu : l'effet de lecture ne doit pas repartir (il écraserait la frappe)
  useEffect(() => {
    close.current = onClose
  })
  useEffect(() => {
    let live = true
    api.memoryFile(file.key).then((r) => {
      if (!live) return
      if (r.text === undefined) {
        toast.error(r.error ?? "Fichier illisible")
        close.current()
      } else setText(r.text)
    })
    return () => {
      live = false
    }
  }, [file.key])
  const save = async () => {
    if (text === null) return
    setBusy(true)
    const ok = report(await api.post("/memory-save", { key: file.key, text }))
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
            aria-label={`Contenu de ${file.name}`}
            className="field-sizing-content min-h-64 w-full flex-1 resize-none overflow-y-auto rounded-lg border bg-background px-3 py-2 font-mono text-xs outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          />
        ) : (
          <div className="min-h-32 flex-1 overflow-y-auto rounded-lg border px-4 py-3 text-sm leading-relaxed">
            {text === null ? (
              <p className="text-muted-foreground">Chargement…</p>
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
                Aperçu
              </Button>
              <Button onClick={save} disabled={text === null || busy} title="Ctrl+Entrée">
                Enregistrer
              </Button>
            </>
          ) : (
            <>
              <Button variant="ghost" onClick={onClose}>
                Fermer
              </Button>
              <Button onClick={() => setEditing(true)} disabled={text === null}>
                <Pencil /> Modifier
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
  const [files, setFiles] = useState<MemFile[] | null>(null)
  const [open, setOpen] = useState(true)
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
  const sorted = [...files].sort((a, b) => +!!b.always - +!!a.always || a.name.localeCompare(b.name, "fr"))

  const setType = async (f: MemFile, type: string) => report(await api.post("/memory-type", { key: f.key, type })) && refresh()
  const toGlobal = async (f: MemFile, copyIt: boolean) => report((await api.post("/memory-move", { key: f.key, to: "global", copy: copyIt })) as { ok: boolean; out: string }) && refresh()
  const copyContent = async (f: MemFile) => {
    const r = await api.memoryFile(f.key)
    if (r.text === undefined) return void toast.error(r.error ?? "Fichier illisible")
    copy(r.text, "Contenu")
  }
  const remove = async () => {
    const f = removing
    setRemoving(null)
    if (f && report(await api.post("/memory-rm", { key: f.key }))) refresh()
  }
  const canType = (f: MemFile) => f.key.startsWith("m/") && !f.key.endsWith("/MEMORY.md")

  return (
    <section className="mb-4 rounded-lg border">
      <button onClick={() => setOpen(!open)} className="flex w-full items-center gap-2 px-3 py-2 text-left text-sm" aria-expanded={open}>
        <ChevronRight className={cn("size-4 shrink-0 transition-transform", open && "rotate-90")} />
        <Brain className="size-4 shrink-0 text-muted-foreground" />
        <span className="font-medium">Mémoire du projet</span>
        <span className="text-xs text-muted-foreground">
          {files.length} fichier{files.length > 1 ? "s" : ""}
          {tokens > 0 && ` · ≈ ${tokens.toLocaleString("fr-FR")} tokens chargés à chaque session`}
        </span>
      </button>
      {open && (
        <ul className="grid gap-px border-t p-1 md:grid-cols-2">
          {sorted.map((f) => {
            const Icon = icon(f.key)
            return (
              <li key={f.key}>
                <ContextMenu>
                  <ContextMenuTrigger asChild>
                    <div className="group/row flex items-center rounded-md hover:bg-muted">
                      <button onClick={() => setShown({ f, edit: false })} className="flex min-w-0 flex-1 items-center gap-2 px-2 py-1 text-left text-sm" title={f.path}>
                        <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                        <span className={cn("min-w-0 flex-1 truncate", f.off && "line-through opacity-60")}>{f.name}</span>
                        {f.type && <span className="shrink-0 rounded bg-muted px-1.5 text-[10px] text-muted-foreground">{f.type}</span>}
                        {f.paths && <span className="shrink-0 text-[10px] text-muted-foreground">scopée</span>}
                        <span className="shrink-0 text-[11px] tabular-nums text-muted-foreground">{f.always ? `${f.tokens} tok.` : "à la demande"}</span>
                      </button>
                      <Button size="icon-xs" variant="ghost" className="mr-1 shrink-0 opacity-0 group-focus-within/row:opacity-100 group-hover/row:opacity-100" onClick={() => go(href({ kind: "memory", key: f.key }))} aria-label={`Ouvrir ${f.name} dans la page Mémoire`} title="Ouvrir dans la page Mémoire">
                        <ExternalLink />
                      </Button>
                    </div>
                  </ContextMenuTrigger>
                  <ContextMenuContent className="w-60">
                    <ContextMenuLabel className="truncate">{f.name}</ContextMenuLabel>
                    <ContextMenuItem onSelect={() => setShown({ f, edit: false })}>
                      <BookOpen /> Afficher
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => go(href({ kind: "memory", key: f.key }))}>
                      <ExternalLink /> Ouvrir dans la page Mémoire
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => setShown({ f, edit: true })}>
                      <Pencil /> Modifier ici
                    </ContextMenuItem>
                    {canType(f) && (
                      <ContextMenuSub>
                        <ContextMenuSubTrigger>
                          <Tag /> Type
                        </ContextMenuSubTrigger>
                        <ContextMenuSubContent>
                          {TYPES.map((t) => (
                            <ContextMenuItem key={t.key} onSelect={() => setType(f, t.key)}>
                              <t.icon /> {t.key} <span className="text-xs text-muted-foreground">({t.label}){f.type === t.key && " ✓"}</span>
                            </ContextMenuItem>
                          ))}
                          <ContextMenuSeparator />
                          <ContextMenuItem onSelect={() => setType(f, "")}>Sans type{!f.type && " ✓"}</ContextMenuItem>
                        </ContextMenuSubContent>
                      </ContextMenuSub>
                    )}
                    <ContextMenuItem onSelect={() => copyContent(f)}>
                      <ClipboardCopy /> Copier le contenu
                    </ContextMenuItem>
                    <ContextMenuItem onSelect={() => copy(f.path, "Chemin")}>
                      <LinkIcon /> Copier le chemin
                    </ContextMenuItem>
                    {movable(f.key) && !f.key.endsWith("/MEMORY.md") && ( // l'index MEMORY.md reste dans son projet
                      <>
                        <ContextMenuSeparator />
                        <ContextMenuItem onSelect={() => toGlobal(f, false)}>
                          <Globe /> Déplacer vers les règles globales
                        </ContextMenuItem>
                        <ContextMenuItem onSelect={() => toGlobal(f, true)}>
                          <Copy /> Copier vers les règles globales
                        </ContextMenuItem>
                      </>
                    )}
                    <ContextMenuSeparator />
                    <ContextMenuItem variant="destructive" onSelect={() => setRemoving(f)}>
                      <Trash2 /> Supprimer
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
            <AlertDialogTitle>Supprimer {removing?.name} ?</AlertDialogTitle>
            <AlertDialogDescription>
              {removing?.key.startsWith("m/") ? "Suppression définitive. Sa ligne dans MEMORY.md est retirée aussi." : "Suppression définitive (une copie reste dans la corbeille)."}
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>Annuler</AlertDialogCancel>
            <AlertDialogAction variant="destructive" onClick={remove}>
              Supprimer
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </section>
  )
}
