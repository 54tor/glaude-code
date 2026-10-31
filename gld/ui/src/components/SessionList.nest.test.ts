import { describe, expect, it } from "vitest"
import { nest } from "./SessionList"
import type { Row } from "./actions"

const r = (id: string, parent?: string) => ({ s: { id, parent } }) as unknown as Row

describe("nest", () => {
  it("range les enfants sous leur parente et laisse en haut les orphelins", () => {
    const { tops, kids } = nest([r("p"), r("a", "p"), r("b", "p"), r("o", "disparue")])
    expect(tops.map((row) => row.s.id)).toEqual(["p", "o"])
    expect(kids.get("p")!.map((row) => row.s.id)).toEqual(["a", "b"])
  })
})
