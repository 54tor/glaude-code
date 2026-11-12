// @vitest-environment jsdom
import { act, cleanup, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

const writtenToTerminal: Uint8Array[] = []
vi.mock("@xterm/xterm", () => ({ Terminal: class { open() {} write(chunk: Uint8Array) { writtenToTerminal.push(chunk) } dispose() {} } }))
vi.mock("@xterm/xterm/css/xterm.css", () => ({}))
vi.mock("@/hooks/useGit", () => ({ refreshGit: vi.fn() }))
vi.mock("@/lib/gitPushStream", () => ({ streamGitPush: vi.fn() }))

import { streamGitPush } from "@/lib/gitPushStream"
import { dismissGitPush, setGitPushTerminalOpen, startGitPush } from "@/hooks/useGitPush"
import { GitPushTerminal } from "./GitPushTerminal"

afterEach(() => {
  cleanup()
  dismissGitPush()
  writtenToTerminal.length = 0
  vi.mocked(streamGitPush).mockReset()
})

describe("GitPushTerminal", () => {
  it("affiche le code de retour d'un hook qui refuse", async () => {
    vi.mocked(streamGitPush).mockResolvedValue(1)
    render(<GitPushTerminal />)

    await act(async () => startGitPush("/depot", "origin", "main"))

    expect(await screen.findByRole("alert")).toBeTruthy()
    expect(streamGitPush).toHaveBeenCalledWith("/depot", "origin", expect.any(Function))
  })

  it("rejoue la sortie déjà reçue quand on rouvre le terminal après l'avoir réduit", async () => {
    vi.mocked(streamGitPush).mockImplementation(async (_dir, _remote, onOutput) => {
      onOutput(new TextEncoder().encode("sortie du hook"))
      return 0
    })
    render(<GitPushTerminal />)
    await act(async () => startGitPush("/depot", "origin", "main"))
    await act(async () => setGitPushTerminalOpen(false))
    writtenToTerminal.length = 0

    await act(async () => setGitPushTerminalOpen(true))

    expect(writtenToTerminal.map((chunk) => new TextDecoder().decode(chunk))).toEqual(["sortie du hook"])
  })
})
