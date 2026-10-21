import { useEffect, useState } from "react"
import { api, type Hit } from "@/lib/api"

// Recherche dans le texte des conversations, côté serveur, après une courte pause de frappe (2 caractères au moins).
// Renvoie les résultats par id de session, ou null tant qu'ils ne sont pas arrivés pour cette recherche.
export function useSearchHits(qt: string) {
  const [hits, setHits] = useState<{ q: string; m: Map<string, Hit> } | null>(null)
  useEffect(() => {
    if (qt.length < 2) return
    let live = true
    const t = setTimeout(() => api.search(qt).then((r) => live && setHits({ q: qt, m: new Map(r.hits.map((h) => [h.id, h])) })), 250)
    return () => {
      live = false
      clearTimeout(t)
    }
  }, [qt])
  return qt.length >= 2 && hits?.q === qt ? hits.m : null
}
