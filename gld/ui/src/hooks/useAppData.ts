import { useCallback, useEffect, useRef, useState } from "react"
import { toast } from "sonner"
import { i18n } from "@/i18n"
import { api, type Data, type SnipScope, type Snippet } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { getAuth } from "@/lib/auth"
import { REFRESH } from "@/lib/sessionsView"

export type Snippets = { dir: string; files: Snippet[]; scopes: SnipScope[] }

const STAMP_CHECK_INTERVAL_MS = 3_000
const SERVER_DOWN_TOAST_ID = "server-down"
const CLOSING_MASK_MS = 15_000

// Sessions dont la fermeture vient d'être demandée : le processus met un instant à finir, et une relecture du serveur les
// annonce encore ouvertes. On les masque jusqu'à ce qu'il ne les liste plus (ou jusqu'à l'échéance).
const closingUntil = new Map<string, number>()

export function markClosing(sessionIds: string[]) {
  const deadline = Date.now() + CLOSING_MASK_MS
  for (const sessionId of sessionIds) closingUntil.set(sessionId, deadline)
}

function withoutClosing(freshData: Data): Data {
  const nowMs = Date.now()
  for (const [sessionId, deadline] of closingUntil) {
    if (deadline <= nowMs || !freshData.live.includes(sessionId)) closingUntil.delete(sessionId)
  }
  if (closingUntil.size === 0) return freshData
  const isOpen = (sessionId: string) => !closingUntil.has(sessionId)
  return { ...freshData, live: freshData.live.filter(isOpen), waiting: freshData.waiting.filter(isOpen) }
}

// Sessions et snippets du serveur, relus toutes les 15 s tant que l'onglet est visible et au retour sur l'onglet, et dès qu'un
// transcript change (vérifié toutes les 3 s).
// now : heure du dernier chargement, pour « Récentes » (le rendu reste pur).
export function useAppData() {
  const [data, setData] = useState<Data | null>(null)
  const [snips, setSnips] = useState<Snippets | null>(null)
  const [now, setNow] = useState(0)
  const [error, setError] = useState<string | null>(null) // serveur injoignable : dit une fois, effacé dès qu'une relecture passe
  const [denied, setDenied] = useState(false) // mode multi-utilisateur sans le droit sessions_read sur ce home : rien à afficher, rien à signaler
  const lastStamp = useRef(0)
  const reload = useCallback(async () => {
    try {
      const [freshData, freshSnippets] = await Promise.all([api.data(), api.snippets()])
      setData(withoutClosing(freshData))
      setSnips(freshSnippets)
      setNow(Date.now() / 1000)
      setError(null)
      setDenied(false)
      toast.dismiss(SERVER_DOWN_TOAST_ID)
    } catch (loadError) {
      const sessionsReadDenied = loadError instanceof ApiError && loadError.status === 403 && getAuth().state === "ok"
      if (sessionsReadDenied) {
        setDenied(true)
        setError(i18n.t("auth.denied.sessions"))
        throw loadError
      }
      const message = loadError instanceof Error ? loadError.message : String(loadError)
      setError(message)
      toast.error(i18n.t("common.serverUnreachable", { error: message }), { id: SERVER_DOWN_TOAST_ID, duration: Infinity })
      throw loadError
    }
  }, [])
  useEffect(() => {
    void Promise.resolve().then(reload).catch(() => {}) // premier chargement, hors du corps synchrone de l'effet
    const reloadIfVisible = () => document.visibilityState === "visible" && reload().catch(() => {})
    const refreshTimer = setInterval(reloadIfVisible, REFRESH)
    // Une session écrite ailleurs (un terminal) : le tampon des transcripts change, on relit tout de suite.
    const stampWatcher = setInterval(async () => {
      if (document.visibilityState !== "visible") return
      const { stamp } = await api.sessionsStamp().catch(() => ({ stamp: 0 }))
      const transcriptsChanged = stamp && lastStamp.current && stamp !== lastStamp.current
      if (transcriptsChanged) reload().catch(() => {})
      if (stamp) lastStamp.current = stamp
    }, STAMP_CHECK_INTERVAL_MS)
    document.addEventListener("visibilitychange", reloadIfVisible)
    return () => {
      clearInterval(refreshTimer)
      clearInterval(stampWatcher)
      document.removeEventListener("visibilitychange", reloadIfVisible)
    }
  }, [reload])
  return { data, setData, snips, now, reload, error, denied }
}
