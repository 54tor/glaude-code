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

const SEARCH_DEBOUNCE_MS = 300
const MARKET_KEY_PREFIX = "mk:"
const UPDATE_ALL_MARKETS_KEY = "mk:*"
const ADD_MARKET_KEY = "mk:add"
const PROJECT_SCOPE_PREFIX_LENGTH = "p/".length

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
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
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

export const pluginKey = (plugin: PluginInst) =>
  `${plugin.id}|${plugin.scope}|${plugin.project ?? ""}`

// Actions sur une installation : le projet est désigné par son dossier, null quand ce projet n'est plus connu.
export function usePluginRun(projects: Project[], after: () => unknown) {
  const [busy, setBusy] = useState("")
  const dirOf = (plugin: PluginInst) => {
    if (plugin.scope === "user") return ""
    return projects.find((candidate) => candidate.project === plugin.project)?.dir ?? null
  }
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
        (response) => setInst(response.installed ?? []),
        () => setInst([])
      ),
    []
  )
  useEffect(() => void load(), [load])
  const changed = () => {
    load()
    onChange()
  }
  const { busy, dirOf, run } = usePluginRun(projects, changed)
  const scopeLabel = (plugin: PluginInst) => {
    if (plugin.scope === "user") return t("skills.plugins.scopeUser")
    return tilde(plugin.project ?? "", home)
  }
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
            {inst.map((plugin) => {
              const dir = dirOf(plugin)
              const key = pluginKey(plugin)
              const isDisabled = busy === key || dir === null
              const toggleLabel = t(plugin.enabled ? "skills.plugins.disable" : "skills.plugins.enable")
              return (
                <li
                  key={key}
                  className="flex items-center gap-2 px-3 py-1.5 text-sm"
                >
                  <div className="min-w-0 flex-1">
                    <div className="truncate font-medium">{plugin.id}</div>
                    <div className="truncate text-xs text-muted-foreground">
                      v{plugin.version} · {scopeLabel(plugin)}
                    </div>
                  </div>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={isDisabled}
                    title={toggleLabel}
                    aria-label={toggleLabel}
                    onClick={() =>
                      run(
                        key,
                        plugin.enabled ? "disable" : "enable",
                        plugin.id,
                        plugin.scope,
                        dir ?? ""
                      )
                    }
                  >
                    <Power
                      className={
                        plugin.enabled ? "text-success" : "text-muted-foreground"
                      }
                    />
                  </Button>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={isDisabled}
                    title={t("skills.plugins.update")}
                    aria-label={t("skills.plugins.update")}
                    onClick={() => run(key, "update", plugin.id, plugin.scope, dir ?? "")}
                  >
                    <RefreshCw />
                  </Button>
                  <Button
                    size="icon-xs"
                    variant="ghost"
                    disabled={isDisabled}
                    title={t("skills.plugins.uninstall")}
                    aria-label={t("skills.plugins.uninstall")}
                    onClick={() =>
                      run(key, "uninstall", plugin.id, plugin.scope, dir ?? "")
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
      <Marketplaces onChange={changed} />
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
  onScope?: (scope: string) => void
  installed?: PluginInst[]
  onChange: () => void
  query?: string
}) {
  const { t } = useTranslation()
  const [ownQuery, setOwnQuery] = useState("")
  const searchQuery = query ?? ownQuery
  const [data, setData] = useState<{
    available: PluginAvail[]
    error?: string
  } | null>(null)
  const [busy, setBusy] = useState("")
  const seq = useRef(0) // seule la dernière recherche s'affiche : une réponse lente pour « a » n'écrase pas celle de « ab »
  const load = useCallback((query: string) => {
    const requestNumber = ++seq.current
    const isLatestRequest = () => requestNumber === seq.current
    return api.plugins(query).then(
      (response) => {
        if (isLatestRequest()) setData({ available: response.available ?? [] })
      },
      (searchError) => {
        if (isLatestRequest()) setData({ available: [], error: errMsg(searchError) })
      }
    )
  }, [])
  useEffect(() => {
    const debounceDelay = searchQuery ? SEARCH_DEBOUNCE_MS : 0
    const searchTimeout = setTimeout(() => load(searchQuery), debounceDelay)
    return () => clearTimeout(searchTimeout)
  }, [searchQuery, load])

  const install = async (id: string) => {
    setBusy(id)
    const [targetScope, targetProject] = scope === "user" ? ["user", ""] : ["project", scope.slice(PROJECT_SCOPE_PREFIX_LENGTH)]
    const ok = await reportAction(() =>
      api.pluginAct("install", id, targetScope, targetProject)
    )
    setBusy("")
    if (!ok) return
    await load(searchQuery)
    onChange()
  }
  // La liste des installés vient du parent quand il l'a : elle suit ses désinstallations sans relancer la recherche.
  const ids = useMemo(
    () => (installed ? new Set(installed.map((plugin) => plugin.id)) : null),
    [installed]
  )
  const byMarket = useMemo(() => {
    const pluginsByMarket = new Map<string, PluginAvail[]>()
    for (const plugin of data?.available ?? []) {
      const isInstalled = ids ? ids.has(plugin.id) : plugin.installed
      if (isInstalled) continue
      pluginsByMarket.set(plugin.marketplace, [...(pluginsByMarket.get(plugin.marketplace) ?? []), plugin])
    }
    return [...pluginsByMarket.entries()]
  }, [data, ids])
  const scopes = [
    ["user", t("skills.plugins.scopeUser")],
    ...projects.map((project) => [`p/${project.dir}`, tilde(project.project, home)] as const),
  ] as const
  const count = byMarket.reduce((total, [, marketPlugins]) => total + marketPlugins.length, 0)

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
              value={searchQuery}
              onChange={(event) => setOwnQuery(event.target.value)}
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
              {scopes.find(([scopeKey]) => scopeKey === scope)?.[1]}
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
          {byMarket.map(([marketName, marketPlugins], marketIndex) => (
            <details
              key={marketName}
              open={marketIndex === 0 || !!searchQuery}
              className="group rounded-md border"
            >
              <summary className="flex cursor-pointer list-none items-center gap-2 px-3 py-2 text-sm [&::-webkit-details-marker]:hidden">
                <ChevronRight className="size-4 shrink-0 text-muted-foreground transition-transform group-open:rotate-90" />
                <span className="truncate font-medium">{marketName}</span>
                <span className="ml-auto text-xs text-muted-foreground tabular-nums">
                  {marketPlugins.length}
                </span>
              </summary>
              <ul className="divide-y border-t">
                {marketPlugins.map((plugin) => (
                  <li
                    key={plugin.id}
                    className="flex items-center gap-2 px-3 py-1.5 text-sm"
                  >
                    <div className="min-w-0 flex-1">
                      <div className="truncate font-medium">{plugin.name}</div>
                      <div className="line-clamp-2 text-xs text-muted-foreground">
                        {plugin.description}
                      </div>
                    </div>
                    <span
                      className="shrink-0 text-2xs text-muted-foreground tabular-nums"
                      title={t("skills.plugins.installs")}
                    >
                      {plugin.installs.toLocaleString()}
                    </span>
                    <Button
                      size="xs"
                      variant="outline"
                      disabled={busy === plugin.id}
                      onClick={() => install(plugin.id)}
                    >
                      {busy === plugin.id ? (
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
    </section>
  )
}

// Gestion des marketplaces : liste, mise à jour, retrait et ajout d'une source.
export function Marketplaces({ onChange }: { onChange: () => void }) {
  const { t } = useTranslation()
  const [busy, setBusy] = useState("")
  const [markets, setMarkets] = useState<Marketplace[]>([])
  const [src, setSrc] = useState("")
  const loadMarkets = useCallback(
    () =>
      api.marketplaces().then(
        (response) => setMarkets(response.marketplaces ?? []),
        () => {}
      ),
    []
  )
  useEffect(() => {
    loadMarkets()
  }, [loadMarkets])
  const market = async (
    key: string,
    action: Parameters<typeof api.marketAct>[0],
    source = "",
    name = ""
  ) => {
    setBusy(key)
    const ok = await reportAction(() => api.marketAct(action, source, name))
    setBusy("")
    if (!ok) return
    if (action === "add") setSrc("")
    await loadMarkets()
    onChange()
  }
  const trimmedSource = src.trim()
  const isValidSource = SOURCE_OK.test(trimmedSource)

  return (
    <section className="space-y-2">
      <h4 className="text-sm font-medium">
        {t("skills.plugins.marketplaces", { count: markets.length })}
      </h4>
      <div className="space-y-2">
          <div className="flex justify-end">
            <Button
              size="xs"
              variant="ghost"
              disabled={busy === UPDATE_ALL_MARKETS_KEY}
              onClick={() => market(UPDATE_ALL_MARKETS_KEY, "update")}
            >
              {busy === UPDATE_ALL_MARKETS_KEY ? (
                <Fan className="animate-spin" />
              ) : (
                <RefreshCw />
              )}{" "}
              {t("skills.plugins.marketUpdateAll")}
            </Button>
          </div>
          <ul className="divide-y rounded-md border">
            {markets.map((marketplace) => (
              <li
                key={marketplace.name}
                className="flex items-center gap-2 px-3 py-1.5 text-sm"
              >
                <div className="min-w-0 flex-1">
                  <div className="truncate font-medium">{marketplace.name}</div>
                  <div className="truncate text-xs text-muted-foreground">
                    {marketplace.repo ?? marketplace.url ?? marketplace.source}
                  </div>
                </div>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy === MARKET_KEY_PREFIX + marketplace.name}
                  title={t("skills.plugins.marketUpdate")}
                  aria-label={t("skills.plugins.marketUpdate")}
                  onClick={() => market(MARKET_KEY_PREFIX + marketplace.name, "update", "", marketplace.name)}
                >
                  <RefreshCw />
                </Button>
                <Button
                  size="icon-xs"
                  variant="ghost"
                  disabled={busy === MARKET_KEY_PREFIX + marketplace.name}
                  title={t("skills.plugins.marketRemove")}
                  aria-label={t("skills.plugins.marketRemove")}
                  onClick={() => market(MARKET_KEY_PREFIX + marketplace.name, "remove", "", marketplace.name)}
                >
                  <Trash2 />
                </Button>
              </li>
            ))}
          </ul>
          <form
            className="flex gap-2"
            onSubmit={(event) => {
              event.preventDefault()
              if (isValidSource) market(ADD_MARKET_KEY, "add", trimmedSource)
            }}
          >
            <Input
              value={src}
              onChange={(event) => setSrc(event.target.value)}
              placeholder={t("skills.plugins.marketAdd")}
              aria-label={t("skills.plugins.marketAdd")}
            />
            <Button
              type="submit"
              size="sm"
              variant="outline"
              disabled={busy === ADD_MARKET_KEY || !isValidSource}
            >
              {busy === ADD_MARKET_KEY ? <Fan className="animate-spin" /> : <Plus />}{" "}
              {t("skills.plugins.marketAddBtn")}
            </Button>
          </form>
        </div>
    </section>
  )
}
