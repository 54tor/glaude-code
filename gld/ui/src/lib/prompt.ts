// Questions du terminal. Deux formes : une liste numérotée sous une phrase (autorisation d'un outil, choix d'un plan) : le bouton envoie
// le chiffre ; une liste à sélection (« ❯ option » suivie des autres, sans numéro) : le bouton déplace la sélection puis valide.
// checked : case d'une question à choix multiples (la touche chiffre la coche ou la décoche sans bouger le curseur) ; reach : touches
// pour amener le curseur sur l'option sans la valider (nécessaire avant d'écrire dans une option « Type something ») ; edit : réponse libre
// déjà donnée (« 3. texte ✔ »), à modifier : le curseur sur la ligne rouvre l'édition du texte.
export type Opt = { label: string; keys: string[]; detail?: string; text?: boolean; chat?: boolean; checked?: boolean; reach?: string[]; edit?: string }
export type Prompt = { question: string; context?: string; preview?: string; options: Opt[]; submit?: string[]; tabs?: { label: string; done: boolean }[] } // tabs : onglets de la question (☒ répondu) ; context : la commande ou le diff d'une demande d'autorisation
// Options qui attendent une réponse libre (« Type something. », « Chat about this », « No, and tell Claude what to do differently »…).
const CHAT = /chat about this/i
const FREE = /type something|chat about this|tell claude|something else|^\d+\.\s*other\b/i
const PREVIEW_PANEL_TOP = /[┌╭]─{2,}[┐╮]\s*$/
const LABEL_BEFORE_PREVIEW_PANEL = /^(.*?\S)\s{2,}(?=[┌╭]─{2,}[┐╮]\s*$)/
const PREVIEW_PANEL_BOTTOM = /^[└╰]─*[┘╯]/
const FRAME_LEFT_EDGE = /^(\s*)[│┃]\s?/
const FRAME_RIGHT_EDGE = /\s*[│┃]\s*$/
const DIALOG_FOOTER = /(esc|escape) to (cancel|exit|go back)|enter to (select|confirm)|to navigate|tab to amend|ready to submit|submit your answers|do you want to|would you like to|proceed\?/i
const NUMBERED_OPTION = /^\s*(?:[❯>›]\s*)?([1-9])[.)]\s+(\S.*)$/
const CURSOR_MARK = /^\s*[❯>›]/
const TABS_HEADER = /^\s*←.*Submit.*→\s*$/
const TAB_OR_RULE_LINE = /[←→☐☒✔]|^[─━╌▔]/
const TAB_OR_RULE_CHARACTER = /[←→☐☒✔─━]/
const ENDS_LIKE_QUESTION = /[?:]$/
const INDENTED_TEXT = /^\s{3,}\S/
const HORIZONTAL_RULE = /^\s*[─━╌▔]/
const KEY_HELP_LINE = /(esc|enter|ctrl\+|shift\+|tab)\b.*(cancel|select|navigate|edit|approve|amend)/i
const CONTEXT_SEPARATOR = /^[╌─━┄┈]{8,}$/
const ANSWERED_FREE_TEXT = /\s✔$/
// Lignes du bas de l'écran examinées : un dialogue tient dedans, le texte de Claude plus haut n'est pas lu.
const TAIL_LINE_COUNT = 32
const INTERRUPT_FOOTER_LINES = 6
const DIALOG_FOOTER_LINES = 8
const PARAGRAPH_LOOKBACK_LINES = 14
const QUESTION_LOOKBACK_LINES = 6
const WRAPPED_QUESTION_LOOKBACK_LINES = 8
// Une ligne repliée par le terminal est presque pleine : plus courte, elle termine un paragraphe précédent.
const WRAPPED_LINE_MIN_WIDTH_RATIO = 0.6
const CONTEXT_MAX_LINES = 40
const CONTEXT_TITLE_LINES = 2
const SELECT_LIST_MAX_SPAN = 8

