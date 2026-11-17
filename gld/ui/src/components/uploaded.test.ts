import { describe, expect, it } from "vitest"
import { UPLOADED } from "./Message"

describe("UPLOADED", () => {
  it("matches an uploaded image reference", () => {
    const regExpExecArrays = [..."look @/home/bob/.claude/gld-web-images/a1.png ok".matchAll(UPLOADED)]
    expect(regExpExecArrays.map((regExpExecArray) => regExpExecArray[1])).toEqual(["a1.png"])
  })
  it("stays fast on a 32000-char run of @", () => {
    const t0 = performance.now()
    for (const text of ["@".repeat(32000), "@a".repeat(16000), "@" + "a".repeat(32000)]) expect([...text.matchAll(UPLOADED)]).toEqual([])
    expect(performance.now() - t0).toBeLessThan(500)
  })
})

describe("UPLOADED fichiers joints", () => {
  it("reconnaît un fichier non image", () => {
    const regExpExecArrays = [..."@~/.claude/gld-web-images/20261117-231631-eff8f81e.md".matchAll(UPLOADED)]
    expect(regExpExecArrays.map((regExpExecArray) => regExpExecArray[1])).toEqual(["20261117-231631-eff8f81e.md"])
  })
})
