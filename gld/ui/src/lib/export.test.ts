import { describe, expect, it, vi } from "vitest"
import { api, type Msg } from "./api"
import { wholeConv } from "./export"

const m = (t: string): Msg => ({ r: "user", t })

describe("wholeConv", () => {
  it("lit les pages depuis la fin, sans limit, et les remet dans l'ordre", async () => {
    const pages: Record<number, { msgs: Msg[]; start: number; more: boolean }> = {
      0: { msgs: [m("c"), m("d")], start: 20, more: true },
      20: { msgs: [m("a"), m("b")], start: 0, more: false },
    }
    const conv = vi.spyOn(api, "conv").mockImplementation(async (_s, _tools, page) => pages[page?.before ?? -1])
    expect((await wholeConv({ dir: "d", id: "i" })).map((x) => x.t)).toEqual(["a", "b", "c", "d"])
    expect(conv.mock.calls.map((c) => c[2])).toEqual([{ before: 0 }, { before: 20 }])
    conv.mockRestore()
  })
  it("s'arrête si le serveur annonce une suite sans décalage", async () => {
    const conv = vi.spyOn(api, "conv").mockResolvedValue({ msgs: [m("x")], start: 0, more: true })
    expect(await wholeConv({ dir: "d", id: "i" })).toHaveLength(1)
    expect(conv).toHaveBeenCalledTimes(1)
    conv.mockRestore()
  })
})
