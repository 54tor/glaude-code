import { useCallback, useEffect, useRef, useState } from "react"
import { api, type FsEntry } from "@/lib/api"
import { useLocal } from "@/hooks/useLocal"

export type Listing = { entries: FsEntry[]; truncated: boolean } | "loading" | "failed"

const parseOpen = (s: string): string[] => {
  try {
    const v: unknown = JSON.parse(s)
    return Array.isArray(v) ? v.filter((x): x is string => typeof x === "string") : []
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

  const load = useCallback(
    (path: string) => {
      setLists((l) => (l[path] && typeof l[path] === "object" ? l : { ...l, [path]: "loading" }))
      api.fsTree(dir, path).then(
        (r) => setLists((l) => ({ ...l, [path]: r })),
        () => setLists((l) => ({ ...l, [path]: "failed" }))
      )
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
    for (const p of parseOpen(raw)) ensure(p)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dir])

  const setOpen = (path: string, on: boolean) => {
    const next = new Set(open)
    if (on) next.add(path)
    else next.delete(path)
    setRaw(JSON.stringify([...next]))
    if (on) {
      started.current.add(path)
      load(path)
    }
  }
  const refresh = (path: string) => load(path)

  return { open, lists, setOpen, refresh }
}
