import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useState } from "react"
import { Download, Loader2, Power, RefreshCw, Search, Trash2 } from "lucide-react"
import { api, type PluginAvail, type PluginInst } from "@/lib/api"
import { tilde } from "@/lib/format"
import { reportResult } from "@/lib/report"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

type Project = { dir: string; project: string }

// Plugins de Claude Code : installés (activer, désactiver, mettre à jour, désinstaller) et installables depuis les marketplaces,
// pour l'utilisateur ou pour un projet. Tout passe par `claude plugin` côté serveur.
export function PluginsDialog({ open, onClose, projects, home, onChange }: { open: boolean; onClose: () => void; projects: Project[]; home: string; onChange: () => void }) {
  const { t } = useTranslation()
  const [q, setQ] = useState("")
  const [scope, setScope] = useState("user") // "user" ou "p/<dossier>"
  const [data, setData] = useState<{ installed: PluginInst[]; available: PluginAvail[]; error?: string } | null>(null)
  const [busy, setBusy] = useState("")
  const load = useCallback((query: string) => api.plugins(query).then((r) => setData({ installed: r.installed ?? [], available: r.available ?? [], error: r.error })), [])
  useEffect(() => {
    if (!open) return
    const h = setTimeout(() => load(q), q ? 300 : 0)
    return () => clearTimeout(h)
  }, [open, q, load])

  const run = async (key: string, action: string, id: string, sc: string, project: string) => {
    setBusy(key)
    const r = await api.post("/plugin-act", { action, id, scope: sc, project })
    setBusy("")
    reportResult(r)
    if (r.ok) {
      await load(q)
      onChange()
    }
  }
  const target = scope === "user" ? { scope: "user", project: "" } : { scope: "project", project: scope.slice(2) }
  const dirOf = (p: PluginInst) => (p.scope === "user" ? "" : (projects.find((x) => x.project === p.project)?.dir ?? null))
  const scopeLabel = (p: PluginInst) => (p.scope === "user" ? t("skills.plugins.scopeUser") : tilde(p.project ?? "", home))

  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex max-h-[85vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("skills.plugins.title")}</DialogTitle>
          <DialogDescription>{t("skills.plugins.description")}</DialogDescription>
        </DialogHeader>
        <div className="flex flex-wrap items-center gap-2">
          <div className="relative min-w-48 flex-1">
            <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
            <Input value={q} onChange={(e) => setQ(e.target.value)} placeholder={t("skills.plugins.search")} className="pl-8" />
          </div>
          <Choose value={scope} onChange={setScope} options={[["user", t("skills.plugins.scopeUser")], ...projects.map((p) => [`p/${p.dir}`, tilde(p.project, home)] as const)]} label={t("skills.plugins.installFor")} />
        </div>
        <ScrollArea className="min-h-0 flex-1">
          {!data && <Loader2 className="m-6 size-5 animate-spin text-muted-foreground" />}
          {data?.error && <p className="p-4 text-sm text-destructive">{data.error}</p>}
          {data && !data.error && (
            <div className="space-y-4 pr-3">
              {!q && (
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
              <section>
                <h3 className="mb-1 text-xs font-semibold text-muted-foreground">{t("skills.plugins.available")}</h3>
                <ul className="divide-y rounded-md border">
                  {data.available.map((p) => (
                    <li key={p.id} className="flex items-center gap-2 px-3 py-1.5 text-sm">
                      <div className="min-w-0 flex-1">
                        <div className="truncate font-medium">
                          {p.name} <span className="font-normal text-muted-foreground">@{p.marketplace}</span>
                        </div>
                        <div className="line-clamp-2 text-xs text-muted-foreground">{p.description}</div>
                      </div>
                      <span className="shrink-0 text-2xs text-muted-foreground tabular-nums" title={t("skills.plugins.installs")}>
                        {p.installs.toLocaleString()}
                      </span>
                      <Button size="xs" variant="outline" disabled={busy === p.id} onClick={() => run(p.id, "install", p.id, target.scope, target.project)}>
                        {busy === p.id ? <Loader2 className="animate-spin" /> : <Download />} {t("skills.plugins.install")}
                      </Button>
                    </li>
                  ))}
                  {data.available.length === 0 && <li className="px-3 py-6 text-center text-sm text-muted-foreground">{t("skills.plugins.none")}</li>}
                </ul>
              </section>
            </div>
          )}
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}
