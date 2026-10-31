import { useEffect, useState } from "react"
import { ShieldAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { ApiError } from "@/lib/http"
import { AlertDialog, AlertDialogAction, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"

type Ban = { at: number; ip: string }

const POLL_INTERVAL_MS = 15_000
const HTTP_NOT_FOUND = 404
const MILLISECONDS_PER_SECOND = 1000

function formatAlertTime(unixSeconds: number) {
  return new Date(unixSeconds * MILLISECONDS_PER_SECOND).toLocaleTimeString()
}

// Alerte dans chaque session connectée quand une adresse extérieure est bannie après des échecs de connexion répétés, ou
// qu'un appareil est jumelé sur mon compte via POST /v1/pairing/codes (une session volée pourrait en créer une : l'alerte pousse à
// vérifier /v1/auth/sessions et à révoquer ce qui n'est pas de moi). Les événements d'avant l'ouverture de l'onglet ne sont pas
// rejoués ; une alerte non acquittée est perdue au rechargement.
export function BanAlert() {
  const { t } = useTranslation()
  const [bans, setBans] = useState<Ban[]>([])
  const [pairings, setPairings] = useState<Ban[]>([])
  useEffect(() => {
    let isMounted = true
    let cursorAfter = -1 // -1 : premier relevé, qui ne rend que le curseur courant (rien d'antérieur à l'ouverture)
    const poll = async () => {
      const isPageVisible = document.visibilityState === "visible"
      if (!isMounted || !isPageVisible) return
      try {
        const alerts = await api.banAlerts(cursorAfter)
        if (!isMounted) return
        const isFirstPoll = cursorAfter < 0
        cursorAfter = alerts.cursor
        if (isFirstPoll) return
        if (alerts.bans.length) setBans((previousBans) => [...previousBans, ...alerts.bans])
        if (alerts.pairings.length) setPairings((previousPairings) => [...previousPairings, ...alerts.pairings])
      } catch (error) {
        // 404 : mode sans comptes, la route n'existe pas ; sinon serveur injoignable, on réessaie au prochain passage.
        const isRouteMissing = error instanceof ApiError && error.status === HTTP_NOT_FOUND
        if (isRouteMissing) isMounted = false
      }
    }
    void poll()
    const pollTimer = setInterval(poll, POLL_INTERVAL_MS)
    return () => {
      isMounted = false
      clearInterval(pollTimer)
    }
  }, [])
  const open = bans.length > 0 || pairings.length > 0
  const dismiss = () => {
    setBans([])
    setPairings([])
  }
  return (
    <AlertDialog open={open} onOpenChange={(isOpen) => !isOpen && dismiss()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="size-5 text-destructive" aria-hidden /> {t("shell.securityAlerts.title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {bans.length > 0 && <div>{t("shell.ban.body")}</div>}
            {pairings.length > 0 && <div>{t("shell.pairAlert.body")}</div>}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="max-h-48 space-y-1 overflow-y-auto text-sm">
          {bans.map((ban, banIndex) => (
            <li key={`ban-${banIndex}`} className="flex justify-between gap-3 rounded-md border px-3 py-1.5">
              {ban.ip ? <code>{ban.ip}</code> : <span>{t("shell.ban.hidden")}</span>}
              <span className="text-muted-foreground tabular-nums">{formatAlertTime(ban.at)}</span>
            </li>
          ))}
          {pairings.map((pairing, pairingIndex) => (
            <li key={`pair-${pairingIndex}`} className="flex justify-between gap-3 rounded-md border px-3 py-1.5">
              <span>{t("shell.pairAlert.item", { ip: pairing.ip })}</span>
              <span className="text-muted-foreground tabular-nums">{formatAlertTime(pairing.at)}</span>
            </li>
          ))}
        </ul>
        <AlertDialogFooter>
          <AlertDialogAction>{t("shell.ban.ok")}</AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  )
}
