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
        const q = draftOf(id).queue
        if (!info.ids.includes(id) || q.length === 0) return
        setDraft(id, { queue: q.slice(1) })
        const r = await api.post("/session-send", { id, text: withFollowUps(q[0], q.length > 1) }, 20_000)
        if (r.ok) return markSent(id)
        setDraft(id, { queue: [q[0], ...draftOf(id).queue] }) // non délivré : il retourne en tête de la file
        lastSent.set(id, Date.now() + 10_000) // pas de nouvel essai avant 10 s
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
