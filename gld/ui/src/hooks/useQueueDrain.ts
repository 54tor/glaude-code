import { useEffect, useRef, useSyncExternalStore } from "react"
import { toast } from "sonner"
import { api } from "@/lib/api"
import { withFollowUps } from "@/lib/queue"
import { draftOf, queuedKey, setDraft, subscribe } from "./useDraft"

// Envoi des messages en file des sessions ouvertes (tmux), fenêtre ouverte ou non : le premier part dès que la session attend
// (ni au travail, ni dans une question, où il répondrait à sa place). Un court délai après chaque envoi laisse le message démarrer
// un tour avant de juger la session libre.
const SETTLE = 4500
const lastSent = new Map<string, number>()
const inflight = new Set<string>()
export const markSent = (id: string) => void lastSent.set(id, Date.now())

export function useQueueDrain(live: Set<string>, waiting: Set<string>, asking: Set<string>) {
  const key = useSyncExternalStore(subscribe, queuedKey)
  const now = useRef({ live, waiting, asking, key })
  useEffect(() => {
    now.current = { live, waiting, asking, key }
  })
  useEffect(() => {
    // Un seul onglet vide la file d'une session : les autres sautent leur tour au lieu d'envoyer le même message.
    const drain = (id: string) => {
      inflight.add(id)
      const run = () => send(id).finally(() => inflight.delete(id))
      if (!navigator.locks) return run()
      return navigator.locks.request("gld.queue." + id, { ifAvailable: true }, (lock) => {
        if (lock) return run()
        inflight.delete(id)
        markSent(id) // l'autre onglet envoie : on lui laisse le délai SETTLE avant de juger la session libre
      })
    }
    const send = async (id: string) => {
      try {
        const info = await api.sendInfo()
        const d = draftOf(id)
        if (!info.ids.includes(id) || d.queue.length === 0 || d.sending !== undefined) return // en cours ailleurs, ou incertain
        const head = d.queue[0]
        setDraft(id, { sending: Date.now() }) // reste en tête de file jusqu'à la réponse : un rechargement ne le perd pas
        const r = await api.post("/session-send", { id, text: withFollowUps(head, d.queue.length > 1) }, 20_000)
        if (r.net) return // livré ou non, on ne sait pas : le marqueur reste, l'interface propose de renvoyer ou de retirer
        const q = draftOf(id).queue
        setDraft(id, { queue: r.ok && q[0] === head ? q.slice(1) : q, sending: undefined })
        if (r.ok) return markSent(id)
        lastSent.set(id, Date.now() + 10_000) // refusé : pas de nouvel essai avant 10 s
        toast.error(r.out)
      } catch {
        /* réseau coupé : la file reste, le prochain passage réessaie */
      }
    }
    const h = setInterval(() => {
      const c = now.current
      for (const id of c.key.split(",").filter(Boolean)) {
        if (c.live.has(id) && c.waiting.has(id) && !c.asking.has(id) && !inflight.has(id) && Date.now() - (lastSent.get(id) ?? 0) >= SETTLE) void drain(id)
      }
    }, 1500)
    return () => clearInterval(h)
  }, [])
}
