import { useCallback, useEffect, useMemo, useState } from "react"
import { useTranslation } from "react-i18next"
import { Link } from "wouter"
import { Loader2, Plug } from "lucide-react"
import { api, type CfgOverview, type McpServer, type PluginInst, type Skill } from "@/lib/api"
import { tilde } from "@/lib/format"
import { href } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { Checkbox } from "@/components/ui/checkbox"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Choose } from "./Choose"
import { PluginsPanel } from "./PluginsDialog"
import { useActions } from "./actions"
import { useConfigEdit } from "./configEdit"

type Enabled = Record<string, boolean>

// Vue unique de ce que Claude Code charge : plugins (activables par portée), skills, hooks et serveurs MCP, pour l'utilisateur
// seul ou pour le contexte d'un projet (utilisateur, puis projet, puis local). Les écritures passent par /config-edit.
export function Extensions({ home }: { home: string }) {
  const { t } = useTranslation()
  const [ov, setOv] = useState<CfgOverview | null>(null)
  const [skills, setSkills] = useState<Skill[]>([])
  const [mcp, setMcp] = useState<McpServer[]>([])
  const [inst, setInst] = useState<PluginInst[]>([])
  const [err, setErr] = useState("")
  const [dir, setDir] = useState("") // "" : utilisateur seul
  const [tab, setTab] = useState("active")
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
  useEffect(() => void reload(), [reload])
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
  if (!ov) return <Skeleton className="m-6 h-40" />

  const files = { u: "u", p: dir && "p/" + dir, l: dir && "l/" + dir }
  const ids = [...new Set([...ov.plugins.map((p) => p.id), ...Object.values(doc).flatMap(Object.keys)])].sort()
  const eff = (id: string) => [files.l, files.p, "u"].map((f) => (f ? doc[f]?.[id] : undefined)).find((v) => v !== undefined)
  const version = (id: string) => inst.find((p) => p.id === id)?.version
  const has = (f: string) => !dir || f === "u" || f === "p/" + dir || f === "l/" + dir // fichier qui s'applique à ce contexte
  const hooks = ov.hooks.filter((h) => has(h.file))
  const mcpHere = mcp.filter((s) => s.scope === "user" || (dir && s.project && s.project === ov.projects.find((p) => p.dir === dir)?.project) || (dir && s.scope === "project"))
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
  const head = (
    <thead className="text-xs text-muted-foreground">
      <tr>
        <th className="py-1 text-left font-normal" />
        {(["u", "p", "l"] as const).map((k) => (
          <th key={k} className="w-20 py-1 font-normal">{scopeName[k]}</th>
        ))}
        <th className="w-24 py-1 font-normal">{t("extensions.effective")}</th>
      </tr>
    </thead>
  )
  const sect = "text-md font-semibold border-b pb-1 mb-2 flex items-center gap-2"

  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto max-w-5xl px-3 py-6 md:px-6">
        <Tabs value={tab} onValueChange={setTab}>
          <TabsList>
            <TabsTrigger value="active">{t("extensions.tabActive")}</TabsTrigger>
            <TabsTrigger value="install">{t("extensions.tabInstall")}</TabsTrigger>
          </TabsList>
          <TabsContent value="active" className="space-y-8 pt-4">
        <div className="flex flex-wrap items-center gap-2 rounded-md border bg-muted/30 px-3 py-2 text-sm">
          {t("extensions.context")}
          <Choose value={dir} onChange={setDir} label={t("extensions.context")} className="min-w-0 max-w-md" options={[["", t("extensions.contextUser")], ...ov.projects.map((p) => [p.dir, tilde(p.project, home)] as const)]} />
          <span className="text-xs text-muted-foreground">{t("extensions.contextHint")}</span>
        </div>

        <section>
          <h3 className={sect}>
            {t("extensions.plugins")} ({ids.length})
            <Button size="xs" variant="outline" className="ml-auto" onClick={() => setTab("install")}>
              <Plug /> {t("extensions.manage")}
            </Button>
          </h3>
          {err && <p className="mb-2 text-xs text-warning">{t("extensions.loadError", { err })}</p>}
          <table className="w-full text-sm">
            <thead className="text-xs text-muted-foreground">
              <tr>
                <th className="py-1 text-left font-normal" />
                {(["u", "p", "l"] as const).map((k) => (
                  <th key={k} className="w-20 py-1 font-normal">{scopeName[k]}</th>
                ))}
                <th className="w-24 py-1 font-normal">{t("extensions.effective")}</th>
              </tr>
            </thead>
            <tbody className="divide-y">
              {ids.map((id) => (
                <tr key={id} className="hover:bg-muted/50">
                  <td className="min-w-0 py-1 pr-2">
                    <span className="break-all">{id}</span> {version(id) && <span className="text-xs text-muted-foreground">{version(id)}</span>}
                    {!ov.plugins.find((p) => p.id === id)?.installed && !version(id) && <span className="ml-2 text-xs text-warning">{t("extensions.notInstalled")}</span>}
                  </td>
                  {(["u", "p", "l"] as const).map((k) => {
                    const f = files[k]
                    return (
                      <td key={k} className="text-center">
                        {f ? (
                          <Checkbox
                            checked={doc[f]?.[id] === true}
                            className={doc[f]?.[id] === undefined ? "opacity-50" : undefined}
                            aria-label={t("extensions.toggle", { id, scope: scopeName[k] })}
                            onCheckedChange={(c) => propose({ file: f, op: "set", path: ["enabledPlugins", id], value: c === true })}
                          />
                        ) : (
                          <span className="text-muted-foreground">·</span>
                        )}
                      </td>
                    )
                  })}
                  <td className={`text-center text-xs ${eff(id) ? "text-success" : "text-muted-foreground"}`}>{eff(id) === undefined ? t("extensions.unset") : eff(id) ? t("extensions.on") : t("extensions.off")}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!ids.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noPlugins")}</p>}
          <p className="mt-2 text-xs text-muted-foreground">{t("extensions.sessionNote")}</p>
        </section>

        <section>
          <h3 className={sect}>
            {t("extensions.skills")} <span className="text-xs font-normal text-muted-foreground">{t("extensions.skillsTotal", { count: skillsHere.length, chars })}</span>
          </h3>
          <table className="w-full text-sm">
            {head}
            <tbody className="divide-y">
              {skillsHere.map((s) => (
                <tr key={s.key} className="hover:bg-muted/50">
                  <td className="min-w-0 py-1 pr-2">
                    <Link href={href({ kind: "skills", key: s.key })} className="break-all hover:underline">{s.name}</Link>{" "}
                    <span className="text-xs text-muted-foreground">{s.plugin ? t("extensions.fromPlugin", { name: s.plugin }) : s.scope === "project" ? t("extensions.fromProject") : t("extensions.fromUser")}</span>
                    {(s.manual || s.hidden) && <span className="ml-2 text-xs text-warning">{s.manual ? t("extensions.manual") : t("extensions.hidden")}</span>}
                  </td>
                  {(["u", "p", "l"] as const).map((k) => {
                    const f = files[k]
                    const m = f && skillMode(f, s.name)
                    return (
                      <td key={k} className="text-center">
                        {f && !s.plugin ? (
                          <Checkbox
                            checked={m !== "off"}
                            className={m === undefined ? "opacity-50" : undefined}
                            aria-label={t("extensions.toggle", { id: s.name, scope: scopeName[k] })}
                            onCheckedChange={(c) => propose(c === true ? { file: f, op: "delete", path: ["skillOverrides", s.name] } : { file: f, op: "set", path: ["skillOverrides", s.name], value: "off" })}
                          />
                        ) : (
                          <span className="text-muted-foreground">·</span>
                        )}
                      </td>
                    )
                  })}
                  <td className={`text-center text-xs ${skillEff(s.name) === "off" ? "text-muted-foreground" : "text-success"}`}>{skillEff(s.name) === "on" ? t("extensions.on") : skillEff(s.name) === "off" ? t("extensions.off") : skillEff(s.name)}</td>
                </tr>
              ))}
            </tbody>
          </table>
          {!skillsHere.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noSkills")}</p>}
        </section>

        <section>
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

        <section>
          <h3 className={sect}>
            {t("extensions.mcp")} ({mcpHere.length + others.length}){!status && <Loader2 className="size-3.5 animate-spin text-muted-foreground" />}
            <Link href={href({ kind: "config", tab: "mcp" })} className="ml-auto text-xs font-normal text-muted-foreground underline">MCP</Link>
          </h3>
          <table className="w-full text-sm">
            {head}
            <tbody className="divide-y">
              {mcpHere.map((m, n) => (
                <tr key={n} className="hover:bg-muted/50 align-top">
                  <td className="min-w-0 py-1 pr-2">
                    {status && <span className={up(m.name) ? "text-success" : "text-warning"} title={status[m.name]}>● </span>}<span className="font-medium">{m.name}</span> <span className="text-xs text-muted-foreground">{m.type} · {m.scope === "user" ? scopeName.u : m.scope === "project" ? ".mcp.json" : scopeName.l}</span>
                    <code className="block text-xs break-all text-muted-foreground">{m.url || m.command}</code>
                  </td>
                  {(["u", "p", "l"] as const).map((k) => {
                    const f = files[k]
                    return (
                      <td key={k} className="pt-1.5 text-center">
                        {f && m.scope === "project" ? (
                          <Checkbox
                            checked={!mcpOff(f, m.name)}
                            aria-label={t("extensions.toggle", { id: m.name, scope: scopeName[k] })}
                            onCheckedChange={(c) => propose({ file: f, op: c === true ? "remove" : "add", path: ["disabledMcpjsonServers"], value: m.name })}
                          />
                        ) : (
                          <span className="text-muted-foreground">·</span>
                        )}
                      </td>
                    )
                  })}
                  <td className={`pt-1.5 text-center text-xs ${m.scope !== "project" ? "text-muted-foreground" : mcpEff(m.name) ? "text-muted-foreground" : "text-success"}`}>{m.scope !== "project" ? "—" : mcpEff(m.name) ? t("extensions.off") : t("extensions.on")}</td>
                </tr>
              ))}
              {others.map((n) => (
                <tr key={n} className="hover:bg-muted/50">
                  <td className="py-1 pr-2">
                    <span className={up(n) ? "text-success" : "text-warning"} title={status?.[n]}>● </span>
                    <span className="font-medium">{n}</span>{" "}
                    <span className="text-xs text-muted-foreground">{n.startsWith("plugin:") ? t("extensions.mcpPlugin") : n.startsWith("claude.ai ") ? t("extensions.mcpAccount") : t("extensions.mcpOther")}</span>
                  </td>
                  {[0, 1, 2].map((i) => (
                    <td key={i} className="text-center text-muted-foreground">·</td>
                  ))}
                  <td className="text-center text-xs text-muted-foreground">—</td>
                </tr>
              ))}
            </tbody>
          </table>
          {(others.length > 0 || mcpHere.some((m) => m.scope !== "project")) && <p className="mt-2 text-xs text-muted-foreground">{t("extensions.mcpManaged")}</p>}
          <p className="mt-1 text-xs text-muted-foreground">{t("extensions.offWins")}</p>
          {!mcpHere.length && !others.length && <p className="py-3 text-sm text-muted-foreground">{t("extensions.noMcp")}</p>}
        </section>
          </TabsContent>
          <TabsContent value="install" className="pt-4">
            <PluginsPanel inline projects={ov.projects} home={home} onChange={reload} />
          </TabsContent>
        </Tabs>
      </div>
      {dialog}
    </ScrollArea>
  )
}
