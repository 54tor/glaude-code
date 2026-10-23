import { loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useMemo, useState } from "react"
import { AlertTriangle, ArrowLeft, BookOpen, Bot, ClipboardCopy, Copy, EyeOff, FileText, FolderInput, Hand, Pencil, Plus, Save, Sparkles, Trash2, X, Zap } from "lucide-react"
import { toast } from "sonner"
import { api, type Skill, type SkillFile } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuLabel, ContextMenuSeparator, ContextMenuSub, ContextMenuSubContent, ContextMenuSubTrigger, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuLabel, DropdownMenuSeparator, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Choose } from "./Choose"
import type { Ask } from "./Confirm"
import { Markdown } from "./Message"
import { SkillWriter } from "./SkillWriter"
import { split } from "./memoryLib"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"

// Origine d'un skill : badge court et explication.
const ORIGINS = {
  store: "bg-info/10 text-info",
  plugin: "bg-info/10 text-info",
  install: "bg-success/10 text-success",
  custom: "bg-muted text-muted-foreground",
} as const

function OriginBadge({ s }: { s: Skill }) {
  const { t } = useTranslation()
  const o = s.origin in ORIGINS ? s.origin : "custom"
  const what = t(`skills.origin.${o}.hint`)
  return (
    <span className={cn("shrink-0 rounded-sm px-1.5 py-px text-2xs font-medium", ORIGINS[o])} title={s.source ? t("skills.origin.withSource", { what, source: s.source }) : what}>
      {t(`skills.origin.${o}.label`)}
    </span>
  )
}

const NAME_OK = /^[a-z0-9][a-z0-9-]{0,63}$/

