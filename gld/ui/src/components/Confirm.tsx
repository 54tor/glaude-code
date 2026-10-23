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
import { focusRow } from "@/lib/focusRow"

export type Ask = {
  title: string
  body: string
  confirm: string
  run: () => void
  rows?: string[] // sessions dont la ligne reprend le focus ; removes : confirmer les fait disparaître
  removes?: boolean
}

export function Confirm({ ask, onClose }: { ask: Ask | null; onClose: () => void }) {
  const last = useRef<{ ask: Ask | null; done: boolean }>({ ask: null, done: false })
  useEffect(() => {
    if (ask) last.current = { ask, done: false }
  }, [ask])
  const back = (e: Event) => {
    const { ask: a, done } = last.current
    if (a?.rows?.length && focusRow(a.rows, done && a.removes)) e.preventDefault()
  }
  return (
    <AlertDialog open={!!ask} onOpenChange={(o) => !o && onClose()}>
      <AlertDialogContent onCloseAutoFocus={back}>
        <AlertDialogHeader>
          <AlertDialogTitle>{ask?.title}</AlertDialogTitle>
          <AlertDialogDescription className="whitespace-pre-line">{ask?.body}</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>Annuler</AlertDialogCancel>
          <AlertDialogAction variant="destructive" onClick={() => {
              last.current.done = true
              ask?.run()
            }}>
            {ask?.confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
