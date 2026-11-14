import { describe, expect, it } from "vitest"
import { contextToMarkdown, splitContextTables } from "./boxTable"

const tableauAvecCelluleRepliee = [
  "- intro",
  "┌────┬──────────┐",
  "│    │ A        │",
  "├────┼──────────┤",
  "│ Pr │ un texte │",
  "│    │ replié   │",
  "└────┴──────────┘",
  "suite",
].join("\n")

describe("splitContextTables", () => {
  it("convertitLeCadreEnTableauEtGardeLeTexteAutour", () => {
    const segmentsObtenus = splitContextTables(tableauAvecCelluleRepliee)

    expect(segmentsObtenus).toEqual([
      { kind: "text", text: "- intro" },
      { kind: "table", rows: [["", "A"], ["Pr", "un texte replié"]] },
      { kind: "text", text: "suite" },
    ])
  })

  it("laisseUnTexteSansCadreInchange", () => {
    expect(splitContextTables("a\nb")).toEqual([{ kind: "text", text: "a\nb" }])
  })
})

describe("contextToMarkdown", () => {
  it("transforme un tableau encadré en tableau Markdown et garde le texte", () => {
    const contexteTerminal = ["Il en reste 2 :", "┌────┬───────┐", "│ #  │ Point │", "├────┼───────┤", "│ 1  │ a|b   │", "└────┴───────┘"].join("\n")

    const markdownObtenu = contextToMarkdown(contexteTerminal)

    expect(markdownObtenu).toBe("Il en reste 2 :\n\n| # | Point |\n| --- | --- |\n| 1 | a\\|b |")
  })
})
