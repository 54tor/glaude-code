// Questions du terminal. Deux formes : une liste numérotée sous une phrase (autorisation d'un outil, choix d'un plan) : le bouton envoie
// le chiffre ; une liste à sélection (« ❯ option » suivie des autres, sans numéro) : le bouton déplace la sélection puis valide.
// checked : case d'une question à choix multiples (la touche chiffre la coche ou la décoche sans bouger le curseur) ; reach : touches
// pour amener le curseur sur l'option sans la valider (nécessaire avant d'écrire dans une option « Type something »).
export type Opt = { label: string; keys: string[]; text?: boolean; chat?: boolean; checked?: boolean; reach?: string[] }
export type Prompt = { question: string; options: Opt[]; submit?: string[] }
// Options qui attendent une réponse libre (« Type something. », « Chat about this », « No, and tell Claude what to do differently »…).
const CHAT = /chat about this/i
const FREE = /type something|chat about this|tell claude|something else|^\d+\.\s*other\b/i
export function parsePrompt(screen: string): Prompt | null {
  // Un vrai dialogue du terminal porte une ligne d'aide en bas (« Esc to cancel », « Enter to confirm »…) ; le texte de Claude, lui, peut
  // contenir des listes numérotées ou un « ❯ » sans que rien n'attende de réponse. On ne regarde que le bas de l'écran.
  // Les dialogues d'autorisation peuvent être encadrés (« │ ❯ 1. Yes │ ») : on retire le cadre avant d'analyser.
  const all = screen
    .split("\n")
    .map((l) => l.replace(/^(\s*)[│┃]\s?/, "$1").replace(/\s*[│┃]\s*$/, ""))
    .filter((l) => l.trim())
  const tail = all.slice(-32)
  // « esc to interrupt » est la ligne d'état de Claude en pleine réflexion : rien n'attend alors de réponse.
  if (/esc to interrupt/i.test(tail.slice(-6).join("\n"))) return null
  // Les confirmations d'opération risquée n'ont pas toujours cette ligne d'aide tout en bas : on la cherche sur les dernières lignes,
  // ou on reconnaît la question elle-même (« Do you want to proceed? »).
  const foot = tail.slice(-8).join("\n")
  if (!/(esc|escape) to (cancel|exit|go back)|enter to (select|confirm)|to navigate|tab to amend|ready to submit|submit your answers|do you want to|would you like to|proceed\?/i.test(foot)) return null
  const lines = tail
  const nums: { n: string; label: string; at: number }[] = []
  lines.forEach((l, i) => {
    const m = /^\s*(?:[❯>›]\s*)?([1-9])[.)]\s+(\S.*)$/.exec(l)
    if (m) nums.push({ n: m[1], label: m[2].trim(), at: i })
  })
  const above = (at: number) => {
    let q = ""
    for (let i = at - 1; i >= 0 && i >= at - 6; i--) {
      const t = lines[i].trim()
      if (t) {
        q = t
        if (/[?:]$/.test(t)) break
      }
    }
    return q
  }
  const multi = multiSelect(lines, above)
  if (multi) return multi
  if (nums.length >= 2 && nums[0].n === "1") return { question: above(nums[0].at), options: nums.map((o) => ({ label: `${o.n}. ${o.label}`, keys: [o.n], text: FREE.test(o.label), chat: CHAT.test(o.label), ...(CHAT.test(o.label) && { keys: [o.n, "Enter"] }) })) }
  const sel = lines.findIndex((l) => /^\s{0,3}❯ \S/.test(l))
  if (sel < 0) return null
  // Le texte des options est aligné sur celui de la ligne sélectionnée ; les autres lignes du dialogue ne le sont pas.
  const col = lines[sel].indexOf("❯") + 2
  const item = (l: string) => l.length > col && l.slice(0, col).trim() === "" && l[col] !== " " && !/^[─━│┌└╭╰]/.test(l[col])
  let from = sel
  while (from > 0 && item(lines[from - 1])) from--
  let to = sel
  while (to + 1 < lines.length && item(lines[to + 1])) to++
  if (to - from < 1 || to - from > 8) return null
  const options: Opt[] = []
  for (let i = from; i <= to; i++) {
    const d = i - sel
    const label = lines[i].slice(col).trim()
    options.push({ label, keys: [...Array(Math.abs(d)).fill(d < 0 ? "Up" : "Down"), "Enter"], text: FREE.test(label) })
  }
  return { question: above(from), options }
}

// Question à choix multiples : « N. [ ] option » (ou [✔]), une ligne « Submit » sans numéro, puis « Chat about this » sous un séparateur.
// Le curseur (❯) se déplace aux flèches ; la touche chiffre coche sans déplacer le curseur ; Entrée sur « Submit » valide.
function multiSelect(lines: string[], above: (at: number) => string): Prompt | null {
  // Signature stricte : les touches envoyées sont aveugles (Bas…Entrée), donc un texte qui imiterait des cases ne doit pas passer pour la vraie
  // question. Il faut l'en-tête « ← … Submit → », le pied « Enter to select » dans les 3 dernières lignes, et aucune demande d'autorisation
  // (qu'un « Valider » mal placé pourrait approuver) sur l'écran.
  if (lines.some((l) => /do you want to|would you like to|proceed\?|tab to amend|bash command/i.test(l))) return null
  const head = lines.findIndex((l) => /^\s*←.*Submit.*→\s*$/.test(l))
  if (head < 0 || !/enter to select/i.test(lines.slice(-3).join("\n"))) return null
  lines = lines.slice(head)
  type Row = { at: number; kind: "box" | "submit" | "other"; n?: string; label: string; checked?: boolean }
  const rows: Row[] = []
  let cursor = -1
  lines.forEach((l, at) => {
    const m = /^\s*(❯)?\s*(\d)[.)]\s+(?:\[(.)\]\s+)?(\S.*)$/.exec(l)
    const sub = /^\s*(❯)?\s*Submit\s*$/.exec(l)
    if (m && m[3] !== undefined) rows.push({ at, kind: "box", n: m[2], label: m[4].trim(), checked: m[3] !== " " })
    else if (sub && rows.some((r) => r.kind === "box")) rows.push({ at, kind: "submit", label: "Submit" })
    else if (m && rows.some((r) => r.kind === "submit")) rows.push({ at, kind: "other", n: m[2], label: m[4].trim() })
    else return
    if ((m?.[1] ?? sub?.[1]) === "❯") cursor = rows.length - 1
  })
  const boxes = rows.filter((r) => r.kind === "box")
  const submit = rows.findIndex((r) => r.kind === "submit")
  if (boxes.length < 2 || submit < 0 || cursor < 0) return null
  const reach = (i: number) => Array<string>(Math.abs(i - cursor)).fill(i < cursor ? "Up" : "Down")
  const options: Opt[] = rows.flatMap((r, i): Opt[] =>
    r.kind === "box"
      ? [{ label: r.label, checked: r.checked, keys: [r.n!], reach: reach(i), text: FREE.test(r.label) }]
      : r.kind === "other"
        ? [{ label: `${r.n}. ${r.label}`, keys: [r.n!, "Enter"], text: FREE.test(r.label), chat: CHAT.test(r.label) }]
        : []
  )
  return { question: above(boxes[0].at), options, submit: [...reach(submit), "Enter"] }
}
