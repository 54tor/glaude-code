import { useCallback, useEffect, useRef, useState } from "react"
import { api, type GitChange } from "@/lib/api"

export type GitMarks = {
  marks: Map<string, string>
  dirs: Set<string>
  more: number
  prefix: string
}
const emptyMarks = (): GitMarks => ({ marks: new Map(), dirs: new Set(), more: 0, prefix: "" })

// « XY » de git status --short → une lettre : conflit U, non suivi ?, sinon l'état de l'index puis celui de l'arbre.
const letter = (status: string) => {
  const isConflict = status.includes("U") || status === "AA" || status === "DD"
  if (isConflict) return "U"
  if (status === "??") return "?"
  const code = status[0] !== " " ? status[0] : (status[1] ?? "M")
  if ("MADR".includes(code)) return code
  if (code === "C") return "A"
  return "M"
}

// prefix : chemin du dossier sous la racine du dépôt ; seuls ses changements comptent, ramenés au dossier.
export function toMarks(all: GitChange[], more = 0, prefix = ""): GitMarks {
  const files = all
    .filter((change) => change.path.startsWith(prefix))
    .map((change) => ({ ...change, path: change.path.slice(prefix.length) }))
  const result = emptyMarks()
  result.more = more
  result.prefix = prefix
  for (const file of files) {
    result.marks.set(file.path, letter(file.status))
    for (let slashIndex = file.path.indexOf("/"); slashIndex > 0; slashIndex = file.path.indexOf("/", slashIndex + 1))
      result.dirs.add(file.path.slice(0, slashIndex))
  }
  return result
}

// Le serveur ne répond que pour un dépôt connu : sinon la liste est vide et rien n'est marqué.
export function useGitMarks(dir: string) {
  const [gitMarks, setGitMarks] = useState<GitMarks>(emptyMarks)
  const latestRequest = useRef(0)
  const refresh = useCallback(() => {
    const requestNumber = ++latestRequest.current
    const isLatest = () => requestNumber === latestRequest.current
    api.gitChanges(dir).then(
      (response) => isLatest() && setGitMarks(toMarks(response.files ?? [], response.more ?? 0, response.prefix ?? "")),
      () => isLatest() && setGitMarks(emptyMarks())
    )
  }, [dir])
  useEffect(refresh, [refresh])
  return { ...gitMarks, refresh }
}
