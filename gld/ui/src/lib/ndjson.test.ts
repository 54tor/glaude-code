import { describe, expect, it } from "vitest"
import { readNdjson } from "./ndjson"

const stream = (chunks: string[]) => {
  const enc = new TextEncoder()
  return new ReadableStream<Uint8Array>({
    start(readableStreamDefaultController) {
      for (const ch of chunks) readableStreamDefaultController.enqueue(enc.encode(ch))
      readableStreamDefaultController.close()
    },
  })
}
const all = async (chunks: string[]) => {
  const out: unknown[] = []
  for await (const value of readNdjson(stream(chunks))) out.push(value)
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
    const uint8Array = new TextEncoder().encode('{"t":"é"}\n')
    const readableStream = new ReadableStream<Uint8Array>({
      start(readableStreamDefaultController) {
        readableStreamDefaultController.enqueue(uint8Array.slice(0, 7))
        readableStreamDefaultController.enqueue(uint8Array.slice(7))
        readableStreamDefaultController.close()
      },
    })
    const out: unknown[] = []
    for await (const value of readNdjson(readableStream)) out.push(value)
    expect(out).toEqual([{ t: "é" }])
  })
  it("lève sur une ligne invalide", async () => {
    await expect(all(["{oops}\n"])).rejects.toThrow()
  })
})
