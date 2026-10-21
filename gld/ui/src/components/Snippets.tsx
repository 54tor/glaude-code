import { useEffect, useRef, useState } from "react"
import { ArrowLeft, Bot, Check, Copy, Download, Loader2, MessageSquarePlus, Pencil, Plus, Save, Sparkles, Terminal, Trash2, Undo2, Upload, X } from "lucide-react"
import { toast } from "sonner"
import { api, type SnipScope, type Snippet } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Choose } from "./Choose"
import type { Ask } from "./Confirm"
import { Markdown } from "./Message"
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
  const [haiku, setHaiku] = useState(false) // fenêtre de mise en forme ouverte
  const [before, setBefore] = useState<{ key: string; text: string } | null>(null) // texte d'avant la mise en forme
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

  const formatted = (t: string) => {
    const cur = editing?.text ?? text ?? ""
    setBefore({ key: sel, text: cur })
    setDraft({ key: sel, text: t })
  }

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
      <div className={cn("shrink-0 flex-col border-b md:flex md:max-h-none md:w-80 md:flex-none md:border-r md:border-b-0", sel ? "hidden" : "flex min-h-0 flex-1")}>
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
      <div className={cn("min-h-0 min-w-0 flex-1 flex-col md:flex", sel ? "flex" : "hidden")}>
        {sel && (
          <div className="border-b px-2 py-1.5 md:hidden">
            <Button variant="ghost" size="sm" onClick={() => onPick("")}>
              <ArrowLeft /> Liste
            </Button>
          </div>
        )}

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
                  {before?.key === sel && before.text !== editing.text && (
                    <Button
                      variant="ghost"
                      size="sm"
                      onClick={() => (setDraft({ key: sel, text: before.text }), setBefore(null))}
                      title="Revenir au texte d'avant la mise en forme"
                    >
                      <Undo2 /> Texte d'avant
                    </Button>
                  )}
                  <Button variant="outline" size="sm" onClick={() => setHaiku(true)}>
                    <Bot /> Améliorer…
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
                  <Button variant="ghost" size="sm" onClick={() => setHaiku(true)} disabled={text === null} title="Améliorer avec un modèle (mise en forme)">
                    <Bot /> Améliorer…
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
      <HaikuFormat open={haiku} text={editing?.text ?? text ?? ""} onClose={() => setHaiku(false)} onDone={formatted} />
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

const MODELS = [
  ["haiku", "Haiku (rapide, quelques dixièmes de centime)"],
  ["sonnet", "Sonnet"],
  ["opus", "Opus"],
] as const

// Mise en forme d'une commande par un petit modèle : le résultat remplace le texte de l'éditeur, sans être enregistré.
function HaikuFormat({ open, text, onClose, onDone }: { open: boolean; text: string; onClose: () => void; onDone: (t: string) => void }) {
  const [instruction, setInstruction] = useState("")
  const [model, setModel] = useState("haiku")
  const [busy, setBusy] = useState(false)
  const run = async () => {
    setBusy(true)
    const r = (await api.post("/snippet-draft", { model, instruction, text })) as { ok: boolean; out: string; text?: string; cost?: number }
    setBusy(false)
    if (!r.ok || !r.text) return void toast.error(r.out)
    toast.success("Mise en forme prête : relis-la puis enregistre", { description: r.cost ? `coût : ${r.cost.toFixed(4)} $` : undefined })
    setInstruction("")
    onClose()
    onDone(r.text)
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && !busy && onClose()}>
      <DialogContent className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>Mettre en forme avec Haiku</DialogTitle>
          <DialogDescription>
            En-tête description et argument-hint, prompt clair, $ARGUMENTS à sa place. Le résultat s'ouvre dans l'éditeur, sans être enregistré.
          </DialogDescription>
        </DialogHeader>
        <textarea
          value={instruction}
          onChange={(e) => setInstruction(e.target.value)}
          onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "Enter" && (e.preventDefault(), run())}
          placeholder={
            text.trim() ? "Consigne facultative : ajoute un format de sortie, prends un fichier en argument…" : "Décris la commande à rédiger…"
          }
          autoFocus
          rows={3}
          disabled={busy}
          className="w-full resize-y rounded-lg border bg-background px-3 py-2 text-sm outline-none focus-visible:ring-3 focus-visible:ring-ring/50"
          aria-label="Consigne"
        />
        <DialogFooter className="flex-wrap items-center gap-2 sm:justify-between">
          <Choose value={model} onChange={setModel} options={MODELS} label="Modèle" size="default" />
          <div className="flex gap-2">
            <Button variant="outline" onClick={onClose} disabled={busy}>
              Annuler
            </Button>
            <Button onClick={run} disabled={busy || (!text.trim() && !instruction.trim())} title="Ctrl+Entrée">
              {busy ? <Loader2 className="animate-spin" /> : <Sparkles />} Mettre en forme
            </Button>
          </div>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
