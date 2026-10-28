import { useTranslation } from "react-i18next"
import { Share2 } from "lucide-react"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { InvitesPanel } from "./InvitesPanel"
import { loopback, TunnelHint } from "./PairDevice"

// Invitations d'une session (share.go) : la vue de l'overlay des invitations, limitée à cette session.
export function ShareDialog({ id, dir, title, onClose }: { id: string; dir: string; title?: string; onClose: () => void }) {
  const { t } = useTranslation()
  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="gap-4 sm:max-w-xl">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Share2 className="size-5 text-primary" aria-hidden /> {t("shell.share.title")}
          </DialogTitle>
          <DialogDescription>{t("shell.share.intro")}</DialogDescription>
        </DialogHeader>
        <p className="text-sm text-muted-foreground">{t("shell.share.singleUse")}</p>
        {loopback(location.origin) && <TunnelHint origin={location.origin} />}
        {location.protocol !== "https:" && <p className="text-sm text-warning">{t("shell.share.insecure")}</p>}
        <InvitesPanel only={{ id, dir, title }} fast />
      </DialogContent>
    </Dialog>
  )
}
