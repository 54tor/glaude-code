import { useEffect, useState } from "react"
import { ShieldAlert } from "lucide-react"
import { useTranslation } from "react-i18next"
import { api } from "@/lib/api"
import { AlertDialog, AlertDialogAction, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"

type Ban = { at: number; ip: string }

// Alerte dans chaque session connectée quand une adresse extérieure est bannie après des échecs de connexion répétés, ou
// qu'un appareil est jumelé sur mon compte via /pair-code (une session volée pourrait en créer une : l'alerte pousse à
// vérifier /auth/sessions et à révoquer ce qui n'est pas de moi). Les événements d'avant l'ouverture de l'onglet ne sont pas
// rejoués ; une alerte non acquittée est perdue au rechargement.
export function BanAlert() {
  const { t } = useTranslation()
  const [bans, setBans] = useState<Ban[]>([])
  const [pairings, setPairings] = useState<Ban[]>([])
  useEffect(() => {
    let live = true
    let after = -1 // -1 : premier relevé, qui ne rend que le curseur courant (rien d'antérieur à l'ouverture)
    const poll = async () => {
      if (document.visibilityState !== "visible") return
      try {
        const r = await api.banAlerts(after)
        if (!live || !r.ok) return
        const first = after < 0
        after = r.cursor
        if (first) return
        if (r.bans.length) setBans((b) => [...b, ...r.bans])
        if (r.pairings.length) setPairings((p) => [...p, ...r.pairings])
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
  const open = bans.length > 0 || pairings.length > 0
  const dismiss = () => {
    setBans([])
    setPairings([])
  }
  return (
    <AlertDialog open={open} onOpenChange={(o) => !o && dismiss()}>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle className="flex items-center gap-2">
            <ShieldAlert className="size-5 text-destructive" aria-hidden /> {t("shell.securityAlerts.title")}
          </AlertDialogTitle>
          <AlertDialogDescription>
            {bans.length > 0 && <div>{t("shell.ban.body")}</div>}
            {pairings.length > 0 && <div>{t("shell.pair.body")}</div>}
          </AlertDialogDescription>
        </AlertDialogHeader>
        <ul className="max-h-48 space-y-1 overflow-y-auto text-sm">
          {bans.map((b, i) => (
            <li key={`ban-${i}`} className="flex justify-between gap-3 rounded-md border px-3 py-1.5">
              {b.ip ? <code>{b.ip}</code> : <span>{t("shell.ban.hidden")}</span>}
              <span className="text-muted-foreground tabular-nums">{new Date(b.at * 1000).toLocaleTimeString()}</span>
            </li>
          ))}
          {pairings.map((p, i) => (
            <li key={`pair-${i}`} className="flex justify-between gap-3 rounded-md border px-3 py-1.5">
              <span>{t("shell.pair.item", { ip: p.ip })}</span>
              <span className="text-muted-foreground tabular-nums">{new Date(p.at * 1000).toLocaleTimeString()}</span>
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
