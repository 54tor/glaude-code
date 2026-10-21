import { useCallback, useEffect, useMemo, useState } from "react"
import { AlertTriangle, Copy, EyeOff, FileText, Hand, Pencil, Plus, Save, Sparkles, Trash2, X, Zap } from "lucide-react"
import { toast } from "sonner"
import { api, type Skill, type SkillFile } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Choose } from "./Choose"
import type { Ask } from "./Confirm"
import { Markdown } from "./Message"
import { SkillWriter } from "./SkillWriter"
import { split } from "./memoryLib"

const NAME_OK = /^[a-z0-9][a-z0-9-]{0,63}$/

// Skills de Claude Code : personnels (~/.claude/skills), de projet (.claude/skills) et de plugins (lecture seule).
// Lecture, modification de SKILL.md, création, copie vers un autre emplacement, suppression (copie à la corbeille), rédaction par un modèle.
export function Skills({ sel, home, q, onPick, onAsk }: { sel: string; home: string; q: string; onPick: (key: string) => void; onAsk: (a: Ask) => void }) {
  const [data, setData] = useState<{ skills: Skill[]; projects: { dir: string; project: string }[] } | null>(null)
  const [loaded, setLoaded] = useState<{ key: string; text: string; files: SkillFile[]; error?: string } | null>(null)
  const [draft, setDraft] = useState<{ key: string; text: string } | null>(null)
  const [creating, setCreating] = useState(false)
  const [writer, setWriter] = useState(false)
  const reload = useCallback(() => api.skills().then(setData), [])
  useEffect(() => {
    reload()
  }, [reload])
  useEffect(() => {
    if (!sel) return
    let live = true
    api.skill(sel).then((r) => live && setLoaded({ key: sel, text: r.text ?? "", files: r.files ?? [], error: r.error }))
    return () => {
      live = false
    }
  }, [sel, data])

  const f = q.toLowerCase()
  const skills = useMemo(() => (data?.skills ?? []).filter((s) => !f || (s.name + " " + s.description + " " + s.project + " " + s.plugin).toLowerCase().includes(f)), [data, f])
  const skill = data?.skills.find((s) => s.key === sel)
  const cur = loaded?.key === sel ? loaded : null
  const editing = draft?.key === sel ? draft : null
  const groups = useMemo(() => {
    const g = new Map<string, { title: string; items: Skill[] }>()
    for (const s of skills) {
      const k = s.scope === "user" ? "u" : s.scope === "project" ? "p:" + s.project : "x:" + s.plugin
      const title = s.scope === "user" ? "Personnels" : s.scope === "project" ? tilde(s.project ?? "", home) : "Plugin " + s.plugin
      if (!g.has(k)) g.set(k, { title, items: [] })
      g.get(k)!.items.push(s)
    }
    return [...g.values()]
  }, [skills, home])
  const budget = (data?.skills ?? []).filter((s) => !s.manual).reduce((n, s) => n + Math.min(s.chars, 1536), 0)

  const save = async () => {
    if (!editing) return
    const r = await api.post("/skill-save", { key: sel, text: editing.text, create: false })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setDraft(null)
      reload()
    }
  }
  const remove = () =>
    skill &&
    onAsk({
      title: `Supprimer le skill ${skill.name} ?`,
      body: "Une copie de SKILL.md est gardée dans la corbeille de la zone Mémoire.",
      confirm: "Supprimer",
      run: async () => {
        const r = await api.post("/skill-rm", { key: skill.key })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          onPick("")
          reload()
        }
      },
    })
  const copyTo = async (to: string) => {
    const r = (await api.post("/skill-copy", { key: sel, to })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      await reload()
      onPick(r.key)
    }
  }
  const [fm, body] = split(cur?.text ?? "")

  return (
    <div className="flex min-h-0 flex-1 flex-col md:flex-row">
      <ScrollArea className="max-h-64 shrink-0 border-b md:max-h-none md:w-96 md:border-r md:border-b-0">
        <nav className="p-2">
          <div className="mb-2 flex items-center gap-2 px-1">
            <Button size="xs" variant="outline" onClick={() => setCreating(true)}>
              <Plus /> Nouveau skill
            </Button>
            {data && (
              <span className="text-[11px] text-muted-foreground" title="Les descriptions des skills que Claude peut lancer seul sont toujours dans son contexte (1 536 caractères au plus chacune)">
                ≈ {budget.toLocaleString("fr-FR")} car. de descriptions en contexte
              </span>
            )}
          </div>
          {!data && <Skeleton className="m-2 h-24" />}
          {groups.map((g) => (
            <section key={g.title} className="mb-3">
              <p className="px-2 py-1 text-xs font-medium text-muted-foreground">
                {g.title} <span className="font-normal">· {g.items.length}</span>
              </p>
              <ul className="space-y-px">
                {g.items.map((s) => (
                  <li key={s.key}>
                    <button
                      onClick={() => onPick(s.key)}
                      className={cn("flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors", s.key === sel ? "bg-accent" : "hover:bg-muted")}
                    >
                      <span className="flex items-center gap-1.5">
                        <Zap className="size-3.5 shrink-0 text-amber-500" />
                        <span className="min-w-0 flex-1 truncate font-medium">{s.name}</span>
                        {s.manual && <Hand className="size-3.5 shrink-0 text-muted-foreground" aria-label="lancé à la main seulement" />}
                        {s.hidden && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label="invisible dans le menu /" />}
                        {s.warn && <AlertTriangle className="size-3.5 shrink-0 text-amber-500" aria-label="à vérifier" />}
                      </span>
                      <span className="line-clamp-2 text-xs text-muted-foreground">{s.description || "(sans description)"}</span>
                    </button>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {data && skills.length === 0 && <p className="px-2 py-6 text-center text-sm text-muted-foreground">Aucun skill{q ? " ne correspond à cette recherche" : ""}.</p>}
        </nav>
      </ScrollArea>

      <div className="flex min-h-0 min-w-0 flex-1 flex-col">
        {!sel ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">Choisis un skill à afficher. Un skill est un dossier avec un SKILL.md : Claude le charge quand sa description correspond à la demande, ou quand tu tapes /nom.</p>
        ) : !skill ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">Skill introuvable.</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b px-6 py-2">
              <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={skill.path}>
                /{skill.dir} · {skill.scope === "plugin" ? `plugin ${skill.plugin} · lecture seule` : skill.scope === "project" ? tilde(skill.project ?? "", home) : "personnel"} · {skill.files} fichier{skill.files > 1 ? "s" : ""} · {ago(skill.mtime)}
              </code>
              {editing ? (
                <>
                  <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                    <X /> Annuler
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === cur?.text}>
                    <Save /> Enregistrer
                  </Button>
                </>
              ) : (
                <>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="sm">
                        <Copy /> Copier vers…
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                      {skill.scope !== "user" && <DropdownMenuItem onSelect={() => copyTo("u")}>Mes skills personnels</DropdownMenuItem>}
                      {skill.scope !== "user" && <DropdownMenuSeparator />}
                      <DropdownMenuLabel>Un projet</DropdownMenuLabel>
                      {(data?.projects ?? []).map((p) => (
                        <DropdownMenuItem key={p.dir} onSelect={() => copyTo("p/" + p.dir)}>
                          <span className="truncate">{tilde(p.project, home)}</span>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                  {!skill.readOnly && (
                    <>
                      <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={remove}>
                        <Trash2 /> Supprimer
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => setWriter(true)}>
                        <Sparkles /> Haiku…
                      </Button>
                      <Button size="sm" onClick={() => cur && setDraft({ key: sel, text: cur.text })} disabled={!cur}>
                        <Pencil /> Modifier
                      </Button>
                    </>
                  )}
                </>
              )}
            </div>
            {skill.warn && !editing && (
              <p className="flex items-start gap-2 border-b bg-amber-500/10 px-6 py-2 text-xs">
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-amber-500" /> {skill.warn}
              </p>
            )}
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(e) => setDraft({ key: sel, text: e.target.value })}
                onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), save())}
                spellCheck={false}
                autoFocus
                aria-label="Contenu de SKILL.md"
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-5xl px-6 py-6 text-sm leading-relaxed">
                  {!cur ? (
                    <Skeleton className="h-32 w-full" />
                  ) : cur.error ? (
                    <p className="text-destructive">{cur.error}</p>
                  ) : (
                    <>
                      <p className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        {skill.manual && <span>lancé à la main seulement (disable-model-invocation)</span>}
                        {skill.hidden && <span>invisible dans le menu / (user-invocable: false)</span>}
                        {skill.tools && <span>outils pré-autorisés : {skill.tools}</span>}
                        <span>description : {skill.chars} car.{skill.chars > 1536 ? " (tronquée à 1 536)" : ""}</span>
                      </p>
                      {fm && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>}
                      <Markdown t={body} />
                      {cur.files.length > 1 && (
                        <div className="mt-6 border-t pt-3">
                          <p className="mb-1 text-xs font-medium text-muted-foreground">Fichiers du dossier</p>
                          <ul className="space-y-px text-xs">
                            {cur.files.map((x) => (
                              <li key={x.rel} className="flex items-center gap-1.5">
                                <FileText className="size-3 shrink-0 text-muted-foreground" />
                                <code>{x.rel}</code>
                                <span className="text-muted-foreground">{x.kb} Ko</span>
                              </li>
                            ))}
                          </ul>
                        </div>
                      )}
                    </>
                  )}
                </div>
              </ScrollArea>
            )}
          </>
        )}
      </div>
      <SkillWriter open={writer} skillKey={sel} name={skill?.name ?? ""} onClose={() => setWriter(false)} onDraft={(text) => setDraft({ key: sel, text })} />
      <NewSkill open={creating} projects={data?.projects ?? []} home={home} onClose={() => setCreating(false)} onCreated={async (key) => (await reload(), onPick(key))} />
    </div>
  )
}

