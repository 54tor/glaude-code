import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { type LucideIcon, ArrowUpCircle, ExternalLink, Fan, Keyboard, MessageSquare, Plug, RefreshCw } from "lucide-react"
import { api, type McpServer } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { toast } from "sonner"
import { cn, copyText } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { Row } from "./actions"
import { CM, SessionItems } from "./SessionMenu"
import { SessionStatusIcon } from "./SessionStatusIcon"

const MAX_ROWS = 8

type DashboardCardProps = React.ComponentProps<"section"> & { icon: LucideIcon; iconClassName?: string; title: string; actions: React.ReactNode }

function DashboardCard({ icon: Icon, iconClassName, title, actions, children, ...sectionProps }: DashboardCardProps) {
  return (
    <section {...sectionProps} className="min-w-0 rounded-xl border bg-card p-4 shadow-xs md:p-5">
      <div className="mb-3 flex items-center gap-2 border-b pb-2">
        <h2 className="flex min-w-0 items-center gap-2 text-md font-semibold">
          <Icon className={cn("size-4 shrink-0", iconClassName)} />
          <span className="truncate">{title}</span>
        </h2>
        <div className="ml-auto flex shrink-0 items-center gap-1">{actions}</div>
      </div>
      {children}
    </section>
  )
}

