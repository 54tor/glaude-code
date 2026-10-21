import { useEffect, useState } from "react"
import { Brain, Copy, FileText, FolderInput, Globe, Merge, Pencil, Save, Trash2, X } from "lucide-react"
import { toast } from "sonner"
import { api, type MemFile, type MemProject, type MemTarget } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import type { Ask } from "./Confirm"
import { Markdown } from "./Conversation"

type List = { global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }

// En-tête YAML (--- … ---) des entrées de mémoire : affiché à part, en petit.
function split(t: string): [string, string] {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(t)
  return m ? [m[1], t.slice(m[0].length)] : ["", t]
}

function Section({
  icon: Icon,
  title,
  files,
  sel,
  onPick,
  picked,
  onToggle,
}: {
  icon: typeof Globe
  title: string
  files: MemFile[]
  sel: string
  onPick: (k: string) => void
  picked: Set<string>
  onToggle: (k: string) => void
}) {
  return (
    <section className="mb-3">
      <p className="flex items-center gap-1.5 px-2 py-1 text-xs font-medium text-muted-foreground" title={title}>
        <Icon className="size-3.5 shrink-0" /> <span className="truncate">{title}</span>
      </p>
      <ul className="space-y-px">
        {files.map((f) => (
          <li key={f.key} className="group flex items-center gap-1.5">
            <Checkbox
              checked={picked.has(f.key)}
              onCheckedChange={() => onToggle(f.key)}
              aria-label={`Sélectionner ${f.name} pour une fusion`}
              className={cn("ml-1 transition-opacity", picked.size === 0 && "md:opacity-0 md:group-hover:opacity-100 md:focus-visible:opacity-100")}
            />
            <button
              onClick={() => onPick(f.key)}
              className={cn(
                "flex w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm transition-colors",
                f.key === sel ? "bg-accent font-medium" : "hover:bg-muted"
              )}
            >
              <FileText className="size-3.5 shrink-0 text-muted-foreground" />
              <span className="min-w-0 flex-1 truncate">{f.name}</span>
              <span className="shrink-0 text-xs font-normal text-muted-foreground">{ago(f.mtime)}</span>
            </button>
          </li>
        ))}
      </ul>
    </section>
  )
}

