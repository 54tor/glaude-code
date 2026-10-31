import { useCallback, useEffect, useRef, useState } from "react"
import { api, type FileLoads, type Issue, type Trashed } from "@/lib/api"
import type { List } from "./memoryLib"

const REFRESH_INTERVAL_MS = 15_000

// Liste de la mémoire, corbeille et santé. La mémoire change hors de l'interface (une session qui écrit, un éditeur) :
// on relit tout toutes les 15 s tant que l'onglet est visible, et au retour sur l'onglet. onTick laisse l'appelant
// relire en plus ce qu'il affiche (le fichier ouvert).
export function useMemoryData(onTick: () => void) {
  const [list, setList] = useState<List | null>(null)
  const [trash, setTrash] = useState<Trashed[]>([])
  const [issues, setIssues] = useState<Issue[]>([])
  const [loads, setLoads] = useState<{ hook: boolean; files: Record<string, FileLoads> }>({ hook: false, files: {} }) // chargements réels (hook InstructionsLoaded)
  const refresh = useCallback(
    () =>
      Promise.all([
        api.memory().then(setList),
        api.trash().then((trashResponse) => setTrash(trashResponse.items)),
        api.memoryHealth().then((healthResponse) => setIssues(healthResponse.issues)),
        api.memoryLoaded().then(setLoads),
      ]),
    []
  )
  const latestOnTick = useRef(onTick)
  useEffect(() => {
    latestOnTick.current = onTick
  })
  useEffect(() => {
    refresh().catch(() => {})
    const refreshIfVisible = () => {
      if (document.visibilityState !== "visible") return
      refresh().catch(() => {})
      latestOnTick.current()
    }
    const refreshInterval = setInterval(refreshIfVisible, REFRESH_INTERVAL_MS)
    document.addEventListener("visibilitychange", refreshIfVisible)
    return () => {
      clearInterval(refreshInterval)
      document.removeEventListener("visibilitychange", refreshIfVisible)
    }
  }, [refresh])
  return { list, trash, issues, loads, refresh }
}
