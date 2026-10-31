import { useEffect, useState } from "react"
import { ChevronRight, Copy, FolderPlus, Globe, Trash2, X } from "lucide-react"
import { Link, useSearchParams } from "wouter"
import { api, type ProjectCfg } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { href } from "@/lib/route"
import { tilde } from "@/lib/format"
import { cn, copyText } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { byRule } from "./ConfigTabs2"

const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const
const SCROLL_TO_TARGET_DELAY_MS = 50

// Les permissions et les hooks ont leur propre section.
const isPlainSetting = (settingRow: { path: string }) => !settingRow.path.startsWith("permissions.") && settingRow.path !== "hooks"

const Badge = ({ count, label }: { count: number; label: string }) =>
  count > 0 ? (
    <span className="rounded-full bg-muted px-2 py-0.5 text-2xs text-muted-foreground">
      {count} {label}
    </span>
  ) : null

// Ce que chaque projet configure en propre, en plus des réglages de l'utilisateur : réglages (projet et local), règles de
// permission, hooks, serveurs MCP, règles .claude/rules, skills et documentation. onEdit ouvre l'onglet qui permet de les modifier.
export function ProjectsTab({ home, version, gone, onAddDir, suggest, onEdit, onTransfer, onRemove }: { home: string; version: number; onAddDir: (file: string, project: string, dir?: string) => void; suggest: (project: string, taken: string[]) => string[]; gone: (file: string, kind: string, rule: string) => boolean; onEdit: (file: string, tab: string) => void; onRemove: (file: string, kind: string, rule: string) => void; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void }) {
  const { t } = useTranslation()
  const [list, setList] = useState<ProjectCfg[] | null>(null)
  const [all, setAll] = useState(false)
  const [query, setQuery] = useState("")
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [params] = useSearchParams()
  const focus = params.get("projet") // arrivée depuis la page d'un dossier : ce projet s'ouvre et s'affiche même sans réglage propre
  useEffect(() => {
    api.configProjects().then((response) => setList(response.projects))
  }, [version])
  const target = list?.find((projectConfig) => projectConfig.project === focus)?.dir
  useEffect(() => {
    if (!target) return
    setTimeout(() => document.getElementById("cfg-" + target)?.scrollIntoView({ block: "start" }), SCROLL_TO_TARGET_DELAY_MS)
  }, [target])
  if (!list) return <p className="py-8 text-sm text-muted-foreground">{t("common.loading")}</p>
  const lowerQuery = query.toLowerCase()
  const shown = list.filter((projectConfig) => {
    const isVisible = all || projectConfig.dir === target || projectConfig.specific > 0
    const matchesQuery = !lowerQuery || projectConfig.project.toLowerCase().includes(lowerQuery)
    return isVisible && matchesQuery
  })
  const hidden = list.filter((projectConfig) => projectConfig.specific === 0).length
  const toggle = (projectDir: string) =>
    setOpen((previouslyOpen) => {
      const nextOpen = new Set(previouslyOpen)
      if (nextOpen.has(projectDir)) nextOpen.delete(projectDir)
      else nextOpen.add(projectDir)
      return nextOpen
    })
  const copyRule = (rule: string) =>
    copyText(rule).then(
      () => toast.success(t("config.shared.ruleCopied")),
      () => toast.error(t("config.shared.copyFailed")),
    )
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input value={query} onChange={(changeEvent) => setQuery(changeEvent.target.value)} placeholder={t("config.projects.filterPlaceholder")} className="h-8 w-64" aria-label={t("config.projects.filterAria")} />
        {hidden > 0 && (
          <Button size="sm" variant={all ? "secondary" : "ghost"} onClick={() => setAll(!all)} aria-pressed={all}>
            {t(all ? "config.projects.hideHidden" : "config.projects.showHidden", { count: hidden })}
          </Button>
        )}
        <span className="text-xs text-muted-foreground">{t("config.projects.intro")}</span>
      </div>
      <ul className="space-y-2">
        {shown.map((projectConfig) => {
          const isOpen = open.has(projectConfig.dir) !== (projectConfig.dir === target) // le projet visé s'ouvre de lui-même ; un clic le referme
          const perms = Object.fromEntries(
            Object.entries(projectConfig.perms).map(([kind, kindRules]) => {
              const remainingRules = kindRules.filter((permRule) => !gone(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule))
              return [kind, remainingRules.sort((leftRule, rightRule) => byRule(leftRule.rule, rightRule.rule))]
            }),
          )
          const nRules = Object.values(perms).reduce((total, kindRules) => total + kindRules.length, 0)
          const nSettings = projectConfig.files.reduce((total, settingsFile) => total + settingsFile.rows.filter(isPlainSetting).length, 0)
          return (
            <li key={projectConfig.dir} id={"cfg-" + projectConfig.dir} className="scroll-mt-4 rounded-lg border">
              <button onClick={() => toggle(projectConfig.dir)} className="flex w-full flex-wrap items-center gap-2 px-3 py-2 text-left" aria-expanded={isOpen}>
                <ChevronRight className={cn("size-4 shrink-0 transition-transform", isOpen && "rotate-90")} />
                <span className="min-w-0 flex-1 truncate text-sm font-medium" title={projectConfig.project}>
                  {tilde(projectConfig.project, home)}
                </span>
                <Badge count={nRules} label={t("config.projects.badgePerms")} />
                <Badge count={nSettings} label={t("config.projects.badgeSettings")} />
                <Badge count={projectConfig.hooks.length} label="hooks" />
                <Badge count={projectConfig.mcp.length} label="MCP" />
                <Badge count={projectConfig.rules.length} label={t("config.projects.badgeRules")} />
                <Badge count={projectConfig.skills.length} label="skills" />
                {projectConfig.specific === 0 && <span className="text-xs text-muted-foreground">{t("config.projects.nothingSpecific")}</span>}
              </button>
              {isOpen && (
                <div className="space-y-4 border-t px-4 py-3 text-sm">
                  <section>
                    <div className="mb-1 flex items-center gap-2">
                      <h3 className="text-xs font-medium text-muted-foreground">{t("config.projects.permsTitle")}</h3>
                      <Button size="xs" variant="ghost" onClick={() => onEdit(projectConfig.files.find((settingsFile) => settingsFile.id[0] === "p")?.id ?? "p/" + projectConfig.dir, "permissions")}>
                        {t("config.projects.manage")}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button size="xs" variant="ghost" title={t("config.projects.addDirTitle")}>
                            <FolderPlus /> {t("config.projects.addDir")}
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start" className="w-72">
                          {suggest(projectConfig.project, (projectConfig.perms.additionalDirectories ?? []).map((dirRule) => dirRule.rule)).map((suggestedDir) => (
                            <DropdownMenuItem key={suggestedDir} onSelect={() => onAddDir("l/" + projectConfig.dir, projectConfig.project, suggestedDir)}>
                              <FolderPlus /> <span className="truncate">{tilde(suggestedDir, home)}</span>
                            </DropdownMenuItem>
                          ))}
                          <DropdownMenuItem onSelect={() => onAddDir("l/" + projectConfig.dir, projectConfig.project)}>{t("config.projects.chooseDir")}</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                    {nRules === 0 ? (
                      <p className="text-xs text-muted-foreground">{t("config.projects.noPerms")}</p>
                    ) : (
                      KINDS.filter((kind) => perms[kind]?.length).map((kind) => (
                        <div key={kind} className="mb-2">
                          <p className="text-xs text-muted-foreground">{t(`config.perms.kind.${kind}`)}</p>
                          <ul className="space-y-px">
                            {perms[kind].map((permRule, ruleIndex) => (
                              <ContextMenu key={kind + ruleIndex}>
                              <ContextMenuTrigger asChild>
                              <li className="flex items-center gap-2">
                                <code className="min-w-0 flex-1 text-xs break-all">{permRule.rule}</code>
                                <span className="shrink-0 text-2xs text-muted-foreground">{permRule.scope === "p" ? t("config.scope.project") : t("config.scope.local")}</span>
                                <DropdownMenu>
                                  <DropdownMenuTrigger asChild>
                                    <Button size="xs" variant="ghost">
                                      <Globe /> {t("config.projects.toGlobal")}
                                    </Button>
                                  </DropdownMenuTrigger>
                                  <DropdownMenuContent align="end">
                                    <DropdownMenuItem onSelect={() => onTransfer(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule, false)}>{t("config.projects.copyToUser")}</DropdownMenuItem>
                                    <DropdownMenuItem onSelect={() => onTransfer(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule, true)}>{t("config.projects.moveFromProject")}</DropdownMenuItem>
                                  </DropdownMenuContent>
                                </DropdownMenu>
                                <Button size="icon-xs" variant="ghost" aria-label={t("config.projects.deleteRule", { rule: permRule.rule })} title={t("config.projects.deleteRuleTitle")} onClick={() => onRemove(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule)}>
                                  <X />
                                </Button>
                              </li>
                              </ContextMenuTrigger>
                              <ContextMenuContent className="w-60">
                                <ContextMenuItem onSelect={() => copyRule(permRule.rule)}>
                                  <Copy /> {t("config.perms.copyRule")}
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule, false)}>
                                  <Globe /> {t("config.perms.copyGlobal")}
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule, true)}>
                                  <Globe /> {t("config.perms.moveGlobal")}
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem variant="destructive" onSelect={() => onRemove(`${permRule.scope}/${projectConfig.dir}`, kind, permRule.rule)}>
                                  <Trash2 /> {t("common.delete")}
                                </ContextMenuItem>
                              </ContextMenuContent>
                              </ContextMenu>
                            ))}
                          </ul>
                        </div>
                      ))
                    )}
                  </section>
                  {projectConfig.files.map((settingsFile) => {
                    const rows = settingsFile.rows.filter(isPlainSetting)
                    return (
                      <section key={settingsFile.id}>
                        <div className="mb-1 flex items-center gap-2">
                          <h3 className="text-xs font-medium text-muted-foreground">
                            {t("config.projects.settingsHeading")} <code>{settingsFile.label}</code> {settingsFile.id[0] === "p" ? t("config.projects.sharedSuffix") : t("config.projects.privateSuffix")}
                          </h3>
                          <Button size="xs" variant="ghost" onClick={() => onEdit(settingsFile.id, "json")}>
                            JSON…
                          </Button>
                        </div>
                        {rows.length === 0 ? (
                          <p className="text-xs text-muted-foreground">{t("config.projects.onlyPerms")}</p>
                        ) : (
                          <ul className="space-y-1">
                            {rows.map((settingRow) => (
                              <li key={settingRow.path} className="text-xs">
                                <code className="font-medium">{settingRow.path}</code> <span className="text-muted-foreground">=</span> <span className="break-all">{settingRow.value}</span>
                                {describeKey(settingRow.path) && <span className="block text-2xs text-muted-foreground">{describeKey(settingRow.path)}</span>}
                              </li>
                            ))}
                          </ul>
                        )}
                      </section>
                    )
                  })}
                  {projectConfig.files.length === 0 && (
                    <p className="text-xs text-muted-foreground">
                      {t("config.projects.noFile")}{" "}
                      <button className="text-primary underline underline-offset-2 hover:decoration-2" onClick={() => onEdit("l/" + projectConfig.dir, "reglages")}>
                        {t("config.projects.createLocal")}
                      </button>
                    </p>
                  )}
                  {projectConfig.hooks.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">{t("config.projects.hooksTitle")}</h3>
                      <ul className="space-y-1">
                        {projectConfig.hooks.map((hook, hookIndex) => (
                          <li key={hookIndex} className="text-xs">
                            <span className="font-medium">{hook.event}</span> {hook.matcher && <code className="rounded-sm bg-muted px-1">{hook.matcher}</code>}{" "}
                            <code className="break-all text-muted-foreground">{hook.command}</code>
                            {hook.missing && <span className="ml-1 text-warning">{t("config.hooks.missing", { path: hook.missing })}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {projectConfig.mcp.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">{t("config.projects.mcpTitle")}</h3>
                      <ul className="space-y-1">
                        {projectConfig.mcp.map((mcpServer) => (
                          <li key={mcpServer.name + mcpServer.scope} className="text-xs">
                            <span className="font-medium">{mcpServer.name}</span> <span className="text-muted-foreground">{mcpServer.type} · {mcpServer.scope === "project" ? ".mcp.json" : t("config.scope.local")}</span>{" "}
                            <code className="break-all text-muted-foreground">{mcpServer.url || mcpServer.command}</code>
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {projectConfig.rules.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">
                        {t("config.projects.rulesHeading")} <code>.claude/rules</code> ·{" "}
                        <Link className="underline" href={href({ kind: "memory", key: `r/${projectConfig.dir}/${projectConfig.rules[0].name}` })}>
                          {t("config.projects.rulesManage")}
                        </Link>
                      </h3>
                      <ul className="space-y-1">
                        {projectConfig.rules.map((projectRule) => (
                          <li key={projectRule.name} className="text-xs">
                            <Link className="font-medium underline-offset-2 hover:underline" href={href({ kind: "memory", key: `r/${projectConfig.dir}/${projectRule.name}` })}>
                              <code>{projectRule.name}</code>
                            </Link>{" "}
                            <span className="text-muted-foreground">{projectRule.paths?.length ? t("config.projects.ruleOnlyFor", { paths: projectRule.paths.join(", ") }) : t("config.projects.ruleAlways")}</span>
                            {projectRule.warn && <span className="ml-1 text-warning">{projectRule.warn}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {(projectConfig.skills.length > 0 || projectConfig.docs.length > 0) && (
                    <p className="text-xs text-muted-foreground">
                      {projectConfig.skills.length > 0 && <>{t("config.projects.skillsLine", { list: projectConfig.skills.map((skillName) => "/" + skillName).join(", ") })} </>}
                      {projectConfig.docs.length > 0 && <>{t("config.projects.docsLine", { list: projectConfig.docs.join(", ") })}</>}
                    </p>
                  )}
                </div>
              )}
            </li>
          )
        })}
        {shown.length === 0 && <li className="py-6 text-center text-sm text-muted-foreground">{query ? t("config.projects.noMatch") : t("config.projects.noOwn")}</li>}
      </ul>
    </div>
  )
}
