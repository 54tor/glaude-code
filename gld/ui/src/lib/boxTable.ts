export type ContextSegment = { kind: "text"; text: string } | { kind: "table"; rows: string[][] }

const BORDER_LINE = /^\s*[┌├└╭╰][─━┬┼┴╌┄]*[┐┤┘╮╯]\s*$/
const CONTENT_LINE = /^\s*[│┃]|[│┃]\s*$|[│┃].*[│┃]/
const CELL_SEPARATOR = /[│┃]/

const isTableLine = (line: string) => BORDER_LINE.test(line) || CONTENT_LINE.test(line)

const splitCells = (line: string) => line.trim().replace(/^[│┃]/, "").replace(/[│┃]$/, "").split(CELL_SEPARATOR).map((cellText) => cellText.trim())

// Une rangée tient sur plusieurs lignes quand une cellule se replie : on recolle les fragments de chaque colonne.
function mergeRowLines(rowLines: string[]): string[] {
  const cellLines = rowLines.map(splitCells)
  const columnCount = Math.max(...cellLines.map((cells) => cells.length))
  const mergedCells: string[] = []
  for (let columnIndex = 0; columnIndex < columnCount; columnIndex++) {
    const fragments = cellLines.map((cells) => cells[columnIndex] ?? "").filter((fragment) => fragment !== "")
    mergedCells.push(fragments.join(" "))
  }
  return mergedCells
}

function parseTable(tableLines: string[]): string[][] {
  const rows: string[][] = []
  let pendingRowLines: string[] = []
  for (const line of tableLines) {
    if (BORDER_LINE.test(line)) {
      if (pendingRowLines.length > 0) rows.push(mergeRowLines(pendingRowLines))
      pendingRowLines = []
      continue
    }
    pendingRowLines.push(line)
  }
  if (pendingRowLines.length > 0) rows.push(mergeRowLines(pendingRowLines))
  return rows
}

// Découpe le contexte d'une question en texte et en tableaux dessinés au terminal (cadres ┌─┬┐│└┘), pour réafficher ceux-ci en vrais tableaux.
export function splitContextTables(context: string): ContextSegment[] {
  const segments: ContextSegment[] = []
  let textLines: string[] = []
  let tableLines: string[] = []
  const flushText = () => {
    if (textLines.length > 0) segments.push({ kind: "text", text: textLines.join("\n") })
    textLines = []
  }
  const flushTable = () => {
    const hasContent = tableLines.some((line) => CONTENT_LINE.test(line))
    if (hasContent) {
      flushText()
      segments.push({ kind: "table", rows: parseTable(tableLines) })
    } else {
      textLines.push(...tableLines)
    }
    tableLines = []
  }
  for (const line of context.split("\n")) {
    if (isTableLine(line)) {
      tableLines.push(line)
      continue
    }
    if (tableLines.length > 0) flushTable()
    textLines.push(line)
  }
  if (tableLines.length > 0) flushTable()
  flushText()
  return segments
}

// Le contexte (texte du terminal et tableaux encadrés) en Markdown GFM, pour l'afficher comme un message de Claude.
export function contextToMarkdown(context: string): string {
  return splitContextTables(context)
    .map((segment) => {
      if (segment.kind === "text") return segment.text
      const escapedRows = segment.rows.map((cells) => cells.map((cellText) => cellText.replace(/\|/g, "\\|")))
      const [headerCells, ...bodyRows] = escapedRows
      const formatRow = (cells: string[]) => `| ${cells.join(" | ")} |`
      const separator = formatRow(headerCells.map(() => "---"))
      return [formatRow(headerCells), separator, ...bodyRows.map(formatRow)].join("\n")
    })
    .join("\n\n")
}
