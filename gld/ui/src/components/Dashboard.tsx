import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { ArrowUpCircle, Fan, Keyboard, MessageSquare, Plug, RefreshCw } from "lucide-react"
import { api, type McpServer } from "@/lib/api"
import { ago, tilde } from "@/lib/format"
import type { Route } from "@/lib/route"
import { Button } from "@/components/ui/button"
import { ScrollArea } from "@/components/ui/scroll-area"
import type { Row } from "./actions"

const MAX = 8

// Accueil : ce qui demande un œil maintenant. Sessions ouvertes (celles qui attendent d'abord), dernières fermées encore au
// cache, état des serveurs MCP et mise à jour disponible.
export function Dashboard({ open, closed, waiting, now, home, go, read }: { open: Row[]; closed: Row[]; waiting: Set<string>; now: number; home: string; go: (r: Route) => void; read: (s: { id: string }) => void }) {
  const { t } = useTranslation()
  const [mcp, setMcp] = useState<McpServer[] | null>(null)
  const [status, setStatus] = useState<Record<string, string> | null>(null)
  const [checking, setChecking] = useState(true) // la première vérification part au montage
  const [u, setU] = useState<{ current: string; latest: string; url: string; newer: boolean } | null>(null)
  const check = () => api.configMcpCheck().then((d) => setStatus(d.status ?? {}), () => setStatus({})).finally(() => setChecking(false))
  const recheck = () => {
    setChecking(true)
    void check()
  }
  useEffect(() => {
    api.configMcp().then((r) => setMcp(r.servers), () => setMcp([]))
    api.updateInfo().then(setU, () => {})
    void check()
  }, [])
  const [checkingUpd, setCheckingUpd] = useState(false)
  const recheckUpd = () => {
    setCheckingUpd(true)
    api.updateInfo(true).then(setU, () => {}).finally(() => setCheckingUpd(false))
  }
  const up = (n: string) => /✔|Connected/.test(status?.[n] ?? "")
  const names = [...new Set([...(mcp ?? []).map((m) => m.name), ...Object.keys(status ?? {})])].sort()
  const card = "rounded-xl border bg-card p-4 shadow-xs md:p-5"
  const head = "mb-3 flex items-center gap-2 border-b pb-2 text-md font-semibold"
  const list = (rows: Row[], empty: string) =>
    rows.length ? (
      <ul className="divide-y">
        {rows.slice(0, MAX).map((r) => (
          <li key={r.s.id}>
            <button type="button" onClick={() => read(r.s)} className="flex w-full items-center gap-2 py-1.5 text-left text-sm hover:bg-muted/50">
              <span className="min-w-0 flex-1 truncate">{r.s.title}</span>
              {waiting.has(r.s.id) && <span className="shrink-0 rounded bg-warning/15 px-1 text-2xs text-warning uppercase">{t("shell.dash.waiting")}</span>}
              <span className="shrink-0 text-xs text-muted-foreground">{tilde(r.g.project, home)}</span>
              <span className="w-14 shrink-0 text-right text-xs text-muted-foreground tabular-nums">{ago(now - r.s.mtime)}</span>
            </button>
          </li>
        ))}
      </ul>
    ) : (
      <p className="py-3 text-sm text-muted-foreground">{empty}</p>
    )
  return (
    <ScrollArea className="min-h-0 flex-1">
      <div className="mx-auto grid max-w-page gap-5 px-3 py-5 md:grid-cols-2 md:px-6">
        <section className={card}>
          <h3 className={head}>
            <MessageSquare className="size-4" /> {t("shell.dash.open")} ({open.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => go({ kind: "active" })}>{t("shell.dash.all")}</Button>
          </h3>
          {list(open, t("shell.dash.noOpen"))}
        </section>
        <section className={card}>
          <h3 className={head}>
            <MessageSquare className="size-4 text-muted-foreground" /> {t("shell.dash.closed")} ({closed.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => go({ kind: "recent" })}>{t("shell.dash.all")}</Button>
          </h3>
          {list(closed, t("shell.dash.noClosed"))}
        </section>
        <section className={card}>
          <h3 className={head}>
            <Plug className="size-4" /> {t("shell.dash.mcp")} ({names.length})
            <Button size="xs" variant="ghost" className="ml-auto" onClick={recheck} disabled={checking} aria-label={t("shell.dash.recheck")}>
              {checking ? <Fan className="animate-spin" /> : <RefreshCw />}
            </Button>
            <Button size="xs" variant="ghost" onClick={() => go({ kind: "extensions" })}>{t("shell.dash.all")}</Button>
          </h3>
          {mcp === null ? (
            <Fan className="m-3 size-4 animate-spin text-muted-foreground" />
          ) : names.length ? (
            <ul className="grid gap-x-4 sm:grid-cols-2">
              {names.map((n) => (
                <li key={n} className="flex items-center gap-2 py-1 text-sm" title={status?.[n]}>
                  <span className={status ? (up(n) ? "text-success" : "text-warning") : "text-muted-foreground"}>●</span>
                  <span className="truncate">{n}</span>
                </li>
              ))}
            </ul>
          ) : (
            <p className="py-3 text-sm text-muted-foreground">{t("shell.dash.noMcp")}</p>
          )}
        </section>
        <section className={card}>
          <h3 className={head}>
            <ArrowUpCircle className="size-4" /> {t("shell.dash.update")}
            <Button size="xs" variant="ghost" className="ml-auto" disabled={checkingUpd} onClick={recheckUpd}>
              <RefreshCw className={checkingUpd ? "animate-spin" : ""} /> {t("shell.dash.updateCheck")}
            </Button>
          </h3>
          {!u ? (
            <p className="py-3 text-sm text-muted-foreground">{t("shell.dash.updateUnknown")}</p>
          ) : u.newer ? (
            <p className="text-sm">
              {t("shell.app.updateAvailable", { latest: u.latest, current: u.current })}{" "}
              <a href={u.url} target="_blank" rel="noopener" className="text-primary underline underline-offset-2">{t("shell.app.updateOpen")}</a>
            </p>
          ) : (
            <p className="text-sm text-muted-foreground">{t("shell.dash.upToDate", { current: u.current })}</p>
          )}
          <p className="mt-4 flex items-center gap-2 text-xs text-muted-foreground">
            <Keyboard className="size-3.5" /> {t("shell.dash.paletteHint")}
          </p>
        </section>
      </div>
    </ScrollArea>
  )
}
