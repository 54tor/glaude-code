import { describe, expect, it } from "vitest"
import { noBreak, rehypeNoBreak } from "./noBreak"

describe("noBreak", () => {
  it("wraps short hyphenated words and leaves long ones", () => {
    const out = noBreak("see UI-01 now")
    expect(out).toHaveLength(3)
    expect(noBreak(`${"a".repeat(20)}-${"b".repeat(20)}`)).toHaveLength(1)
  })
  it("stays fast on a 32000-letter token", () => {
    const big = "a".repeat(32000)
    const t0 = performance.now()
    noBreak(big)
    noBreak(big + "-b")
    const tree = { type: "root", children: [{ type: "text", value: big }] }
    rehypeNoBreak()(tree)
    expect(performance.now() - t0).toBeLessThan(500)
  })
})
