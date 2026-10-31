import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from "@/components/ui/alert-dialog"
import { useEffect, useRef } from "react"
import { useTranslation } from "react-i18next"
import { focusRow, rowAfter } from "@/lib/focusRow"

export type Ask = {
  title: string
  body: string
  confirm: string
  run: () => void
  rows?: string[] // sessions dont la ligne reprend le focus ; removes : confirmer les fait disparaître
  removes?: boolean
}

export function Confirm({ ask, onClose }: { ask: Ask | null; onClose: () => void }) {
  const { t } = useTranslation()
  // next : ligne voisine, retenue au clic (la ligne supprimée a souvent disparu quand le dialogue finit de se fermer)
  const lastAsk = useRef<{ ask: Ask | null; next?: string }>({ ask: null })
  useEffect(() => {
    if (ask) lastAsk.current = { ask }
  }, [ask])
  const restoreRowFocus = (closeEvent: Event) => {
    const { ask: rememberedAsk, next: nextRow } = lastAsk.current
    if (focusRow(nextRow ?? rememberedAsk?.rows?.[0])) closeEvent.preventDefault()
  }
  return (
    <AlertDialog open={!!ask} onOpenChange={(isOpen) => !isOpen && onClose()}>
      <AlertDialogContent onCloseAutoFocus={restoreRowFocus}>
        <AlertDialogHeader>
          <AlertDialogTitle>{ask?.title}</AlertDialogTitle>
          <AlertDialogDescription className="min-w-0 whitespace-pre-line [overflow-wrap:anywhere]">{ask?.body}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>{t("common.cancel")}</AlertDialogCancel>
          <AlertDialogAction
            variant="destructive"
            onClick={() => {
              if (ask?.removes && ask.rows) lastAsk.current.next = rowAfter(ask.rows)
              ask?.run()
            }}
          >
            {ask?.confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
