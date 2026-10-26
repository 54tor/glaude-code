import { useEffect, useRef } from "react"
import { i18n } from "@/i18n"
import type { Row } from "@/components/actions"

// Affiche par le service worker quand il est là (seul moyen sur mobile et dans l'application installée), sinon par la page.
async function show(title: string, opts: NotificationOptions) {
  const reg = await navigator.serviceWorker?.getRegistration().catch(() => undefined)
  if (reg) return void reg.showNotification(title, opts)
  new Notification(title, opts)
}

// Notification du navigateur (onglet en arrière-plan) quand une session ouverte demande une réponse (question, autorisation)
// ou qu'elle a fini et attend votre message. Permission demandée au premier clic ; un clic sur la notification ouvre la session.
export function useWaitingNotice(waiting: Set<string>, asking: Set<string>, everyId: Map<string, Row>) {
  const was = useRef<{ waiting: Set<string>; asking: Set<string> } | null>(null)
  useEffect(() => {
    if (!("Notification" in window) || Notification.permission !== "default") return
    const ask = () => void Notification.requestPermission()
    window.addEventListener("pointerdown", ask, { once: true })
    return () => window.removeEventListener("pointerdown", ask)
  }, [])
  useEffect(() => {
    const prev = was.current
    was.current = { waiting, asking }
    if (!prev || !document.hidden || !("Notification" in window) || Notification.permission !== "granted") return
    const notify = (id: string, key: string) =>
      void show(i18n.t(key), { body: everyId.get(id)?.s.title ?? id, tag: id, data: { url: `/?s=${encodeURIComponent(id)}` } })
    for (const id of asking) if (!prev.asking.has(id)) notify(id, "shell.notice.asking")
    // Une session qui pose une question attend aussi : la question prime, pas de seconde notification.
    for (const id of waiting) if (!prev.waiting.has(id) && !asking.has(id)) notify(id, "shell.notice.waiting")
  }, [waiting, asking, everyId])
}