// Nouveau skill : un nom (minuscules, chiffres, tirets), un emplacement ; le SKILL.md part d'un modèle à compléter.
function NewSkill({ open, projects, home, onClose, onCreated }: { open: boolean; projects: { dir: string; project: string }[]; home: string; onClose: () => void; onCreated: (key: string) => void }) {
  const [name, setName] = useState("")
  const [where, setWhere] = useState("u")
  const ok = NAME_OK.test(name)
  const create = async () => {
    const key = where === "u" ? `u/${name}` : `p/${where}/${name}`
    const text = `---\nname: ${name}\ndescription: \n---\n\n# ${name}\n\nInstructions du skill.\n`
    const r = await api.post("/skill-save", { key, text, create: true })
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok) {
      setName("")
      onClose()
      onCreated(key)
    }
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>Nouveau skill</DialogTitle>
          <DialogDescription>Crée le dossier et un SKILL.md à compléter (ou à faire rédiger par un modèle).</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (ok) create()
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="nom-du-skill" autoFocus aria-label="Nom du skill" className="font-mono" />
          {name && !ok && <p className="text-xs text-destructive">Minuscules, chiffres et tirets seulement, 64 caractères au plus.</p>}
          <Choose value={where} onChange={setWhere} options={[["u", "Mes skills personnels"], ...projects.map((p) => [p.dir, `Projet · ${tilde(p.project, home)}`] as const)]} label="Emplacement" />
          <DialogFooter>
            <Button variant="ghost" type="button" onClick={onClose}>
              Annuler
            </Button>
            <Button type="submit" disabled={!ok}>
              Créer
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
