import { describe, expect, it } from "vitest"
import { stepIn } from "./carousel"

describe("stepIn", () => {
  const is = (content: string) => (input: string) => input === content
  it("avance et recule en boucle", () => {
    expect(stepIn(["a", "b", "c"], is("c"), 1)).toBe("a")
    expect(stepIn(["a", "b", "c"], is("a"), -1)).toBe("c")
    expect(stepIn(["a", "b", "c"], is("b"), 1)).toBe("c")
  })
  it("session lue absente de la file : première ou dernière", () => {
    expect(stepIn(["a", "b"], is("z"), 1)).toBe("a")
    expect(stepIn(["a", "b"], is("z"), -1)).toBe("b")
  })
  it("rien d'autre à lire", () => {
    expect(stepIn(["a"], is("a"), 1)).toBeUndefined()
    expect(stepIn([], is("a"), -1)).toBeUndefined()
  })
})
