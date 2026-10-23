import { useEffect, useState } from "react"
import { ChevronRight, ClipboardCopy, FolderPlus, Globe, X } from "lucide-react"
import { Link, useSearchParams } from "wouter"
import { api, type ProjectCfg } from "@/lib/api"
import { describeKey } from "@/lib/configKeys"
import { href } from "@/lib/route"
import { tilde } from "@/lib/format"
import { cn } from "@/lib/utils"
import { Button } from "@/components/ui/button"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from "@/components/ui/dropdown-menu"
import { Input } from "@/components/ui/input"
import { byRule } from "./ConfigTabs2"

const KINDS = ["allow", "ask", "deny", "additionalDirectories"] as const

const Badge = ({ n, label }: { n: number; label: string }) =>
  n > 0 ? (
    <span className="rounded-full bg-muted px-2 py-0.5 text-2xs text-muted-foreground">
      {n} {label}
    </span>
  ) : null

// Ce que chaque projet configure en propre, en plus des réglages de l'utilisateur : réglages (projet et local), règles de
// permission, hooks, serveurs MCP, règles .claude/rules, skills et documentation. onEdit ouvre l'onglet qui permet de les modifier.
export function ProjectsTab({ home, version, gone, onAddDir, suggest, onEdit, onTransfer, onRemove }: { home: string; version: number; onAddDir: (file: string, project: string, dir?: string) => void; suggest: (project: string, taken: string[]) => string[]; gone: (file: string, kind: string, rule: string) => boolean; onEdit: (file: string, tab: string) => void; onRemove: (file: string, kind: string, rule: string) => void; onTransfer: (file: string, kind: string, rule: string, move: boolean) => void }) {
  const { t } = useTranslation()
  const [list, setList] = useState<ProjectCfg[] | null>(null)
  const [all, setAll] = useState(false)
  const [q, setQ] = useState("")
  const [open, setOpen] = useState<Set<string>>(new Set())
  const [params] = useSearchParams()
  const focus = params.get("projet") // arrivée depuis la page d'un dossier : ce projet s'ouvre et s'affiche même sans réglage propre
  useEffect(() => {
    api.configProjects().then((r) => setList(r.projects))
  }, [version])
  const target = list?.find((p) => p.project === focus)?.dir
  useEffect(() => {
    if (target) setTimeout(() => document.getElementById("cfg-" + target)?.scrollIntoView({ block: "start" }), 50)
  }, [target])
  if (!list) return <p className="py-8 text-sm text-muted-foreground">{t("common.loading")}</p>
  const f = q.toLowerCase()
  const shown = list.filter((p) => (all || p.dir === target || p.specific > 0) && (!f || p.project.toLowerCase().includes(f)))
  const hidden = list.filter((p) => p.specific === 0).length
  const toggle = (d: string) =>
    setOpen((o) => {
      const n = new Set(o)
      if (n.has(d)) n.delete(d)
      else n.add(d)
      return n
    })
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("config.projects.filterPlaceholder")} className="h-8 w-64" aria-label={t("config.projects.filterAria")} />
        {hidden > 0 && (
          <Button size="sm" variant={all ? "secondary" : "ghost"} onClick={() => setAll(!all)} aria-pressed={all}>
            {t(all ? "config.projects.hideHidden" : "config.projects.showHidden", { count: hidden })}
          </Button>
        )}
        <span className="text-xs text-muted-foreground">{t("config.projects.intro")}</span>
      </div>
      <ul className="space-y-2">
        {shown.map((p) => {
          const isOpen = open.has(p.dir) !== (p.dir === target) // le projet visé s'ouvre de lui-même ; un clic le referme
          const perms = Object.fromEntries(Object.entries(p.perms).map(([k, l]) => [k, l.filter((r) => !gone(`${r.scope}/${p.dir}`, k, r.rule)).sort((x, y) => byRule(x.rule, y.rule))]))
          const nRules = Object.values(perms).reduce((n, l) => n + l.length, 0)
          const nSettings = p.files.reduce((n, x) => n + x.rows.filter((r) => !r.path.startsWith("permissions.") && r.path !== "hooks").length, 0)
          return (
            <li key={p.dir} id={"cfg-" + p.dir} className="scroll-mt-4 rounded-lg border">
              <button onClick={() => toggle(p.dir)} className="flex w-full flex-wrap items-center gap-2 px-3 py-2 text-left" aria-expanded={isOpen}>
                <ChevronRight className={cn("size-4 shrink-0 transition-transform", isOpen && "rotate-90")} />
                <span className="min-w-0 flex-1 truncate text-sm font-medium" title={p.project}>
                  {tilde(p.project, home)}
                </span>
                <Badge n={nRules} label={t("config.projects.badgePerms")} />
                <Badge n={nSettings} label={t("config.projects.badgeSettings")} />
                <Badge n={p.hooks.length} label="hooks" />
                <Badge n={p.mcp.length} label="MCP" />
                <Badge n={p.rules.length} label={t("config.projects.badgeRules")} />
                <Badge n={p.skills.length} label="skills" />
                {p.specific === 0 && <span className="text-xs text-muted-foreground">{t("config.projects.nothingSpecific")}</span>}
              </button>
              {isOpen && (
                <div className="space-y-4 border-t px-4 py-3 text-sm">
                  <section>
                    <div className="mb-1 flex items-center gap-2">
                      <h3 className="text-xs font-medium text-muted-foreground">{t("config.projects.permsTitle")}</h3>
                      <Button size="xs" variant="ghost" onClick={() => onEdit(p.files.find((x) => x.id[0] === "p")?.id ?? "p/" + p.dir, "permissions")}>
                        {t("config.projects.manage")}
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button size="xs" variant="ghost" title={t("config.projects.addDirTitle")}>
                            <FolderPlus /> {t("config.projects.addDir")}
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start" className="w-72">
                          {suggest(p.project, (p.perms.additionalDirectories ?? []).map((r) => r.rule)).map((d) => (
                            <DropdownMenuItem key={d} onSelect={() => onAddDir("l/" + p.dir, p.project, d)}>
                              <FolderPlus /> <span className="truncate">{tilde(d, home)}</span>
                            </DropdownMenuItem>
                          ))}
                          <DropdownMenuItem onSelect={() => onAddDir("l/" + p.dir, p.project)}>{t("config.projects.chooseDir")}</DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </div>
                    {nRules === 0 ? (
                      <p className="text-xs text-muted-foreground">{t("config.projects.noPerms")}</p>
                    ) : (
                      KINDS.filter((k) => perms[k]?.length).map((k) => (
                        <div key={k} className="mb-2">
                          <p className="text-xs text-muted-foreground">{t(`config.perms.kind.${k}`)}</p>
                          <ul className="space-y-px">
                            {perms[k].map((r, i) => (
                              <ContextMenu key={k + i}>
                              <ContextMenuTrigger asChild>
                              <li className="flex items-center gap-2">
                                <code className="min-w-0 flex-1 text-xs break-all">{r.rule}</code>
                                <span className="shrink-0 text-2xs text-muted-foreground">{r.scope === "p" ? t("config.scope.project") : t("config.scope.local")}</span>
                                <DropdownMenu>
                                  <DropdownMenuTrigger asChild>
                                    <Button size="xs" variant="ghost">
                                      <Globe /> {t("config.projects.toGlobal")}
                                    </Button>
                                  </DropdownMenuTrigger>
                                  <DropdownMenuContent align="end">
                                    <DropdownMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, false)}>{t("config.projects.copyToUser")}</DropdownMenuItem>
                                    <DropdownMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, true)}>{t("config.projects.moveFromProject")}</DropdownMenuItem>
                                  </DropdownMenuContent>
                                </DropdownMenu>
                                <Button size="icon-xs" variant="ghost" aria-label={t("config.projects.deleteRule", { rule: r.rule })} title={t("config.projects.deleteRuleTitle")} onClick={() => onRemove(`${r.scope}/${p.dir}`, k, r.rule)}>
                                  <X />
                                </Button>
                              </li>
                              </ContextMenuTrigger>
                              <ContextMenuContent className="w-60">
                                <ContextMenuItem onSelect={() => navigator.clipboard.writeText(r.rule).then(() => toast.success(t("config.shared.ruleCopied")), () => toast.error(t("config.shared.copyFailed")))}>
                                  <ClipboardCopy /> {t("config.perms.copyRule")}
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, false)}>
                                  <Globe /> {t("config.perms.copyGlobal")}
                                </ContextMenuItem>
                                <ContextMenuItem onSelect={() => onTransfer(`${r.scope}/${p.dir}`, k, r.rule, true)}>
                                  <Globe /> {t("config.perms.moveGlobal")}
                                </ContextMenuItem>
                                <ContextMenuSeparator />
                                <ContextMenuItem variant="destructive" onSelect={() => onRemove(`${r.scope}/${p.dir}`, k, r.rule)}>
                                  <X /> {t("common.delete")}
                                </ContextMenuItem>
                              </ContextMenuContent>
                              </ContextMenu>
                            ))}
                          </ul>
                        </div>
                      ))
                    )}
                  </section>
                  {p.files.map((fl) => {
                    const rows = fl.rows.filter((r) => !r.path.startsWith("permissions.") && r.path !== "hooks") // les hooks ont leur section
                    return (
                      <section key={fl.id}>
                        <div className="mb-1 flex items-center gap-2">
                          <h3 className="text-xs font-medium text-muted-foreground">
                            {t("config.projects.settingsHeading")} <code>{fl.label}</code> {fl.id[0] === "p" ? t("config.projects.sharedSuffix") : t("config.projects.privateSuffix")}
                          </h3>
                          <Button size="xs" variant="ghost" onClick={() => onEdit(fl.id, "json")}>
                            JSON…
                          </Button>
                        </div>
                        {rows.length === 0 ? (
                          <p className="text-xs text-muted-foreground">{t("config.projects.onlyPerms")}</p>
                        ) : (
                          <ul className="space-y-1">
                            {rows.map((r) => (
                              <li key={r.path} className="text-xs">
                                <code className="font-medium">{r.path}</code> <span className="text-muted-foreground">=</span> <span className="break-all">{r.value}</span>
                                {describeKey(r.path) && <span className="block text-2xs text-muted-foreground">{describeKey(r.path)}</span>}
                              </li>
                            ))}
                          </ul>
                        )}
                      </section>
                    )
                  })}
                  {p.files.length === 0 && (
                    <p className="text-xs text-muted-foreground">
                      {t("config.projects.noFile")}{" "}
                      <button className="underline" onClick={() => onEdit("l/" + p.dir, "reglages")}>
                        {t("config.projects.createLocal")}
                      </button>
                    </p>
                  )}
                  {p.hooks.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">{t("config.projects.hooksTitle")}</h3>
                      <ul className="space-y-1">
                        {p.hooks.map((h, i) => (
                          <li key={i} className="text-xs">
                            <span className="font-medium">{h.event}</span> {h.matcher && <code className="rounded-sm bg-muted px-1">{h.matcher}</code>}{" "}
                            <code className="break-all text-muted-foreground">{h.command}</code>
                            {h.missing && <span className="ml-1 text-warning">{t("config.hooks.missing", { path: h.missing })}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {p.mcp.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">{t("config.projects.mcpTitle")}</h3>
                      <ul className="space-y-1">
                        {p.mcp.map((m) => (
                          <li key={m.name + m.scope} className="text-xs">
                            <span className="font-medium">{m.name}</span> <span className="text-muted-foreground">{m.type} · {m.scope === "project" ? ".mcp.json" : t("config.scope.local")}</span>{" "}
                            <code className="break-all text-muted-foreground">{m.url || m.command}</code>
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {p.rules.length > 0 && (
                    <section>
                      <h3 className="mb-1 text-xs font-medium text-muted-foreground">
                        {t("config.projects.rulesHeading")} <code>.claude/rules</code> ·{" "}
                        <Link className="underline" href={href({ kind: "memory", key: `r/${p.dir}/${p.rules[0].name}` })}>
                          {t("config.projects.rulesManage")}
                        </Link>
                      </h3>
                      <ul className="space-y-1">
                        {p.rules.map((r) => (
                          <li key={r.name} className="text-xs">
                            <Link className="font-medium underline-offset-2 hover:underline" href={href({ kind: "memory", key: `r/${p.dir}/${r.name}` })}>
                              <code>{r.name}</code>
                            </Link>{" "}
                            <span className="text-muted-foreground">{r.paths?.length ? t("config.projects.ruleOnlyFor", { paths: r.paths.join(", ") }) : t("config.projects.ruleAlways")}</span>
                            {r.warn && <span className="ml-1 text-warning">{r.warn}</span>}
                          </li>
                        ))}
                      </ul>
                    </section>
                  )}
                  {(p.skills.length > 0 || p.docs.length > 0) && (
                    <p className="text-xs text-muted-foreground">
                      {p.skills.length > 0 && <>{t("config.projects.skillsLine", { list: p.skills.map((s) => "/" + s).join(", ") })} </>}
                      {p.docs.length > 0 && <>{t("config.projects.docsLine", { list: p.docs.join(", ") })}</>}
                    </p>
                  )}
                </div>
              )}
            </li>
          )
        })}
        {shown.length === 0 && <li className="py-6 text-center text-sm text-muted-foreground">{q ? t("config.projects.noMatch") : t("config.projects.noOwn")}</li>}
      </ul>
    </div>
  )
}
