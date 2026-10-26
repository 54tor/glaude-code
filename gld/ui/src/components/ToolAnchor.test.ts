import { describe, expect, it } from "vitest"
import type { Msg } from "@/lib/api"
import { groupTools } from "./ToolAnchor"

describe("groupTools", () => {
  it("regroupe les ancres consécutives et donne le décalage du message suivant", () => {
    const m = (r: Msg["r"], o: number, t = ""): Msg => ({ r, t, o })
    const out = groupTools([m("user", 0, "a"), m("tool", 10), m("tool", 20), m("assistant", 30, "b"), m("tool", 40)])
    expect(out).toEqual([m("user", 0, "a"), { count: 2, from: 10, until: 30 }, m("assistant", 30, "b"), { count: 1, from: 40, until: 0 }])
  })
})
