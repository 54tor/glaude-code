import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import {
  ArchiveRestore,
  ChevronRight,
  Fan,
  RefreshCw,
  Search,
  Trash2,
} from "lucide-react"
import {
  api,
  type CfgOverview,
  type McpServer,
  type PluginInst,
  type Skill,
} from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { tilde } from "@/lib/format"
import { href } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Switch } from "@/components/ui/switch"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Choose } from "./Choose"
import { PluginsCatalog, pluginKey, usePluginRun } from "./PluginsDialog"
import { SkillsNpxPanel } from "./SkillsNpxDialog"
import { Skills } from "./Skills"
import type { Ask } from "./Confirm"
import { useActions } from "./actions"
import { useConfigEdit } from "./configEdit"

type Enabled = Record<string, boolean>
type ScopeCode = "u" | "p" | "l"
type ScopeFileData = { skillOverrides?: Record<string, string>; disabledMcpjsonServers?: string[] }

const GIT_HASH_PATTERN = /^[0-9a-f]{12}/
const SHORT_HASH_LENGTH = 7

// Vue unique de ce que Claude Code charge : plugins (activables par portée), skills, hooks et serveurs MCP, pour l'utilisateur
// seul ou pour le contexte d'un projet (utilisateur, puis projet, puis local). Les écritures passent par /config/edits.
export function Extensions({
  home,
  onAsk,
}: {
  home: string
  onAsk: (ask: Ask) => void
}) {
  const { t } = useTranslation()
  const [overview, setOverview] = useState<CfgOverview | null>(null)
  const [skills, setSkills] = useState<Skill[]>([])
  const [mcp, setMcp] = useState<McpServer[]>([])
  const [installedPlugins, setInstalledPlugins] = useState<PluginInst[]>([])
  const [err, setErr] = useState("")
  const [dir, setDir] = useState("") // "" : utilisateur seul
  const [tab, setTab] = useState("plugins")
  const [pluginQuery, setPluginQuery] = useState("") // recherche plugins : installés et catalogue
  const [skillQuery, setSkillQuery] = useState("") // recherche skills : installés et npx
  const [where, setWhere] = useState<ScopeCode>("u") // portée où un interrupteur écrit
  const [skillKey, setSkillKey] = useState("") // skill ouvert dans l'éditeur
  const openSkill = (key: string) => {
    setSkillKey(key)
    setTab("editor")
  }
  const [status, setStatus] = useState<Record<string, string> | null>(null) // `claude mcp list` : tous les serveurs vus par Claude Code, lent (quelques secondes)
  const actions = useActions()
  const reload = useCallback(
    () =>
      Promise.all([
        api.config().then(setOverview),
        api.skills().then((skillsResponse) => setSkills(skillsResponse.skills)),
        api.configMcp().then((mcpResponse) => setMcp(mcpResponse.servers)),
        api.plugins().then(
          (pluginsResponse) => setInstalledPlugins(pluginsResponse.installed ?? []),
          (error) => setErr(errMsg(error))
        ),
      ]),
    []
  )
  useEffect(() => void reload().catch((error) => setErr(String(error))), [reload]) // sinon la page reste sur le squelette
  useEffect(() => {
    api.configMcpCheck().then(
      (checkResponse) => setStatus(checkResponse.status),
      () => {}
    )
  }, [])
  const { propose, dialog } = useConfigEdit(reload, actions.live.size)
  const plug = usePluginRun(overview?.projects ?? [], reload)
  const enabledByFile = useMemo(() => {
    const enabledPluginsByFileId: Record<string, Enabled> = {}
    for (const configFile of overview?.files ?? []) {
      const fileData = configFile.data as { enabledPlugins?: Enabled } | undefined
      enabledPluginsByFileId[configFile.id] = (fileData?.enabledPlugins ?? {}) as Enabled
    }
    return enabledPluginsByFileId
  }, [overview])
  if (!overview) {
    if (err) return <p className="m-6 text-sm text-destructive">{err}</p>
    return <Skeleton className="m-6 h-40" />
  }

  const files = { u: "u", p: dir && "p/" + dir, l: dir && "l/" + dir }
  const filesByPriority = [files.l, files.p, "u"]
  const allIds = [
    ...new Set([
      ...overview.plugins.map((plugin) => plugin.id),
      ...Object.values(enabledByFile).flatMap(Object.keys),
    ]),
  ].sort()
  const normalizedPluginQuery = pluginQuery.trim().toLowerCase()
  const ids = allIds.filter((id) => id.toLowerCase().includes(normalizedPluginQuery))
  const effectiveEnabled = (id: string) =>
    filesByPriority
      .map((fileId) => (fileId ? enabledByFile[fileId]?.[id] : undefined))
      .find((enabled) => enabled !== undefined)
  const version = (id: string) => installedPlugins.find((plugin) => plugin.id === id)?.version
  const appliesToContext = (fileId: string) =>
    !dir || fileId === "u" || fileId === "p/" + dir || fileId === "l/" + dir // fichier qui s'applique à ce contexte
  const hooks = overview.hooks.filter((hook) => appliesToContext(hook.file))
  const contextProjectName = overview.projects.find((project) => project.dir === dir)?.project
  const mcpHere = mcp.filter(
    (server) =>
      server.scope === "user" ||
      (dir && server.project && server.project === contextProjectName)
  )
  const skillsHere = skills.filter((skill) => {
    if (skill.scope === "user" || skill.scope === "plugin") return true
    if (skill.scope !== "project") return false
    return !dir || skill.project === contextProjectName
  })
  const knownServerNames = new Set(mcp.map((server) => server.name))
  const others = Object.keys(status ?? {})
    .filter((serverName) => !knownServerNames.has(serverName))
    .sort() // plugins et connecteurs claude.ai : pas dans ~/.claude.json
  const isServerUp = (serverName: string) => /✔|Connected/.test(status?.[serverName] ?? "")
  const contextCharacters = (skillList: Skill[]) =>
    skillList.filter((skill) => !skill.manual).reduce((characterTotal, skill) => characterTotal + skill.chars, 0)
  const chars = contextCharacters(skillsHere)
  const fileData = (fileId: string) =>
    overview.files.find((configFile) => configFile.id === fileId)?.data as ScopeFileData | undefined
  const skillMode = (fileId: string, name: string) => fileData(fileId)?.skillOverrides?.[name]
  const effectiveSkillMode = (name: string) =>
    filesByPriority
      .map((fileId) => (fileId ? skillMode(fileId, name) : undefined))
      .find(Boolean) ?? "on"
  const isMcpOff = (fileId: string, name: string) =>
    fileData(fileId)?.disabledMcpjsonServers?.includes(name) === true
  const isMcpEffectivelyOff = (name: string) =>
    filesByPriority.some((fileId) => fileId && isMcpOff(fileId, name))
  const scopeName = {
    u: t("extensions.scopeU"),
    p: t("extensions.scopeP"),
    l: t("extensions.scopeL"),
  }
  const to = dir ? where : "u" // sans projet choisi, seul le réglage personnel existe
  const order = (["l", "p", "u"] as const).filter((scope) => files[scope]) // du plus prioritaire au moins prioritaire
  const higher = order.slice(0, order.indexOf(to)) // portées qui l'emportent sur celle où l'on écrit
  const fileOf = (scope: ScopeCode) => files[scope] as string
  const decidingScope = (readSetting: (fileId: string) => unknown) =>
    order.find((scope) => readSetting(fileOf(scope)) !== undefined) // portée qui décide
  const why = (scope: ScopeCode | undefined) => {
    if (!scope) return t("extensions.byDefault")
    return t("extensions.setIn", { scope: scopeName[scope] })
  }
  const edit = (file: string, op: string, path: string[], value?: unknown) =>
    propose({ file, op, path, value } as Parameters<typeof propose>[0])
  // Interrupteur d'un plugin : écrit dans la portée choisie et retire les réglages contraires des portées plus prioritaires.
  const setPlugin = async (id: string, on: boolean) => {
    await edit(fileOf(to), "set", ["enabledPlugins", id], on)
    for (const scope of higher) {
      if (enabledByFile[fileOf(scope)]?.[id] !== undefined) await edit(fileOf(scope), "delete", ["enabledPlugins", id])
    }
  }
  const setSkill = async (name: string, on: boolean) => {
    if (on) {
      for (const scope of order) {
        if (skillMode(fileOf(scope), name) === "off") await edit(fileOf(scope), "delete", ["skillOverrides", name])
      }
      return
    }
    await edit(fileOf(to), "set", ["skillOverrides", name], "off")
    for (const scope of higher) {
      if (skillMode(fileOf(scope), name) !== undefined) await edit(fileOf(scope), "delete", ["skillOverrides", name])
    }
  }
  const toggleMcp = async (name: string, on: boolean) => {
    if (!on) {
      await edit(fileOf(to), "add", ["disabledMcpjsonServers"], name)
      return
    }
    for (const scope of order) {
      if (isMcpOff(fileOf(scope), name)) await edit(fileOf(scope), "remove", ["disabledMcpjsonServers"], name)
    }
  }
  const here = dir ? "p/" + dir : "user" // portée d'installation : le contexte affiché
  // L'installation sur laquelle portent mise à jour et désinstallation : celle du contexte, sinon la première trouvée.
  const record = (id: string) => {
    const recs = installedPlugins.filter((plugin) => plugin.id === id)
    const isContextInstall = (plugin: PluginInst) => {
      if (!dir) return plugin.scope === "user"
      return plugin.scope !== "user" && plug.dirOf(plugin) === dir
    }
    return {
      recs,
      rec: recs.find(isContextInstall) ?? recs[0],
    }
  }
  const instLabel = (plugin: PluginInst) =>
    plugin.scope === "user" ? t("extensions.fromUser") : tilde(plugin.project ?? "", home)
  const row = "flex items-start gap-3 py-2"
  const shortVersion = (versionText?: string) => {
    const isGitHash = !!versionText && GIT_HASH_PATTERN.test(versionText)
    return isGitHash ? versionText.slice(0, SHORT_HASH_LENGTH) : versionText // empreinte git : 7 caractères suffisent
  }
  const normalizedSkillQuery = skillQuery.trim().toLowerCase()
  const matchesSkillQuery = (skill: Skill) =>
    !normalizedSkillQuery || (skill.name + " " + skill.description).toLowerCase().includes(normalizedSkillQuery)
  const ownSkills = skillsHere.filter((skill) => !skill.plugin && matchesSkillQuery(skill))
  const skillsByPluginId: Record<string, Skill[]> = {}
  for (const skill of skillsHere.filter(matchesSkillQuery)) {
    if (skill.plugin) (skillsByPluginId[skill.plugin] ??= []).push(skill)
  }
  const byPlugin = Object.entries(skillsByPluginId).sort(([firstPluginId], [secondPluginId]) => firstPluginId.localeCompare(secondPluginId))
  const skillRow = (skill: Skill) => {
    const scope = decidingScope((fileId) => skillMode(fileId, skill.name))
    const off = effectiveSkillMode(skill.name) === "off"
    return (
      <li key={skill.key} className={row}>
        {!skill.plugin && (
          <Switch
            checked={!off}
            aria-label={t("extensions.toggleOne", { id: skill.name })}
            onCheckedChange={(checked) => setSkill(skill.name, checked)}
            className="mt-0.5"
          />
        )}
        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-baseline gap-x-2">
            <button
              type="button"
              onClick={() => openSkill(skill.key)}
              className="font-medium break-all text-primary underline underline-offset-2 hover:decoration-2"
            >
              {skill.name}
            </button>
            {!skill.plugin && (
              <span className="text-xs text-muted-foreground">
                {skill.scope === "project"
                  ? t("extensions.fromProject")
                  : t("extensions.fromUser")}
              </span>
            )}
            {(skill.manual || skill.hidden) && (
              <span className="text-xs text-warning">
                {skill.manual ? t("extensions.manual") : t("extensions.hidden")}
              </span>
            )}
            {!skill.plugin && (dir || off) && (
              <span className="text-xs text-muted-foreground">{why(scope)}</span>
            )}
            <span className="ml-auto text-xs whitespace-nowrap text-muted-foreground tabular-nums">
              {t("extensions.tokens", { n: skill.descTokens })}
            </span>
          </div>
          <div
            className="line-clamp-1 text-xs text-muted-foreground"
            title={skill.description}
          >
            {skill.description}
          </div>
        </div>
      </li>
    )
  }
  const sect =
    "text-md font-semibold border-b pb-2 mb-3 flex items-center gap-2"
  const search = (value: string, setValue: (value: string) => void, label: string) => (
    <div className="relative mb-4">
      <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
      <Input
        value={value}
        onChange={(changeEvent) => setValue(changeEvent.target.value)}
        placeholder={label}
        aria-label={label}
        className="pl-8"
      />
    </div>
  )
  const otherServerOrigin = (serverName: string) => {
    if (serverName.startsWith("plugin:")) return t("extensions.mcpPlugin")
    if (serverName.startsWith("claude.ai ")) return t("extensions.mcpAccount")
    return t("extensions.mcpOther")
  }
  const pane = (name: string, body: React.ReactNode) => (
    <TabsContent value={name} className="min-h-0 flex-1">
      <ScrollArea className="h-full">
        <div className="mx-auto max-w-page px-3 py-5 md:px-6">{body}</div>
      </ScrollArea>
    </TabsContent>
  )

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <div className="mx-auto w-full max-w-page px-3 pt-4 md:px-6">
        <div className="space-y-2 rounded-md border bg-muted/30 px-3 py-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{t("extensions.viewing")}</span>
            <Choose
              value={dir}
              onChange={setDir}
              label={t("extensions.viewing")}
              className="max-w-md min-w-0"
              options={[
                ["", t("extensions.contextUser")],
                ...overview.projects.map(
                  (project) => [project.dir, tilde(project.project, home)] as const
                ),
              ]}
            />
          </div>
          {dir && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{t("extensions.saveIn")}</span>
              <Choose
                value={where}
                onChange={(value) => setWhere(value as ScopeCode)}
                label={t("extensions.saveIn")}
                className="max-w-md min-w-0"
                options={[
                  ["u", t("extensions.whereU")],
                  ["p", t("extensions.whereP")],
                  ["l", t("extensions.whereL")],
                ]}
              />
            </div>
          )}
          <p className="text-xs text-muted-foreground">
            {dir ? t("extensions.hintProject") : t("extensions.hintUser")}
          </p>
        </div>
      </div>
      <Tabs
        value={tab}
        onValueChange={setTab}
        className="flex min-h-0 flex-1 flex-col gap-0"
      >
        <div className="mx-auto w-full max-w-page px-3 pt-3 md:px-6">
          <ScrollArea className="w-full [mask-image:linear-gradient(to_right,black_calc(100%-2.5rem),transparent)]">
            <div className="pb-2">
              <TabsList className="w-max justify-start">
                <TabsTrigger value="plugins">
                  {t("extensions.plugins")}{" "}
                  <span className="text-muted-foreground tabular-nums">
                    {allIds.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="skills">
                  {t("extensions.skills")}{" "}
                  <span className="text-muted-foreground tabular-nums">
                    {skillsHere.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="hooks">
                  {t("extensions.hooks")}{" "}
                  <span className="text-muted-foreground tabular-nums">
                    {hooks.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="mcp">
                  {t("extensions.mcp")}{" "}
                  <span className="text-muted-foreground tabular-nums">
                    {mcpHere.length + others.length}
                  </span>
                </TabsTrigger>
                <TabsTrigger value="editor">
                  {t("extensions.tabEditor")}
                </TabsTrigger>
              </TabsList>
            </div>
          </ScrollArea>
        </div>
        {pane(
          "plugins",
          <>
            {search(pluginQuery, setPluginQuery, t("extensions.searchPlugins"))}
            <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
              <h3 className={sect}>
                {t("extensions.plugins")} ({ids.length})
                <Button size="xs" variant="outline" className="ml-auto" asChild>
                  <Link to="/sauvegarde?cats=plugins">
                    <ArchiveRestore /> {t("extensions.exportImport")}
                  </Link>
                </Button>
              </h3>
              <p className="mb-1 text-xs text-muted-foreground">
                {t("extensions.pluginsHelp")}
              </p>
              {err && (
                <p className="mb-2 text-xs text-warning">
                  {t("extensions.loadError", { err })}
                </p>
              )}
              <ul className="grid grid-cols-1 gap-x-8 md:grid-cols-2">
                {ids.map((id) => {
                  const scope = decidingScope((fileId) => enabledByFile[fileId]?.[id])
                  const [name, market] = id.split("@")
                  const missing =
                    !overview.plugins.find((plugin) => plugin.id === id)?.installed &&
                    !version(id)
                  const { recs, rec } = record(id)
                  const recordDir = rec && plug.dirOf(rec)
                  const key = rec ? pluginKey(rec) : id
                  const off =
                    plug.busy === key || recordDir === null || recordDir === undefined
                  const runPluginAction = (action: "update" | "uninstall") =>
                    rec && plug.run(key, action, rec.id, rec.scope, recordDir ?? "")
                  const isEnabled = effectiveEnabled(id) === true
                  return (
                    <li key={id} className={row + " border-b"}>
                      <Switch
                        checked={isEnabled}
                        aria-label={t("extensions.toggleOne", { id })}
                        onCheckedChange={(checked) => setPlugin(id, checked)}
                        className="mt-0.5"
                      />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-baseline gap-2">
                          <span
                            className={
                              "truncate font-medium" +
                              (isEnabled ? "" : " text-muted-foreground")
                            }
                            title={id}
                          >
                            {name}
                          </span>
                          {version(id) && (
                            <span
                              className="text-xs whitespace-nowrap text-muted-foreground tabular-nums"
                              title={version(id)}
                            >
                              {shortVersion(version(id))}
                            </span>
                          )}
                        </div>
                        <div className="truncate text-xs text-muted-foreground">
                          {market && "@" + market}
                          {dir && ` · ${why(scope)}`}
                          {missing && (
                            <span className="ml-2 text-warning">
                              {t("extensions.notInstalled")}
                            </span>
                          )}
                          {(recs.length > 1 || rec?.scope === "project") && (
                            <span className="ml-2">
                              {t("extensions.installedIn", {
                                where: recs.map(instLabel).join(", "),
                              })}
                            </span>
                          )}
                        </div>
                      </div>
                      {rec && (
                        <div className="flex shrink-0 gap-0.5">
                          <Button
                            size="icon-xs"
                            variant="ghost"
                            disabled={off}
                            title={t("skills.plugins.update")}
                            aria-label={t("extensions.pluginAct", {
                              act: t("skills.plugins.update"),
                              id,
                            })}
                            onClick={() => runPluginAction("update")}
                          >
                            {plug.busy === key ? (
                              <Fan className="animate-spin" />
                            ) : (
                              <RefreshCw />
                            )}
                          </Button>
                          <Button
                            size="icon-xs"
                            variant="ghost"
                            disabled={off}
                            title={t("skills.plugins.uninstall")}
                            aria-label={t("extensions.pluginAct", {
                              act: t("skills.plugins.uninstall"),
                              id,
                            })}
                            onClick={() => runPluginAction("uninstall")}
                          >
                            <Trash2 />
                          </Button>
                        </div>
                      )}
                    </li>
                  )
                })}
              </ul>
              {!ids.length && (
                <p className="py-3 text-sm text-muted-foreground">
                  {pluginQuery ? t("extensions.noMatch") : t("extensions.noPlugins")}
                </p>
              )}
              <p className="mt-2 mb-4 text-xs text-muted-foreground">
                {t("extensions.sessionNote")}
              </p>
              <PluginsCatalog
                projects={overview.projects}
                home={home}
                scope={here}
                installed={installedPlugins}
                onChange={reload}
                query={pluginQuery}
              />
            </section>
          </>
        )}
        {pane(
          "skills",
          <>
            {search(skillQuery, setSkillQuery, t("extensions.searchSkills"))}
            <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
              <h3 className={sect}>
                {t("extensions.skills")}{" "}
                <span className="text-xs font-normal text-muted-foreground">
                  {t("extensions.skillsTotal", {
                    count: skillsHere.length,
                    chars,
                  })}
                </span>
                <button
                  type="button"
                  onClick={() => openSkill("")}
                  className="ml-auto text-xs font-normal text-primary underline underline-offset-2 hover:decoration-2"
                >
                  {t("extensions.editor")}
                </button>
              </h3>
              <p className="mb-1 text-xs text-muted-foreground">
                {t("extensions.skillsHelp")}
              </p>
              <ul className="divide-y">{ownSkills.map(skillRow)}</ul>
              <div className="mt-3 space-y-2">
                {byPlugin.map(([pluginId, pluginSkills]) => (
                  <details key={pluginId} className="group rounded-md border">
                    <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm [&::-webkit-details-marker]:hidden">
                      <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
                      <span className="truncate font-medium">
                        {pluginId.split("@")[0]}
                      </span>
                      <span className="ml-auto text-xs whitespace-nowrap text-muted-foreground">
                        {t("extensions.skillsTotal", {
                          count: pluginSkills.length,
                          chars: contextCharacters(pluginSkills),
                        })}
                      </span>
                    </summary>
                    <ul className="divide-y border-t px-3">
                      {pluginSkills.map(skillRow)}
                    </ul>
                  </details>
                ))}
              </div>
              {!ownSkills.length && !byPlugin.length && (
                <p className="py-3 text-sm text-muted-foreground">
                  {normalizedSkillQuery ? t("extensions.noMatch") : t("extensions.noSkills")}
                </p>
              )}
              <h4 className="mt-5 mb-2 text-sm font-medium">
                {t("extensions.skillsAvailable")}
              </h4>
              <SkillsNpxPanel
                projects={overview.projects}
                home={home}
                onChange={reload}
                fixed={here}
                query={skillQuery}
              />
            </section>
          </>
        )}
        {pane(
          "hooks",
          <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
            <h3 className={sect}>
              {t("extensions.hooks")} ({hooks.length})
              <Link
                href={href({ kind: "config", tab: "json" })}
                className="ml-auto text-xs font-normal text-muted-foreground underline"
              >
                {t("extensions.editHooks")}
              </Link>
            </h3>
            <ul className="space-y-1">
              {hooks.map((hook, hookIndex) => (
                <li key={hookIndex} className="rounded-md border px-3 py-1.5 text-sm">
                  <p className="flex flex-wrap items-center gap-2">
                    <span className="font-medium">{hook.event}</span>
                    {hook.matcher && (
                      <code className="rounded-sm bg-muted px-1 text-xs">
                        {hook.matcher}
                      </code>
                    )}
                    <span className="text-xs text-muted-foreground">
                      {scopeName[hook.scope as ScopeCode]}
                    </span>
                    {hook.missing && (
                      <span className="text-xs text-warning">
                        {t("extensions.missing", { path: hook.missing })}
                      </span>
                    )}
                  </p>
                  <code className="block text-xs break-all text-muted-foreground">
                    {hook.command || `(${hook.type})`}
                  </code>
                </li>
              ))}
            </ul>
            {!hooks.length && (
              <p className="py-3 text-sm text-muted-foreground">
                {t("extensions.noHooks")}
              </p>
            )}
          </section>
        )}
        {pane(
          "mcp",
          <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
            <h3 className={sect}>
              {t("extensions.mcp")} ({mcpHere.length + others.length})
              {!status && (
                <Fan className="size-3.5 animate-spin text-muted-foreground" />
              )}
            </h3>
            <p className="mb-1 text-xs text-muted-foreground">
              {t("extensions.mcpHelp")}
            </p>
            <ul className="divide-y">
              {mcpHere.map((server, serverIndex) => (
                <li key={serverIndex} className={row}>
                  {server.scope === "project" ? (
                    <Switch
                      checked={!isMcpEffectivelyOff(server.name)}
                      aria-label={t("extensions.toggleOne", { id: server.name })}
                      onCheckedChange={(checked) => toggleMcp(server.name, checked)}
                      className="mt-0.5"
                    />
                  ) : (
                    <span
                      className="mt-0.5 w-9 shrink-0 text-center text-muted-foreground"
                      title={t("extensions.mcpManaged")}
                    >
                      ·
                    </span>
                  )}
                  <div className="min-w-0 flex-1">
                    <div>
                      {status && (
                        <span
                          className={
                            isServerUp(server.name) ? "text-success" : "text-warning"
                          }
                          title={status[server.name]}
                        >
                          ●{" "}
                        </span>
                      )}
                      <span className="font-medium">{server.name}</span>{" "}
                      <span className="text-xs text-muted-foreground">
                        {server.type}
                      </span>
                    </div>
                    <code className="block text-xs break-all text-muted-foreground">
                      {server.url || server.command}
                    </code>
                    {!!server.envKeys?.length && (
                      <span className="block text-xs text-muted-foreground">
                        {t("extensions.mcpVars", {
                          list: server.envKeys.join(", "),
                        })}
                      </span>
                    )}
                  </div>
                </li>
              ))}
              {others.map((serverName) => (
                <li key={serverName} className={row}>
                  <span
                    className="mt-0.5 w-9 shrink-0 text-center text-muted-foreground"
                    title={t("extensions.mcpManaged")}
                  >
                    ·
                  </span>
                  <div className="min-w-0 flex-1">
                    <span
                      className={isServerUp(serverName) ? "text-success" : "text-warning"}
                      title={status?.[serverName]}
                    >
                      ●{" "}
                    </span>
                    <span className="font-medium">{serverName}</span>{" "}
                    <span className="text-xs text-muted-foreground">
                      {otherServerOrigin(serverName)}
                    </span>
                  </div>
                </li>
              ))}
            </ul>
            {(others.length > 0 ||
              mcpHere.some((server) => server.scope !== "project")) && (
              <p className="mt-2 text-xs text-muted-foreground">
                {t("extensions.mcpManaged")}
              </p>
            )}
            {!mcpHere.length && !others.length && (
              <p className="py-3 text-sm text-muted-foreground">
                {t("extensions.noMcp")}
              </p>
            )}
          </section>
        )}
        <TabsContent
          value="editor"
          className="mx-auto flex min-h-0 w-full max-w-page flex-1 flex-col px-3 pb-5 md:px-6"
        >
          <div className="flex min-h-0 flex-1 flex-col overflow-hidden rounded-xl border bg-card shadow-xs">
            <Skills
              sel={skillKey}
              home={home}
              q=""
              onPick={setSkillKey}
              onAsk={onAsk}
            />
          </div>
        </TabsContent>
      </Tabs>
      {dialog}
    </div>
  )
}
