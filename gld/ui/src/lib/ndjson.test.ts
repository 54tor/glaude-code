import { describe, expect, it } from "vitest"
import { readNdjson } from "./ndjson"

const stream = (chunks: string[]) => {
  const enc = new TextEncoder()
  return new ReadableStream<Uint8Array>({
    start(c) {
      for (const ch of chunks) c.enqueue(enc.encode(ch))
      c.close()
    },
  })
}
const all = async (chunks: string[]) => {
  const out: unknown[] = []
  for await (const v of readNdjson(stream(chunks))) out.push(v)
  return out
}

describe("readNdjson", () => {
  it("rend chaque ligne, même coupée entre deux morceaux, et ignore les lignes vides", async () => {
    expect(await all(['{"a":1}\n{"b"', ':2}\n\n', '{"c":3}\n'])).toEqual([{ a: 1 }, { b: 2 }, { c: 3 }])
  })
  it("garde la dernière ligne sans saut final", async () => {
    expect(await all(['{"a":1}\n{"done":true}'])).toEqual([{ a: 1 }, { done: true }])
  })
  it("recolle un caractère multioctet coupé en deux", async () => {
    const b = new TextEncoder().encode('{"t":"é"}\n')
    const s = new ReadableStream<Uint8Array>({
      start(c) {
        c.enqueue(b.slice(0, 7))
        c.enqueue(b.slice(7))
        c.close()
      },
    })
    const out: unknown[] = []
    for await (const v of readNdjson(s)) out.push(v)
    expect(out).toEqual([{ t: "é" }])
  })
  it("lève sur une ligne invalide", async () => {
    await expect(all(["{oops}\n"])).rejects.toThrow()
  })
})
