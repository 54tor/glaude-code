import { describe, expect, it } from "vitest"
import { lineDiff } from "./MemoryDiff"

describe("lineDiff", () => {
  it("marque les lignes retirées et ajoutées sans ligne vide finale en trop", () => {
    expect(lineDiff("a\nb\nc\n", "a\nx\nc\n")).toEqual([
      { op: " ", t: "a" },
      { op: "-", t: "b" },
      { op: "+", t: "x" },
      { op: " ", t: "c" },
    ])
  })

  it("textes identiques : aucune ligne modifiée", () => {
    expect(lineDiff("a\nb", "a\nb").every((row) => row.op === " ")).toBe(true)
  })

  it("texte vide contre texte : tout est ajouté", () => {
    expect(lineDiff("", "a\nb\n")).toEqual([
      { op: "+", t: "a" },
      { op: "+", t: "b" },
    ])
  })
})
