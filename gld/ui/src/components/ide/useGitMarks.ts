import { useCallback, useEffect, useRef, useState } from "react"
import { api, type GitChange } from "@/lib/api"

export type GitMarks = {
  marks: Map<string, string>
  dirs: Set<string>
  more: number
}
const none = (): GitMarks => ({ marks: new Map(), dirs: new Set(), more: 0 })

// « XY » de git status --short → une lettre : conflit U, non suivi ?, sinon l'état de l'index puis celui de l'arbre.
const letter = (s: string) => {
  if (s.includes("U") || s === "AA" || s === "DD") return "U"
  if (s === "??") return "?"
  const c = s[0] !== " " ? s[0] : (s[1] ?? "M")
  return "MADR".includes(c) ? c : c === "C" ? "A" : "M"
}

export function toMarks(files: GitChange[], more = 0): GitMarks {
  const r = none()
  r.more = more
  for (const f of files) {
    r.marks.set(f.path, letter(f.status))
    for (let i = f.path.indexOf("/"); i > 0; i = f.path.indexOf("/", i + 1))
      r.dirs.add(f.path.slice(0, i))
  }
  return r
}

// Le serveur ne répond que pour un dépôt connu : sinon la liste est vide et rien n'est marqué.
export function useGitMarks(dir: string) {
  const [g, setG] = useState<GitMarks>(none)
  const seq = useRef(0)
  const refresh = useCallback(() => {
    const n = ++seq.current
    api.gitChanges(dir).then(
      (r) => n === seq.current && setG(toMarks(r.files ?? [], r.more ?? 0)),
      () => n === seq.current && setG(none())
    )
  }, [dir])
  useEffect(refresh, [refresh])
  return { ...g, refresh }
}
