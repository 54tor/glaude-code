import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/lib/gitPushStream", () => ({ streamGitPush: vi.fn() }))
vi.mock("@/hooks/useGit", () => ({ refreshGit: vi.fn() }))

import { streamGitPush } from "@/lib/gitPushStream"
import { dismissGitPush, gitPushOutputTail, startGitPush } from "./useGitPush"

const flushPromises = () => new Promise((resolve) => setTimeout(resolve, 0))

afterEach(() => {
  dismissGitPush()
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

    expect(gitPushOutputTail(5)).toBe("rouge\nligne 2")
    expect(gitPushOutputTail(1)).toBe("ligne 2")
  })

  it("ne lance pas un second push tant que le premier tourne", async () => {
    let finishFirstPush: (exitCode: number) => void = () => {}
    vi.mocked(streamGitPush).mockImplementation(() => new Promise<number>((resolve) => (finishFirstPush = resolve)))

    startGitPush("/depot", "origin", "main")
    startGitPush("/depot", "autre", "main")

    expect(streamGitPush).toHaveBeenCalledTimes(1)
    finishFirstPush(0)
    await flushPromises()
  })
})
