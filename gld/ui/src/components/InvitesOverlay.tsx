import { useTranslation } from "react-i18next"
import { Users } from "lucide-react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { InvitesPanel } from "./InvitesPanel"

// Invitations de toutes les sessions partagées de l'hôte.
export function InvitesOverlay({ onClose, onOpenSession }: { onClose: () => void; onOpenSession?: (id: string) => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open onOpenChange={(isOpen) => !isOpen && onClose()}>
      <DialogContent className="max-h-[85vh] gap-4 overflow-y-auto sm:max-w-2xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Users className="size-5 text-primary" aria-hidden /> {t("shell.invites.title")}
          </DialogTitle>
          <DialogDescription>{t("shell.invites.intro")}</DialogDescription>
        </DialogHeader>
        {location.protocol !== "https:" && <p className="text-sm text-warning">{t("shell.share.insecure")}</p>}
        <p className="text-sm text-muted-foreground">{t("shell.share.singleUse")}</p>
        <InvitesPanel fast onOpenSession={onOpenSession} />
      </DialogContent>
    </Dialog>
  )
}
