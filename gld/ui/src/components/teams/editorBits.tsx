import type { ComponentProps } from "react"
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Button } from "@/components/ui/button"
import { useTranslation } from "react-i18next"

export const sameLabel = (firstLabel: string, secondLabel: string) =>
  firstLabel.trim().toLowerCase() === secondLabel.trim().toLowerCase()

export const bytes = (text: string) => new TextEncoder().encode(text).length

export const fieldCls =
  "w-full rounded-lg border border-input bg-transparent px-2.5 py-1 text-sm outline-none focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/80"

export function Field({
  label,
  children,
}: {
  label: string
  children: React.ReactNode
}) {
  return (
    <label className="block space-y-1 text-sm">
      <span className="font-medium">{label}</span>
      {children}
    </label>
  )
}

export function Select(props: ComponentProps<"select">) {
  return (
    <select {...props} className={`${fieldCls} h-8 ${props.className ?? ""}`} />
  )
}

// Gabarit commun des deux éditeurs : titre, corps, erreur serveur en texte, Supprimer (perso) / Annuler / Enregistrer.
export function EditorDialog({
  title,
  err,
  canSave,
  busy,
  onSave,
  onDelete,
  onClose,
  children,
}: {
  title: string
  err: string
  canSave: boolean
  busy: boolean
  onSave: () => void
  onDelete?: () => void
  onClose: () => void
  children: React.ReactNode
}) {
  const { t } = useTranslation()
  return (
    <Dialog open onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent aria-describedby={undefined} className="sm:max-w-xl">
        <DialogHeader>
          <DialogTitle>{title}</DialogTitle>
        </DialogHeader>
        <div className="space-y-3">{children}</div>
        {err && <p className="text-sm text-destructive">{err}</p>}
        <DialogFooter>
          {onDelete && (
            <Button
              variant="destructive"
              className="sm:mr-auto"
              disabled={busy}
              onClick={onDelete}
            >
              {t("common.delete")}
            </Button>
          )}
          <Button variant="outline" onClick={onClose}>
            {t("common.cancel")}
          </Button>
          <Button disabled={!canSave || busy} onClick={onSave}>
            {t("common.save")}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
