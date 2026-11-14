// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

vi.mock("@/hooks/useGit", () => ({ refreshGit: vi.fn() }))
vi.mock("@/lib/gitPushStream", () => ({ streamGitPush: vi.fn(() => new Promise(() => {})) }))

import { act } from "@testing-library/react"
import { setGitPushTerminalOpen, startGitPush, useGitPushRuns } from "@/hooks/useGitPush"
import { GitPushPill } from "./GitPushPill"

afterEach(cleanup)

function PillOfCurrentRun() {
  const run = useGitPushRuns()[0]
  return run ? <><span data-testid="etat">{String(run.isTerminalOpen)}</span><GitPushPill run={run} position={0} /></> : null
}

describe("GitPushPill", () => {
  it("rouvre le terminal du push en cours au clic", () => {
    render(<PillOfCurrentRun />)
    act(() => startGitPush("/depot", "origin", "main"))
    act(() => setGitPushTerminalOpen(1, false))
    expect(screen.getByTestId("etat").textContent).toBe("false")

    fireEvent.click(screen.getByRole("button"))

    expect(screen.getByTestId("etat").textContent).toBe("true")
  })
})
