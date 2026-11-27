import { useCallback, useEffect, useState } from "react"
import { RotateCcw, Shredder, Trash2 } from "lucide-react"
import { toast } from "@/lib/notify"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { errMsg } from "@/lib/apiError"
import { reportAction } from "@/lib/report"
import { ago, size } from "@/lib/format"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"

const DEFAULT_RETENTION_DAYS = 30
const BYTES_PER_KB = 1024
const SHORT_ID_LENGTH = 8

type Orphan = Awaited<ReturnType<typeof api.orphanFiles>>["files"][number]

// Pièces jointes déposées qu'aucune session ne cite : mise à la corbeille, restauration, suppression définitive.
export function OrphanFiles({
  open,
  onClose,
}: {
  open: boolean
  onClose: () => void
}) {
  const { t } = useTranslation()
  const [rows, setRows] = useState<Orphan[] | null>(null)
  const [days, setDays] = useState(DEFAULT_RETENTION_DAYS)
  const [confirm, setConfirm] = useState<string[] | null>(null)
  const load = useCallback(
    () =>
      api.orphanFiles().then(
        (orphanResponse) => {
          setRows(orphanResponse.files)
          setDays(orphanResponse.days)
        },
        (error) => toast.error(errMsg(error))
      ),
    []
  )
  useEffect(() => {
    if (open) void load()
  }, [open, load])
  const runAndReload = async (run: () => Promise<{ message: string }>) => {
    await reportAction(run)
    setConfirm(null)
    await load()
  }
  const trashedRows = (rows ?? []).filter((row) => row.trashed)
  const isEmpty = rows !== null && rows.length === 0
  return (
    <Dialog open={open} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-h-[85vh] min-w-0 overflow-hidden sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle>{t("shell.sidebar.orphans")}</DialogTitle>
          <DialogDescription>
            {t("shell.orphans.description", { days })}
          </DialogDescription>
        </DialogHeader>
        {rows === null && (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("common.loading")}
          </p>
        )}
        {isEmpty && (
          <p className="py-6 text-center text-sm text-muted-foreground">
            {t("shell.orphans.empty")}
          </p>
        )}
        {!!rows?.length && (
          <div className="max-h-[50vh] min-w-0 overflow-y-auto rounded-md border">
            <ul className="divide-y">
              {rows.map((row) => (
                <li
                  key={`${row.trashed}-${row.name}`}
                  className="flex items-center gap-2 px-3 py-2"
                >
                  <div className="min-w-0 flex-1">
                    <p
                      className="truncate text-sm font-medium"
                      title={row.name}
                    >
                      {row.name}
                    </p>
                    <p className="truncate text-xs text-muted-foreground">
                      {row.trashed
                        ? t("shell.orphans.trashed", { ago: ago(row.at) })
                        : row.owner
                          ? t("shell.orphans.owner", {
                              id: row.owner.slice(0, SHORT_ID_LENGTH),
                            })
                          : t("shell.orphans.unowned")}{" "}
                      · {size(Math.ceil(row.bytes / BYTES_PER_KB))}
                    </p>
                  </div>
                  {row.trashed ? (
                    <>
                      <Button
                        size="xs"
                        variant="outline"
                        onClick={() =>
                          runAndReload(() => api.orphanFilesRestore([row.name]))
                        }
                      >
                        <RotateCcw /> {t("shell.orphans.restore")}
                      </Button>
                      <Button
                        size="icon-xs"
                        variant="ghost"
                        aria-label={t("shell.orphans.purge")}
                        title={t("shell.orphans.purge")}
                        onClick={() => setConfirm([row.name])}
                      >
                        <Shredder />
                      </Button>
                    </>
                  ) : (
                    <Button
                      size="xs"
                      variant="outline"
                      onClick={() =>
                        runAndReload(() => api.orphanFilesTrash([row.name]))
                      }
                    >
                      <Trash2 /> {t("shell.orphans.trash")}
                    </Button>
                  )}
                </li>
              ))}
            </ul>
          </div>
        )}
        {confirm && (
          <div className="flex flex-wrap items-center gap-2 rounded-md border border-destructive/50 bg-destructive/10 px-3 py-2 text-sm">
            {t("shell.orphans.confirm", { count: confirm.length })}
            <Button
              size="xs"
              variant="ghost"
              className="ml-auto"
              onClick={() => setConfirm(null)}
            >
              {t("common.cancel")}
            </Button>
            <Button
              size="xs"
              variant="destructive"
              onClick={() => runAndReload(() => api.orphanFilesPurge(confirm))}
            >
              {t("common.delete")}
            </Button>
          </div>
        )}
        <DialogFooter>
          {!!trashedRows.length && (
            <Button
              variant="outline"
              className="mr-auto"
              onClick={() => setConfirm(trashedRows.map((row) => row.name))}
            >
              <Shredder /> {t("shell.orphans.emptyAll")}
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