const arrowKeysToward = (targetIndex: number, cursorIndex: number) =>
  Array<string>(Math.abs(targetIndex - cursorIndex)).fill(targetIndex < cursorIndex ? "Up" : "Down")

// Aperçu côte à côte (option.preview) : un cadre ┌─┐ à droite des options, aligné sur toute sa hauteur. On le retire des lignes (sinon il
// se mêle aux libellés) et on garde son contenu, ligne à ligne, tel que dessiné : c'est l'aperçu de l'option sélectionnée.
function splitPreview(rows: string[]): { rows: string[]; preview?: string } {
  const rowsWithoutPanel: string[] = []
  const panelLines: string[] = []
  let panelColumn = -1
  for (const rawRow of rows) {
    const isInsidePanel = panelColumn >= 0
    if (!isInsidePanel) {
      const opensPanel = LABEL_BEFORE_PREVIEW_PANEL.test(rawRow) && panelLines.length === 0
      if (opensPanel) {
        panelColumn = rawRow.search(PREVIEW_PANEL_TOP)
        rowsWithoutPanel.push(rawRow.slice(0, panelColumn).trimEnd())
        continue
      }
      rowsWithoutPanel.push(rawRow)
      continue
    }
    const panelPart = rawRow.slice(panelColumn)
    rowsWithoutPanel.push(rawRow.slice(0, panelColumn).trimEnd())
    if (PREVIEW_PANEL_BOTTOM.test(panelPart)) {
      panelColumn = -1
      continue
    }
    panelLines.push(panelPart.replace(/^[│┃] ?/, "").replace(FRAME_RIGHT_EDGE, ""))
  }
  const preview = panelLines.length > 0 ? panelLines.join("\n").trimEnd() : undefined
  return { rows: rowsWithoutPanel, preview }
}
export function parsePrompt(screen: string): Prompt | null {
  const { rows, preview } = splitPreview(screen.split("\n"))
  const prompt = parseLines(rows)
  if (prompt && preview) return { ...prompt, preview }
  return prompt
}
function parseLines(src: string[]): Prompt | null {
  // Un vrai dialogue du terminal porte une ligne d'aide en bas (« Esc to cancel », « Enter to confirm »…) ; le texte de Claude, lui, peut
  // contenir des listes numérotées ou un « ❯ » sans que rien n'attende de réponse. On ne regarde que le bas de l'écran.
  // Les dialogues d'autorisation peuvent être encadrés (« │ ❯ 1. Yes │ ») : on retire le cadre avant d'analyser.
  // paragraphStarts : la ligne suit une ligne vide, donc commence un paragraphe (une question longue est repliée sur plusieurs lignes).
  const paragraphStarts = new Set<number>()
  let previousWasBlank = false
  const allLines: string[] = []
  for (const rawLine of src) {
    const unframedLine = rawLine.replace(FRAME_LEFT_EDGE, "$1").replace(FRAME_RIGHT_EDGE, "")
    if (!unframedLine.trim()) {
      previousWasBlank = true
      continue
    }
    if (previousWasBlank) paragraphStarts.add(allLines.length)
    previousWasBlank = false
    allLines.push(unframedLine)
  }
  const tailStart = Math.max(0, allLines.length - TAIL_LINE_COUNT)
  const lines = allLines.slice(-TAIL_LINE_COUNT)
  const startsParagraph = (tailIndex: number) => paragraphStarts.has(tailStart + tailIndex)
  // « esc to interrupt » est la ligne d'état de Claude en pleine réflexion : rien n'attend alors de réponse.
  if (/esc to interrupt/i.test(lines.slice(-INTERRUPT_FOOTER_LINES).join("\n"))) return null
  // Les confirmations d'opération risquée n'ont pas toujours cette ligne d'aide tout en bas : on la cherche sur les dernières lignes,
  // ou on reconnaît la question elle-même (« Do you want to proceed? »).
  const footer = lines.slice(-DIALOG_FOOTER_LINES).join("\n")
  if (!DIALOG_FOOTER.test(footer)) return null
  let numberedOptions: { n: string; label: string; at: number }[] = []
  lines.forEach((line, lineIndex) => {
    const optionMatch = NUMBERED_OPTION.exec(line)
    if (optionMatch) numberedOptions.push({ n: optionMatch[1], label: optionMatch[2].trim(), at: lineIndex })
  })
  // Un plan ou une liste de Claude, affichés au-dessus du dialogue, sont numérotés aussi : on ne garde que la dernière suite 1, 2, 3…
  const lastOptionOne = numberedOptions.map((option) => option.n).lastIndexOf("1")
  if (lastOptionOne > 0) numberedOptions = numberedOptions.slice(lastOptionOne)
  // La question au-dessus de l'option `at` : ses lignes repliées jusqu'au début du paragraphe (ou de la phrase précédente).
  const widestLine = Math.max(...lines.map((line) => line.trim().length))
  const paragraphAbove = (at: number): string => {
    const parts: string[] = []
    for (let lineIndex = at - 1; lineIndex >= 0 && lineIndex >= at - PARAGRAPH_LOOKBACK_LINES; lineIndex--) {
      const trimmed = lines[lineIndex].trim()
      if (TAB_OR_RULE_LINE.test(trimmed)) break
      parts.unshift(trimmed)
      if (startsParagraph(lineIndex)) break
    }
    return parts.join(" ")
  }
  const questionAbove = (at: number): string => {
    // Une ligne vide sépare la question des options : la question est le paragraphe entier au-dessus, replié ou non par le terminal.
    if (startsParagraph(at)) {
      const paragraph = paragraphAbove(at)
      if (paragraph) return paragraph
    }
    let question = ""
    let questionLine = -1
    for (let lineIndex = at - 1; lineIndex >= 0 && lineIndex >= at - QUESTION_LOOKBACK_LINES; lineIndex--) {
      const trimmed = lines[lineIndex].trim()
      if (!trimmed) continue
      question = trimmed
      questionLine = lineIndex
      if (ENDS_LIKE_QUESTION.test(trimmed)) break
    }
    // Une question longue est repliée par le terminal : les lignes pleines qui la précèdent, sans fin de phrase ni en-tête, en font partie.
    const mayBeWrapped = questionLine > 0 && ENDS_LIKE_QUESTION.test(question)
    if (!mayBeWrapped) return question
    for (let lineIndex = questionLine - 1; lineIndex >= 0 && lineIndex >= questionLine - WRAPPED_QUESTION_LOOKBACK_LINES; lineIndex--) {
      const trimmed = lines[lineIndex].trim()
      const isShortLine = trimmed.length < widestLine * WRAPPED_LINE_MIN_WIDTH_RATIO
      const endsParagraph = startsParagraph(lineIndex + 1)
      if (isShortLine || TAB_OR_RULE_CHARACTER.test(trimmed) || endsParagraph) break
      question = trimmed + " " + question
      if (startsParagraph(lineIndex)) break
    }
    return question
  }
  const multiSelectPrompt = multiSelect(lines, questionAbove, (headerIndex) => recap(allLines, tailStart + headerIndex))
  if (multiSelectPrompt) return multiSelectPrompt
  // Lignes suivant une option, avant la prochaine : suite d'un libellé trop long, ou description (pas le pied ni les séparateurs).
  const detailOf = (optionIndex: number) => {
    const nextOptionLine = numberedOptions[optionIndex + 1]?.at ?? lines.length
    return lines
      .slice(numberedOptions[optionIndex].at + 1, nextOptionLine)
      .filter((line) => INDENTED_TEXT.test(line) && !HORIZONTAL_RULE.test(line) && !KEY_HELP_LINE.test(line))
      .map((line) => line.trim())
      .join(" ")
  }
  // Demande d'autorisation : la commande ou le diff sont dans un encadré (deux traits ╌ ou ─), avec un titre au-dessus. Sans ce contexte,
  // « Do you want to proceed? » ne dit pas sur quoi porte la réponse.
  const contextOf = (at: number, question: string) => {
    const collected: string[] = []
    let separatorsSeen = 0
    for (let lineIndex = at - 1; lineIndex >= 0 && collected.length < CONTEXT_MAX_LINES; lineIndex--) {
      const trimmed = lines[lineIndex].trim()
      if (!CONTEXT_SEPARATOR.test(trimmed)) {
        collected.unshift(lines[lineIndex].replace(/\s+$/, ""))
        continue
      }
      separatorsSeen++
      if (separatorsSeen === 2) {
        // le titre (type et description), au-dessus du trait
        for (let offset = 1; offset <= CONTEXT_TITLE_LINES && lineIndex - offset >= 0; offset++) collected.unshift(lines[lineIndex - offset].trim())
        break
      }
    }
    if (separatorsSeen < 2) return undefined
    const contextLines = collected.filter((line) => line.trim() && !question.includes(line.trim()))
    if (contextLines.length === 0) return undefined
    return contextLines.join("\n")
  }
  const isNumberedDialog = numberedOptions.length >= 2 && numberedOptions[0].n === "1"
  if (isNumberedDialog) {
    const firstOptionLine = numberedOptions[0].at
    const question = questionAbove(firstOptionLine)
    // Question à onglets à choix unique (« ← ☐ Q1 ☐ Q2 ✔ Submit → ») : le message de Claude au-dessus et l'état des onglets.
    const tabsHeaderLine = lines.findIndex((line) => TABS_HEADER.test(line))
    const isTabbed = tabsHeaderLine >= 0 && tabsHeaderLine < firstOptionLine
    const cursorOption = numberedOptions.findIndex((option) => CURSOR_MARK.test(lines[option.at]))
    // Question à onglets déjà répondue en réponse libre : « 3. texte ✔ » juste avant « Chat about this ». Le chiffre la revaliderait ; le curseur
    // sur la ligne rouvre l'édition du texte.
    const isAnsweredFreeText = (optionIndex: number) => {
      if (!isTabbed || cursorOption < 0) return false
      if (!ANSWERED_FREE_TEXT.test(numberedOptions[optionIndex].label)) return false
      return CHAT.test(numberedOptions[optionIndex + 1]?.label ?? "")
    }
    const toOption = (option: { n: string; label: string }, optionIndex: number): Opt => {
      const isChat = CHAT.test(option.label)
      const isAnswered = isAnsweredFreeText(optionIndex)
      const result: Opt = {
        label: `${option.n}. ${option.label}`.replace(/\s+/g, " "),
        detail: detailOf(optionIndex) || undefined,
        keys: [option.n],
        text: FREE.test(option.label) || isAnswered,
        chat: isChat,
      }
      if (isChat) result.keys = [option.n, "Enter"]
      if (isAnswered) {
        result.edit = option.label.replace(ANSWERED_FREE_TEXT, "").trim()
        result.reach = arrowKeysToward(optionIndex, cursorOption)
      }
      return result
    }
    const permissionContext = contextOf(firstOptionLine, question)
    const tabsContext = isTabbed ? recap(allLines, tailStart + tabsHeaderLine) : undefined
    return {
      question,
      context: permissionContext ?? tabsContext,
      tabs: isTabbed ? tabsOf(lines[tabsHeaderLine]) : undefined,
      options: numberedOptions.map(toOption),
    }
  }
  const selectedLine = lines.findIndex((line) => /^\s{0,3}❯ \S/.test(line))
  if (selectedLine < 0) return null
  // Le texte des options est aligné sur celui de la ligne sélectionnée ; les autres lignes du dialogue ne le sont pas.
  const optionColumn = lines[selectedLine].indexOf("❯") + 2
  const isAlignedOption = (line: string) => {
    if (line.length <= optionColumn) return false
    if (line.slice(0, optionColumn).trim() !== "") return false
    if (line[optionColumn] === " ") return false
    return !/^[─━│┌└╭╰]/.test(line[optionColumn])
  }
  let firstOptionLine = selectedLine
  while (firstOptionLine > 0 && isAlignedOption(lines[firstOptionLine - 1])) firstOptionLine--
  let lastOptionLine = selectedLine
  while (lastOptionLine + 1 < lines.length && isAlignedOption(lines[lastOptionLine + 1])) lastOptionLine++
  const optionSpan = lastOptionLine - firstOptionLine
  if (optionSpan < 1 || optionSpan > SELECT_LIST_MAX_SPAN) return null
  const options: Opt[] = []
  for (let lineIndex = firstOptionLine; lineIndex <= lastOptionLine; lineIndex++) {
    const label = lines[lineIndex].slice(optionColumn).trim()
    options.push({ label, keys: [...arrowKeysToward(lineIndex, selectedLine), "Enter"], text: FREE.test(label) })
  }
  return { question: questionAbove(firstOptionLine), options }
}

