import { useCallback, useEffect, useState } from "react"
import { RotateCcw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { ago, size, tilde } from "@/lib/format"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { ScrollArea } from "@/components/ui/scroll-area"
import { useActions } from "./actions"

type Trashed = { dir: string; id: string; title: string; cwd: string; at: number; kb: number }

// Corbeille des sessions : restaurer, supprimer pour de bon, ou tout vider. Les sessions y restent 30 jours.
export function SessionTrash({ open, onClose, reload }: { open: boolean; onClose: () => void; reload: () => Promise<void> }) {
  const { t } = useTranslation()
  const a = useActions()
  const [rows, setRows] = useState<Trashed[] | null>(null)
  const [days, setDays] = useState(30)
  const [confirm, setConfirm] = useState<string[] | null>(null) // ids à supprimer pour de bon, après confirmation
  const load = useCallback(() => api.sessionTrash().then((r) => (setRows(r.sessions as Trashed[]), setDays(r.days))), [])
  useEffect(() => {
    if (open) void load()
  }, [open, load])
  const act = async (url: string, ids: string[]) => {
    const r = await api.post(url, { ids })
    ;(r.ok ? toast.success : toast.error)(r.out.split("\n")[0])
    setConfirm(null)
    await Promise.all([load(), reload()])
  }
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-h-[85vh] sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("shell.sidebar.trash")}</DialogTitle>
          <DialogDescription>{t("shell.trash.description", { days })}</DialogDescription>
        </DialogHeader>
        {rows === null ? (
          <p className="py-6 text-center text-sm text-muted-foreground">{t("common.loading")}</p>
        ) : rows.length === 0 ? (
          <p className="py-6 text-center text-sm text-muted-foreground">{t("shell.trash.empty")}</p>
        ) : (
          <ScrollArea className="max-h-[50vh] rounded-md border">
            <ul className="divide-y">
              {rows.map((r) => (
                <li key={r.id} className="flex items-center gap-2 px-3 py-2">
                  <div className="min-w-0 flex-1">
                    <p className="truncate text-sm font-medium" title={r.title}>
                      {r.title || r.id}
                    </p>
                    <p className="truncate text-xs text-muted-foreground" title={r.cwd}>
                      {r.cwd ? tilde(r.cwd, a.home) : r.dir} · {t("shell.trash.deleted", { ago: ago(r.at) })} · {size(r.kb)}
                    </p>
                  </div>
                  <Button size="xs" variant="outline" onClick={() => act("/session-trash-restore", [r.id])}>
                    <RotateCcw /> {t("shell.trash.restore")}
                  </Button>
                  <Button size="icon-xs" variant="ghost" aria-label={t("shell.trash.purge")} title={t("shell.trash.purge")} onClick={() => setConfirm([r.id])}>
                    <Trash2 />
                  </Button>
                </li>
              ))}
            </ul>
          </ScrollArea>
        )}
        {confirm && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm">
            {t("shell.trash.confirm", { count: confirm.length })}
            <Button size="xs" variant="ghost" className="ml-auto" onClick={() => setConfirm(null)}>
              {t("common.cancel")}
            </Button>
            <Button size="xs" variant="destructive" onClick={() => act("/session-trash-purge", confirm)}>
              {t("common.delete")}
            </Button>
          </div>
        )}
        <DialogFooter>
          {!!rows?.length && (
            <Button variant="outline" className="mr-auto" onClick={() => setConfirm(rows.map((r) => r.id))}>
              {t("shell.trash.emptyAll")}
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
