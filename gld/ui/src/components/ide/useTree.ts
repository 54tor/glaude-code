import { useCallback, useEffect, useRef, useState } from "react"
import { api, type FsEntry } from "@/lib/api"
import { useLocal } from "@/hooks/useLocal"

export type Listing = { entries: FsEntry[]; truncated: boolean } | "loading" | "failed"

const parseOpen = (serialized: string): string[] => {
  try {
    const parsed: unknown = JSON.parse(serialized)
    if (!Array.isArray(parsed)) return []
    return parsed.filter((path): path is string => typeof path === "string")
  } catch {
    return []
  }
}

// Dossiers dépliés (mémorisés par navigateur) et listings chargés un dossier à la fois ; la racine a le chemin "".
export function useTree(dir: string) {
  const [raw, setRaw] = useLocal(`gld.ide.${dir}.open`, "[]")
  const open = new Set(parseOpen(raw))
  const [lists, setLists] = useState<Record<string, Listing>>({})
  const started = useRef(new Set<string>())
  const latestRequestByPath = useRef(new Map<string, number>()) // chemin → dernière lecture lancée : une réponse plus ancienne est ignorée

  const load = useCallback(
    (path: string) => {
      const requestNumber = (latestRequestByPath.current.get(path) ?? 0) + 1
      latestRequestByPath.current.set(path, requestNumber)
      const storeListing = (listing: Listing) =>
        latestRequestByPath.current.get(path) === requestNumber && setLists((previous) => ({ ...previous, [path]: listing }))
      setLists((previous) => {
        const alreadyLoaded = previous[path] && typeof previous[path] === "object"
        return alreadyLoaded ? previous : { ...previous, [path]: "loading" }
      })
      api.fsTree(dir, path).then(storeListing, () => storeListing("failed"))
    },
    [dir]
  )

  const ensure = useCallback(
    (path: string) => {
      if (started.current.has(path)) return
      started.current.add(path)
      load(path)
    },
    [load]
  )

  // Au montage : la racine et les dossiers dépliés d'une visite précédente.
  useEffect(() => {
    ensure("")
    for (const openPath of parseOpen(raw)) ensure(openPath)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir])

  const setOpen = (path: string, expanded: boolean) => {
    const next = new Set(open)
    if (expanded) next.add(path)
    else next.delete(path)
    setRaw(JSON.stringify([...next]))
    if (expanded) {
      started.current.add(path)
      load(path)
    }
  }
  const refresh = (path: string) => load(path)
  // Dossiers repliés : leur listing peut rester périmé, il sera relu au prochain dépliage.
  const refreshAll = () => {
    load("")
    for (const openPath of open) load(openPath)
  }
  const collapseAll = () => setRaw("[]")

  return { open, lists, setOpen, refresh, refreshAll, collapseAll }
}
