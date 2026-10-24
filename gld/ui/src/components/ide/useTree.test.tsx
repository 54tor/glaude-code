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

  it("refreshAll relit la racine et les seuls dossiers dépliés ; collapseAll replie tout sans appel", async () => {
    localStorage.setItem("gld.ide./p.open", JSON.stringify(["src"]))
    const tree = vi.spyOn(api, "fsTree").mockResolvedValue(listing("x"))
    const { result } = renderHook(() => useTree("/p"))
    await waitFor(() => expect(tree).toHaveBeenCalledTimes(2))
    act(() => result.current.refreshAll())
    expect(tree.mock.calls.slice(2)).toEqual([["/p", ""], ["/p", "src"]])
    act(() => result.current.collapseAll())
    expect(result.current.open.size).toBe(0)
    expect(tree).toHaveBeenCalledTimes(4)
  })
  it("refreshAll garde la garde par chemin : une réponse plus ancienne est ignorée", async () => {
    let first!: (v: L) => void
    vi.spyOn(api, "fsTree").mockReturnValueOnce(new Promise<L>((r) => (first = r))).mockResolvedValue(listing("neuf"))
    const { result } = renderHook(() => useTree("/p"))
    act(() => result.current.refreshAll())
    await waitFor(() => expect(result.current.lists[""]).toEqual(listing("neuf")))
    await act(async () => first(listing("vieux")))
    expect(result.current.lists[""]).toEqual(listing("neuf"))
  })
})
