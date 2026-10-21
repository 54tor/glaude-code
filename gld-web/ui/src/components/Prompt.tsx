import { useState } from "react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"

export type Ask = { title: string; value: string; placeholder?: string; optional?: boolean; confirm: string; run: (v: string) => void }

// Saisie d'un nom (renommer une session, créer ou renommer un groupe).
export function Prompt({ ask, onClose }: { ask: Ask | null; onClose: () => void }) {
  return (
    <Dialog open={!!ask} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md">{ask && <Form key={ask.title + ask.value} ask={ask} onClose={onClose} />}</DialogContent>
    </Dialog>
  )
}

function Form({ ask, onClose }: { ask: Ask; onClose: () => void }) {
  const [v, setV] = useState(ask.value)
  const ok = ask.optional || v.trim() !== ""
  const submit = (e: React.FormEvent) => {
    e.preventDefault()
    if (!ok) return
    onClose()
    ask.run(v.trim())
  }
  return (
    <form onSubmit={submit} className="grid gap-4">
      <DialogHeader>
        <DialogTitle>{ask.title}</DialogTitle>
        <DialogDescription className="sr-only">Saisir un nom</DialogDescription>
      </DialogHeader>
      <Input
        value={v}
        onChange={(e) => setV(e.target.value)}
        placeholder={ask.placeholder}
        autoFocus
        onFocus={(e) => e.target.select()}
        maxLength={200}
      />
      <DialogFooter>
        <Button type="button" variant="outline" onClick={onClose}>
          Annuler
        </Button>
        <Button type="submit" disabled={!ok}>
          {ask.confirm}
        </Button>
      </DialogFooter>
    </form>
  )
}
