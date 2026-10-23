import { useEffect, useState } from "react"

type Draft = { key: string; text: string } | null

// Brouillon d'un éditeur (mémoire, skill, snippet, réglages JSON) gardé dans ce navigateur : recharger la page ou fermer
// l'onglet ne le perd pas. Un seul brouillon par éditeur, comme à l'écran.
export function useEditorDraft(name: string) {
  const k = "gld.editor." + name
  const [draft, setDraft] = useState<Draft>(() => {
    try {
      const d = JSON.parse(localStorage.getItem(k) ?? "null")
      return d && typeof d.key === "string" && typeof d.text === "string" ? { key: d.key, text: d.text } : null
    } catch {
      return null
    }
  })
  useEffect(() => {
    try {
      if (draft) localStorage.setItem(k, JSON.stringify(draft))
      else localStorage.removeItem(k)
    } catch {
      /* stockage plein ou indisponible : le brouillon reste en mémoire */
    }
  }, [k, draft])
  return [draft, setDraft] as const
}

// Tant qu'une modification n'est pas enregistrée, le navigateur demande confirmation avant de quitter ou recharger la page.
export function useLeaveGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return
    const stop = (e: BeforeUnloadEvent) => e.preventDefault()
    addEventListener("beforeunload", stop)
    return () => removeEventListener("beforeunload", stop)
  }, [dirty])
}