// Question à choix multiples : « N. [ ] option » (ou [✔]), une ligne « Submit » sans numéro, puis « Chat about this » sous un séparateur.
// Le curseur (❯) se déplace aux flèches ; la touche chiffre coche sans déplacer le curseur ; Entrée sur « Submit » valide.
function multiSelect(screenLines: string[], above: (at: number) => string, recapAt: (head: number) => string | undefined): Prompt | null {
  // Signature stricte : les touches envoyées sont aveugles (Bas…Entrée), donc un texte qui imiterait des cases ne doit pas passer pour la vraie
  // question. Il faut l'en-tête « ← … Submit → », le pied « Enter to select » dans les 3 dernières lignes, et aucune demande d'autorisation
  // (qu'un « Valider » mal placé pourrait approuver) sur l'écran.
  const hasPermissionRequest = screenLines.some((line) => /do you want to|would you like to|proceed\?|tab to amend|bash command/i.test(line))
  if (hasPermissionRequest) return null
  const headerLine = screenLines.findIndex((line) => TABS_HEADER.test(line))
  if (headerLine < 0) return null
  if (!/enter to select/i.test(screenLines.slice(-3).join("\n"))) return null
  const lines = screenLines.slice(headerLine) // `above` indexe les lignes d'origine : décaler de headerLine
  type Row = { at: number; kind: "box" | "submit" | "other"; n?: string; label: string; checked?: boolean }
  const rows: Row[] = []
  let cursorRow = -1
  lines.forEach((line, at) => {
    const optionMatch = /^\s*(❯)?\s*(\d)[.)]\s+(?:\[(.)\]\s+)?(\S.*)$/.exec(line)
    const submitMatch = /^\s*(❯)?\s*Submit\s*$/.exec(line)
    const isCheckbox = optionMatch !== null && optionMatch[3] !== undefined
    if (isCheckbox) {
      rows.push({ at, kind: "box", n: optionMatch[2], label: optionMatch[4].trim(), checked: optionMatch[3] !== " " })
    } else if (submitMatch && rows.some((row) => row.kind === "box")) {
      rows.push({ at, kind: "submit", label: "Submit" })
    } else if (optionMatch && rows.some((row) => row.kind === "submit")) {
      rows.push({ at, kind: "other", n: optionMatch[2], label: optionMatch[4].trim() })
    } else {
      return
    }
    const cursorMark = optionMatch?.[1] ?? submitMatch?.[1]
    if (cursorMark === "❯") cursorRow = rows.length - 1
  })
  const checkboxRows = rows.filter((row) => row.kind === "box")
  const submitRow = rows.findIndex((row) => row.kind === "submit")
  if (checkboxRows.length < 2 || submitRow < 0 || cursorRow < 0) return null
  // Description d'une case : les lignes indentées jusqu'à la ligne suivante (autre case, Submit).
  const detailOf = (rowIndex: number) =>
    lines
      .slice(rows[rowIndex].at + 1, rows[rowIndex + 1]?.at ?? lines.length)
      .filter((line) => INDENTED_TEXT.test(line) && !HORIZONTAL_RULE.test(line) && !/enter to select|esc to cancel/i.test(line))
      .map((line) => line.trim())
      .join(" ")
  const toOptions = (row: Row, rowIndex: number): Opt[] => {
    if (row.kind === "box") {
      return [{ label: row.label, detail: detailOf(rowIndex) || undefined, checked: row.checked, keys: [row.n!], reach: arrowKeysToward(rowIndex, cursorRow), text: FREE.test(row.label) }]
    }
    if (row.kind === "other") {
      return [{ label: `${row.n}. ${row.label}`, keys: [row.n!, "Enter"], text: FREE.test(row.label), chat: CHAT.test(row.label) }]
    }
    return []
  }
  const options: Opt[] = rows.flatMap(toOptions)
  return {
    question: above(checkboxRows[0].at + headerLine),
    context: recapAt(headerLine),
    tabs: tabsOf(lines[0]),
    options,
    submit: [...arrowKeysToward(submitRow, cursorRow), "Enter"],
  }
}

