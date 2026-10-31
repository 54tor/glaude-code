import { useEffect, useRef } from "react"
import { i18n } from "@/i18n"
import type { Row } from "@/components/actions"

// Affiche par le service worker quand il est là (seul moyen sur mobile et dans l'application installée), sinon par la page.
async function showNotification(title: string, options: NotificationOptions) {
  const registration = await navigator.serviceWorker?.getRegistration().catch(() => undefined)
  if (registration) return void registration.showNotification(title, options)
  new Notification(title, options)
}

// Notification du navigateur (onglet en arrière-plan) quand une session ouverte demande une réponse (question, autorisation)
// ou qu'elle a fini et attend votre message. Permission demandée au premier clic ; un clic sur la notification ouvre la session.
export function useWaitingNotice(waiting: Set<string>, asking: Set<string>, everyId: Map<string, Row>) {
  const previousSets = useRef<{ waiting: Set<string>; asking: Set<string> } | null>(null)
  useEffect(() => {
    if (!("Notification" in window) || Notification.permission !== "default") return
    const requestPermission = () => void Notification.requestPermission()
    window.addEventListener("pointerdown", requestPermission, { once: true })
    return () => window.removeEventListener("pointerdown", requestPermission)
  }, [])
  useEffect(() => {
    const previous = previousSets.current
    previousSets.current = { waiting, asking }
    if (!previous || !document.hidden) return
    const notificationsGranted = "Notification" in window && Notification.permission === "granted"
    if (!notificationsGranted) return
    const notify = (sessionId: string, messageKey: string) =>
      void showNotification(i18n.t(messageKey), {
        body: everyId.get(sessionId)?.s.title ?? sessionId,
        tag: sessionId,
        data: { url: `/?s=${encodeURIComponent(sessionId)}` },
      })
    for (const sessionId of asking) {
      if (!previous.asking.has(sessionId)) notify(sessionId, "shell.notice.asking")
    }
    // Une session qui pose une question attend aussi : la question prime, pas de seconde notification.
    for (const sessionId of waiting) {
      if (!previous.waiting.has(sessionId) && !asking.has(sessionId)) notify(sessionId, "shell.notice.waiting")
    }
  }, [waiting, asking, everyId])
}
