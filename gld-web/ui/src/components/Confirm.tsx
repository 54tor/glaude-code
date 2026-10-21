import { useState } from "react"
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
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"

export type Ask = {
  title: string
  body: string
  confirm: string
  // Si défini : il faut retaper ce nombre pour confirmer (un motif trop large viderait tout).
  count?: number
  run: () => void
}

export function Confirm({ ask, onClose }: { ask: Ask | null; onClose: () => void }) {
  const [typed, setTyped] = useState("")
  const needs = ask?.count !== undefined
  return (
    <AlertDialog
      open={!!ask}
      onOpenChange={(o) => {
        if (!o) {
          setTyped("")
          onClose()
        }
      }}
    >
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>{ask?.title}</AlertDialogTitle>
          <AlertDialogDescription className="whitespace-pre-line">{ask?.body}</AlertDialogDescription>
        </AlertDialogHeader>
        {needs && (
          <div className="space-y-1.5">
            <Label htmlFor="cnt" className="text-xs">
              Tape {ask?.count} pour confirmer
            </Label>
            <Input id="cnt" value={typed} inputMode="numeric" autoComplete="off" onChange={(e) => setTyped(e.target.value)} />
          </div>
        )}
        <AlertDialogFooter>
          <AlertDialogCancel>Annuler</AlertDialogCancel>
          <AlertDialogAction variant="destructive" disabled={needs && typed.trim() !== String(ask?.count)} onClick={() => ask?.run()}>
            {ask?.confirm}
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
