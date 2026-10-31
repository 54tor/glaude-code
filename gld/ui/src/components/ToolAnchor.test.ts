import { describe, expect, it } from "vitest"
import type { Msg } from "@/lib/api"
import { groupTools } from "./ToolAnchor"

describe("groupTools", () => {
  it("regroupe les ancres consécutives et donne le décalage du message suivant", () => {
    const callback = (raw: Msg["r"], number: number, t = ""): Msg => ({ r: raw, t, o: number })
    const out = groupTools([callback("user", 0, "a"), callback("tool", 10), callback("tool", 20), callback("assistant", 30, "b"), callback("tool", 40)])
    expect(out).toEqual([callback("user", 0, "a"), { count: 2, from: 10, until: 30 }, callback("assistant", 30, "b"), { count: 1, from: 40, until: 0 }])
  })
})