// Skills de Claude Code : personnels (~/.claude/skills), de projet (.claude/skills) et de plugins (lecture seule).
// Lecture, modification de SKILL.md, création, copie vers un autre emplacement, suppression (copie à la corbeille), rédaction par un modèle.
export function Skills({ sel, home, q, onPick, onAsk }: { sel: string; home: string; q: string; onPick: (key: string) => void; onAsk: (a: Ask) => void }) {
  const { t } = useTranslation()
  const [data, setData] = useState<{ skills: Skill[]; projects: { dir: string; project: string }[] } | null>(null)
  const [loaded, setLoaded] = useState<{ key: string; text: string; files: SkillFile[]; error?: string } | null>(null)
  const [draft, setDraft] = useEditorDraft("skill")
  const [creating, setCreating] = useState(false)
  const [origin, setOrigin] = useState("all") // filtre sur l'origine
  const [writer, setWriter] = useState("") // clé du skill que le modèle rédige ; vide = fermé
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
  const skills = useMemo(() => (data?.skills ?? []).filter((s) => (origin === "all" || s.origin === origin) && (!f || (s.name + " " + s.description + " " + s.project + " " + s.plugin).toLowerCase().includes(f))), [data, f, origin])
  const skill = data?.skills.find((s) => s.key === sel)
  const cur = loaded?.key === sel ? loaded : null
  const editing = draft?.key === sel ? draft : null
  useLeaveGuard(!!draft && !(draft.key === sel && draft.text === cur?.text))
  const groups = useMemo(() => {
    const g = new Map<string, { title: string; items: Skill[] }>()
    for (const s of skills) {
      const k = s.scope === "user" ? "u" : s.scope === "project" ? "p:" + s.project : s.scope === "store" ? "s" : "x:" + s.plugin
      const title = s.scope === "user" ? t("skills.skills.groupUser") : s.scope === "project" ? tilde(s.project ?? "", home) : s.scope === "store" ? t("skills.skills.groupStore") : t("skills.skills.groupPlugin", { name: s.plugin })
      if (!g.has(k)) g.set(k, { title, items: [] })
      g.get(k)!.items.push(s)
    }
    return [...g.values()]
  }, [skills, home, t])
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
  const remove = (skill: Skill | undefined = data?.skills.find((s) => s.key === sel)) =>
    skill &&
    !skill.readOnly &&
    onAsk({
      title: t("skills.skills.deleteTitle", { name: skill.name }),
      body: t("skills.skills.deleteBody"),
      confirm: t("common.delete"),
      run: async () => {
        const r = await api.post("/skill-rm", { key: skill.key })
        ;(r.ok ? toast.success : toast.error)(r.out)
        if (r.ok) {
          if (skill.key === sel) onPick("")
          reload()
        }
      },
    })
  const copyTo = async (to: string, key = sel) => {
    const r = (await api.post("/skill-copy", { key, to })) as { ok: boolean; out: string; key?: string }
    ;(r.ok ? toast.success : toast.error)(r.out)
    if (r.ok && r.key) {
      await reload()
      onPick(r.key)
    }
  }
  // Modifier depuis le menu : ouvre le skill, charge son texte et passe en édition.
  const edit = async (key: string) => {
    const r = await api.skill(key)
    if (r.error || r.text === undefined) return void toast.error(r.error ?? t("skills.skills.readFailed"))
    onPick(key)
    setDraft({ key, text: r.text })
  }
  const copyName = (s: Skill) => navigator.clipboard.writeText("/" + s.dir).then(() => toast.success(t("skills.skills.nameCopied", { dir: s.dir })), () => toast.error(t("skills.skills.copyFailed")))
  const [fm, body] = split(cur?.text ?? "")

  return (
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <ScrollArea className={cn("shrink-0 border-b bg-muted/30 lg:block lg:max-h-none lg:w-96 lg:flex-none lg:border-r lg:border-b-0", sel ? "hidden" : "min-h-0 flex-1")}>
        <nav className="p-2">
          <div className="mb-2 flex flex-wrap items-center gap-2 px-1">
            <Button size="xs" variant="outline" onClick={() => setCreating(true)}>
              <Plus /> {t("skills.skills.newSkill")}
            </Button>
            <Choose
              value={origin}
              onChange={setOrigin}
              options={[["all", t("skills.origin.all")], ...Object.keys(ORIGINS).map((k) => [k, t(`skills.origin.${k}.label`)] as const)]}
              label={t("skills.origin.label")}
              className="min-w-0"
            />
            {data && (
              <span className="basis-full text-2xs text-muted-foreground" title={t("skills.skills.budgetHint")}>
                {t("skills.skills.budget", { n: budget.toLocaleString(loc()) })}
              </span>
            )}
          </div>
          {!data && <Skeleton className="m-2 h-24" />}
          {groups.map((g) => (
            <section key={g.title} className="mb-3">
              <p className="flex items-center px-2 py-1 text-xs font-semibold text-muted-foreground">
                <span className="flex-1 truncate">{g.title}</span>
                <span className="font-normal tabular-nums">{g.items.length}</span>
              </p>
              <ul className="space-y-px">
                {g.items.map((s) => (
                  <li key={s.key}>
                    <ContextMenu>
                      <ContextMenuTrigger asChild>
                          <button
                            onClick={() => onPick(s.key)}
                            className={cn("flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors", s.key === sel ? "bg-accent text-accent-foreground" : "hover:bg-muted")}
                          >
                            <span className="flex items-center gap-1.5">
                              <Zap className="size-3.5 shrink-0 text-warning" />
                              <span className="min-w-0 flex-1 truncate font-medium">{s.name}</span>
                        <OriginBadge s={s} />
                              {s.manual && <Hand className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("skills.skills.manualOnly")} />}
                              {s.hidden && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("skills.skills.hiddenInMenu")} />}
                              {s.warn && <AlertTriangle className="size-3.5 shrink-0 text-warning" aria-label={t("skills.skills.toCheck")} />}
                            </span>
                            <span className="line-clamp-2 text-xs text-muted-foreground">{s.description || t("skills.skills.noDescription")}</span>
                          </button>
                      </ContextMenuTrigger>
                      <ContextMenuContent className="w-60">
                        <ContextMenuLabel className="truncate">{s.name}</ContextMenuLabel>
                        <ContextMenuItem onSelect={() => onPick(s.key)}>
                          <BookOpen /> {t("skills.skills.open")}
                        </ContextMenuItem>
                        {!s.readOnly && (
                          <>
                            <ContextMenuItem onSelect={() => edit(s.key)}>
                              <Pencil /> {t("skills.skills.edit")}
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => (onPick(s.key), setWriter(s.key))}>
                              <Sparkles /> {t("skills.skills.writeWithModel")}
                            </ContextMenuItem>
                          </>
                        )}
                        <ContextMenuItem onSelect={() => copyName(s)}>
                          <ClipboardCopy /> {t("skills.skills.copyName", { dir: s.dir })}
                        </ContextMenuItem>
                        <ContextMenuSub>
                          <ContextMenuSubTrigger>
                            <FolderInput /> {t("skills.skills.copyTo")}
                          </ContextMenuSubTrigger>
                          <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
                            {s.scope !== "user" && (
                              <>
                                <ContextMenuItem onSelect={() => copyTo("u", s.key)}>{t("skills.skills.personal")}</ContextMenuItem>
                                <ContextMenuSeparator />
                              </>
                            )}
                            {(data?.projects ?? []).map((p) => (
                              <ContextMenuItem key={p.dir} onSelect={() => copyTo("p/" + p.dir, s.key)}>
                                <span className="truncate">{tilde(p.project, home)}</span>
                              </ContextMenuItem>
                            ))}
                          </ContextMenuSubContent>
                        </ContextMenuSub>
                        {!s.readOnly && (
                          <>
                            <ContextMenuSeparator />
                            <ContextMenuItem variant="destructive" onSelect={() => remove(s)}>
                              <Trash2 /> {t("common.delete")}
                            </ContextMenuItem>
                          </>
                        )}
                      </ContextMenuContent>
                    </ContextMenu>
                  </li>
                ))}
              </ul>
            </section>
          ))}
          {data && skills.length === 0 && <p className="px-2 py-6 text-center text-sm text-muted-foreground">{q ? t("skills.skills.emptySearch") : t("skills.skills.empty")}</p>}
        </nav>
      </ScrollArea>

      <div className={cn("min-h-0 min-w-0 flex-1 flex-col lg:flex", sel ? "flex" : "hidden")}>
        {sel && (
          <div className="border-b px-2 py-1.5 lg:hidden">
            <Button variant="ghost" size="sm" onClick={() => onPick("")}>
              <ArrowLeft /> {t("skills.skills.list")}
            </Button>
          </div>
        )}

        {!sel ? (
          <p className="mx-auto max-w-md px-6 py-20 text-center text-sm text-muted-foreground">{t("skills.skills.pick")}</p>
        ) : !skill ? (
          <p className="px-6 py-20 text-center text-sm text-muted-foreground">{t("skills.skills.notFound")}</p>
        ) : (
          <>
            <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b bg-muted/40 px-6 py-1.5">
              <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={skill.path}>
                /{skill.dir} · {skill.scope === "plugin" ? t("skills.skills.scopePlugin", { name: skill.plugin }) : skill.scope === "store" ? t("skills.skills.scopeStore") : skill.scope === "project" ? tilde(skill.project ?? "", home) : t("skills.skills.scopeUser")} · {t("skills.skills.files", { count: skill.files })} · {ago(skill.mtime)}
              </code>
              {editing ? (
                <>
                  <Button variant="ghost" size="sm" onClick={() => setDraft(null)}>
                    <X /> {t("common.cancel")}
                  </Button>
                  <Button size="sm" onClick={save} disabled={editing.text === cur?.text}>
                    <Save /> {t("common.save")}
                  </Button>
                </>
              ) : (
                <>
                  <DropdownMenu>
                    <DropdownMenuTrigger asChild>
                      <Button variant="ghost" size="sm">
                        <Copy /> {t("skills.skills.copyToEllipsis")}
                      </Button>
                    </DropdownMenuTrigger>
                    <DropdownMenuContent align="end" className="max-h-80 w-72 overflow-y-auto">
                      {skill.scope !== "user" && <DropdownMenuItem onSelect={() => copyTo("u")}>{t("skills.skills.personal")}</DropdownMenuItem>}
                      {skill.scope !== "user" && <DropdownMenuSeparator />}
                      <DropdownMenuLabel>{t("skills.skills.projectLabel")}</DropdownMenuLabel>
                      {(data?.projects ?? []).map((p) => (
                        <DropdownMenuItem key={p.dir} onSelect={() => copyTo("p/" + p.dir)}>
                          <span className="truncate">{tilde(p.project, home)}</span>
                        </DropdownMenuItem>
                      ))}
                    </DropdownMenuContent>
                  </DropdownMenu>
                  {!skill.readOnly && (
                    <>
                      <Button variant="ghost" size="sm" className="text-destructive hover:text-destructive" onClick={() => remove()}>
                        <Trash2 /> {t("common.delete")}
                      </Button>
                      <Button variant="ghost" size="sm" onClick={() => setWriter(sel)}>
                        <Bot /> {t("skills.skills.improve")}
                      </Button>
                      <Button size="sm" onClick={() => cur && setDraft({ key: sel, text: cur.text })} disabled={!cur}>
                        <Pencil /> {t("skills.skills.edit")}
                      </Button>
                    </>
                  )}
                </>
              )}
            </div>
            <p className="flex items-center gap-2 border-b px-6 py-1.5 text-xs text-muted-foreground">
              <OriginBadge s={skill} /> {skill.source || t(`skills.origin.${skill.origin in ORIGINS ? skill.origin : "custom"}.hint`)}
              {skill.readOnly && t("skills.skills.readOnlyHint")}
            </p>
            {skill.warn && !editing && (
              <p className="flex items-start gap-2 border-b bg-warning/10 px-6 py-2 text-xs">
                <AlertTriangle className="mt-0.5 size-3.5 shrink-0 text-warning" /> {skill.warn}
              </p>
            )}
            {editing ? (
              <textarea
                value={editing.text}
                onChange={(e) => setDraft({ key: sel, text: e.target.value })}
                onKeyDown={(e) => (e.ctrlKey || e.metaKey) && e.key === "s" && (e.preventDefault(), save())}
                spellCheck={false}
                autoFocus
                aria-label={t("skills.skills.editorLabel")}
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
                        {skill.manual && <span>{t("skills.skills.manualDetail")}</span>}
                        {skill.hidden && <span>{t("skills.skills.hiddenDetail")}</span>}
                        {skill.tools && <span>{t("skills.skills.tools", { tools: skill.tools })}</span>}
                        <span>{t(skill.chars > 1536 ? "skills.skills.charsTruncated" : "skills.skills.chars", { chars: skill.chars })}</span>
                      </p>
                      {fm && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{fm}</pre>}
                      <Markdown t={body} />
                      {cur.files.length > 1 && (
                        <div className="mt-6 border-t pt-3">
                          <p className="mb-1 text-xs font-medium text-muted-foreground">{t("skills.skills.folderFiles")}</p>
                          <ul className="space-y-px text-xs">
                            {cur.files.map((x) => (
                              <li key={x.rel} className="flex items-center gap-1.5">
                                <FileText className="size-3 shrink-0 text-muted-foreground" />
                                <code>{x.rel}</code>
                                <span className="text-muted-foreground">{t("skills.skills.kb", { kb: x.kb })}</span>
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
      <SkillWriter open={!!writer} skillKey={writer} name={data?.skills.find((s) => s.key === writer)?.name ?? ""} onClose={() => setWriter("")} onDraft={(text) => setDraft({ key: writer, text })} />
      <NewSkill open={creating} projects={data?.projects ?? []} home={home} onClose={() => setCreating(false)} onCreated={async (key) => (await reload(), onPick(key))} />
    </div>
  )
}

// Nouveau skill : un nom (minuscules, chiffres, tirets), un emplacement ; le SKILL.md part d'un modèle à compléter.
function NewSkill({ open, projects, home, onClose, onCreated }: { open: boolean; projects: { dir: string; project: string }[]; home: string; onClose: () => void; onCreated: (key: string) => void }) {
  const { t } = useTranslation()
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
          <DialogTitle>{t("skills.newSkill.title")}</DialogTitle>
          <DialogDescription>{t("skills.newSkill.description")}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-3"
          onSubmit={(e) => {
            e.preventDefault()
            if (ok) create()
          }}
        >
          <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder={t("skills.newSkill.placeholder")} autoFocus aria-label={t("skills.newSkill.nameLabel")} className="font-mono" />
          {name && !ok && <p className="text-xs text-destructive">{t("skills.newSkill.invalid")}</p>}
          <Choose value={where} onChange={setWhere} options={[["u", t("skills.skills.personal")], ...projects.map((p) => [p.dir, t("skills.newSkill.project", { path: tilde(p.project, home) })] as const)]} label={t("skills.newSkill.location")} />
          <DialogFooter>
            <Button variant="ghost" type="button" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!ok}>
              {t("skills.newSkill.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
