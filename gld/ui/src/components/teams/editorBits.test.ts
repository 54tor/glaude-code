import { describe, expect, it } from "vitest"
import { sameLabel } from "./editorBits"

describe("sameLabel", () => {
  it("ignores case and surrounding spaces", () => {
    expect(sameLabel("Testeur", " testeur ")).toBe(true)
  })
  it("keeps a different description", () => {
    expect(sameLabel("Développement backend", "développeuse backend")).toBe(false)
  })
})
