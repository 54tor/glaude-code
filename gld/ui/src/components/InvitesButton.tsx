import { useState } from "react"
import { useTranslation } from "react-i18next"
import { Share2 } from "lucide-react"
import { useSearchParams } from "wouter"
import { useShares } from "@/hooks/useShares"
import { InvitesOverlay } from "./InvitesOverlay"

// Entrée du menu latéral : visible seulement quand un partage existe ; la pastille compte invités connectés + messages d'invités en attente.
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
      <button
        aria-label={t("shell.invites.buttonAria", { connected, pending })}
        onClick={() => setOpen(true)}
        className="flex h-8 w-full items-center gap-2.5 rounded-md px-2 text-sm text-sidebar-foreground/85 transition-colors hover:bg-sidebar-accent/60 hover:text-sidebar-foreground"
      >
        <Share2 className="size-4 shrink-0 text-muted-foreground" />
        <span className="flex-1 truncate text-left">{t("shell.invites.title")}</span>
        {badge > 0 && <span className="text-xs text-muted-foreground tabular-nums">{badge}</span>}
      </button>
      {open && <InvitesOverlay onClose={() => setOpen(false)} onOpenSession={read} />}
    </>
  )
}
