import { useEffect, useState } from "react"
import { api, type Hit } from "@/lib/api"

const MIN_QUERY_LENGTH = 2
const TYPING_PAUSE_MS = 250

// Recherche dans le texte des conversations, côté serveur, après une courte pause de frappe (2 caractères au moins).
// Renvoie les résultats par id de session, ou null tant qu'ils ne sont pas arrivés pour cette recherche.
export function useSearchHits(query: string) {
  const [hits, setHits] = useState<{ query: string; bySession: Map<string, Hit> } | null>(null)
  useEffect(() => {
    if (query.length < MIN_QUERY_LENGTH) return
    let mounted = true
    const debounceTimer = setTimeout(
      () =>
        api.search(query).then((response) => {
          if (!mounted) return
          setHits({ query, bySession: new Map(response.hits.map((hit) => [hit.id, hit])) })
        }),
      TYPING_PAUSE_MS
    )
    return () => {
      mounted = false
      clearTimeout(debounceTimer)
    }
  }, [query])
  const queryIsLongEnough = query.length >= MIN_QUERY_LENGTH
  return queryIsLongEnough && hits?.query === query ? hits.bySession : null
}
