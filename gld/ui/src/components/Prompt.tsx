import { useEffect, useRef, useState } from "react"
import { Sparkles } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { focusRow } from "@/lib/focusRow"

// run peut rendre une promesse (action serveur) : le dialogue attend alors son issue et ne se ferme qu'en cas de succès.
// row : session dont la ligne reprend le focus à la fermeture.
export type Ask = {
  title: string
  value: string
  placeholder?: string
  optional?: boolean
  confirm: string
  run: (v: string) => void | Promise<boolean>
  auto?: { label: string; hint: string; run: () => void }
  row?: string
}

// Saisie d'un nom (renommer une session, créer ou renommer un groupe).
export function Prompt({ ask, onClose }: { ask: Ask | null; onClose: () => void }) {
  const last = useRef<Ask | null>(null) // à la fermeture, ask vaut déjà null
  useEffect(() => {
    if (ask) last.current = ask
  }, [ask])
  return (
    <Dialog open={!!ask} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="sm:max-w-md" onCloseAutoFocus={(e) => focusRow(last.current?.row) && e.preventDefault()}>
        {ask && <Form key={ask.title + ask.value} ask={ask} onClose={onClose} />}
      </DialogContent>
    </Dialog>
  )
}

function Form({ ask, onClose }: { ask: Ask; onClose: () => void }) {
  const [v, setV] = useState(ask.value)
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  const ok = ask.optional || v.trim() !== ""
  const submit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!ok || busy) return
    const r = ask.run(v.trim())
    if (!(r instanceof Promise)) return onClose()
    setBusy(true)
    setFailed(false)
    if (await r) return onClose()
    setBusy(false)
    setFailed(true)
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
        aria-label={ask.title}
        aria-invalid={failed || undefined}
        aria-describedby={failed ? "prompt-error" : undefined}
      />
      {failed && (
        <p id="prompt-error" role="alert" className="text-sm text-destructive">
          Échec : le détail est dans la notification. La saisie est conservée.
        </p>
      )}
      <DialogFooter className="sm:items-center">
        {ask.auto && (
          <Button
            type="button"
            variant="ghost"
            className="sm:mr-auto"
            title={ask.auto.hint}
            onClick={() => {
              onClose()
              ask.auto!.run()
            }}
          >
            <Sparkles /> {ask.auto.label}
          </Button>
        )}
        <Button type="button" variant="outline" onClick={onClose}>
          Annuler
        </Button>
        <Button type="submit" disabled={!ok || busy}>
          {ask.confirm}
        </Button>
      </DialogFooter>
    </form>
  )
}
