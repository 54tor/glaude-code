import { describe, expect, it } from "vitest"
import { freeName, packOf, parsePack, projectLabel } from "./memoryPack"

describe("memoryPack", () => {
  it("relit ce qu'il a écrit", () => {
    const records = [{ project: "site", name: "a.md", text: "---\nname: a\n---\nx" }]
    expect(parsePack(packOf(records))).toEqual(records)
  })

  it("refuse un nom qui sort du dossier, l'index et un autre format", () => {
    const pack = (name: string) => JSON.stringify({ format: "gld-memory", version: 1, entries: [{ project: "p", name, text: "" }] })
    for (const name of ["../x.md", "a/b.md", ".cache.md", "MEMORY.md", "x.txt"]) expect(() => parsePack(pack(name))).toThrow()
    expect(() => parsePack("{}")).toThrow()
    expect(() => parsePack("pas du json")).toThrow()
  })

  it("garde le dernier segment du chemin et trouve un nom libre", () => {
    expect(projectLabel("/home/bob/Projets/site/")).toBe("site")
    expect(freeName("a.md", new Set(["a.md", "a-2.md"]))).toBe("a-3.md")
  })
})
