// Le navigateur coupe volontiers après un tiret (« UI-01 » → « UI- » / « 01 ») : les mots courts à tirets restent d'un bloc, sans
// changer le texte (la copie est intacte). Au-delà de 24 caractères ils restent coupables, sinon un identifiant long ferait déborder
// la ligne.
const HYPHENATED = /(?<![\p{L}\p{N}_])[\p{L}\p{N}_]{1,24}(?:-[\p{L}\p{N}_]{1,24}){1,8}/gu
const NOBREAK = "whitespace-nowrap"
const MAX = 24

export function noBreak(text: string): React.ReactNode[] {
  const fragments: React.ReactNode[] = []
  let consumedUpTo = 0
  for (const hyphenatedMatch of text.matchAll(HYPHENATED)) {
    const word = hyphenatedMatch[0]
    if (word.length > MAX) continue
    if (hyphenatedMatch.index > consumedUpTo) fragments.push(text.slice(consumedUpTo, hyphenatedMatch.index))
    fragments.push(
      <span key={hyphenatedMatch.index} className={NOBREAK}>
        {word}
      </span>
    )
    consumedUpTo = hyphenatedMatch.index + word.length
  }
  fragments.push(text.slice(consumedUpTo))
  return fragments
}

type Hast = { type: string; tagName?: string; value?: string; children?: Hast[]; properties?: Record<string, unknown> }

// Découpe un nœud texte en texte et en <span> insécables ; null si aucun mot à tirets ne s'y trouve.
function splitTextNode(text: string): Hast[] | null {
  const fragments: Hast[] = []
  let consumedUpTo = 0
  for (const hyphenatedMatch of text.matchAll(HYPHENATED)) {
    const word = hyphenatedMatch[0]
    if (word.length > MAX) continue
    if (hyphenatedMatch.index > consumedUpTo) fragments.push({ type: "text", value: text.slice(consumedUpTo, hyphenatedMatch.index) })
    fragments.push({ type: "element", tagName: "span", properties: { className: [NOBREAK] }, children: [{ type: "text", value: word }] })
    consumedUpTo = hyphenatedMatch.index + word.length
  }
  if (fragments.length === 0) return null
  if (consumedUpTo < text.length) fragments.push({ type: "text", value: text.slice(consumedUpTo) })
  return fragments
}

// Même découpage pour react-markdown (plugin rehype) ; le code, en police fixe, n'est pas touché.
export function rehypeNoBreak() {
  const walk = (node: Hast) => {
    const isCode = node.tagName === "code" || node.tagName === "pre"
    if (!node.children || isCode) return
    node.children = node.children.flatMap((child): Hast[] => {
      const isNonEmptyText = child.type === "text" && !!child.value
      if (!isNonEmptyText) {
        walk(child)
        return [child]
      }
      return splitTextNode(child.value!) ?? [child]
    })
  }
  return (tree: Hast) => walk(tree)
}
