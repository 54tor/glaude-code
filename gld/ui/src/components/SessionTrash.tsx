import { useCallback, useEffect, useMemo, useState } from "react"
import { RotateCcw, Shredder } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { reportAction } from "@/lib/report"
import { ago, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { useActions } from "./actions"

const DEFAULT_RETENTION_DAYS = 30

type Trashed = { dir: string; id: string; title: string; cwd: string; at: number; kb: number }

// Corbeille des sessions : restaurer, supprimer pour de bon, ou tout vider. Les sessions y restent 30 jours.
export function SessionTrash({ open, onClose, reload }: { open: boolean; onClose: () => void; reload: () => Promise<void> }) {
  const { t } = useTranslation()
  const actions = useActions()
  const [rows, setRows] = useState<Trashed[] | null>(null)
  const [days, setDays] = useState(DEFAULT_RETENTION_DAYS)
  const [query, setQuery] = useState("")
  const [confirm, setConfirm] = useState<string[] | null>(null) // ids à supprimer pour de bon, après confirmation
  const load = useCallback(
    () =>
      api.sessionTrash().then(
        (trashResponse) => {
          setRows(trashResponse.sessions as Trashed[])
          setDays(trashResponse.days)
        },
        (error) => toast.error(errMsg(error))
      ),
    []
  )
  useEffect(() => {
    if (open) void load()
  }, [open, load])
  const shown = useMemo(() => {
    const normalizedQuery = query.trim().toLowerCase()
    const sortedByDeletionDate = [...(rows ?? [])].sort((firstRow, secondRow) => secondRow.at - firstRow.at)
    if (!normalizedQuery) return sortedByDeletionDate
    return sortedByDeletionDate.filter((row) => `${row.title} ${row.cwd} ${row.dir} ${row.id}`.toLowerCase().includes(normalizedQuery))
  }, [rows, query])
  const runAndReload = async (run: () => Promise<{ message: string }>) => {
    await reportAction(run)
    setConfirm(null)
    await Promise.all([load(), reload()])
  }
  const isLoading = rows === null
  const isEmpty = rows !== null && rows.length === 0
  const hasRows = rows !== null && rows.length > 0
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-h-[85vh] min-w-0 overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("shell.sidebar.trash")}</DialogTitle>
          <DialogDescription>{t("shell.trash.description", { days })}</DialogDescription>
        </DialogHeader>
        {isLoading && <p className="py-6 text-center text-sm text-muted-foreground">{t("common.loading")}</p>}
        {isEmpty && <p className="py-6 text-center text-sm text-muted-foreground">{t("shell.trash.empty")}</p>}
        {hasRows && (
          <>
            <Input type="search" value={query} onChange={(changeEvent) => setQuery(changeEvent.target.value)} placeholder={t("shell.trash.search")} aria-label={t("shell.trash.search")} />
            {shown.length === 0 ? (
              <p className="py-6 text-center text-sm text-muted-foreground">{t("shell.trash.noMatch")}</p>
            ) : (
          <div className="max-h-[50vh] min-w-0 overflow-y-auto rounded-md border">
            <ul className="divide-y">
              {shown.map((row) => (
                <li key={row.id} className="flex items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium" title={row.title}>
                      {row.title || row.id}
                    </p>
                    <p className="truncate text-xs text-muted-foreground" title={row.cwd}>
                      {row.cwd ? tilde(row.cwd, actions.home) : row.dir} · {t("shell.trash.deleted", { ago: ago(row.at) })} · {size(row.kb)}
                    </p>
                  </div>
                  <Button size="xs" variant="outline" onClick={() => runAndReload(() => api.trashSessionsRestore([row.id]))}>
                    <RotateCcw /> {t("shell.trash.restore")}
                  </Button>
                  <Button size="icon-xs" variant="ghost" aria-label={t("shell.trash.purge")} title={t("shell.trash.purge")} onClick={() => setConfirm([row.id])}>
                    <Shredder />
                  </Button>
                </li>
              ))}
            </ul>
          </div>
            )}
          </>
        )}
        {confirm && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm">
            {t("shell.trash.confirm", { count: confirm.length })}
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => setConfirm(null)}>
              {t("common.cancel")}
            </Button>
            <Button size="xs" variant="destructive" onClick={() => runAndReload(() => api.trashSessionsPurge(confirm))}>
              {t("common.delete")}
            </Button>
          </div>
        )}
        <DialogFooter>
          {!!shown.length && (
            <Button variant="outline" className="mr-auto" onClick={() => setConfirm(shown.map((row) => row.id))}>
              <Shredder /> {t("shell.trash.emptyAll")}
            </Button>
          )}
          <Button variant="ghost" onClick={onClose}>
            {t("common.close")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
