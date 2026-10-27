import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Users } from "lucide-react"
import { useSearchParams } from "wouter"
import { useShares } from "@/hooks/useShares"
import { Button } from "@/components/ui/button"
import { InvitesOverlay } from "./InvitesOverlay"

// Bouton de l'en-tête : visible seulement quand un partage existe ; la pastille compte invités connectés + messages d'invités en attente.
export function InvitesButton() {
  const { t } = useTranslation()
  const { shares } = useShares()
  const [open, setOpen] = useState(false)
  const [, setParams] = useSearchParams()
  if (!shares?.length) return null
  const invites = shares.flatMap((s) => s.invites)
  const connected = invites.filter((i) => i.state === "connected").length
  const pending = invites.reduce((n, i) => n + i.queue.length, 0)
  const badge = connected + pending
  const read = (id: string) => {
    setParams((p) => {
      p.set("s", id)
      return p
    })
    setOpen(false)
  }
  return (
    <>
      <Button variant="ghost" size="icon-sm" className="relative" aria-label={t("shell.invites.buttonAria", { connected, pending })} title={t("shell.invites.button")} onClick={() => setOpen(true)}>
        <Users />
        {badge > 0 && <span className="absolute -top-0.5 -right-0.5 flex h-4 min-w-4 items-center justify-center rounded-full bg-primary px-1 text-2xs text-primary-foreground tabular-nums">{badge}</span>}
      </Button>
      {open && <InvitesOverlay onClose={() => setOpen(false)} onOpenSession={read} />}
    </>
  )
}
