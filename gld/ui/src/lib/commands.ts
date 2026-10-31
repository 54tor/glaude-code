const YAML_FRONT_MATTER = /^---\n[\s\S]*?\n---\n?/
const ARGUMENT_PLACEHOLDER = /\$ARGUMENTS|\$\d/

// Corps d'une commande Claude Code (sans son en-tête YAML), pour l'insérer dans un prompt.
export const body = (commandText: string) => commandText.replace(YAML_FRONT_MATTER, "").trim()

// Une commande prend ses arguments par $ARGUMENTS (ou $1, $2…) : on les met en évidence après insertion.
export const argAt = (text: string, from = 0) => {
  const placeholderMatch = ARGUMENT_PLACEHOLDER.exec(text.slice(from))
  if (!placeholderMatch) return null
  return { at: from + placeholderMatch.index, len: placeholderMatch[0].length }
}
