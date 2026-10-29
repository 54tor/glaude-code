import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { Download, Fan, Plus, Power, RefreshCw, Search, Trash2 } from "lucide-react"
import { api, type Marketplace, type PluginAvail, type PluginInst } from "@/lib/api"
import { tilde } from "@/lib/format"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Choose } from "./Choose"

const SOURCE_OK = /^([\w.-]+\/[\w.-]+|https:\/\/\S+)$/

type Project = { dir: string; project: string }

// Plugins de Claude Code : installés (activer, désactiver, mettre à jour, désinstaller) et installables depuis les marketplaces,
// pour l'utilisateur ou pour un projet. Tout passe par `claude plugin` côté serveur.
export function PluginsDialog({ open, onClose, projects, home, onChange }: { open: boolean; onClose: () => void; projects: Project[]; home: string; onChange: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[85vh] max-h-[85vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("skills.plugins.title")}</DialogTitle>
          <DialogDescription>{t("skills.plugins.description")}</DialogDescription>
        </DialogHeader>
        <PluginsPanel projects={projects} home={home} onChange={onChange} />
      </DialogContent>
    </Dialog>
  )
}

// Le corps de la fenêtre, utilisable seul : dans la page Extensions (inline : défile avec la page).
export function PluginsPanel({ projects, home, onChange, inline }: { projects: Project[]; home: string; onChange: () => void; inline?: boolean }) {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [sub, setSub] = useState("available") // available | installed | markets
  const [scope, setScope] = useState("user") // "user" ou "p/<dossier>"
  const [data, setData] = useState<{ installed: PluginInst[]; available: PluginAvail[]; error?: string } | null>(null)
  const [busy, setBusy] = useState("")
  const [markets, setMarkets] = useState<Marketplace[]>([])
  const [src, setSrc] = useState("")
  const loadMarkets = useCallback(() => api.marketplaces().then((r) => setMarkets(r.marketplaces ?? []), () => {}), [])
  const seq = useRef(0) // seule la dernière recherche s'affiche : une réponse lente pour « a » n'écrase pas celle de « ab »
  const load = useCallback((query: string) => {
    const n = ++seq.current
    return api.plugins(query).then(
      (r) => {
        if (n === seq.current) setData({ installed: r.installed ?? [], available: r.available ?? [] })
      },
      (e) => {
        if (n === seq.current) setData({ installed: [], available: [], error: errMsg(e) })
      }
    )
  }, [])
  useEffect(() => {
    loadMarkets()
  }, [loadMarkets])
  useEffect(() => {
    const h = setTimeout(() => load(q), q ? 300 : 0)
    return () => clearTimeout(h)
  }, [q, load])

  const run = async (key: string, action: Parameters<typeof api.pluginAct>[0], id: string, sc: string, project: string) => {
    setBusy(key)
    const ok = await reportAction(() => api.pluginAct(action, id, sc, project))
    setBusy("")
    if (ok) {
      await load(q)
      onChange()
    }
  }
  const market = async (key: string, action: Parameters<typeof api.marketAct>[0], source = "", name = "") => {
    setBusy(key)
    const ok = await reportAction(() => api.marketAct(action, source, name))
    setBusy("")
    if (ok) {
      if (action === "add") setSrc("")
      await Promise.all([loadMarkets(), load(q)])
      onChange()
    }
  }
  const byMarket = useMemo(() => {
    const g = new Map<string, PluginAvail[]>()
    for (const p of data?.available ?? []) g.set(p.marketplace, [...(g.get(p.marketplace) ?? []), p])
    return [...g.entries()]
  }, [data])
  const target = scope === "user" ? { scope: "user", project: "" } : { scope: "project", project: scope.slice(2) }
  const dirOf = (p: PluginInst) => (p.scope === "user" ? "" : (projects.find((x) => x.project === p.project)?.dir ?? null))
  const scopeLabel = (p: PluginInst) => (p.scope === "user" ? t("skills.plugins.scopeUser") : tilde(p.project ?? "", home))

  const body = (
    <>
          {!data && <Fan className="m-6 size-5 animate-spin text-muted-foreground" />}
          {data?.error && <p className="p-4 text-sm text-destructive">{data.error}</p>}
          {data && !data.error && (
            <div className="space-y-4 pr-3">
              {sub === "markets" && (
                <section>
                  <h3 className="mb-1 flex items-center text-xs font-semibold text-muted-foreground">
                    <span className="flex-1">{t("skills.plugins.marketplaces", { count: markets.length })}</span>
                    <Button size="xs" variant="ghost" disabled={busy === "mk:*"} onClick={() => market("mk:*", "update")}>
                      {busy === "mk:*" ? <Fan className="animate-spin" /> : <RefreshCw />} {t("skills.plugins.marketUpdateAll")}
                    </Button>
                  </h3>
                  <ul className="divide-y rounded-md border">
                    {markets.map((m) => (
                      <li key={m.name} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                        <div className="min-w-0 flex-1">
                          <div className="truncate font-medium">{m.name}</div>
                          <div className="truncate text-xs text-muted-foreground">{m.repo ?? m.url ?? m.source}</div>
                        </div>
                        <Button size="icon-xs" variant="ghost" disabled={busy === "mk:" + m.name} title={t("skills.plugins.marketUpdate")} onClick={() => market("mk:" + m.name, "update", "", m.name)}>
                          <RefreshCw />
                        </Button>
                        <Button size="icon-xs" variant="ghost" disabled={busy === "mk:" + m.name} title={t("skills.plugins.marketRemove")} onClick={() => market("mk:" + m.name, "remove", "", m.name)}>
                          <Trash2 />
                        </Button>
                      </li>
                    ))}
                  </ul>
                  <form
                    className="mt-2 flex gap-2"
                    onSubmit={(e) => {
                      e.preventDefault()
                      if (SOURCE_OK.test(src.trim())) market("mk:add", "add", src.trim())
                    }}
                  >
                    <Input value={src} onChange={(e) => setSrc(e.target.value)} placeholder={t("skills.plugins.marketAdd")} />
                    <Button type="submit" size="sm" variant="outline" disabled={busy === "mk:add" || !SOURCE_OK.test(src.trim())}>
                      {busy === "mk:add" ? <Fan className="animate-spin" /> : <Plus />} {t("skills.plugins.marketAddBtn")}
                    </Button>
                  </form>
                </section>
              )}
              {sub === "installed" && (
                <section>
                  <h3 className="mb-1 text-xs font-semibold text-muted-foreground">{t("skills.plugins.installed", { count: data.installed.length })}</h3>
                  <ul className="divide-y rounded-md border">
                    {data.installed.map((p) => {
                      const dir = dirOf(p)
                      const key = `${p.id}|${p.scope}|${p.project ?? ""}`
                      const off = busy === key || dir === null
                      return (
                        <li key={key} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                          <div className="min-w-0 flex-1">
                            <div className="truncate font-medium">{p.id}</div>
                            <div className="truncate text-xs text-muted-foreground">
                              v{p.version} · {scopeLabel(p)}
                            </div>
                          </div>
                          <Button size="icon-xs" variant="ghost" disabled={off} title={t(p.enabled ? "skills.plugins.disable" : "skills.plugins.enable")} onClick={() => run(key, p.enabled ? "disable" : "enable", p.id, p.scope, dir ?? "")}>
                            <Power className={p.enabled ? "text-success" : "text-muted-foreground"} />
                          </Button>
                          <Button size="icon-xs" variant="ghost" disabled={off} title={t("skills.plugins.update")} onClick={() => run(key, "update", p.id, p.scope, dir ?? "")}>
                            <RefreshCw />
                          </Button>
                          <Button size="icon-xs" variant="ghost" disabled={off} title={t("skills.plugins.uninstall")} onClick={() => run(key, "uninstall", p.id, p.scope, dir ?? "")}>
                            <Trash2 />
                          </Button>
                        </li>
                      )
                    })}
                  </ul>
                </section>
              )}
              {sub === "available" && (
              <section>
                <h3 className="mb-1 text-xs font-semibold text-muted-foreground">{t("skills.plugins.available")}</h3>
                {byMarket.map(([mk, list]) => (
                  <div key={mk} className="mb-3">
                    <h4 className="mb-1 flex items-center gap-2 text-sm font-medium">
                      {mk} <span className="text-xs font-normal text-muted-foreground">({list.length})</span>
                    </h4>
                    <ul className="divide-y rounded-md border">
                      {list.map((p) => (
                        <li key={p.id} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                          <div className="min-w-0 flex-1">
                            <div className="truncate font-medium">{p.name}</div>
                            <div className="line-clamp-2 text-xs text-muted-foreground">{p.description}</div>
                          </div>
                          <span className="shrink-0 text-2xs text-muted-foreground tabular-nums" title={t("skills.plugins.installs")}>
                            {p.installs.toLocaleString()}
                          </span>
                          <Button size="xs" variant="outline" disabled={busy === p.id} onClick={() => run(p.id, "install", p.id, target.scope, target.project)}>
                            {busy === p.id ? <Fan className="animate-spin" /> : <Download />} {t("skills.plugins.install")}
                          </Button>
                        </li>
                      ))}
                    </ul>
                  </div>
                ))}
                {data.available.length === 0 && <p className="rounded-md border px-3 py-6 text-center text-sm text-muted-foreground">{t("skills.plugins.none")}</p>}
              </section>
              )}
            </div>
          )}
    </>
  )
  return (
    <>
        <Tabs value={sub} onValueChange={setSub} className="gap-0">
          <TabsList>
            <TabsTrigger value="available">{t("skills.plugins.tabAvailable")}</TabsTrigger>
            <TabsTrigger value="installed">{t("skills.plugins.tabInstalled", { count: data?.installed.length ?? 0 })}</TabsTrigger>
            <TabsTrigger value="markets">{t("skills.plugins.tabMarkets", { count: markets.length })}</TabsTrigger>
          </TabsList>
        </Tabs>
        {sub === "available" && <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-48 flex-1">
            <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("skills.plugins.search")} className="pl-8" />
          </div>
          <Choose value={scope} onChange={setScope} options={[["user", t("skills.plugins.scopeUser")], ...projects.map((p) => [`p/${p.dir}`, tilde(p.project, home)] as const)]} label={t("skills.plugins.installFor")} />
        </div>}
        {inline ? <div>{body}</div> : <ScrollArea className="min-h-0 flex-1">{body}</ScrollArea>}
    </>
  )
}
