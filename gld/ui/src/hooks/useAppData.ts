import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { api, type Data, type SnipScope, type Snippet } from "@/lib/api"
import { REFRESH } from "@/lib/sessionsView"

export type Snippets = { dir: string; files: Snippet[]; scopes: SnipScope[] }

// Sessions et snippets du serveur, relus toutes les 15 s tant que l'onglet est visible et au retour sur l'onglet, et dès qu'un
// transcript change (vérifié toutes les 3 s).
// now : heure du dernier chargement, pour « Récentes » (le rendu reste pur).
export function useAppData() {
  const [data, setData] = useState<Data | null>(null)
  const [snips, setSnips] = useState<Snippets | null>(null)
  const [now, setNow] = useState(0)
  const [error, setError] = useState<string | null>(null) // serveur injoignable : dit une fois, effacé dès qu'une relecture passe
  const stamps = useRef(0)
  const reload = useCallback(async () => {
    try {
      const [d, s] = await Promise.all([api.data(), api.snippets()])
      setData(d)
      setSnips(s)
      setNow(Date.now() / 1000)
      setError(null)
      toast.dismiss("server-down")
    } catch (e) {
      const msg = e instanceof Error ? e.message : String(e)
      setError(msg)
      toast.error(i18n.t("common.serverUnreachable", { error: msg }), { id: "server-down", duration: Infinity })
      throw e
    }
  }, [])
  useEffect(() => {
    void Promise.resolve().then(reload).catch(() => {}) // premier chargement, hors du corps synchrone de l'effet
    const tick = () => document.visibilityState === "visible" && reload().catch(() => {})
    const t = setInterval(tick, REFRESH)
    // Une session écrite ailleurs (un terminal) : le tampon des transcripts change, on relit tout de suite.
    const watch = setInterval(async () => {
      if (document.visibilityState !== "visible") return
      const { stamp } = await api.sessionsStamp().catch(() => ({ stamp: 0 }))
      if (stamp && stamps.current && stamp !== stamps.current) reload().catch(() => {})
      if (stamp) stamps.current = stamp
    }, 3_000)
    document.addEventListener("visibilitychange", tick)
    return () => {
      clearInterval(t)
      clearInterval(watch)
      document.removeEventListener("visibilitychange", tick)
    }
  }, [reload])
  return { data, setData, snips, now, reload, error }
}
