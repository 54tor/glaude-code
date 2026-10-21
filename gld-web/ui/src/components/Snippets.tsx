import { useEffect, useRef, useState } from "react"
import { Check, Copy, Download, MessageSquarePlus, Pencil, Plus, Save, Terminal, Trash2, Upload, X } from "lucide-react"
import { toast } from "sonner"
import { api, type SnipScope, type Snippet } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Choose } from "./Choose"
import type { Ask } from "./Confirm"
import { Markdown } from "./Conversation"
import type { Ask as PromptAsk } from "./Prompt"

// Snippets = commandes Claude Code (/nom) : personnelles (~/.claude/commands) et de chaque projet (.claude/commands).
// Lecture, copie, édition, création, suppression (vers la corbeille), import et export en zip.
export function Snippets({
  files,
  scopes,
  dir,
  sel,
  q,
  home,
  onPick,
  onLaunch,
  onChanged,
  onAsk,
  onPrompt,
}: {
  files: Snippet[]
  scopes: SnipScope[]
  dir: string // dossier des commandes personnelles
  sel: string // clé du snippet affiché
  q: string
  home: string
  onPick: (key: string) => void
  onLaunch: (s: Snippet, text: string) => void
  onChanged: () => void
  onAsk: (a: Ask) => void
  onPrompt: (a: PromptAsk) => void
}) {
  const f = q.toLowerCase()
  const shown = files.filter((s) => !f || (s.path + " " + s.command + " " + s.description + " " + s.project).toLowerCase().includes(f))
  const [loaded, setLoaded] = useState<{ key: string; text: string } | null>(null)
  const [draft, setDraft] = useState<{ key: string; text: string } | null>(null)
  const [creating, setCreating] = useState("") // nouveau snippet pas encore enregistré
  const [scope, setScope] = useState("") // emplacement de Nouveau / Importer / Exporter ("" : personnelles)
  const [copied, setCopied] = useState(false)
  const upload = useRef<HTMLInputElement>(null)
  const text = loaded?.key === sel ? loaded.text : null
  const editing = draft?.key === sel ? draft : null
  const snip = files.find((s) => s.key === sel)

  useEffect(() => {
    if (!sel || sel === creating) return
    let live = true
    api.snippet(sel).then((r) => live && setLoaded({ key: sel, text: r.text ?? r.error ?? "" }))
    return () => {
      live = false
    }
  }, [sel, creating])

  const label = (sc: string) => (sc ? tilde(scopes.find((x) => x.dir === sc)?.project ?? sc, home) : "Personnelles")
  const prefix = (sc: string) => (sc ? `p/${sc}/` : "u/")

  const copy = () =>
    text !== null &&
    navigator.clipboard.writeText(text).then(
      () => {
        setCopied(true)
        setTimeout(() => setCopied(false), 1200)
      },
      () => toast.error("Copie impossible")
    )
  const save = async () => {
    if (!editing) return
    const r = await api.post("/snippet-save", { key: sel, text: editing.text, create: sel === creating })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (!r.ok) return
    setLoaded({ key: sel, text: editing.text })
    setDraft(null)
    setCreating("")
    onChanged()
  }
  const create = () =>
    onPrompt({
      title: `Nouvelle commande · ${label(scope)}`,
      value: "",
      placeholder: "nom, ou dossier/nom : devient /nom ou /dossier:nom",
      confirm: "Créer",
      run: (name) => {
        const key = prefix(scope) + (name.endsWith(".md") ? name : name + ".md")
        if (files.some((s) => s.key === key)) return void toast.error("Cette commande existe déjà")
        setCreating(key)
        setDraft({ key, text: "---\ndescription: \n---\n\n" })
        onPick(key)
      },
    })
  const remove = () =>
    onAsk({
      title: `Supprimer ${snip?.command ?? sel} ?`,
      body: "Elle est d'abord copiée dans la corbeille (zone Mémoire), d'où elle peut être restaurée.",
      confirm: "Supprimer",
      run: async () => {
        const r = await api.post("/snippet-rm", { key: sel })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          onPick("")
          onChanged()
        }
      },
    })
  // Import : fichiers .md ou archives .zip ; un snippet déjà présent est laissé tel quel.
  const importFiles = async (list: FileList | null) => {
    if (!list?.length) return
    const fd = new FormData()
    fd.set("scope", scope)
    for (const file of list) fd.append("files", file)
    const id = toast.loading("Import…")
    try {
      const r = (await (await fetch("/snippets-import", { method: "POST", body: fd })).json()) as { ok: boolean; out: string }
      ;(r.ok ? toast.success : toast.error)(r.out, { id })
    } catch (e) {
      toast.error(e instanceof Error ? e.message : String(e), { id })
    }
    if (upload.current) upload.current.value = ""
    onChanged()
  }

  // Sections : personnelles d'abord, puis chaque projet qui a des commandes.
  const sections = [["", files.filter((s) => !s.scope)] as const, ...scopes.map((x) => [x.dir, files.filter((s) => s.scope === x.dir)] as const)]

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <div className="flex max-h-72 shrink-0 flex-col border-b md:max-h-none md:w-80 md:border-r md:border-b-0">
        <div className="space-y-2 border-b px-3 py-2">
          <Choose
            value={scope}
            onChange={setScope}
            options={[["", "Personnelles"] as const, ...scopes.map((x) => [x.dir, tilde(x.project, home)] as const)]}
            label="Emplacement"
            className="w-full"
          />
          <div className="flex gap-1">
            <Button variant="outline" size="xs" onClick={create} title="Nouvelle commande à cet emplacement">
              <Plus /> Nouveau
            </Button>
            <Button variant="ghost" size="xs" onClick={() => upload.current?.click()} title="Importer des .md ou une archive .zip">
              <Upload /> Importer
            </Button>
            <Button variant="ghost" size="xs" asChild title="Exporter les commandes de cet emplacement en .zip">
              <a href={`/snippets-export?scope=${encodeURIComponent(scope)}`} download>
                <Download /> Exporter
              </a>
            </Button>
            <input ref={upload} type="file" multiple accept=".md,.zip" className="hidden" onChange={(e) => importFiles(e.target.files)} />
          </div>
        </div>
        <ScrollArea className="min-h-0 flex-1">
          <nav className="p-2">
            {sections.map(([sc, list]) => {
              const vis = list.filter((s) => shown.includes(s))
              if (!vis.length && sc) return null
              return (
                <section key={sc || "u"} className="mb-3">
                  <p className="truncate px-2 py-1 text-xs font-medium text-muted-foreground" title={sc ? undefined : dir}>
                    {label(sc)}
                  </p>
                  {vis.length === 0 && (
                    <p className="px-2 py-1 text-xs text-muted-foreground">{files.length ? "Aucune ne correspond." : "Aucune commande."}</p>
                  )}
                  <ul className="space-y-px">
                    {vis.map((s) => (
                      <li key={s.key}>
                        <button
                          onClick={() => onPick(s.key)}
                          title={s.description || s.path}
                          className={cn(
                            "flex w-full items-start gap-2 rounded-md px-2 py-1.5 text-left text-sm transition-colors",
                            s.key === sel ? "bg-accent font-medium" : "hover:bg-muted"
                          )}
                        >
                          <Terminal className="mt-0.5 size-4 shrink-0 text-muted-foreground" />
                          <span className="min-w-0">
                            <span className="block truncate font-mono text-[13px]">{s.command}</span>
                            <span className="block truncate text-xs font-normal text-muted-foreground">
                              {s.description || `${ago(s.mtime)} · ${size(s.kb)}`}
                            </span>
                          </span>
                        </button>
                      </li>
                    ))}
                  </ul>
                </section>
              )
            })}
          </nav>
        </ScrollArea>
      </div>
      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">
            Choisis une commande, ou crée-en une. Elle est aussi utilisable en <code>/nom</code> dans Claude Code.
          </p>
        ) : (
          <>
            <div className="flex flex-wrap items-center gap-2 border-b px-6 py-2">
              <code className="min-w-0 flex-1 truncate text-xs text-muted-foreground" title={sel}>
                {snip ? `${snip.command} · ${snip.path}` : sel.replace(/^(u|p\/[^/]+)\//, "")}
                {sel === creating && " (nouvelle)"}
              </code>
              {editing ? (
                <>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => {
                      setDraft(null)
                      if (sel === creating) {
                        setCreating("")
                        onPick("")
                      }
                    }}
                  >
                    <X /> Annuler
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === text}>
                    <Save /> Enregistrer
                  </Button>
                </>
              ) : (
                <>
                  <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={remove} disabled={text === null}>
                    <Trash2 /> Supprimer
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => text !== null && setDraft({ key: sel, text })} disabled={text === null}>
                    <Pencil /> Modifier
                  </Button>
                  <Button variant="outline" size="sm" onClick={copy} disabled={text === null}>
                    {copied ? <Check /> : <Copy />} {copied ? "Copié" : "Copier"}
                  </Button>
                  <Button size="sm" onClick={() => snip && text !== null && onLaunch(snip, text)} disabled={!snip || text === null}>
                    <MessageSquarePlus /> Nouvelle session avec
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
                aria-label="Contenu de la commande"
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-7xl px-6 py-6 text-sm leading-relaxed">
                  {text === null ? <Skeleton className="h-32 w-full" /> : <CommandView text={text} />}
                </div>
              </ScrollArea>
            )}
          </>
        )}
      </div>
    </div>
  )
}

// En-tête YAML (description, argument-hint, model…) en petit, puis le prompt rendu en Markdown.
function CommandView({ text }: { text: string }) {
  const m = /^---\n([\s\S]*?)\n---\n?/.exec(text)
  return (
    <>
      {m && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{m[1]}</pre>}
      <Markdown t={m ? text.slice(m[0].length) : text} />
    </>
  )
}
