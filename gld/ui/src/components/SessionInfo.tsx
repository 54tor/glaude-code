import { useEffect, useState } from "react"
import { useTranslation } from "react-i18next"
import { api, type Detail } from "@/lib/api"
import { compact, full, size, tilde, tokTotal } from "@/lib/format"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useActions, type Row } from "./actions"

// Fiche d'une session (clic droit → Infos) : identité, emplacement, volume et consommation.
export function SessionInfo({ row, onClose }: { row: Row | null; onClose: () => void }) {
  const { t } = useTranslation()
  const a = useActions()
  const [info, setInfo] = useState<{ id: string; d: Detail } | null>(null)
  const s = row?.s
  const dir = s?.dir
  const id = s?.id
  useEffect(() => {
    if (!dir || !id) return
    let live = true
    api.info({ dir, id }).then((d) => live && setInfo({ id, d }), () => {})
    return () => {
      live = false
    }
  }, [dir, id])
  const d = info && info.id === id ? info.d : null
  const rows: [string, React.ReactNode][] = s
    ? [
        [t("shell.info.id"), <code key="i" className="text-xs break-all">{s.id}</code>],
        [t("shell.info.cwd"), tilde(s.cwd, a.home)],
        [t("shell.info.project"), tilde(row!.g.project, a.home)],
        [t("shell.info.modified"), full(s.mtime)],
        [t("shell.info.size"), size(s.kb)],
        [t("shell.info.tokens"), `${compact(tokTotal(s.tok))} (${t("shell.info.inOut", { in: compact(s.tok.in), out: compact(s.tok.out) })})`],
        [t("shell.info.state"), a.live.has(s.id) ? t("shell.info.open") : t("shell.info.closed")],
        ...(d
          ? ([
              [t("shell.info.prompts"), String(d.n)],
              d.subagents ? [t("shell.info.subagents"), String(d.subagents)] : null,
              d.branch ? [t("shell.info.branch"), d.branch] : null,
              d.models.length ? [t("shell.info.models"), d.models.filter((m) => !m.startsWith("<")).join(", ")] : null,
              d.t0 ? [t("shell.info.started"), full(d.t0)] : null,
              d.t1 ? [t("shell.info.last"), full(d.t1)] : null,
            ].filter(Boolean) as [string, React.ReactNode][])
          : []),
      ]
    : []
  return (
    <Dialog open={!!row} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] min-w-0 overflow-hidden sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="truncate">{s?.title}</DialogTitle>
          <DialogDescription className="sr-only">{t("shell.info.description")}</DialogDescription>
        </DialogHeader>
        <dl className="grid min-h-0 grid-cols-[auto_1fr] gap-x-4 gap-y-1.5 overflow-y-auto text-sm">
          {rows.map(([k, v]) => (
            <div key={k} className="contents">
              <dt className="text-muted-foreground">{k}</dt>
              <dd className="min-w-0 break-words">{v}</dd>
            </div>
          ))}
        </dl>
      </DialogContent>
    </Dialog>
  )
}
