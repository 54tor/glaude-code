import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { RotateCcw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, type FsTrashItem } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Confirm, type Ask } from "@/components/Confirm"

const CONFLICT = 409

type Props = {
  dir: string
  open: boolean
  canWrite: boolean
  onClose: () => void
  onRestored: () => void
}

export function TrashDialog({ open, onClose, ...rest }: Props) {
  return (
    <Dialog open={open} onOpenChange={(nextOpen) => !nextOpen && onClose()}>
      <DialogContent className="sm:max-w-xl">
        {open && <Body {...rest} />}
      </DialogContent>
    </Dialog>
  )
}

// Monté à chaque ouverture : la liste repart de « chargement », jamais d'un état d'une ouverture précédente.
function Body({ dir, canWrite, onRestored }: Omit<Props, "open" | "onClose">) {
  const { t, i18n } = useTranslation()
  const [items, setItems] = useState<FsTrashItem[] | "loading" | "failed">(
    "loading"
  )
  const [busy, setBusy] = useState("")
  const [error, setError] = useState("")
  const [confirm, setConfirm] = useState<Ask | null>(null)

  // Une requête à la fois (restaurer ou purger) ; plus aucune mise à jour d'état une fois le dialogue fermé.
  const pending = useRef(false)
  const alive = useRef(true)
  useEffect(() => {
    alive.current = true
    return () => {
      alive.current = false
    }
  }, [])
  const load = useCallback(
    () =>
      api
        .fsTrash(dir)
        .then(
          (response) => alive.current && setItems(response.items),
          () => alive.current && setItems("failed")
        ),
    [dir]
  )
  useEffect(() => {
    load()
  }, [load])
  const exclusive = async (id: string, task: () => Promise<void>) => {
    if (pending.current) return
    pending.current = true
    setBusy(id)
    setError("")
    try {
      await task()
    } finally {
      pending.current = false
      if (alive.current) setBusy("")
    }
  }

  const failMsg = (error: unknown) => (error instanceof ApiError ? error.message : t("shell.act.failed"))
  const restore = (item: FsTrashItem) =>
    exclusive(item.id, async () => {
      try {
        await api.fsTrashRestore(dir, item.id)
      } catch (restoreError) {
        if (!alive.current) return
        const alreadyExists = restoreError instanceof ApiError && restoreError.status === CONFLICT
        return setError(alreadyExists ? t("ide.trash.exists", { path: item.path }) : failMsg(restoreError))
      }
      // Le fichier est revenu sur le disque même si la fenêtre a été fermée entre-temps : l'arbre doit le montrer.
      onRestored()
      if (!alive.current) return
      toast(t("ide.trash.restored", { path: item.path }))
      void load()
    })
  const purge = (item: FsTrashItem) =>
    setConfirm({
      title: t("ide.trash.purgeTitle", { path: item.path }),
      body: t("ide.trash.purgeBody"),
      confirm: t("ide.trash.purgeConfirm"),
      run: () =>
        void exclusive(item.id, async () => {
          try {
            await api.fsTrashPurge(dir, item.id)
          } catch (purgeError) {
            if (alive.current) setError(failMsg(purgeError))
            return
          }
          if (!alive.current) return
          void load()
        }),
    })
  const date = new Intl.DateTimeFormat(i18n.language, {
    dateStyle: "short",
    timeStyle: "short",
  })

  return (
    <>
      <DialogHeader>
        <DialogTitle>{t("ide.trash.title")}</DialogTitle>
        <DialogDescription>{t("ide.trash.hint")}</DialogDescription>
      </DialogHeader>
      {!canWrite && (
        <p className="text-xs text-muted-foreground">
          {t("ide.reason.noWrite")}
        </p>
      )}
      {error && (
        <p role="alert" className="text-sm text-destructive">
          {error}
        </p>
      )}
      {items === "loading" && (
        <p className="text-sm text-muted-foreground">
          {t("ide.trash.loading")}
        </p>
      )}
      {items === "failed" && (
        <p role="alert" className="text-sm text-destructive">
          {t("ide.trash.failed")}
        </p>
      )}
      {Array.isArray(items) && items.length === 0 && (
        <p className="text-sm text-muted-foreground">{t("ide.trash.empty")}</p>
      )}
      {Array.isArray(items) && items.length > 0 && (
        <ul className="max-h-80 divide-y overflow-auto text-sm">
          {items.map((item) => (
            <li key={item.id} className="flex items-center gap-2 py-1.5">
              <span
                className="min-w-0 flex-1 truncate font-mono text-xs"
                title={item.path}
              >
                {item.path}
              </span>
              <time
                className="shrink-0 text-xs text-muted-foreground"
                dateTime={new Date(item.mtime * 1000).toISOString()}
              >
                {date.format(item.mtime * 1000)}
              </time>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite || !!busy}
                aria-label={t("ide.trash.restore", { path: item.path })}
                onClick={() => restore(item)}
              >
                <RotateCcw />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite || !!busy}
                aria-label={t("ide.trash.purge", { path: item.path })}
                onClick={() => purge(item)}
              >
                <Trash2 />
              </Button>
            </li>
          ))}
        </ul>
      )}
      <Confirm ask={confirm} onClose={() => setConfirm(null)} />
    </>
  )
}
