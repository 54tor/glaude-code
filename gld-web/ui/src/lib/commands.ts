// Corps d'une commande Claude Code (sans son en-tête YAML), pour l'insérer dans un prompt.
export const body = (t: string) => t.replace(/^---\n[\s\S]*?\n---\n?/, "").trim()

// Une commande prend ses arguments par $ARGUMENTS (ou $1, $2…) : on les met en évidence après insertion.
export const argAt = (t: string, from = 0) => {
  const m = /\$ARGUMENTS|\$\d/.exec(t.slice(from))
  return m ? { at: from + m.index, len: m[0].length } : null
}
