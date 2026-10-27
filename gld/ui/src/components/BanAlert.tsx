import { useEffect, useState } from "react"
import { ShieldAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { AlertDialog, AlertDialogAction, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"

type Ban = { at: number; ip: string }

// Alerte dans chaque session connectée quand une adresse extérieure est bannie après des échecs de connexion répétés. Les bans
// d'avant l'ouverture de l'onglet ne sont pas rejoués ; une alerte non acquittée est perdue au rechargement.
export function BanAlert() {
  const { t } = useTranslation()
  const [bans, setBans] = useState<Ban[]>([])
  useEffect(() => {
    let live = true
    let since = -1 // -1 : pas encore calé sur l'horloge du serveur
    const poll = async () => {
      if (document.visibilityState !== "visible") return
      try {
        const r = await api.banAlerts(since < 0 ? 9_999_999_999 : since)
        if (!live || !r.ok) return
        if (since < 0) since = r.now
        else if (r.bans.length) {
          since = r.bans[r.bans.length - 1].at
          setBans((b) => [...b, ...r.bans])
        }
      } catch {
        /* serveur injoignable ou mode sans comptes : on réessaie au prochain passage */
      }
    }
    void poll()
    const h = setInterval(poll, 15_000)
    return () => {
      live = false
      clearInterval(h)
    }
  }, [])
  return (
    <AlertDialog open={bans.length > 0} onOpenChange={(o) => !o && setBans([])}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="size-5 text-destructive" aria-hidden /> {t("shell.ban.title", { count: bans.length })}
          </AlertDialogTitle>
          <AlertDialogDescription>{t("shell.ban.body")}</AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="max-h-48 space-y-1 overflow-y-auto text-sm">
          {bans.map((b, i) => (
            <li key={i} className="flex justify-between gap-3 rounded-md border px-3 py-1.5">
              {b.ip ? <code>{b.ip}</code> : <span>{t("shell.ban.hidden")}</span>}
              <span className="text-muted-foreground tabular-nums">{new Date(b.at * 1000).toLocaleTimeString()}</span>
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
