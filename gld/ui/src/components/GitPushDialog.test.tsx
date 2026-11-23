// @vitest-environment jsdom
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

vi.mock("@/hooks/useGitPush", () => ({ startGitPushes: vi.fn() }))
vi.mock("@/lib/api", () => ({
  api: {
    gitRemotes: vi.fn(),
    gitUnpushed: vi.fn(),
    gitTags: vi.fn(),
  },
}))

import { startGitPushes } from "@/hooks/useGitPush"
import { api } from "@/lib/api"
import { GitPushDialog } from "./GitPushDialog"

const flush = () => act(async () => {})

const commit = { hash: "abc1234", subject: "fix: un", date: "2026-11-23" }

beforeEach(() => {
  vi.mocked(api.gitRemotes).mockResolvedValue({
    remotes: [
      { name: "amont", url: "https://example.org/amont.git" },
      { name: "origin", url: "git@example.org:x.git" },
    ],
  })
  vi.mocked(api.gitUnpushed).mockResolvedValue({
    remotes: [
      { name: "amont", tracked: false, total: 18, commits: [commit] },
      { name: "origin", tracked: true, total: 2, commits: [commit] },
    ],
  })
  vi.mocked(api.gitTags).mockResolvedValue({ tags: ["v1.0.0", "v0.9.0"] })
})

afterEach(() => {
  cleanup()
  vi.clearAllMocks()
})

async function ouvrir() {
  render(<GitPushDialog dir="/depot" branch="main" onOpenChange={vi.fn()} />)
  await flush()
}

describe("GitPushDialog", () => {
  it("ne coche par défaut que le remote suivi par la branche", async () => {
    await ouvrir()

    const cases = screen.getAllByRole("checkbox")
    const nomsCoches = ["amont", "origin"].filter(
      (_, indexRemote) =>
        cases[indexRemote].getAttribute("data-state") === "checked"
    )
    expect(nomsCoches).toEqual(["origin"])
  })

  it("sans remote suivi, coche tous les remotes en retard", async () => {
    vi.mocked(api.gitUnpushed).mockResolvedValue({
      remotes: [
        { name: "amont", tracked: false, total: 3, commits: [commit] },
        { name: "origin", tracked: false, total: 0, commits: [] },
      ],
    })
    await ouvrir()

    const cases = screen.getAllByRole("checkbox")
    expect(cases[0].getAttribute("data-state")).toBe("checked")
    expect(cases[1].getAttribute("data-state")).toBe("unchecked")
  })

  it("pousse les remotes et les tags cochés, en forçant les tags sur demande", async () => {
    await ouvrir()

    const cases = screen.getAllByRole("checkbox")
    fireEvent.click(cases[0])
    fireEvent.click(cases[2])
    await flush()
    fireEvent.click(screen.getAllByRole("checkbox").at(-1)!)
    fireEvent.click(screen.getAllByRole("button").at(-2)!)

    expect(startGitPushes).toHaveBeenCalledWith(
      "/depot",
      ["amont", "origin"],
      "main",
      ["v1.0.0"],
      true
    )
  })

  it("affiche l'échec de l'aperçu et une liste de remotes vide", async () => {
    vi.mocked(api.gitRemotes).mockResolvedValue({ remotes: [] })
    vi.mocked(api.gitUnpushed).mockRejectedValue(new Error("boom"))
    await ouvrir()

    expect(screen.queryAllByRole("checkbox")).toHaveLength(2)
  })
})
