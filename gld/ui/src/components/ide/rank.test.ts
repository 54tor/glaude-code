import { describe, expect, it } from "vitest"
import { rank } from "./rank"

describe("rank", () => {
  it("sous-séquence insensible à la casse", () => {
    expect(rank(["src/App.tsx", "src/lib/api.ts", "README.md"], "apx")).toEqual(["src/App.tsx"])
    expect(rank(["src/App.tsx", "README.md"], "ZZ")).toEqual([])
  })
  it("le nom de fichier passe avant le chemin", () => {
    expect(rank(["ide/a.ts", "x/ide.ts"], "ide")).toEqual(["x/ide.ts", "ide/a.ts"])
  })
  it("plafond de 50 ; requête vide : les 50 premiers", () => {
    const many = Array.from({ length: 80 }, (_, index) => `f${index}.ts`)
    expect(rank(many, "f")).toHaveLength(50)
    expect(rank(many, "")).toEqual(many.slice(0, 50))
  })
})
