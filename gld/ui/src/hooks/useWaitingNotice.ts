import { useEffect, useRef } from "react"
import { i18n } from "@/i18n"
import type { Row } from "@/components/actions"

// Notification du navigateur quand une session se met à attendre (onglet en arrière-plan) ; permission demandée au premier clic.
export function useWaitingNotice(waiting: Set<string>, everyId: Map<string, Row>) {
  const wasWaiting = useRef<Set<string> | null>(null)
  useEffect(() => {
    if (!("Notification" in window) || Notification.permission !== "default") return
    const ask = () => void Notification.requestPermission()
    window.addEventListener("pointerdown", ask, { once: true })
    return () => window.removeEventListener("pointerdown", ask)
  }, [])
  useEffect(() => {
    const prev = wasWaiting.current
    wasWaiting.current = waiting
    if (!prev || !document.hidden || !("Notification" in window) || Notification.permission !== "granted") return
    for (const id of waiting)
      if (!prev.has(id))
        new Notification(i18n.t("shell.notice.waiting"), {
          body: everyId.get(id)?.s.title ?? id,
          tag: id,
        })
  }, [waiting, everyId])
}
