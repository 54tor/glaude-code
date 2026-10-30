import { describe, expect, it } from "vitest"
import { UPLOADED } from "./Message"

describe("UPLOADED", () => {
  it("matches an uploaded image reference", () => {
    const m = [..."look @/home/bob/.claude/gld-web-images/a1.png ok".matchAll(UPLOADED)]
    expect(m.map((x) => x[1])).toEqual(["a1.png"])
  })
  it("stays fast on a 32000-char run of @", () => {
    const t0 = performance.now()
    for (const s of ["@".repeat(32000), "@a".repeat(16000), "@" + "a".repeat(32000)]) expect([...s.matchAll(UPLOADED)]).toEqual([])
    expect(performance.now() - t0).toBeLessThan(500)
  })
})
