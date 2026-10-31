import { useEffect, useState } from "react"

type Draft = { key: string; text: string } | null

const readStoredDraft = (storageKey: string): Draft => {
  try {
    const storedDraft = JSON.parse(localStorage.getItem(storageKey) ?? "null")
    const isValidDraft = storedDraft && typeof storedDraft.key === "string" && typeof storedDraft.text === "string"
    return isValidDraft ? { key: storedDraft.key, text: storedDraft.text } : null
  } catch {
    return null
  }
}

// Brouillon d'un éditeur (mémoire, skill, snippet, réglages JSON) gardé dans ce navigateur : recharger la page ou fermer
// l'onglet ne le perd pas. Un seul brouillon par éditeur, comme à l'écran.
export function useEditorDraft(name: string) {
  const storageKey = "gld.editor." + name
  const [draft, setDraft] = useState<Draft>(() => readStoredDraft(storageKey))
  useEffect(() => {
    try {
      if (draft) localStorage.setItem(storageKey, JSON.stringify(draft))
      else localStorage.removeItem(storageKey)
    } catch {
      /* stockage plein ou indisponible : le brouillon reste en mémoire */
    }
  }, [storageKey, draft])
  return [draft, setDraft] as const
}

// Tant qu'une modification n'est pas enregistrée, le navigateur demande confirmation avant de quitter ou recharger la page.
export function useLeaveGuard(dirty: boolean) {
  useEffect(() => {
    if (!dirty) return
    const askBeforeLeaving = (event: BeforeUnloadEvent) => {
      event.preventDefault()
      event.returnValue = "" // navigateurs qui ignorent encore preventDefault seul
    }
    addEventListener("beforeunload", askBeforeLeaving)
    return () => removeEventListener("beforeunload", askBeforeLeaving)
  }, [dirty])
}
