import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpCircle, ExternalLink, Fan, Keyboard, MessageSquare, Plug, RefreshCw } from "lucide-react"
import { api, type McpServer } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { ContextMenu, ContextMenuContent, ContextMenuItem, ContextMenuSeparator, ContextMenuTrigger } from "@/components/ui/context-menu"
import { toast } from "sonner"
import { copyText } from "@/lib/utils"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { Row } from "./actions"
import { CM, SessionItems } from "./SessionMenu"

const MAX_ROWS = 8

// Accueil : ce qui demande un œil maintenant. Sessions ouvertes (celles qui attendent d'abord), dernières fermées encore au
// cache, état des serveurs MCP et mise à jour disponible.
export function Dashboard({ open, closed, waiting, now, home, go, read }: { open: Row[]; closed: Row[]; waiting: Set<string>; now: number; home: string; go: (r: Route) => void; read: (s: { id: string }) => void }) {
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
  const cardClassName = "rounded-xl border bg-card p-4 shadow-xs md:p-5"
  const headingClassName = "mb-3 flex items-center gap-2 border-b pb-2 text-md font-semibold"
  const renderSessionList = (rows: Row[], emptyMessage: string) => {
    if (rows.length === 0) return <p className="py-3 text-sm text-muted-foreground">{emptyMessage}</p>
    return (
      <ul className="divide-y">
        {rows.slice(0, MAX_ROWS).map((row) => (
          <ContextMenu key={row.s.id}>
            <ContextMenuTrigger asChild>
              <li>
                <button type="button" onClick={() => read(row.s)} className="flex w-full items-center gap-2 py-1.5 text-left text-sm hover:bg-muted/50">
                  <span className="min-w-0 flex-1 truncate">{row.s.title}</span>
                  {waiting.has(row.s.id) && <span className="shrink-0 rounded bg-warning/15 px-1 text-2xs text-warning uppercase">{t("shell.dash.waiting")}</span>}
                  <span className="shrink-0 text-xs text-muted-foreground">{tilde(row.g.project, home)}</span>
                  <span className="w-14 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{ago(now - row.s.mtime)}</span>
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
      <ul className="grid gap-x-4 sm:grid-cols-2">
        {serverNames.map((serverName) => (
          <ContextMenu key={serverName}>
            <ContextMenuTrigger asChild>
              <li className="flex items-center gap-2 py-1 text-sm" title={status?.[serverName]}>
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
      <div className="mx-auto grid max-w-page gap-5 px-3 py-5 md:grid-cols-2 md:px-6">
        <section className={cardClassName}>
          <h3 className={headingClassName}>
            <MessageSquare className="size-4" /> {t("shell.dash.open")} ({open.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => go({ kind: "active" })}>{t("shell.dash.all")}</Button>
          </h3>
          {renderSessionList(open, t("shell.dash.noOpen"))}
        </section>
        <section className={cardClassName}>
          <h3 className={headingClassName}>
            <MessageSquare className="size-4 text-muted-foreground" /> {t("shell.dash.closed")} ({closed.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => go({ kind: "recent" })}>{t("shell.dash.all")}</Button>
          </h3>
          {renderSessionList(closed, t("shell.dash.noClosed"))}
        </section>
        <section className={cardClassName}>
          <h3 className={headingClassName}>
            <Plug className="size-4" /> {t("shell.dash.mcp")} ({serverNames.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={recheck} disabled={checking} aria-label={t("shell.dash.recheck")}>
              {checking ? <Fan className="animate-spin" /> : <RefreshCw />}
            </Button>
            <Button size="xs" variant="ghost" onClick={() => go({ kind: "extensions" })}>{t("shell.dash.all")}</Button>
          </h3>
          {renderMcpServers()}
        </section>
        <ContextMenu>
        <ContextMenuTrigger asChild>
        <section className={cardClassName}>
          <h3 className={headingClassName}>
            <ArrowUpCircle className="size-4" /> {t("shell.dash.update")}
            <Button size="xs" variant="ghost" className="ml-auto" disabled={checkingUpdate} onClick={recheckUpdate}>
              <RefreshCw className={checkingUpdate ? "animate-spin" : ""} /> {t("shell.dash.updateCheck")}
            </Button>
          </h3>
          {renderGldUpdate()}
          {renderClaudeUpdate()}
          <p className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
            <Keyboard className="size-3.5" /> {t("shell.dash.paletteHint")}
          </p>
        </section>
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
    </ScrollArea>
  )
}
