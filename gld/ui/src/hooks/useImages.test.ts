import { describe, expect, it } from "vitest"
import { isLongPaste } from "./useImages"

describe("isLongPaste", () => {
  it("laisse passer un collage court", () => {
    expect(isLongPaste("a\nb")).toBe(false)
  })
  it("bascule en fichier au-delà de 1000 lignes", () => {
    expect(isLongPaste("x\n".repeat(1000))).toBe(true)
  })
  it("bascule en fichier au-delà de 32000 caractères", () => {
    expect(isLongPaste("x".repeat(32001))).toBe(true)
  })
})
