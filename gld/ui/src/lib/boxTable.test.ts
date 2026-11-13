import { describe, expect, it } from "vitest"
import { splitContextTables } from "./boxTable"

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
