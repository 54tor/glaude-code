import { describe, expect, it } from "vitest"
import type { GitState } from "@/lib/api"
import { needsPush } from "./GitPush"
import { syncOf } from "./GitBadge"

const clean: GitState = { branch: "main", upstream: true, ahead: 0, behind: 0, changed: 0, untracked: 0 }

describe("état git sur tous les remotes", () => {
  it("amont à jour mais un autre remote en retard : à pousser", () => {
    const state = { ...clean, remotes: [{ name: "origin", ahead: 0, behind: 0 }, { name: "backup", ahead: 3, behind: 0 }] }
    expect(needsPush(state)).toBe(true)
    expect(syncOf(state)).toMatchObject({ ahead: 3, behind: 0, missing: [] })
  })
  it("un remote sans la branche : à pousser", () => {
    const state = { ...clean, remotes: [{ name: "origin", ahead: 0, behind: 0 }, { name: "mirror", ahead: 0, behind: 0, missing: true }] }
    expect(needsPush(state)).toBe(true)
    expect(syncOf(state).missing.map((remote) => remote.name)).toEqual(["mirror"])
  })
  it("tout à jour : rien à pousser", () => {
    expect(needsPush({ ...clean, remotes: [{ name: "origin", ahead: 0, behind: 0 }] })).toBe(false)
  })
})
