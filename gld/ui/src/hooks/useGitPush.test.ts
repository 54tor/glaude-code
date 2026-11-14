// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/gitPushStream", () => ({ streamGitPush: vi.fn() }))
vi.mock("@/hooks/useGit", () => ({ refreshGit: vi.fn() }))

import { streamGitPush } from "@/lib/gitPushStream"
import { dismissGitPush, gitPushOutputTail, startGitPush, listGitPushRuns } from "./useGitPush"

const flushPromises = () => new Promise((resolve) => setTimeout(resolve, 0))

afterEach(() => {
  listGitPushRuns().forEach((run) => dismissGitPush(run.id))
  vi.mocked(streamGitPush).mockReset()
})

describe("useGitPush store", () => {
  it("garde la sortie, sans séquences de couleur, et en rend la fin", async () => {
    vi.mocked(streamGitPush).mockImplementation(async (_dir, _remote, onOutput) => {
      onOutput(new TextEncoder().encode("\u001b[31mrouge\u001b[0m\nligne 2\n"))
      return 1
    })

    startGitPush("/depot", "origin", "main")
    await flushPromises()

    const runId = listGitPushRuns()[0].id
    expect(gitPushOutputTail(runId, 5)).toBe("rouge\nligne 2")
    expect(gitPushOutputTail(runId, 1)).toBe("ligne 2")
  })

  it("ne lance pas un second push sur le même dépôt tant que le premier tourne", async () => {
    let finishFirstPush: (exitCode: number) => void = () => {}
    vi.mocked(streamGitPush).mockImplementation(() => new Promise<number>((resolve) => (finishFirstPush = resolve)))

    startGitPush("/depot", "origin", "main")
    startGitPush("/depot", "autre", "main")

    expect(streamGitPush).toHaveBeenCalledTimes(1)
    finishFirstPush(0)
    await flushPromises()
  })

  it("lance en parallèle des pushes sur des dépôts différents", async () => {
    vi.mocked(streamGitPush).mockImplementation(() => new Promise<number>(() => {}))

    startGitPush("/depot-a", "origin", "main")
    startGitPush("/depot-b", "origin", "main")

    expect(streamGitPush).toHaveBeenCalledTimes(2)
  })
})
