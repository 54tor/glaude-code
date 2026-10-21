import { useCallback, useEffect, useRef, useState } from "react"
import { api, type Issue, type Trashed } from "@/lib/api"
import type { List } from "./memoryLib"

// Liste de la mémoire, corbeille et santé. La mémoire change hors de l'interface (une session qui écrit, un éditeur) :
// on relit tout toutes les 15 s tant que l'onglet est visible, et au retour sur l'onglet. onTick laisse l'appelant
// relire en plus ce qu'il affiche (le fichier ouvert).
export function useMemoryData(onTick: () => void) {
  const [list, setList] = useState<List | null>(null)
  const [trash, setTrash] = useState<Trashed[]>([])
  const [issues, setIssues] = useState<Issue[]>([])
  const refresh = useCallback(
    () => Promise.all([api.memory().then(setList), api.trash().then((r) => setTrash(r.items)), api.memoryHealth().then((r) => setIssues(r.issues))]),
    []
  )
  const tick = useRef(onTick)
  useEffect(() => {
    tick.current = onTick
  })
  useEffect(() => {
    refresh().catch(() => {})
    const run = () => {
      if (document.visibilityState !== "visible") return
      refresh().catch(() => {})
      tick.current()
    }
    const t = setInterval(run, 15_000)
    document.addEventListener("visibilitychange", run)
    return () => {
      clearInterval(t)
      document.removeEventListener("visibilitychange", run)
    }
  }, [refresh])
  return { list, trash, issues, refresh }
}
