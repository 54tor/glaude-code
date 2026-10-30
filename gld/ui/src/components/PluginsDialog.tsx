import { useTranslation } from "react-i18next"
import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import {
  ChevronRight,
  Download,
  Fan,
  Plus,
  Power,
  RefreshCw,
  Search,
  Trash2,
} from "lucide-react"
import {
  api,
  type Marketplace,
  type PluginAvail,
  type PluginInst,
} from "@/lib/api"
import { tilde } from "@/lib/format"
import { reportAction } from "@/lib/report"
import { errMsg } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { ScrollArea } from "@/components/ui/scroll-area"
import { Choose } from "./Choose"

const SOURCE_OK = /^([\w.-]+\/[\w.-]+|https:\/\/\S+)$/

type Project = { dir: string; project: string }

// Plugins de Claude Code : installés (activer, désactiver, mettre à jour, désinstaller) et installables depuis les marketplaces,
// pour l'utilisateur ou pour un projet. Tout passe par `claude plugin` côté serveur.
export function PluginsDialog({
  open,
  onClose,
  projects,
  home,
  onChange,
}: {
  open: boolean
  onClose: () => void
  projects: Project[]
  home: string
  onChange: () => void
}) {
  const { t } = useTranslation()
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="flex h-[85vh] max-h-[85vh] flex-col sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("skills.plugins.title")}</DialogTitle>
          <DialogDescription>
            {t("skills.plugins.description")}
          </DialogDescription>
        </DialogHeader>
        <ScrollArea className="min-h-0 flex-1">
          <PluginsPanel projects={projects} home={home} onChange={onChange} />
        </ScrollArea>
      </DialogContent>
    </Dialog>
  )
}

export const pluginKey = (p: PluginInst) =>
  `${p.id}|${p.scope}|${p.project ?? ""}`

// Actions sur une installation : le projet est désigné par son dossier, null quand ce projet n'est plus connu.
export function usePluginRun(projects: Project[], after: () => unknown) {
  const [busy, setBusy] = useState("")
  const dirOf = (p: PluginInst) =>
    p.scope === "user"
      ? ""
      : (projects.find((x) => x.project === p.project)?.dir ?? null)
  const run = async (
    key: string,
    action: Parameters<typeof api.pluginAct>[0],
    id: string,
    scope: string,
    project: string
  ) => {
    setBusy(key)
    const ok = await reportAction(() =>
      api.pluginAct(action, id, scope, project)
    )
    setBusy("")
    if (ok) await after()
  }
  return { busy, dirOf, run }
}

// Installés puis disponibles sur le même panneau (dans la fenêtre ouverte depuis l'éditeur de skills).
export function PluginsPanel({
  projects,
  home,
  onChange,
}: {
  projects: Project[]
  home: string
  onChange: () => void
}) {
  const { t } = useTranslation()
  const [inst, setInst] = useState<PluginInst[] | null>(null)
  const [scope, setScope] = useState("user")
  const load = useCallback(
    () =>
      api.plugins().then(
        (r) => setInst(r.installed ?? []),
        () => setInst([])
      ),
    []
  )
  useEffect(() => void load(), [load])
  const changed = () => (load(), onChange())
  const { busy, dirOf, run } = usePluginRun(projects, changed)
  const scopeLabel = (p: PluginInst) =>
    p.scope === "user"
      ? t("skills.plugins.scopeUser")
      : tilde(p.project ?? "", home)
  return (
    <div className="space-y-4 pr-3">
      <section>
        <h3 className="mb-1 text-xs font-semibold text-muted-foreground">
          {t("skills.plugins.installed", { count: inst?.length ?? 0 })}
        </h3>
        {!inst ? (
          <Fan className="m-3 size-4 animate-spin text-muted-foreground" />
        ) : (
          <ul className="divide-y rounded-md border">
            {inst.map((p) => {
              const dir = dirOf(p)
              const key = pluginKey(p)
              const off = busy === key || dir === null
              return (
                <li
                  key={key}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{p.id}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      v{p.version} · {scopeLabel(p)}
                    </div>
                  </div>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={off}
                    title={t(
                      p.enabled
                        ? "skills.plugins.disable"
                        : "skills.plugins.enable"
                    )}
                    aria-label={t(
                      p.enabled
                        ? "skills.plugins.disable"
                        : "skills.plugins.enable"
                    )}
                    onClick={() =>
                      run(
                        key,
                        p.enabled ? "disable" : "enable",
                        p.id,
                        p.scope,
                        dir ?? ""
                      )
                    }
                  >
                    <Power
                      className={
                        p.enabled ? "text-success" : "text-muted-foreground"
                      }
                    />
                  </Button>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={off}
                    title={t("skills.plugins.update")}
                    aria-label={t("skills.plugins.update")}
                    onClick={() => run(key, "update", p.id, p.scope, dir ?? "")}
                  >
                    <RefreshCw />
                  </Button>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={off}
                    title={t("skills.plugins.uninstall")}
                    aria-label={t("skills.plugins.uninstall")}
                    onClick={() =>
                      run(key, "uninstall", p.id, p.scope, dir ?? "")
                    }
                  >
                    <Trash2 />
                  </Button>
                </li>
              )
            })}
          </ul>
        )}
      </section>
      <PluginsCatalog
        projects={projects}
        home={home}
        scope={scope}
        onScope={setScope}
        installed={inst ?? undefined}
        onChange={changed}
      />
    </div>
  )
}

