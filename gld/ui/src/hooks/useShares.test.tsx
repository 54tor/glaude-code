// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import { api, type ShareInfo } from "@/lib/api"
import { ApiError } from "@/lib/apiError"
import { resetShares, useShares } from "./useShares"

const share = (id: string): ShareInfo => ({ id, dir: "/d", title: "t", handle: "h", open: true, invites: [] })
let seen: ReturnType<typeof useShares>[] = []
const Probe = ({ fast }: { fast?: boolean }) => {
  seen.push(useShares(fast))
  return null
}
const last = () => seen[seen.length - 1]

beforeEach(() => {
  vi.useFakeTimers()
  seen = []
  resetShares()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe("useShares", () => {
  it("lit au montage puis toutes les 10 s, et s'arrête au démontage", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [share("a")] })
    const { unmount } = render(<Probe />)
    await act(async () => {})
    expect(mock).toHaveBeenCalledTimes(1)
    expect(last().shares?.[0].id).toBe("a")
    await act(async () => void vi.advanceTimersByTime(10000))
    expect(mock).toHaveBeenCalledTimes(2)
    unmount()
    await act(async () => void vi.advanceTimersByTime(60000))
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it("5 s quand l'overlay est ouvert", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
    render(<Probe fast />)
    await act(async () => {})
    await act(async () => void vi.advanceTimersByTime(10000))
    expect(mock).toHaveBeenCalledTimes(3)
  })

  it("onglet masqué : pas de lecture", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
    render(<Probe />)
    await act(async () => {})
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden")
    await act(async () => void vi.advanceTimersByTime(30000))
    expect(mock).toHaveBeenCalledTimes(1)
  })

  it("un seul sondage pour plusieurs consommateurs", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [share("a")] })
    render(
      <>
        <Probe />
        <Probe />
      </>
    )
    await act(async () => {})
    await act(async () => void vi.advanceTimersByTime(10000))
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it("403 et 404 : rien partagé, sans erreur", async () => {
    vi.spyOn(api, "shares").mockRejectedValue(new ApiError(403, "forbidden", "x"))
    render(<Probe />)
    await act(async () => {})
    expect(last().shares).toEqual([])
    expect(last().error).toBe(false)
  })

  it("autre erreur : error, shares reste nul", async () => {
    vi.spyOn(api, "shares").mockRejectedValue(new ApiError(500, "internal", "x"))
    render(<Probe />)
    await act(async () => {})
    expect(last().shares).toBeNull()
    expect(last().error).toBe(true)
  })

  it("refresh relit tout de suite", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
    render(<Probe />)
    await act(async () => {})
    await act(async () => void (await last().refresh()))
    expect(mock).toHaveBeenCalledTimes(2)
  })

  it("un sondage lent et ancien n'écrase pas une lecture plus récente", async () => {
    let slow!: (record: { shares: ShareInfo[] }) => void
    vi.spyOn(api, "shares")
      .mockResolvedValueOnce({ shares: [] })
      .mockReturnValueOnce(new Promise((callback) => (slow = callback)))
      .mockResolvedValueOnce({ shares: [share("new")] })
    render(<Probe />)
    await act(async () => {})
    await act(async () => void vi.advanceTimersByTime(10000))
    await act(async () => void (await last().refresh()))
    expect(last().shares?.[0].id).toBe("new")
    await act(async () => slow({ shares: [share("old")] }))
    expect(last().shares?.[0].id).toBe("new")
  })

  it("relit dès que l'onglet redevient visible", async () => {
    const mock = vi.spyOn(api, "shares").mockResolvedValue({ shares: [] })
    render(<Probe />)
    await act(async () => {})
    await act(async () => void document.dispatchEvent(new Event("visibilitychange")))
    expect(mock).toHaveBeenCalledTimes(2)
  })
})
