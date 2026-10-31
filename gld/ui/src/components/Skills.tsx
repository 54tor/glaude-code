import { loc } from "@/i18n"
import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useMemo, useState } from "react"
import { AlertTriangle, ArrowLeft, BookOpen, Bot, Copy, EyeOff, FileText, Hand, Package, Pencil, Plug, Plus, RefreshCw, Save, Sparkles, Trash2, X, Zap } from "lucide-react"
import { toast } from "sonner"
import { api, type Skill, type SkillFile } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import { cn, copyText } from "@/lib/utils"
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
import { PluginsDialog } from "./PluginsDialog"
import { SkillsNpxDialog, npxAct, npxCommand } from "./SkillsNpxDialog"
import { SkillWriter } from "./SkillWriter"
import { split } from "./memoryLib"
import { useEditorDraft, useLeaveGuard } from "@/hooks/useEditorDraft"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"

// Origine d'un skill : badge court et explication.
const ORIGINS = {
  store: "bg-info/10 text-info",
  plugin: "bg-info/10 text-info",
  install: "bg-success/10 text-success",
  custom: "bg-muted text-muted-foreground",
} as const

function OriginBadge({ skill }: { skill: Skill }) {
  const { t } = useTranslation()
  const origin = skill.origin in ORIGINS ? skill.origin : "custom"
  const what = t(`skills.origin.${origin}.hint`)
  return (
    <span className={cn("shrink-0 rounded-sm px-1.5 py-px text-2xs font-medium", ORIGINS[origin])} title={skill.source ? t("skills.origin.withSource", { what, source: skill.source }) : what}>
      {t(`skills.origin.${origin}.label`)}
    </span>
  )
}

const NAME_OK = /^[a-z0-9][a-z0-9-]{0,63}$/
const DESCRIPTION_CHAR_LIMIT = 1536

