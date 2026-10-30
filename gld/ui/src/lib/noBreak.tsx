// Le navigateur coupe volontiers après un tiret (« UI-01 » → « UI- » / « 01 ») : les mots courts à tirets restent d'un bloc, sans
// changer le texte (la copie est intacte). Au-delà de 24 caractères ils restent coupables, sinon un identifiant long ferait déborder
// la ligne.
const HYPHENATED = /(?<![\p{L}\p{N}_])[\p{L}\p{N}_]{1,24}(?:-[\p{L}\p{N}_]{1,24}){1,8}/gu
const NOBREAK = "whitespace-nowrap"
const MAX = 24

export function noBreak(t: string): React.ReactNode[] {
  const out: React.ReactNode[] = []
  let at = 0
  for (const m of t.matchAll(HYPHENATED)) {
    if (m[0].length > MAX) continue
    if (m.index > at) out.push(t.slice(at, m.index))
    out.push(
      <span key={m.index} className={NOBREAK}>
        {m[0]}
      </span>
    )
    at = m.index + m[0].length
  }
  out.push(t.slice(at))
  return out
}

type Hast = { type: string; tagName?: string; value?: string; children?: Hast[]; properties?: Record<string, unknown> }

// Même découpage pour react-markdown (plugin rehype) ; le code, en police fixe, n'est pas touché.
export function rehypeNoBreak() {
  const walk = (n: Hast) => {
    if (!n.children || n.tagName === "code" || n.tagName === "pre") return
    n.children = n.children.flatMap((c): Hast[] => {
      if (c.type !== "text" || !c.value) {
        walk(c)
        return [c]
      }
      const out: Hast[] = []
      let at = 0
      for (const m of c.value.matchAll(HYPHENATED)) {
        if (m[0].length > MAX) continue
        if (m.index > at) out.push({ type: "text", value: c.value.slice(at, m.index) })
        out.push({ type: "element", tagName: "span", properties: { className: [NOBREAK] }, children: [{ type: "text", value: m[0] }] })
        at = m.index + m[0].length
      }
      if (!out.length) return [c]
      if (at < c.value.length) out.push({ type: "text", value: c.value.slice(at) })
      return out
    })
  }
  return (tree: Hast) => walk(tree)
}
