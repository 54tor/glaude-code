// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { getNotifications, resetNotifications, toast } from "@/lib/notify"
import { ActionsCtx, type Actions } from "./actions"
import { CleanupWarning } from "./CleanupWarning"

const NOW_SECONDS = Date.now() / 1000
const session = (id: string, hoursLeft: number) => ({
  dir: "d",
  id,
  title: `titre ${id}`,
  cwd: "/home/bob/p",
  expires: NOW_SECONDS + hoursLeft * 3600,
})
const mount = async () => {
  render(
    <ActionsCtx.Provider value={{ read: vi.fn() } as unknown as Actions}>
      <CleanupWarning />
    </ActionsCtx.Provider>
  )
  await act(async () => {})
}

describe("CleanupWarning", () => {
  beforeEach(() => resetNotifications())
  afterEach(() => {
    cleanup()
    vi.restoreAllMocks()
  })

  it("ne rend rien dans le menu et publie une alerte persistante", async () => {
    vi.spyOn(api, "cleanupWarning").mockResolvedValue({
      days: 30,
      sessions: [session("a", 5)],
    })
    await mount()
    const [alert] = getNotifications().visible
    expect(alert.kind).toBe("warning")
    expect(alert.id).toBe("cleanup-warning")
    expect(document.body.textContent).toBe("")
  })

  it("retire l'alerte quand plus aucune session n'est menacée", async () => {
    vi.spyOn(api, "cleanupWarning").mockResolvedValue({
      days: 30,
      sessions: [],
    })
    toast.warning("ancienne", { id: "cleanup-warning" })
    await mount()
    expect(getNotifications().visible).toHaveLength(0)
  })

  it("une alerte fermée à la main ne revient pas tant que les sessions sont les mêmes", async () => {
    vi.useFakeTimers({ shouldAdvanceTime: true })
    const warning = vi.spyOn(api, "cleanupWarning").mockResolvedValue({
      days: 30,
      sessions: [session("a", 5)],
    })
    await mount()
    toast.dismiss("cleanup-warning")
    await act(async () => void (await vi.advanceTimersByTimeAsync(11 * 60_000)))
    expect(warning.mock.calls.length).toBeGreaterThan(1)
    expect(getNotifications().visible).toHaveLength(0)
    vi.useRealTimers()
  })
})