// Skills de Claude Code : personnels (~/.claude/skills), de projet (.claude/skills) et de plugins (lecture seule).
// Lecture, modification de SKILL.md, création, copie vers un autre emplacement, suppression (copie à la corbeille), rédaction par un modèle.
export function Skills({ sel, home, q, onPick, onAsk }: { sel: string; home: string; q: string; onPick: (key: string) => void; onAsk: (ask: Ask) => void }) {
  const { t } = useTranslation()
  const [data, setData] = useState<{ skills: Skill[]; projects: { dir: string; project: string }[] } | null>(null)
  const [loaded, setLoaded] = useState<{ key: string; text: string; files: SkillFile[]; error?: string } | null>(null)
  const [draft, setDraft] = useEditorDraft("skill")
  const [creating, setCreating] = useState(false)
  const [plugins, setPlugins] = useState(false)
  const [npx, setNpx] = useState(false)
  const [updates, setUpdates] = useState<Record<string, string>>({}) // clé du skill → update | current | unknown
  const [origin, setOrigin] = useState("all") // filtre sur l'origine
  const [writer, setWriter] = useState("") // clé du skill que le modèle rédige ; vide = fermé
  const reload = useCallback(() => api.skills().then(setData, () => {}), [])
  useEffect(() => {
    reload()
  }, [reload])
  const check = useCallback(
    (force: boolean) =>
      api
        .skillsUpdates(force)
        .then((updatesResponse) => {
          setUpdates(updatesResponse.states ?? {})
          if (force) toast.success(t("skills.npx.checked"))
        })
        .catch(() => {}),
    [t],
  )
  useEffect(() => {
    check(false)
  }, [check])
  useEffect(() => {
    if (!sel) return
    let isMounted = true
    api.skill(sel).then(
      (skillResponse) => isMounted && setLoaded({ key: sel, text: skillResponse.text, files: skillResponse.files ?? [] }),
      (error) => isMounted && setLoaded({ key: sel, text: "", files: [], error: errMsg(error) })
    )
    return () => {
      isMounted = false
    }
  }, [sel, data])

  const normalizedQuery = q.toLowerCase()
  const skills = useMemo(
    () =>
      (data?.skills ?? []).filter((candidateSkill) => {
        const matchesOrigin = origin === "all" || candidateSkill.origin === origin
        const searchableText = (candidateSkill.name + " " + candidateSkill.description + " " + candidateSkill.project + " " + candidateSkill.plugin).toLowerCase()
        const matchesQuery = !normalizedQuery || searchableText.includes(normalizedQuery)
        return matchesOrigin && matchesQuery
      }),
    [data, normalizedQuery, origin]
  )
  const skill = data?.skills.find((candidateSkill) => candidateSkill.key === sel)
  const cur = loaded?.key === sel ? loaded : null
  const editing = draft?.key === sel ? draft : null
  useLeaveGuard(!!draft && !(draft.key === sel && draft.text === cur?.text))
  const groups = useMemo(() => {
    const groupKeyOf = (groupedSkill: Skill) => {
      if (groupedSkill.scope === "user") return "u"
      if (groupedSkill.scope === "project") return "p:" + groupedSkill.project
      if (groupedSkill.scope === "store") return "s"
      return "x:" + groupedSkill.plugin
    }
    const groupTitleOf = (groupedSkill: Skill) => {
      if (groupedSkill.scope === "user") return t("skills.skills.groupUser")
      if (groupedSkill.scope === "project") return tilde(groupedSkill.project ?? "", home)
      if (groupedSkill.scope === "store") return t("skills.skills.groupStore")
      return t("skills.skills.groupPlugin", { name: groupedSkill.plugin })
    }
    const groupsByKey = new Map<string, { title: string; items: Skill[] }>()
    for (const groupedSkill of skills) {
      const groupKey = groupKeyOf(groupedSkill)
      if (!groupsByKey.has(groupKey)) groupsByKey.set(groupKey, { title: groupTitleOf(groupedSkill), items: [] })
      groupsByKey.get(groupKey)!.items.push(groupedSkill)
    }
    return [...groupsByKey.values()]
  }, [skills, home, t])
  const budget = (data?.skills ?? [])
    .filter((candidateSkill) => !candidateSkill.manual)
    .reduce((tokenTotal, candidateSkill) => tokenTotal + candidateSkill.descTokens, 0)

  const save = async () => {
    if (!editing) return
    if (await reportAction(() => api.skillSave(sel, editing.text, false))) {
      setDraft(null)
      reload()
    }
  }
  const remove = (targetSkill: Skill | undefined = data?.skills.find((candidateSkill) => candidateSkill.key === sel)) =>
    targetSkill &&
    !targetSkill.readOnly &&
    onAsk({
      title: t("skills.skills.deleteTitle", { name: targetSkill.name }),
      body: t("skills.skills.deleteBody"),
      confirm: t("common.delete"),
      run: async () => {
        if (await reportAction(() => api.skillRemove([targetSkill.key]))) {
          if (targetSkill.key === sel) onPick("")
          reload()
        }
      },
    })
  const copyTo = async (to: string, key = sel) => {
    let copiedKey = ""
    const ok = await reportAction(async () => {
      const copyResponse = await api.skillCopy(key, to)
      copiedKey = copyResponse.key
      return copyResponse
    })
    if (ok) {
      await reload()
      onPick(copiedKey)
    }
  }
  // Modifier depuis le menu : ouvre le skill, charge son texte et passe en édition.
  const edit = async (key: string) => {
    const skillResponse = await api.skill(key).catch((error) => void toast.error(errMsg(error)))
    if (!skillResponse) return
    onPick(key)
    setDraft({ key, text: skillResponse.text })
  }
  const npxAsk = (action: "update" | "remove", targetSkill: Skill) => {
    const scope = targetSkill.scope === "user" ? "user" : "p/" + targetSkill.key.split("/")[1]
    onAsk({
      title: t(`skills.npx.${action}Title`, { name: targetSkill.name }),
      body: t("skills.npx.actBody", { cmd: npxCommand(action, "", targetSkill.dir, targetSkill.scope === "user") }),
      confirm: t(`skills.npx.${action}`),
      run: async () => {
        if (await npxAct(action, "", targetSkill.dir, scope)) {
          if (action === "remove" && targetSkill.key === sel) onPick("")
          reload()
          check(true)
        }
      },
    })
  }
  const copyName = (targetSkill: Skill) =>
    copyText("/" + targetSkill.dir).then(
      () => toast.success(t("skills.skills.nameCopied", { dir: targetSkill.dir })),
      () => toast.error(t("skills.skills.copyFailed"))
    )
  const [frontMatter, body] = split(cur?.text ?? "")
  const scopeLabelOf = (shownSkill: Skill) => {
    if (shownSkill.scope === "plugin") return t("skills.skills.scopePlugin", { name: shownSkill.plugin })
    if (shownSkill.scope === "store") return t("skills.skills.scopeStore")
    if (shownSkill.scope === "project") return tilde(shownSkill.project ?? "", home)
    return t("skills.skills.scopeUser")
  }
  const openAndWrite = (skillKey: string) => {
    onPick(skillKey)
    setWriter(skillKey)
  }
  const saveOnCtrlS = (keyboardEvent: React.KeyboardEvent<HTMLTextAreaElement>) => {
    const isSaveShortcut = (keyboardEvent.ctrlKey || keyboardEvent.metaKey) && keyboardEvent.key === "s"
    if (!isSaveShortcut) return
    keyboardEvent.preventDefault()
    save()
  }

  return (
    <div className="flex min-h-0 flex-1 flex-col lg:flex-row">
      <ScrollArea className={cn("shrink-0 border-b bg-muted/30 lg:block lg:max-h-none lg:w-96 lg:flex-none lg:border-r lg:border-b-0", sel ? "hidden" : "min-h-0 flex-1")}>
        <nav className="p-2">
          <div className="mb-2 flex flex-wrap items-center gap-2 px-1">
            <Button size="xs" variant="outline" onClick={() => setCreating(true)}>
              <Plus /> {t("skills.skills.newSkill")}
            </Button>
            <Button size="xs" variant="outline" onClick={() => setPlugins(true)}>
              <Plug /> {t("skills.plugins.button")}
            </Button>
            <Button size="xs" variant="outline" onClick={() => setNpx(true)}>
              <Package /> {t("skills.npx.button")}
            </Button>
            <Button size="xs" variant="ghost" onClick={() => check(true)} title={t("skills.npx.check")} aria-label={t("skills.npx.check")}>
              <RefreshCw />
            </Button>
            <Choose
              value={origin}
              onChange={setOrigin}
              options={[["all", t("skills.origin.all")], ...Object.keys(ORIGINS).map((originKey) => [originKey, t(`skills.origin.${originKey}.label`)] as const)]}
              label={t("skills.origin.label")}
              size="xs"
              className="min-w-0"
            />
            {data && (
              <span className="basis-full text-2xs text-muted-foreground" title={t("skills.skills.budgetHint")}>
                {t("skills.skills.budget", { n: budget.toLocaleString(loc()) })}
              </span>
            )}
          </div>
          {!data && <Skeleton className="m-2 h-24" />}
          {groups.map((skillGroup) => (
            <section key={skillGroup.title} className="mb-3">
              <h2 className="flex items-center px-2 py-1 text-xs font-semibold text-muted-foreground">
                <span className="flex-1 truncate">{skillGroup.title}</span>
                <span className="font-normal tabular-nums">{skillGroup.items.length}</span>
              </h2>
              <ul className="space-y-px">
                {skillGroup.items.map((listedSkill) => (
                  <li key={listedSkill.key}>
                    <ContextMenu>
                      <ContextMenuTrigger asChild>
                          <button
                            onClick={() => onPick(listedSkill.key)}
                            className={cn("flex w-full flex-col gap-0.5 rounded-md px-2 py-1.5 text-left text-sm transition-colors", listedSkill.key === sel ? "bg-accent text-accent-foreground" : "hover:bg-muted")}
                          >
                            <span className="flex items-center gap-1.5">
                              <Zap className="size-3.5 shrink-0 text-warning" />
                              <span className="min-w-0 flex-1 truncate font-medium">{listedSkill.name}</span>
                        <OriginBadge skill={listedSkill} />
                              {updates[listedSkill.key] === "update" && <span className="shrink-0 rounded-sm bg-warning/10 px-1.5 py-px text-2xs font-medium text-warning">{t("skills.npx.updateBadge")}</span>}
                              {listedSkill.manual && <Hand className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("skills.skills.manualOnly")} />}
                              {listedSkill.hidden && <EyeOff className="size-3.5 shrink-0 text-muted-foreground" aria-label={t("skills.skills.hiddenInMenu")} />}
                              {listedSkill.warn && <AlertTriangle className="size-3.5 shrink-0 text-warning" aria-label={t("skills.skills.toCheck")} />}
                            </span>
                            <span className="line-clamp-2 text-xs text-muted-foreground">{listedSkill.description || t("skills.skills.noDescription")}</span>
                          </button>
                      </ContextMenuTrigger>
                      <ContextMenuContent className="w-60">
                        <ContextMenuLabel className="truncate">{listedSkill.name}</ContextMenuLabel>
                        <ContextMenuItem onSelect={() => onPick(listedSkill.key)}>
                          <BookOpen /> {t("skills.skills.open")}
                        </ContextMenuItem>
                        <ContextMenuSeparator />
                        <ContextMenuItem onSelect={() => copyName(listedSkill)}>
                          <Copy /> {t("skills.skills.copyName", { dir: listedSkill.dir })}
                        </ContextMenuItem>
                        <ContextMenuSub>
                          <ContextMenuSubTrigger>
                            <Copy /> {t("skills.skills.copyTo")}
                          </ContextMenuSubTrigger>
                          <ContextMenuSubContent className="max-h-80 w-72 overflow-y-auto">
                            {listedSkill.scope !== "user" && (
                              <>
                                <ContextMenuItem onSelect={() => copyTo("u", listedSkill.key)}>{t("skills.skills.personal")}</ContextMenuItem>
                                <ContextMenuSeparator />
                              </>
                            )}
                            {(data?.projects ?? []).map((knownProject) => (
                              <ContextMenuItem key={knownProject.dir} onSelect={() => copyTo("p/" + knownProject.dir, listedSkill.key)}>
                                <span className="truncate">{tilde(knownProject.project, home)}</span>
                              </ContextMenuItem>
                            ))}
                          </ContextMenuSubContent>
                        </ContextMenuSub>
                        {!listedSkill.readOnly && (
                          <>
                            <ContextMenuSeparator />
                            <ContextMenuItem onSelect={() => edit(listedSkill.key)}>
                              <Pencil /> {t("skills.skills.edit")}
                            </ContextMenuItem>
                            <ContextMenuItem onSelect={() => openAndWrite(listedSkill.key)}>
                              <Sparkles /> {t("skills.skills.writeWithModel")}
                            </ContextMenuItem>
                            <ContextMenuSeparator />
                            <ContextMenuItem variant="destructive" onSelect={() => remove(listedSkill)}>
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

        {!sel && <p className="mx-auto max-w-md px-6 py-20 text-center text-sm text-muted-foreground">{t("skills.skills.pick")}</p>}
        {sel && !skill && <p className="px-6 py-20 text-center text-sm text-muted-foreground">{t("skills.skills.notFound")}</p>}
        {sel && skill && (
          <>
            <div className="flex flex-wrap items-center justify-end gap-x-2 gap-y-1 border-b bg-muted/40 px-6 py-1.5">
              <code className="min-w-40 flex-1 truncate text-xs text-muted-foreground" title={skill.path}>
                /{skill.dir} · {scopeLabelOf(skill)} · {t("skills.skills.files", { count: skill.files })} · {ago(skill.mtime)}
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
                      {(data?.projects ?? []).map((knownProject) => (
                        <DropdownMenuItem key={knownProject.dir} onSelect={() => copyTo("p/" + knownProject.dir)}>
                          <span className="truncate">{tilde(knownProject.project, home)}</span>
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
            {skill.lock && (
              <div className="flex flex-wrap items-center gap-2 border-b px-6 py-1.5 text-xs text-muted-foreground">
                <span className="min-w-40 flex-1">
                  {t("skills.npx.lockLine", {
                    source: skill.lock.source,
                    mode: skill.lock.link ? t("skills.npx.modeLink") : t("skills.npx.modeCopy"),
                    date: new Date(skill.lock.installedAt).toLocaleDateString(loc()),
                  })}
                </span>
                {updates[skill.key] === "update" && <span className="rounded-sm bg-warning/10 px-1.5 py-px text-2xs font-medium text-warning">{t("skills.npx.updateBadge")}</span>}
                {skill.lock.managed && (
                  <>
                    <Button size="xs" variant="outline" onClick={() => npxAsk("update", skill)}>
                      <RefreshCw /> {t("skills.npx.update")}
                    </Button>
                    <Button size="xs" variant="outline" onClick={() => npxAsk("remove", skill)}>
                      <Trash2 /> {t("skills.npx.remove")}
                    </Button>
                  </>
                )}
              </div>
            )}
            <p className="flex items-center gap-2 border-b px-6 py-1.5 text-xs text-muted-foreground">
              <OriginBadge skill={skill} /> {skill.source || t(`skills.origin.${skill.origin in ORIGINS ? skill.origin : "custom"}.hint`)}
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
                onChange={(changeEvent) => setDraft({ key: sel, text: changeEvent.target.value })}
                onKeyDown={saveOnCtrlS}
                spellCheck={false}
                autoFocus
                aria-label={t("skills.skills.editorLabel")}
                className="session-text min-h-0 flex-1 resize-none bg-background px-6 py-4 outline-none"
              />
            ) : (
              <ScrollArea className="min-h-0 flex-1">
                <div className="mx-auto max-w-page px-6 py-6 text-sm leading-relaxed">
                  {!cur && <Skeleton className="h-32 w-full" />}
                  {cur?.error && <p className="text-destructive">{cur.error}</p>}
                  {cur && !cur.error && (
                    <>
                      <p className="mb-3 flex flex-wrap gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        {skill.manual && <span>{t("skills.skills.manualDetail")}</span>}
                        {skill.hidden && <span>{t("skills.skills.hiddenDetail")}</span>}
                        {skill.tools && <span>{t("skills.skills.tools", { tools: skill.tools })}</span>}
                        <span>{t(skill.chars > DESCRIPTION_CHAR_LIMIT ? "skills.skills.charsTruncated" : "skills.skills.chars", { chars: skill.chars })}</span>
                        <span>{t("skills.skills.tokens", { desc: skill.descTokens, all: skill.tokens })}</span>
                      </p>
                      {frontMatter && <pre className="mb-4 rounded-lg border bg-muted/40 px-3 py-2 text-xs whitespace-pre-wrap text-muted-foreground">{frontMatter}</pre>}
                      <Markdown t={body} />
                      {cur.files.length > 1 && (
                        <div className="mt-6 border-t pt-3">
                          <p className="mb-1 text-xs font-medium text-muted-foreground">{t("skills.skills.folderFiles")}</p>
                          <ul className="space-y-px text-xs">
                            {cur.files.map((skillFile) => (
                              <li key={skillFile.rel} className="flex items-center gap-1.5">
                                <FileText className="size-3 shrink-0 text-muted-foreground" />
                                <code>{skillFile.rel}</code>
                                <span className="text-muted-foreground">{t("skills.skills.kb", { kb: skillFile.kb })}</span>
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
      <SkillWriter open={!!writer} skillKey={writer} name={data?.skills.find((candidateSkill) => candidateSkill.key === writer)?.name ?? ""} onClose={() => setWriter("")} onDraft={(text) => setDraft({ key: writer, text })} />
      <SkillsNpxDialog open={npx} projects={data?.projects ?? []} home={home} onClose={() => setNpx(false)} onChange={reload} />
      <PluginsDialog open={plugins} projects={data?.projects ?? []} home={home} onClose={() => setPlugins(false)} onChange={reload} />
      <NewSkill open={creating} projects={data?.projects ?? []} home={home} onClose={() => setCreating(false)} onCreated={async (key) => {
          await reload()
          onPick(key)
        }} />
    </div>
  )
}

// Nouveau skill : un nom (minuscules, chiffres, tirets), un emplacement ; le SKILL.md part d'un modèle à compléter.
function NewSkill({ open, projects, home, onClose, onCreated }: { open: boolean; projects: { dir: string; project: string }[]; home: string; onClose: () => void; onCreated: (key: string) => void }) {
  const { t } = useTranslation()
  const [name, setName] = useState("")
  const [where, setWhere] = useState("u")
  const isNameValid = NAME_OK.test(name)
  const create = async () => {
    const key = where === "u" ? `u/${name}` : `p/${where}/${name}`
    const text = `---\nname: ${name}\ndescription: \n---\n\n# ${name}\n\nInstructions du skill.\n`
    if (await reportAction(() => api.skillSave(key, text, true))) {
      setName("")
      onClose()
      onCreated(key)
    }
  }
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle>{t("skills.newSkill.title")}</DialogTitle>
          <DialogDescription>{t("skills.newSkill.description")}</DialogDescription>
        </DialogHeader>
        <form
          className="grid gap-3"
          onSubmit={(submitEvent) => {
            submitEvent.preventDefault()
            if (isNameValid) create()
          }}
        >
          <Input value={name} onChange={(changeEvent) => setName(changeEvent.target.value.toLowerCase())} placeholder={t("skills.newSkill.placeholder")} autoFocus aria-label={t("skills.newSkill.nameLabel")} className="font-mono" />
          {name && !isNameValid && <p className="text-xs text-destructive">{t("skills.newSkill.invalid")}</p>}
          <Choose value={where} onChange={setWhere} options={[["u", t("skills.skills.personal")], ...projects.map((knownProject) => [knownProject.dir, t("skills.newSkill.project", { path: tilde(knownProject.project, home) })] as const)]} label={t("skills.newSkill.location")} />
          <DialogFooter>
            <Button variant="ghost" type="button" onClick={onClose}>
              {t("common.cancel")}
            </Button>
            <Button type="submit" disabled={!isNameValid}>
              {t("skills.newSkill.create")}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  )
}