// Mémoire de Claude : globale (CLAUDE.md, rules/) et par projet (memory/, CLAUDE.md du projet), lecture et édition.
export function Memory({
  sel,
  home,
  q,
  onPick,
  onAsk,
  args,
}: {
  sel: string
  home: string
  q: string
  onPick: (k: string) => void
  onAsk: (a: Ask) => void
  args: string
}) {
  const [picked, setPicked] = useState(() => new Set<string>())
  const toggle = (k: string) =>
    setPicked((p) => {
      const n = new Set(p)
      if (n.has(k)) n.delete(k)
      else n.add(k)
      return n
    })
  // Fusion par Claude : le serveur rédige le prompt ; on le copie, ou on l'ouvre dans une nouvelle session.
  const merge = async (launch: boolean) => {
    const r = (await api.post("/memory-merge", {
      keys: [...picked],
      launch,
      args,
    })) as { ok: boolean; out: string; prompt?: string }
    if (!launch && r.ok && r.prompt) {
      await navigator.clipboard.writeText(r.prompt).then(
        () =>
          toast.success("Prompt de fusion copié", {
            description: "Colle-le dans une session Claude.",
          }),
        () => toast.error("Copie impossible")
      )
      return
    }
    ;(r.ok ? toast.success : toast.error)(r.out.trim() || (r.ok ? "Session lancée" : "Échec"))
    if (r.ok) setPicked(new Set())
  }
  const [list, setList] = useState<List | null>(null)
  const [loaded, setLoaded] = useState<{ key: string; text: string } | null>(null)
  const [draft, setDraft] = useState<{ key: string; text: string } | null>(null)
  const text = loaded?.key === sel ? loaded.text : null
  const editing = draft?.key === sel ? draft : null

  const refresh = () => api.memory().then(setList)
  useEffect(() => {
    let live = true
    api.memory().then((l) => live && setList(l))
    return () => {
      live = false
    }
  }, [])
  useEffect(() => {
    if (!sel) return
    let live = true
    api.memoryFile(sel).then((r) => live && setLoaded({ key: sel, text: r.text ?? r.error ?? "" }))
    return () => {
      live = false
    }
  }, [sel])

  const all = list ? [...list.global, ...list.projects.flatMap((p) => p.files)] : []
  const file = all.find((f) => f.key === sel)
  const f = q.toLowerCase()
  const match = (x: MemFile, scope = "") => !f || x.name.toLowerCase().includes(f) || scope.toLowerCase().includes(f)

  const save = async () => {
    if (!editing) return
    const r = await api.post("/memory-save", { key: sel, text: editing.text })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setLoaded({ key: sel, text: editing.text })
      setDraft(null)
      refresh()
    }
  }
  // Une entrée de mémoire de projet ou une règle globale peut changer de projet (ou devenir une règle globale).
  const movable = sel.startsWith("m/") || sel.startsWith("g/rules/")
  const here = sel.startsWith("m/") ? sel.split("/")[1] : "global"
  const move = async (to: string) => {
    const r = (await api.post("/memory-move", { key: sel, to })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      refresh()
      onPick(r.key)
    }
  }
  const remove = () =>
    onAsk({
      title: `Supprimer ${file?.name ?? "ce fichier"} ?`,
      body: sel.startsWith("m/") ? "Suppression définitive. Sa ligne dans MEMORY.md est retirée aussi." : "Suppression définitive.",
      confirm: "Supprimer",
      run: async () => {
        const r = await api.post("/memory-rm", { key: sel })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          onPick("")
          refresh()
        }
      },
    })

  const [fm, body] = split(text ?? "")
  return (
    <div className="relative flex min-h-0 flex-1 flex-col md:flex-row">
      <ScrollArea className="max-h-64 shrink-0 border-b md:max-h-none md:w-80 md:border-r md:border-b-0">
        <nav className="p-2">
          {!list && <Skeleton className="m-2 h-24" />}
          {list && list.global.some((x) => match(x, "globale")) && (
            <Section
              icon={Globe}
              title="Globale"
              files={list.global.filter((x) => match(x, "globale"))}
              sel={sel}
              onPick={onPick}
              picked={picked}
              onToggle={toggle}
            />
          )}
          {list?.projects.map((p) => {
            const files = p.files.filter((x) => match(x, p.project))
            return files.length ? (
              <Section
                key={p.dir}
                icon={Brain}
                title={tilde(p.project, home) + (p.missing ? " (introuvable)" : "")}
                files={files}
                sel={sel}
                onPick={onPick}
                picked={picked}
                onToggle={toggle}
              />
            ) : null
          })}
        </nav>
      </ScrollArea>
      {picked.size > 0 && (
        <div className="flex flex-wrap items-center gap-1 border-b bg-popover px-3 py-2 text-xs md:absolute md:bottom-4 md:left-2 md:w-[19rem] md:rounded-xl md:border md:shadow-lg">
          <span className="mr-auto font-medium">
            {picked.size} fichier{picked.size > 1 ? "s" : ""}
          </span>
          <Button variant="ghost" size="xs" onClick={() => merge(false)} disabled={picked.size < 2} title="Copier le prompt de fusion">
            <Copy /> Prompt
          </Button>
          <Button size="xs" onClick={() => merge(true)} disabled={picked.size < 2} title="Nouvelle session Claude qui fusionne ces fichiers">
            <Merge /> Fusionner avec Claude
          </Button>
          <Button variant="ghost" size="icon-xs" onClick={() => setPicked(new Set())} aria-label="Vider la sélection">
            <X />
          </Button>
        </div>
      )}
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">Choisis un fichier de mémoire à afficher.</p>
        ) : (
          <>
            <div className="flex items-center gap-2 border-b px-6 py-2">
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground">{file ? `${file.name} · ${size(file.kb)}` : sel}</code>
              {editing ? (
                <>
                  <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                    <X /> Annuler
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === text}>
                    <Save /> Enregistrer
                  </Button>
                </>
              ) : (
                <>
                  {movable && list && (
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="sm" disabled={!file}>
                          <FolderInput /> Déplacer vers…
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                        {here !== "global" && (
                          <>
                            <DropdownMenuItem onSelect={() => move("global")}>
                              <Globe /> Règles globales (rules/)
                            </DropdownMenuItem>
                            <DropdownMenuSeparator />
                          </>
                        )}
                        <DropdownMenuLabel>Mémoire du projet</DropdownMenuLabel>
                        {list.targets
                          .filter((t) => t.dir !== here)
                          .map((t) => (
                            <DropdownMenuItem key={t.dir} onSelect={() => move(t.dir)}>
                              <Brain /> <span className="truncate">{tilde(t.project, home)}</span>
                            </DropdownMenuItem>
                          ))}
                      </DropdownMenuContent>
                    </DropdownMenu>
                  )}
                  <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={remove} disabled={!file}>
                    <Trash2 /> Supprimer
                  </Button>
                  <Button size="sm" onClick={() => text !== null && setDraft({ key: sel, text })} disabled={text === null}>
                    <Pencil /> Modifier
                  </Button>
                </>
              )}
            </div>
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(e) => setDraft({ key: sel, text: e.target.value })}
                onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), save())}
                spellCheck={false}
                autoFocus
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
                aria-label="Contenu du fichier"
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-3xl px-6 py-6 text-sm leading-relaxed">
                  {text === null ? (
                    <Skeleton className="h-32 w-full" />
                  ) : (
                    <>
                      {fm && (
                        <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>
                      )}
                      <Markdown t={body} onLink={(h) => /^[^/:?#]+\.md$/.test(h) && (onPick(sel.slice(0, sel.lastIndexOf("/") + 1) + h), true)} />
                    </>
                  )}
                </div>
              </ScrollArea>
            )}
          </>
        )}
      </div>
    </div>
  )
}
