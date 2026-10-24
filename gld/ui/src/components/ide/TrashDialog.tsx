import { useCallback, useEffect, useRef, useState } from "react"
import { useTranslation } from "react-i18next"
import { RotateCcw, Trash2 } from "lucide-react"
import { toast } from "sonner"
import { api, type FsTrashItem } from "@/lib/api"
import { Button } from "@/components/ui/button"
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Confirm, type Ask } from "@/components/Confirm"

type Props = {
  dir: string
  open: boolean
  canWrite: boolean
  onClose: () => void
  onRestored: () => void
}

export function TrashDialog({ open, onClose, ...rest }: Props) {
  return (
    <Dialog open={open} onOpenChange={(o) => !o && onClose()}>
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
          (r) =>
            alive.current &&
            setItems(r.ok && r.body.items ? r.body.items : "failed")
        ),
    [dir]
  )
  useEffect(() => {
    load()
  }, [load])
  const exclusive = async (id: string, f: () => Promise<void>) => {
    if (pending.current) return
    pending.current = true
    setBusy(id)
    setError("")
    try {
      await f()
    } finally {
      pending.current = false
      if (alive.current) setBusy("")
    }
  }

  const restore = (x: FsTrashItem) =>
    exclusive(x.id, async () => {
      const r = await api.fsTrashRestore(dir, x.id)
      if (!alive.current) return
      if (!r.ok)
        return setError(
          r.status === 409
            ? t("ide.trash.exists", { path: x.path })
            : r.body.error || t("shell.act.failed")
        )
      toast(t("ide.trash.restored", { path: x.path }))
      onRestored()
      void load()
    })
  const purge = (x: FsTrashItem) =>
    setConfirm({
      title: t("ide.trash.purgeTitle", { path: x.path }),
      body: t("ide.trash.purgeBody"),
      confirm: t("ide.trash.purgeConfirm"),
      run: () =>
        void exclusive(x.id, async () => {
          const r = await api.fsTrashPurge(dir, x.id)
          if (!alive.current) return
          if (!r.ok) return setError(r.body.error || t("shell.act.failed"))
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
          {items.map((x) => (
            <li key={x.id} className="flex items-center gap-2 py-1.5">
              <span
                className="min-w-0 flex-1 truncate font-mono text-xs"
                title={x.path}
              >
                {x.path}
              </span>
              <time
                className="shrink-0 text-xs text-muted-foreground"
                dateTime={new Date(x.mtime * 1000).toISOString()}
              >
                {date.format(x.mtime * 1000)}
              </time>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite || !!busy}
                aria-label={t("ide.trash.restore", { path: x.path })}
                onClick={() => restore(x)}
              >
                <RotateCcw />
              </Button>
              <Button
                variant="ghost"
                size="icon-sm"
                disabled={!canWrite || !!busy}
                aria-label={t("ide.trash.purge", { path: x.path })}
                onClick={() => purge(x)}
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
