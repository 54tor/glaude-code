type Apply = (ratio: number) => void

// Défilement lié entre l'éditeur et l'aperçu Markdown : position en proportion (0 à 1) de la hauteur défilable.
// Chaque côté s'inscrit avec sa fonction d'application et renvoie la fonction de désinscription.
export type ScrollLink = {
  attachEditor(apply: Apply): () => void
  attachPreview(apply: Apply): () => void
  fromEditor(ratio: number): void
  fromPreview(ratio: number): void
}

export const SCROLL_TOLERANCE_PX = 2

export function createScrollLink(): ScrollLink {
  let editor: Apply | null = null
  let preview: Apply | null = null
  return {
    attachEditor(apply) {
      editor = apply
      return () => void (editor = null)
    },
    attachPreview(apply) {
      preview = apply
      return () => void (preview = null)
    },
    fromEditor: (ratio) => preview?.(ratio),
    fromPreview: (ratio) => editor?.(ratio),
  }
}
