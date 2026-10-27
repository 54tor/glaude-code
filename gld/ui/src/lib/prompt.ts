// Questions du terminal. Deux formes : une liste numérotée sous une phrase (autorisation d'un outil, choix d'un plan) : le bouton envoie
// le chiffre ; une liste à sélection (« ❯ option » suivie des autres, sans numéro) : le bouton déplace la sélection puis valide.
// checked : case d'une question à choix multiples (la touche chiffre la coche ou la décoche sans bouger le curseur) ; reach : touches
// pour amener le curseur sur l'option sans la valider (nécessaire avant d'écrire dans une option « Type something »).
export type Opt = { label: string; keys: string[]; detail?: string; text?: boolean; chat?: boolean; checked?: boolean; reach?: string[] }
export type Prompt = { question: string; context?: string; preview?: string; options: Opt[]; submit?: string[]; tabs?: { label: string; done: boolean }[] } // tabs : onglets de la question (☒ répondu) ; context : la commande ou le diff d'une demande d'autorisation
// Options qui attendent une réponse libre (« Type something. », « Chat about this », « No, and tell Claude what to do differently »…).
const CHAT = /chat about this/i
const FREE = /type something|chat about this|tell claude|something else|^\d+\.\s*other\b/i
// Aperçu côte à côte (option.preview) : un cadre ┌─┐ à droite des options, aligné sur toute sa hauteur. On le retire des lignes (sinon il
// se mêle aux libellés) et on garde son contenu, ligne à ligne, tel que dessiné : c'est l'aperçu de l'option sélectionnée.
function splitPreview(rows: string[]): { rows: string[]; preview?: string } {
  const out: string[] = []
  const panel: string[] = []
  let col = -1
  for (const raw of rows) {
    if (col < 0) {
      const m = /^(.*?\S)\s{2,}(?=[┌╭]─{2,}[┐╮]\s*$)/.exec(raw)
      if (m && !panel.length) {
        col = raw.search(/[┌╭]─{2,}[┐╮]\s*$/)
        out.push(raw.slice(0, col).trimEnd())
        continue
      }
      out.push(raw)
      continue
    }
    const right = raw.slice(col)
    out.push(raw.slice(0, col).trimEnd())
    if (/^[└╰]─*[┘╯]/.test(right)) col = -1
    else panel.push(right.replace(/^[│┃] ?/, "").replace(/\s*[│┃]\s*$/, ""))
  }
  return { rows: out, preview: panel.length ? panel.join("\n").trimEnd() : undefined }
}
export function parsePrompt(screen: string): Prompt | null {
  const { rows, preview } = splitPreview(screen.split("\n"))
  const p = parseLines(rows)
  return p && preview ? { ...p, preview } : p
}
function parseLines(src: string[]): Prompt | null {
  // Un vrai dialogue du terminal porte une ligne d'aide en bas (« Esc to cancel », « Enter to confirm »…) ; le texte de Claude, lui, peut
  // contenir des listes numérotées ou un « ❯ » sans que rien n'attende de réponse. On ne regarde que le bas de l'écran.
  // Les dialogues d'autorisation peuvent être encadrés (« │ ❯ 1. Yes │ ») : on retire le cadre avant d'analyser.
  // gap : la ligne suit une ligne vide, donc commence un paragraphe (une question longue est repliée sur plusieurs lignes).
  const gap = new Set<string>()
  let blank = false
  const all: string[] = []
  for (const raw of src) {
    const l = raw.replace(/^(\s*)[│┃]\s?/, "$1").replace(/\s*[│┃]\s*$/, "")
    if (!l.trim()) {
      blank = true
      continue
    }
    if (blank) gap.add(String(all.length))
    blank = false
    all.push(l)
  }
  const tailStart = Math.max(0, all.length - 32)
  const tail = all.slice(-32)
  // « esc to interrupt » est la ligne d'état de Claude en pleine réflexion : rien n'attend alors de réponse.
  if (/esc to interrupt/i.test(tail.slice(-6).join("\n"))) return null
  // Les confirmations d'opération risquée n'ont pas toujours cette ligne d'aide tout en bas : on la cherche sur les dernières lignes,
  // ou on reconnaît la question elle-même (« Do you want to proceed? »).
  const foot = tail.slice(-8).join("\n")
  if (!/(esc|escape) to (cancel|exit|go back)|enter to (select|confirm)|to navigate|tab to amend|ready to submit|submit your answers|do you want to|would you like to|proceed\?/i.test(foot)) return null
  const lines = tail
  let nums: { n: string; label: string; at: number }[] = []
  lines.forEach((l, i) => {
    const m = /^\s*(?:[❯>›]\s*)?([1-9])[.)]\s+(\S.*)$/.exec(l)
    if (m) nums.push({ n: m[1], label: m[2].trim(), at: i })
  })
  // Un plan ou une liste de Claude, affichés au-dessus du dialogue, sont numérotés aussi : on ne garde que la dernière suite 1, 2, 3…
  const last1 = nums.map((o) => o.n).lastIndexOf("1")
  if (last1 > 0) nums = nums.slice(last1)
  // La question au-dessus de l'option `at` : ses lignes repliées jusqu'au début du paragraphe (ou de la phrase précédente).
  const width = Math.max(...lines.map((l) => l.trim().length))
  const above = (at: number) => {
    // Une ligne vide sépare la question des options : la question est le paragraphe entier au-dessus, replié ou non par le terminal.
    if (gap.has(String(tailStart + at))) {
      const parts: string[] = []
      for (let i = at - 1; i >= 0 && i >= at - 14; i--) {
        const t = lines[i].trim()
        if (/[←→☐☒✔]|^[─━╌▔]/.test(t)) break
        parts.unshift(t)
        if (gap.has(String(tailStart + i))) break
      }
      if (parts.length) return parts.join(" ")
    }
    let q = ""
    let from = -1
    for (let i = at - 1; i >= 0 && i >= at - 6; i--) {
      const t = lines[i].trim()
      if (t) {
        q = t
        from = i
        if (/[?:]$/.test(t)) break
      }
    }
    // Une question longue est repliée par le terminal : les lignes pleines qui la précèdent, sans fin de phrase ni en-tête, en font partie.
    if (from > 0 && /[?:]$/.test(q))
      for (let i = from - 1; i >= 0 && i >= from - 8; i--) {
        const t = lines[i].trim()
        if (t.length < width * 0.6 || /[←→☐☒✔─━]/.test(t) || gap.has(String(tailStart + i + 1))) break
        q = t + " " + q
        if (gap.has(String(tailStart + i))) break
      }
    return q
  }
  const multi = multiSelect(lines, above, (head) => recap(all, tailStart + head))
  if (multi) return multi
  // Lignes suivant une option, avant la prochaine : suite d'un libellé trop long, ou description (pas le pied ni les séparateurs).
  const detail = (i: number) => {
    const next = nums[i + 1]?.at ?? lines.length
    return lines
      .slice(nums[i].at + 1, next)
      .filter((l) => /^\s{3,}\S/.test(l) && !/^\s*[─━╌▔]/.test(l) && !/(esc|enter|ctrl\+|shift\+|tab)\b.*(cancel|select|navigate|edit|approve|amend)/i.test(l))
      .map((l) => l.trim())
      .join(" ")
  }
  // Demande d'autorisation : la commande ou le diff sont dans un encadré (deux traits ╌ ou ─), avec un titre au-dessus. Sans ce contexte,
  // « Do you want to proceed? » ne dit pas sur quoi porte la réponse.
  const contextOf = (at: number, question: string) => {
    const got: string[] = []
    let seps = 0
    for (let i = at - 1; i >= 0 && got.length < 40; i--) {
      const t = lines[i].trim()
      if (/^[╌─━┄┈]{8,}$/.test(t)) {
        if (++seps === 2) {
          for (let k = 1; k <= 2 && i - k >= 0; k++) got.unshift(lines[i - k].trim()) // le titre (type et description), au-dessus du trait
          break
        }
        continue
      }
      got.unshift(lines[i].replace(/\s+$/, ""))
    }
    if (seps < 2) return undefined
    const rest = got.filter((l) => l.trim() && !question.includes(l.trim()))
    return rest.length ? rest.join("\n") : undefined
  }
  if (nums.length >= 2 && nums[0].n === "1") {
    const question = above(nums[0].at)
    // Question à onglets à choix unique (« ← ☐ Q1 ☐ Q2 ✔ Submit → ») : le message de Claude au-dessus et l'état des onglets.
    const head = lines.findIndex((l) => /^\s*←.*Submit.*→\s*$/.test(l))
    const tabbed = head >= 0 && head < nums[0].at
    return { question, context: contextOf(nums[0].at, question) ?? (tabbed ? recap(all, tailStart + head) : undefined), tabs: tabbed ? tabsOf(lines[head]) : undefined, options: nums.map((o, i) => ({ label: `${o.n}. ${o.label}`, detail: detail(i) || undefined, keys: [o.n], text: FREE.test(o.label), chat: CHAT.test(o.label), ...(CHAT.test(o.label) && { keys: [o.n, "Enter"] }) })) }
  }
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
function multiSelect(lines: string[], above: (at: number) => string, recapAt: (head: number) => string | undefined): Prompt | null {
  // Signature stricte : les touches envoyées sont aveugles (Bas…Entrée), donc un texte qui imiterait des cases ne doit pas passer pour la vraie
  // question. Il faut l'en-tête « ← … Submit → », le pied « Enter to select » dans les 3 dernières lignes, et aucune demande d'autorisation
  // (qu'un « Valider » mal placé pourrait approuver) sur l'écran.
  if (lines.some((l) => /do you want to|would you like to|proceed\?|tab to amend|bash command/i.test(l))) return null
  const head = lines.findIndex((l) => /^\s*←.*Submit.*→\s*$/.test(l))
  if (head < 0 || !/enter to select/i.test(lines.slice(-3).join("\n"))) return null
  const off = head // `above` indexe les lignes d'origine
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
  // Description d'une case : les lignes indentées jusqu'à la ligne suivante (autre case, Submit).
  const detail = (i: number) =>
    lines
      .slice(rows[i].at + 1, rows[i + 1]?.at ?? lines.length)
      .filter((l) => /^\s{3,}\S/.test(l) && !/^\s*[─━╌▔]/.test(l) && !/enter to select|esc to cancel/i.test(l))
      .map((l) => l.trim())
      .join(" ")
  const options: Opt[] = rows.flatMap((r, i): Opt[] =>
    r.kind === "box"
      ? [{ label: r.label, detail: detail(i) || undefined, checked: r.checked, keys: [r.n!], reach: reach(i), text: FREE.test(r.label) }]
      : r.kind === "other"
        ? [{ label: `${r.n}. ${r.label}`, keys: [r.n!, "Enter"], text: FREE.test(r.label), chat: CHAT.test(r.label) }]
        : []
  )
  return { question: above(boxes[0].at + off), context: recapAt(head), tabs: tabsOf(lines[0]), options, submit: [...reach(submit), "Enter"] }
}

