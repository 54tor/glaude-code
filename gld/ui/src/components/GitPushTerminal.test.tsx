// @vitest-environment jsdom
import { cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@xterm/xterm", () => ({ Terminal: class { open() {} write() {} dispose() {} } }))
vi.mock("@xterm/xterm/css/xterm.css", () => ({}))
vi.mock("@/hooks/useGit", () => ({ refreshGit: vi.fn() }))
vi.mock("@/lib/gitPushStream", () => ({ streamGitPush: vi.fn() }))

import { streamGitPush } from "@/lib/gitPushStream"
import { GitPushTerminal } from "./GitPushTerminal"

afterEach(cleanup)

describe("GitPushTerminal", () => {
  it("lance la poussée à l'ouverture et affiche le code de retour d'un hook qui refuse", async () => {
    vi.mocked(streamGitPush).mockResolvedValue(1)

    render(<GitPushTerminal dir="/depot" remote="origin" branch="main" onClose={() => {}} />)

    expect(await screen.findByRole("alert")).toBeTruthy()
    expect(streamGitPush).toHaveBeenCalledWith("/depot", "origin", expect.any(Function), expect.any(AbortSignal))
  })
})