// Ce qui s'installe : recherche dans les marketplaces, groupée par marketplace, puis la gestion des marketplaces.
// scope : "user" ou "p/<dossier>" ; sans onScope, la portée est imposée par la page (le contexte affiché).
// query : recherche portée par le parent ; le champ local disparaît alors.
export function PluginsCatalog({
  projects,
  home,
  scope,
  onScope,
  installed,
  onChange,
  query,
}: {
  projects: Project[]
  home: string
  scope: string
  onScope?: (s: string) => void
  installed?: PluginInst[]
  onChange: () => void
  query?: string
}) {
  const { t } = useTranslation()
  const [own, setQ] = useState("")
  const q = query ?? own
  const [data, setData] = useState<{
    available: PluginAvail[]
    error?: string
  } | null>(null)
  const [busy, setBusy] = useState("")
  const [markets, setMarkets] = useState<Marketplace[]>([])
  const [src, setSrc] = useState("")
  const loadMarkets = useCallback(
    () =>
      api.marketplaces().then(
        (r) => setMarkets(r.marketplaces ?? []),
        () => {}
      ),
    []
  )
  const seq = useRef(0) // seule la dernière recherche s'affiche : une réponse lente pour « a » n'écrase pas celle de « ab »
  const load = useCallback((query: string) => {
    const n = ++seq.current
    return api.plugins(query).then(
      (r) => {
        if (n === seq.current) setData({ available: r.available ?? [] })
      },
      (e) => {
        if (n === seq.current) setData({ available: [], error: errMsg(e) })
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

  const install = async (id: string) => {
    setBusy(id)
    const target = scope === "user" ? ["user", ""] : ["project", scope.slice(2)]
    const ok = await reportAction(() =>
      api.pluginAct("install", id, target[0], target[1])
    )
    setBusy("")
    if (ok) {
      await load(q)
      onChange()
    }
  }
  const market = async (
    key: string,
    action: Parameters<typeof api.marketAct>[0],
    source = "",
    name = ""
  ) => {
    setBusy(key)
    const ok = await reportAction(() => api.marketAct(action, source, name))
    setBusy("")
    if (ok) {
      if (action === "add") setSrc("")
      await Promise.all([loadMarkets(), load(q)])
      onChange()
    }
  }
  // La liste des installés vient du parent quand il l'a : elle suit ses désinstallations sans relancer la recherche.
  const ids = useMemo(
    () => (installed ? new Set(installed.map((p) => p.id)) : null),
    [installed]
  )
  const byMarket = useMemo(() => {
    const g = new Map<string, PluginAvail[]>()
    for (const p of data?.available ?? [])
      if (!(ids ? ids.has(p.id) : p.installed))
        g.set(p.marketplace, [...(g.get(p.marketplace) ?? []), p])
    return [...g.entries()]
  }, [data, ids])
  const scopes = [
    ["user", t("skills.plugins.scopeUser")],
    ...projects.map((p) => [`p/${p.dir}`, tilde(p.project, home)] as const),
  ] as const
  const count = byMarket.reduce((n, [, l]) => n + l.length, 0)

  return (
    <section className="space-y-2">
      <h4 className="flex flex-wrap items-center gap-2 text-sm font-medium">
        {t("skills.plugins.available")}
        {data && !data.error && (
          <span className="text-xs font-normal text-muted-foreground">
            ({count})
          </span>
        )}
      </h4>
      <div className="flex flex-wrap items-center gap-2">
        {query === undefined && (
          <div className="relative min-w-48 flex-1">
            <Search className="absolute top-2.5 left-2.5 size-4 text-muted-foreground" />
            <Input
              value={q}
              onChange={(e) => setQ(e.target.value)}
              placeholder={t("skills.plugins.search")}
              aria-label={t("skills.plugins.search")}
              className="pl-8"
            />
          </div>
        )}
        {onScope ? (
          <Choose
            value={scope}
            onChange={onScope}
            options={scopes}
            label={t("skills.plugins.installFor")}
          />
        ) : (
          <span className="text-xs text-muted-foreground">
            {t("skills.plugins.installFor")} :{" "}
            <span className="font-medium text-foreground">
              {scopes.find(([k]) => k === scope)?.[1]}
            </span>
          </span>
        )}
      </div>
      {!data && (
        <Fan className="m-3 size-4 animate-spin text-muted-foreground" />
      )}
      {data?.error && <p className="text-sm text-destructive">{data.error}</p>}
      {data && !data.error && (
        <div className="space-y-2">
          {byMarket.map(([mk, list], i) => (
            <details
              key={mk}
              open={i === 0 || !!q}
              className="group rounded-md border"
            >
              <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm [&::-webkit-details-marker]:hidden">
                <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
                <span className="truncate font-medium">{mk}</span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                  {list.length}
                </span>
              </summary>
              <ul className="divide-y border-t">
                {list.map((p) => (
                  <li
                    key={p.id}
                    className="flex items-center gap-2 px-3 py-1.5 text-sm"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{p.name}</div>
                      <div className="line-clamp-2 text-xs text-muted-foreground">
                        {p.description}
                      </div>
                    </div>
                    <span
                      className="shrink-0 text-2xs text-muted-foreground tabular-nums"
                      title={t("skills.plugins.installs")}
                    >
                      {p.installs.toLocaleString()}
                    </span>
                    <Button
                      size="xs"
                      variant="outline"
                      disabled={busy === p.id}
                      onClick={() => install(p.id)}
                    >
                      {busy === p.id ? (
                        <Fan className="animate-spin" />
                      ) : (
                        <Download />
                      )}{" "}
                      {t("skills.plugins.install")}
                    </Button>
                  </li>
                ))}
              </ul>
            </details>
          ))}
          {count === 0 && (
            <p className="rounded-md border px-3 py-6 text-center text-sm text-muted-foreground">
              {t("skills.plugins.none")}
            </p>
          )}
        </div>
      )}
      <details className="group rounded-md border">
        <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm [&::-webkit-details-marker]:hidden">
          <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
          <span className="font-medium">
            {t("skills.plugins.marketplaces", { count: markets.length })}
          </span>
        </summary>
        <div className="space-y-2 border-t p-3">
          <div className="flex justify-end">
            <Button
              size="xs"
              variant="ghost"
              disabled={busy === "mk:*"}
              onClick={() => market("mk:*", "update")}
            >
              {busy === "mk:*" ? (
                <Fan className="animate-spin" />
              ) : (
                <RefreshCw />
              )}{" "}
              {t("skills.plugins.marketUpdateAll")}
            </Button>
          </div>
          <ul className="divide-y rounded-md border">
            {markets.map((m) => (
              <li
                key={m.name}
                className="flex items-center gap-2 px-3 py-1.5 text-sm"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{m.name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {m.repo ?? m.url ?? m.source}
                  </div>
                </div>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy === "mk:" + m.name}
                  title={t("skills.plugins.marketUpdate")}
                  aria-label={t("skills.plugins.marketUpdate")}
                  onClick={() => market("mk:" + m.name, "update", "", m.name)}
                >
                  <RefreshCw />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy === "mk:" + m.name}
                  title={t("skills.plugins.marketRemove")}
                  aria-label={t("skills.plugins.marketRemove")}
                  onClick={() => market("mk:" + m.name, "remove", "", m.name)}
                >
                  <Trash2 />
                </Button>
              </li>
            ))}
          </ul>
          <form
            className="flex gap-2"
            onSubmit={(e) => {
              e.preventDefault()
              if (SOURCE_OK.test(src.trim()))
                market("mk:add", "add", src.trim())
            }}
          >
            <Input
              value={src}
              onChange={(e) => setSrc(e.target.value)}
              placeholder={t("skills.plugins.marketAdd")}
              aria-label={t("skills.plugins.marketAdd")}
            />
            <Button
              type="submit"
              size="sm"
              variant="outline"
              disabled={busy === "mk:add" || !SOURCE_OK.test(src.trim())}
            >
              {busy === "mk:add" ? <Fan className="animate-spin" /> : <Plus />}{" "}
              {t("skills.plugins.marketAddBtn")}
            </Button>
          </form>
        </div>
      </details>
    </section>
  )
}
