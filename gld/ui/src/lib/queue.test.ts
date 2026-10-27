import { describe, expect, it } from "vitest"
import { FOLLOW_PREFIX, withoutFollowUps } from "./queue"

describe("file d'attente", () => {
  it("retire le préfixe de suite s'il est présent", () => {
    expect(withoutFollowUps(FOLLOW_PREFIX + "a")).toBe("a")
    expect(withoutFollowUps("a")).toBe("a")
  })
})