// Le message de Claude affiché au-dessus d'un dialogue à onglets (récapitulatif des choix) : de sa puce « ● » jusqu'à l'en-tête, ou de
// l'écran visible s'il est déjà remonté.
const RECAP_MAX_LINES = 200
function recap(allLines: string[], headerIndex: number): string | undefined {
  let firstLine = headerIndex
  const startsTurnOrMessage = (line: string) => /^\s*●/.test(line) || /^\s*❯/.test(line)
  while (firstLine > 0 && headerIndex - firstLine < RECAP_MAX_LINES && !startsTurnOrMessage(allLines[firstLine])) firstLine--
  if (/^\s*❯/.test(allLines[firstLine] ?? "")) firstLine++ // la saisie de l'utilisateur ouvre le tour : ce qui précède est le tour d'avant
  const text = allLines
    .slice(firstLine, headerIndex)
    .filter((line) => !/^[─━╌▔\s]+$/.test(line))
    .join("\n")
    .trim()
  return text || undefined
}

// « ←  ☒ UI-01  ☐ Périmètre  ✔ Submit  → » : les onglets avant Submit, répondus (☒) ou non (☐).
function tabsOf(headerLine: string): { label: string; done: boolean }[] | undefined {
  const tabMatches = [...headerLine.matchAll(/([☐☒])\s+(.+?)(?=\s{2,}[☐☒✔]|\s*$)/g)]
  const tabs = tabMatches.map((tabMatch) => ({ label: tabMatch[2].trim(), done: tabMatch[1] === "☒" }))
  if (tabs.length === 0) return undefined
  return tabs
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

const BTW_FOOTER_MAX_DISTANCE = 4
const BTW_MAX_INDENT = 99

// Surcouche d'une question /btw : la réponse entre la ligne « /btw … » et le pied d'aide, sans l'indentation commune.
export function parseBtw(screen: string): { question: string; answer: string } | null {
  const lines = screen.replace(/\s+$/, "").split("\n")
  const footerLine = lines.findLastIndex((line) => line.includes("Esc to close") && line.includes("to scroll"))
  if (footerLine < 0 || footerLine < lines.length - BTW_FOOTER_MAX_DISTANCE) return null
  const questionLine = lines.findLastIndex((line, lineIndex) => lineIndex < footerLine && /^\s*\/btw\b/.test(line))
  if (questionLine < 0) return null
  const answerLines = lines.slice(questionLine + 1, footerLine)
  const indentOf = (line: string) => line.length - line.trimStart().length
  const commonIndent = Math.min(...answerLines.filter((line) => line.trim()).map(indentOf), BTW_MAX_INDENT)
  const question = lines[questionLine].trim().replace(/^\/btw\s*/, "")
  const answer = answerLines.map((line) => line.slice(commonIndent)).join("\n").trim()
  return { question, answer }
}
