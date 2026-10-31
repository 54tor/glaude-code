import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { pickFeed, runFeed } from "./feedTs"

const f = (path: string, size = 10) => ({ path, size })
afterEach(() => vi.restoreAllMocks())

describe("pickFeed", () => {
  it("ne garde que .ts .tsx .js .jsx .json .d.ts de 100 Ko au plus", () => {
    const got = pickFeed([f("a.ts"), f("b.tsx"), f("c.js"), f("d.jsx"), f("e.json"), f("g.d.ts"), f("h.go"), f("i.md"), f("big.ts", 100 * 1024 + 1), f("ok.ts", 100 * 1024)], "")
    expect(got.sort()).toEqual(["a.ts", "b.tsx", "c.js", "d.jsx", "e.json", "g.d.ts", "ok.ts"])
  })
  it("plafonne à 300 fichiers et 5 Mo cumulés", () => {
    expect(pickFeed(Array.from({ length: 400 }, (_, index) => f(`f${index}.ts`)), "")).toHaveLength(300)
    expect(pickFeed(Array.from({ length: 60 }, (_, index) => f(`f${index}.ts`, 100 * 1024)), "")).toHaveLength(51)
  })
  it("trie par proximité de dossier avec le fichier actif", () => {
    expect(pickFeed([f("x/y/z.ts"), f("a/b/far.ts"), f("a/b/c/deep.ts"), f("a/b/near.ts"), f("root.ts")], "a/b/cur.ts")).toEqual(["a/b/far.ts", "a/b/near.ts", "a/b/c/deep.ts", "root.ts", "x/y/z.ts"])
  })
})

describe("runFeed", () => {
  it("lots de 50, enregistre chaque fichier, s'arrête quand on annule", async () => {
    const paths = Array.from({ length: 120 }, (_, index) => `f${index}.ts`)
    const ac = new AbortController()
    const files = vi.spyOn(api, "fsFiles").mockImplementation(async (_d, ps) => {
      if (files.mock.calls.length === 2) ac.abort()
      return { files: ps.map((path) => ({ path, content: "x" })) }
    })
    const sink = vi.fn()
    await runFeed("/p", paths, sink, ac.signal)
    expect(files.mock.calls.map((value) => value[1].length)).toEqual([50, 50])
    expect(files.mock.calls[0][1]).toEqual(paths.slice(0, 50))
    expect(sink).toHaveBeenCalledTimes(50)
    expect(sink).toHaveBeenCalledWith("f0.ts", "x")
  })
  it("s'arrête au-delà de 5 Mo de contenu cumulé", async () => {
    const big = "x".repeat(2 * 1024 * 1024)
    vi.spyOn(api, "fsFiles").mockImplementation(async (_d, ps) => ({ files: ps.map((path) => ({ path, content: big })) }))
    const sink = vi.fn()
    await runFeed("/p", ["a.ts", "b.ts", "c.ts", "d.ts"], sink, new AbortController().signal)
    expect(sink).toHaveBeenCalledTimes(2)
  })
})
