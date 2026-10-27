// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { draftOf, sendUncertain, setDraft } from "./useDraft"
import { useQueueDrain } from "./useQueueDrain"

afterEach(() => {
  vi.restoreAllMocks()
  vi.useRealTimers()
})

describe("useQueueDrain", () => {
  it("garde le message en tête tant que l'envoi n'est pas confirmé", async () => {
    vi.useFakeTimers()
    vi.spyOn(api, "sendInfo").mockResolvedValue({ ids: ["s9"], why: "" } as Awaited<ReturnType<typeof api.sendInfo>>)
    const post = vi.spyOn(api, "post").mockResolvedValue({ ok: false, net: true, out: "délai" })
    setDraft("s9", { queue: ["un", "deux"] })
    const ids = new Set(["s9"])
    renderHook(() => useQueueDrain(ids, ids, new Set()))
    await act(async () => void (await vi.advanceTimersByTimeAsync(6000)))
    expect(post).toHaveBeenCalledTimes(1)
    expect(draftOf("s9").queue).toEqual(["un", "deux"]) // réseau : livré ou non, on ne sait pas
    expect(draftOf("s9").sending).toBeDefined()
    await act(async () => void (await vi.advanceTimersByTimeAsync(30_000)))
    expect(post).toHaveBeenCalledTimes(1) // jamais renvoyé tout seul
    expect(sendUncertain(draftOf("s9"))).toBe(true)

    post.mockResolvedValue({ ok: true, out: "" })
    setDraft("s9", { sending: undefined }) // « Renvoyer »
    await act(async () => void (await vi.advanceTimersByTimeAsync(3000))) // un passage, moins que SETTLE avant le suivant
    expect(draftOf("s9").queue).toEqual(["deux"])
    expect(draftOf("s9").sending).toBeUndefined()
  })
})