// Le message de Claude affiché au-dessus d'un dialogue à onglets (récapitulatif des choix) : de sa puce « ● » jusqu'à l'en-tête, ou de
// l'écran visible s'il est déjà remonté.
function recap(all: string[], head: number): string | undefined {
  let from = head
  while (from > 0 && head - from < 200 && !/^\s*●/.test(all[from])) from--
  const text = all.slice(from, head).filter((l) => !/^[─━╌▔\s]+$/.test(l)).join("\n").trim()
  return text || undefined
}

// « ←  ☒ UI-01  ☐ Périmètre  ✔ Submit  → » : les onglets avant Submit, répondus (☒) ou non (☐).
function tabsOf(head: string): { label: string; done: boolean }[] | undefined {
  const tabs = [...head.matchAll(/([☐☒])\s+(.+?)(?=\s{2,}[☐☒✔]|\s*$)/g)].map((m) => ({ label: m[2].trim(), done: m[1] === "☒" }))
  return tabs.length ? tabs : undefined
}

export type LiveMode = "default" | "plan" | "acceptEdits" | "bypass" | "auto"

// Mode de permission d'une session ouverte, lu dans le pied de l'écran tmux (« ⏸ plan mode on (shift+tab to cycle) »…).
// Sans mention, c'est le mode normal ; sans écran, inconnu.
export function modeOfScreen(screen: string): LiveMode | null {
  if (!screen.trim()) return null
  const foot = screen.split("\n").slice(-6).join("\n").toLowerCase()
  if (foot.includes("plan mode on")) return "plan"
  if (foot.includes("accept edits on")) return "acceptEdits"
  if (foot.includes("bypass permissions on")) return "bypass"
  if (foot.includes("auto mode on")) return "auto"
  return "default"
}
