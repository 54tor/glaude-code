import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { Loader2, Plug } from "lucide-react"
import { api, type CfgOverview, type McpServer, type PluginInst, type Skill } from "@/lib/api"
import { tilde } from "@/lib/format"
import { href } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Choose } from "./Choose"
import { PluginsPanel } from "./PluginsDialog"
import { Skills } from "./Skills"
import type { Ask } from "./Confirm"
import { useActions } from "./actions"
import { useConfigEdit } from "./configEdit"

type Enabled = Record<string, boolean>

// Vue unique de ce que Claude Code charge : plugins (activables par portée), skills, hooks et serveurs MCP, pour l'utilisateur
// seul ou pour le contexte d'un projet (utilisateur, puis projet, puis local). Les écritures passent par /config-edit.
export function Extensions({ home, onAsk }: { home: string; onAsk: (a: Ask) => void }) {
  const { t } = useTranslation()
  const [ov, setOv] = useState<CfgOverview | null>(null)
  const [skills, setSkills] = useState<Skill[]>([])
  const [mcp, setMcp] = useState<McpServer[]>([])
  const [inst, setInst] = useState<PluginInst[]>([])
  const [err, setErr] = useState("")
  const [dir, setDir] = useState("") // "" : utilisateur seul
  const [tab, setTab] = useState("active")
  const [where, setWhere] = useState<"u" | "p" | "l">("u") // portée où un interrupteur écrit
  const [skillKey, setSkillKey] = useState("") // skill ouvert dans l'éditeur
  const openSkill = (key: string) => (setSkillKey(key), setTab("editor"))
  const [status, setStatus] = useState<Record<string, string> | null>(null) // `claude mcp list` : tous les serveurs vus par Claude Code, lent (quelques secondes)
  const a = useActions()
  const reload = useCallback(
    () =>
      Promise.all([
        api.config().then(setOv),
        api.skills().then((r) => setSkills(r.skills)),
        api.configMcp().then((r) => setMcp(r.servers)),
        api.plugins().then((r) => (setInst(r.installed ?? []), setErr(r.error ?? ""))),
      ]),
    [],
  )
  useEffect(() => void reload().catch((e) => setErr(String(e))), [reload]) // sinon la page reste sur le squelette
  useEffect(() => {
    api.post("/config-mcp-status", {}).then((r) => {
      const d = r as unknown as { ok: boolean; status?: Record<string, string> }
      if (d.ok && d.status) setStatus(d.status)
    })
  }, [])
  const { propose, dialog } = useConfigEdit(reload, a.live.size)
  const doc = useMemo(() => {
    const m: Record<string, Enabled> = {}
    for (const f of ov?.files ?? []) m[f.id] = ((f.data as { enabledPlugins?: Enabled } | undefined)?.enabledPlugins ?? {}) as Enabled
    return m
  }, [ov])
  if (!ov) return err ? <p className="m-6 text-sm text-destructive">{err}</p> : <Skeleton className="m-6 h-40" />

  const files = { u: "u", p: dir && "p/" + dir, l: dir && "l/" + dir }
  const ids = [...new Set([...ov.plugins.map((p) => p.id), ...Object.values(doc).flatMap(Object.keys)])].sort()
  const eff = (id: string) => [files.l, files.p, "u"].map((f) => (f ? doc[f]?.[id] : undefined)).find((v) => v !== undefined)
  const version = (id: string) => inst.find((p) => p.id === id)?.version
  const has = (f: string) => !dir || f === "u" || f === "p/" + dir || f === "l/" + dir // fichier qui s'applique à ce contexte
  const hooks = ov.hooks.filter((h) => has(h.file))
  const mcpHere = mcp.filter((s) => s.scope === "user" || (dir && s.project && s.project === ov.projects.find((p) => p.dir === dir)?.project))
  const skillsHere = skills.filter((s) => s.scope === "user" || s.scope === "plugin" || (s.scope === "project" && (!dir || s.project === ov.projects.find((p) => p.dir === dir)?.project)))
  const known = new Set(mcp.map((m) => m.name))
  const others = Object.keys(status ?? {}).filter((n) => !known.has(n)).sort() // plugins et connecteurs claude.ai : pas dans ~/.claude.json
  const up = (n: string) => /✔|Connected/.test(status?.[n] ?? "")
  const chars = skillsHere.filter((s) => !s.manual).reduce((n, s) => n + s.chars, 0)
  const cfg = (f: string) => ov.files.find((x) => x.id === f)?.data as { skillOverrides?: Record<string, string>; disabledMcpjsonServers?: string[] } | undefined
  const skillMode = (f: string, name: string) => cfg(f)?.skillOverrides?.[name]
  const skillEff = (name: string) => [files.l, files.p, "u"].map((f) => (f ? skillMode(f, name) : undefined)).find(Boolean) ?? "on"
  const mcpOff = (f: string, name: string) => cfg(f)?.disabledMcpjsonServers?.includes(name) === true
  const mcpEff = (name: string) => [files.l, files.p, "u"].some((f) => f && mcpOff(f, name))
  const scopeName = { u: t("extensions.scopeU"), p: t("extensions.scopeP"), l: t("extensions.scopeL") }
  const to = dir ? where : "u" // sans projet choisi, seul le réglage personnel existe
  const order = (["l", "p", "u"] as const).filter((k) => files[k]) // du plus prioritaire au moins prioritaire
  const higher = order.slice(0, order.indexOf(to)) // portées qui l'emportent sur celle où l'on écrit
  const src = (find: (f: string) => unknown) => order.find((k) => find(files[k] as string) !== undefined) // portée qui décide
  const why = (k: "u" | "p" | "l" | undefined) => (k ? t("extensions.setIn", { scope: scopeName[k] }) : t("extensions.byDefault"))
  const edit = (file: string, op: string, path: string[], value?: unknown) => propose({ file, op, path, value } as Parameters<typeof propose>[0])
  // Interrupteur d'un plugin : écrit dans la portée choisie et retire les réglages contraires des portées plus prioritaires.
  const setPlugin = async (id: string, on: boolean) => {
    await edit(files[to] as string, "set", ["enabledPlugins", id], on)
    for (const k of higher) if (doc[files[k] as string]?.[id] !== undefined) await edit(files[k] as string, "delete", ["enabledPlugins", id])
  }
  const setSkill = async (name: string, on: boolean) => {
    if (on) {
      for (const k of order) if (skillMode(files[k] as string, name) === "off") await edit(files[k] as string, "delete", ["skillOverrides", name])
      return
    }
    await edit(files[to] as string, "set", ["skillOverrides", name], "off")
    for (const k of higher) if (skillMode(files[k] as string, name) !== undefined) await edit(files[k] as string, "delete", ["skillOverrides", name])
  }
  const toggleMcp = async (name: string, on: boolean) => {
    if (!on) return void (await edit(files[to] as string, "add", ["disabledMcpjsonServers"], name))
    for (const k of order) if (mcpOff(files[k] as string, name)) await edit(files[k] as string, "remove", ["disabledMcpjsonServers"], name)
  }
  const row = "flex items-start gap-3 py-2"
  const sect = "text-md font-semibold border-b pb-2 mb-3 flex items-center gap-2"

  return (
    <div className="flex min-h-0 flex-1 flex-col">
      <Tabs value={tab} onValueChange={setTab} className="flex min-h-0 flex-1 flex-col gap-0">
        <div className="mx-auto w-full max-w-5xl px-3 pt-4 md:px-6">
          <TabsList>
            <TabsTrigger value="active">{t("extensions.tabActive")}</TabsTrigger>
            <TabsTrigger value="install">{t("extensions.tabInstall")}</TabsTrigger>
            <TabsTrigger value="editor">{t("extensions.tabEditor")}</TabsTrigger>
          </TabsList>
        </div>
        <TabsContent value="active" className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="mx-auto max-w-5xl space-y-5 px-3 py-5 md:px-6">
        <div className="space-y-2 rounded-md border bg-muted/30 px-3 py-3 text-sm">
          <div className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{t("extensions.viewing")}</span>
            <Choose value={dir} onChange={setDir} label={t("extensions.viewing")} className="min-w-0 max-w-md" options={[["", t("extensions.contextUser")], ...ov.projects.map((p) => [p.dir, tilde(p.project, home)] as const)]} />
          </div>
          {dir && (
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-medium">{t("extensions.saveIn")}</span>
              <Choose value={where} onChange={(v) => setWhere(v as "u" | "p" | "l")} label={t("extensions.saveIn")} className="min-w-0 max-w-md" options={[["u", t("extensions.whereU")], ["p", t("extensions.whereP")], ["l", t("extensions.whereL")]]} />
            </div>
          )}
          <p className="text-xs text-muted-foreground">{dir ? t("extensions.hintProject") : t("extensions.hintUser")}</p>
        </div>

        <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
          <h3 className={sect}>
            {t("extensions.plugins")} ({ids.length})
            <Button size="xs" variant="outline" className="ml-auto" onClick={() => setTab("install")}>
              <Plug /> {t("extensions.manage")}
            </Button>
          </h3>
          <p className="mb-1 text-xs text-muted-foreground">{t("extensions.pluginsHelp")}</p>
          {err && <p className="mb-2 text-xs text-warning">{t("extensions.loadError", { err })}</p>}
          <ul className="divide-y">
            {ids.map((id) => {
              const k = src((f) => doc[f]?.[id])
              return (
                <li key={id} className={row}>
                  <Switch checked={eff(id) === true} aria-label={t("extensions.toggleOne", { id })} onCheckedChange={(c) => setPlugin(id, c)} className="mt-0.5" />
                  <div className="min-w-0 flex-1">
                    <div className="break-all">{id} {version(id) && <span className="text-xs text-muted-foreground">{version(id)}</span>}</div>
                    <div className="text-xs text-muted-foreground">
                      {eff(id) === true ? t("extensions.on") : t("extensions.off")} · {why(k)}
                      {!ov.plugins.find((p) => p.id === id)?.installed && !version(id) && <span className="ml-2 text-warning">{t("extensions.notInstalled")}</span>}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
          {!ids.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noPlugins")}</p>}
          <p className="mt-2 text-xs text-muted-foreground">{t("extensions.sessionNote")}</p>
        </section>


        <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
          <h3 className={sect}>
            {t("extensions.skills")} <span className="text-xs font-normal text-muted-foreground">{t("extensions.skillsTotal", { count: skillsHere.length, chars })}</span>
            <button type="button" onClick={() => openSkill("")} className="ml-auto text-xs font-normal text-muted-foreground underline">{t("extensions.editor")}</button>
          </h3>
          <p className="mb-1 text-xs text-muted-foreground">{t("extensions.skillsHelp")}</p>
          <ul className="divide-y">
            {skillsHere.map((s) => {
              const k = src((f) => skillMode(f, s.name))
              const off = skillEff(s.name) === "off"
              return (
                <li key={s.key} className={row}>
                  {s.plugin ? <span className="mt-0.5 w-9 shrink-0 text-center text-muted-foreground" title={t("extensions.pluginManaged")}>·</span> : <Switch checked={!off} aria-label={t("extensions.toggleOne", { id: s.name })} onCheckedChange={(c) => setSkill(s.name, c)} className="mt-0.5" />}
                  <div className="min-w-0 flex-1">
                    <div>
                      <button type="button" onClick={() => openSkill(s.key)} className="break-all hover:underline">{s.name}</button>{" "}
                      <span className="text-xs text-muted-foreground">{s.plugin ? t("extensions.fromPlugin", { name: s.plugin }) : s.scope === "project" ? t("extensions.fromProject") : t("extensions.fromUser")}</span>
                      {(s.manual || s.hidden) && <span className="ml-2 text-xs text-warning">{s.manual ? t("extensions.manual") : t("extensions.hidden")}</span>}
                    </div>
                    <div className="line-clamp-2 text-xs text-muted-foreground">{s.description}</div>
                    <div className="text-xs text-muted-foreground">
                      {off ? t("extensions.off") : t("extensions.on")}{!s.plugin && ` · ${why(k)}`} · {t("extensions.tokens", { n: s.descTokens })}
                    </div>
                  </div>
                </li>
              )
            })}
          </ul>
          {!skillsHere.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noSkills")}</p>}
        </section>


        <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
          <h3 className={sect}>
            {t("extensions.hooks")} ({hooks.length})
            <Link href={href({ kind: "config", tab: "json" })} className="ml-auto text-xs font-normal text-muted-foreground underline">{t("extensions.editHooks")}</Link>
          </h3>
          <ul className="space-y-1">
            {hooks.map((h, n) => (
              <li key={n} className="rounded-md border px-3 py-1.5 text-sm">
                <p className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{h.event}</span>
                  {h.matcher && <code className="rounded-sm bg-muted px-1 text-xs">{h.matcher}</code>}
                  <span className="text-xs text-muted-foreground">{scopeName[h.scope as "u" | "p" | "l"]}</span>
                  {h.missing && <span className="text-xs text-warning">{t("extensions.missing", { path: h.missing })}</span>}
                </p>
                <code className="block text-xs break-all text-muted-foreground">{h.command || `(${h.type})`}</code>
              </li>
            ))}
          </ul>
          {!hooks.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noHooks")}</p>}
        </section>

        <section className="rounded-xl border bg-card p-4 shadow-xs md:p-5">
          <h3 className={sect}>
            {t("extensions.mcp")} ({mcpHere.length + others.length}){!status && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
            <Link href={href({ kind: "config", tab: "mcp" })} className="ml-auto text-xs font-normal text-muted-foreground underline">MCP</Link>
          </h3>
          <p className="mb-1 text-xs text-muted-foreground">{t("extensions.mcpHelp")}</p>
          <ul className="divide-y">
            {mcpHere.map((m, n) => (
              <li key={n} className={row}>
                {m.scope === "project" ? <Switch checked={!mcpEff(m.name)} aria-label={t("extensions.toggleOne", { id: m.name })} onCheckedChange={(c) => toggleMcp(m.name, c)} className="mt-0.5" /> : <span className="mt-0.5 w-9 shrink-0 text-center text-muted-foreground" title={t("extensions.mcpManaged")}>·</span>}
                <div className="min-w-0 flex-1">
                  <div>{status && <span className={up(m.name) ? "text-success" : "text-warning"} title={status[m.name]}>● </span>}<span className="font-medium">{m.name}</span> <span className="text-xs text-muted-foreground">{m.type}</span></div>
                  <code className="block text-xs break-all text-muted-foreground">{m.url || m.command}</code>
                </div>
              </li>
            ))}
            {others.map((n) => (
              <li key={n} className={row}>
                <span className="mt-0.5 w-9 shrink-0 text-center text-muted-foreground" title={t("extensions.mcpManaged")}>·</span>
                <div className="min-w-0 flex-1">
                  <span className={up(n) ? "text-success" : "text-warning"} title={status?.[n]}>● </span>
                  <span className="font-medium">{n}</span>{" "}
                  <span className="text-xs text-muted-foreground">{n.startsWith("plugin:") ? t("extensions.mcpPlugin") : n.startsWith("claude.ai ") ? t("extensions.mcpAccount") : t("extensions.mcpOther")}</span>
                </div>
              </li>
            ))}
          </ul>
          {(others.length > 0 || mcpHere.some((m) => m.scope !== "project")) && <p className="mt-2 text-xs text-muted-foreground">{t("extensions.mcpManaged")}</p>}
          {!mcpHere.length && !others.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noMcp")}</p>}

        </section>
            </div>
          </ScrollArea>
        </TabsContent>
        <TabsContent value="install" className="min-h-0 flex-1">
          <ScrollArea className="h-full">
            <div className="mx-auto max-w-5xl px-3 py-4 md:px-6">
              <PluginsPanel inline projects={ov.projects} home={home} onChange={reload} />
            </div>
          </ScrollArea>
        </TabsContent>
        <TabsContent value="editor" className="flex min-h-0 flex-1 flex-col">
          <Skills sel={skillKey} home={home} q="" onPick={setSkillKey} onAsk={onAsk} />
        </TabsContent>
      </Tabs>
      {dialog}
    </div>
  )
}
