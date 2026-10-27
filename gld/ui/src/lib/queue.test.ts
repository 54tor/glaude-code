import { describe, expect, it } from "vitest"
import { FOLLOW_PREFIX, withFollowUps, withoutFollowUps } from "./queue"

describe("file d'attente", () => {
  it("préfixe seulement quand d'autres messages suivent, sans doubler le préfixe", () => {
    expect(withFollowUps("a", false)).toBe("a")
    const p = withFollowUps("a", true)
    expect(p).toBe(FOLLOW_PREFIX + "a")
    expect(withFollowUps(p, true)).toBe(p)
    expect(withoutFollowUps(p)).toBe("a")
  })
})
