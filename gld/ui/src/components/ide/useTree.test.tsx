// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api } from "@/lib/api"
import { useTree } from "./useTree"

type L = Awaited<ReturnType<typeof api.fsTree>>
const listing = (name: string): L => ({ entries: [{ name, type: "file", size: 1, mtime: 1 }], truncated: false })
afterEach(() => (vi.restoreAllMocks(), localStorage.clear()))

describe("useTree", () => {
  it("une réponse plus ancienne n'écrase pas un listing plus récent du même dossier", async () => {
    let first!: (v: L) => void
    const tree = vi.spyOn(api, "fsTree").mockReturnValueOnce(new Promise<L>((r) => (first = r))).mockResolvedValue(listing("neuf"))
    const { result } = renderHook(() => useTree("/p"))
    act(() => result.current.refresh(""))
    await waitFor(() => expect(result.current.lists[""]).toEqual(listing("neuf")))
    await act(async () => first(listing("vieux")))
    expect(result.current.lists[""]).toEqual(listing("neuf"))
    expect(tree).toHaveBeenCalledTimes(2)
  })
})