// Accueil : ce qui demande un œil maintenant. Sessions ouvertes (celles qui attendent d'abord), dernières fermées encore au
// cache, état des serveurs MCP et mise à jour disponible.
export function Dashboard({ open, closed, waiting, now, ttl, home, go, read }: { open: Row[]; closed: Row[]; waiting: Set<string>; now: number; ttl: number; home: string; go: (route: Route) => void; read: (record: { id: string }) => void }) {
  const { t } = useTranslation()
  const [mcp, setMcp] = useState<McpServer[] | null>(null)
  const [status, setStatus] = useState<Record<string, string> | null>(null)
  const [checking, setChecking] = useState(true) // la première vérification part au montage
  const [updateInfo, setUpdateInfo] = useState<Awaited<ReturnType<typeof api.updateInfo>> | null>(null)
  const checkMcpStatus = () =>
    api
      .configMcpCheck()
      .then((checkResponse) => setStatus(checkResponse.status ?? {}), () => setStatus({}))
      .finally(() => setChecking(false))
  const recheck = () => {
    setChecking(true)
    void checkMcpStatus()
  }
  useEffect(() => {
    api.configMcp().then((mcpResponse) => setMcp(mcpResponse.servers), () => setMcp([]))
    api.updateInfo().then(setUpdateInfo, () => {})
    void checkMcpStatus()
  }, [])
  const [checkingUpdate, setCheckingUpdate] = useState(false)
  const recheckUpdate = () => {
    setCheckingUpdate(true)
    api.updateInfo(true).then(setUpdateInfo, () => {}).finally(() => setCheckingUpdate(false))
  }
  const isServerUp = (serverName: string) => /✔|Connected/.test(status?.[serverName] ?? "")
  const serverStatusColor = (serverName: string) => {
    if (!status) return "text-muted-foreground"
    if (isServerUp(serverName)) return "text-success"
    return "text-warning"
  }
  const configuredServerNames = (mcp ?? []).map((server) => server.name)
  const serverNames = [...new Set([...configuredServerNames, ...Object.keys(status ?? {})])].sort()
  const renderSessionList = (rows: Row[], emptyMessage: string) => {
    if (rows.length === 0) return <p className="py-3 text-sm text-muted-foreground">{emptyMessage}</p>
    return (
      <ul className="divide-y">
        {rows.slice(0, MAX_ROWS).map((row) => (
          <ContextMenu key={row.s.id}>
            <ContextMenuTrigger asChild>
              <li>
                <button type="button" onClick={() => read(row.s)} className="grid w-full grid-cols-[auto_minmax(0,1fr)_auto] items-center gap-x-2 py-1.5 text-left text-sm hover:bg-muted/50">
                  <SessionStatusIcon session={row.s} now={now} ttl={ttl} />
                  <span className="truncate">{row.s.title}</span>
                  {waiting.has(row.s.id) ? <span className="rounded bg-warning/15 px-1 text-2xs text-warning">{t("shell.dash.waiting")}</span> : <span />}
                  <span className="col-start-2 col-end-4 flex min-w-0 gap-2 text-xs text-muted-foreground">
                    <span className="truncate">{tilde(row.g.project, home)}</span>
                    <span className="shrink-0 tabular-nums">· {ago(row.s.mtime)}</span>
                  </span>
                </button>
              </li>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-64">
              <SessionItems M={CM} row={row} full />
            </ContextMenuContent>
          </ContextMenu>
        ))}
      </ul>
    )
  }
  const renderMcpServers = () => {
    if (mcp === null) return <Fan className="m-3 size-4 animate-spin text-muted-foreground" />
    if (serverNames.length === 0) return <p className="py-3 text-sm text-muted-foreground">{t("shell.dash.noMcp")}</p>
    return (
      <ul className="grid grid-cols-[repeat(auto-fill,minmax(12rem,1fr))] gap-x-4">
        {serverNames.map((serverName) => (
          <ContextMenu key={serverName}>
            <ContextMenuTrigger asChild>
              <li className="flex min-w-0 items-center gap-2 py-1 text-sm" title={status?.[serverName]}>
                <span className={serverStatusColor(serverName)}>●</span>
                <span className="truncate">{serverName}</span>
              </li>
            </ContextMenuTrigger>
            <ContextMenuContent className="w-56">
              <ContextMenuItem onSelect={recheck} disabled={checking}>
                <RefreshCw /> {t("shell.dash.recheck")}
              </ContextMenuItem>
              <ContextMenuItem onSelect={() => go({ kind: "extensions" })}>
                <Plug /> {t("shell.dash.openExtensions")}
              </ContextMenuItem>
              <ContextMenuSeparator />
              <ContextMenuItem onSelect={() => void copyText(serverName).then(() => toast.success(t("shell.dash.copied")))}>{t("shell.dash.copyName")}</ContextMenuItem>
            </ContextMenuContent>
          </ContextMenu>
        ))}
      </ul>
    )
  }
  const renderGldUpdate = () => {
    if (!updateInfo) return <p className="py-3 text-sm text-muted-foreground">{t("shell.dash.updateUnknown")}</p>
    if (updateInfo.newer) {
      return (
        <p className="text-sm">
          {t("shell.app.updateAvailable", { latest: updateInfo.latest, current: updateInfo.current })}{" "}
          <a href={updateInfo.url} target="_blank" rel="noopener" className="text-primary underline underline-offset-2">{t("shell.app.updateOpen")}</a>
        </p>
      )
    }
    return <p className="text-sm text-muted-foreground">{t("shell.dash.upToDate", { current: updateInfo.current })}</p>
  }
  const renderClaudeUpdate = () => {
    if (!updateInfo?.claudeCurrent) return null
    if (updateInfo.claudeNewer) {
      return <p className="mt-2 text-sm text-warning">{t("shell.app.claudeOutdated", { current: updateInfo.claudeCurrent, latest: updateInfo.claudeLatest })}</p>
    }
    return <p className="mt-2 text-sm text-muted-foreground">{t("shell.dash.claudeUpToDate", { current: updateInfo.claudeCurrent })}</p>
  }
  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="@container mx-auto max-w-page px-3 py-5 md:px-6">
      <div className="grid gap-5 @min-[50rem]:grid-cols-2">
        <DashboardCard icon={MessageSquare} title={`${t("shell.dash.open")} (${open.length})`} actions={<Button size="xs" variant="ghost" onClick={() => go({ kind: "active" })}>{t("shell.dash.all")}</Button>}>
          {renderSessionList(open, t("shell.dash.noOpen"))}
        </DashboardCard>
        <DashboardCard icon={MessageSquare} iconClassName="text-muted-foreground" title={`${t("shell.dash.closed")} (${closed.length})`} actions={<Button size="xs" variant="ghost" onClick={() => go({ kind: "recent" })}>{t("shell.dash.all")}</Button>}>
          {renderSessionList(closed, t("shell.dash.noClosed"))}
        </DashboardCard>
        <DashboardCard
          icon={Plug}
          title={`${t("shell.dash.mcp")} (${serverNames.length})`}
          actions={
            <>
              <Button size="xs" variant="ghost" onClick={recheck} disabled={checking} aria-label={t("shell.dash.recheck")}>
                {checking ? <Fan className="animate-spin" /> : <RefreshCw />}
              </Button>
              <Button size="xs" variant="ghost" onClick={() => go({ kind: "extensions" })}>{t("shell.dash.all")}</Button>
            </>
          }
        >
          {renderMcpServers()}
        </DashboardCard>
        <ContextMenu>
        <ContextMenuTrigger asChild>
        <DashboardCard
          icon={ArrowUpCircle}
          title={t("shell.dash.update")}
          actions={
            <Button size="xs" variant="ghost" disabled={checkingUpdate} onClick={recheckUpdate}>
              <RefreshCw className={checkingUpdate ? "animate-spin" : ""} /> {t("shell.dash.updateCheck")}
            </Button>
          }
        >
          {renderGldUpdate()}
          {renderClaudeUpdate()}
          <p className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
            <Keyboard className="size-3.5 shrink-0" /> {t("shell.dash.paletteHint")}
          </p>
        </DashboardCard>
        </ContextMenuTrigger>
        <ContextMenuContent className="w-64">
          <ContextMenuItem onSelect={recheckUpdate} disabled={checkingUpdate}>
            <RefreshCw /> {t("shell.dash.updateCheck")}
          </ContextMenuItem>
          {updateInfo?.newer && (
            <ContextMenuItem onSelect={() => window.open(updateInfo.url, "_blank", "noopener")}>
              <ExternalLink /> {t("shell.app.updateOpen")}
            </ContextMenuItem>
          )}
          {updateInfo?.claudeNewer && (
            <ContextMenuItem onSelect={() => void copyText("claude update").then(() => toast.success(t("shell.dash.copied")))}>
              {t("shell.dash.copyClaudeUpdate")}
            </ContextMenuItem>
          )}
        </ContextMenuContent>
        </ContextMenu>
      </div>
      </div>
    </ScrollArea>
  )
}
