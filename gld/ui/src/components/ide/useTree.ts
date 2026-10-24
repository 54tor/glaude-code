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
  const seq = useRef(new Map<string, number>()) // chemin → dernière lecture lancée : une réponse plus ancienne est ignorée

  const load = useCallback(
    (path: string) => {
      const n = (seq.current.get(path) ?? 0) + 1
      seq.current.set(path, n)
      const set = (v: Listing) => seq.current.get(path) === n && setLists((l) => ({ ...l, [path]: v }))
      setLists((l) => (l[path] && typeof l[path] === "object" ? l : { ...l, [path]: "loading" }))
      api.fsTree(dir, path).then(set, () => set("failed"))
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
