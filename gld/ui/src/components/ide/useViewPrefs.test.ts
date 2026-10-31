// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react"
import { beforeEach, describe, expect, it } from "vitest"
import { DEFAULT_PREFS, useViewPrefs, zoomed } from "./useViewPrefs"

beforeEach(() => localStorage.clear())

describe("useViewPrefs", () => {
  it("défauts sans stockage", () => {
    const { result } = renderHook(useViewPrefs)
    expect(result.current).toEqual({ ...DEFAULT_PREFS, set: expect.any(Function) })
    expect(DEFAULT_PREFS).toEqual({
      wordWrap: false,
      minimap: false,
      fontSize: 13,
      showTree: true,
      showStatus: true,
      showPreview: true,
    })
  })

  it("persiste sous gld.ide.view.* et relit au montage suivant", () => {
    const { result, unmount } = renderHook(useViewPrefs)
    act(() => result.current.set("wordWrap", true))
    act(() => result.current.set("fontSize", 16))
    act(() => result.current.set("showTree", false))
    expect(localStorage.getItem("gld.ide.view.wordWrap")).toBe("1")
    expect(localStorage.getItem("gld.ide.view.fontSize")).toBe("16")
    expect(localStorage.getItem("gld.ide.view.showTree")).toBe("0")
    unmount()
    const again = renderHook(useViewPrefs).result.current
    expect([again.wordWrap, again.fontSize, again.showTree]).toEqual([
      true,
      16,
      false,
    ])
  })

  it("valeurs illisibles (JSON, texte, nombres hors bornes) : défauts ou bornes", () => {
    localStorage.setItem("gld.ide.view.wordWrap", '{"a":1')
    localStorage.setItem("gld.ide.view.minimap", "true")
    localStorage.setItem("gld.ide.view.showStatus", "nimporte")
    localStorage.setItem("gld.ide.view.fontSize", "999")
    const r = renderHook(useViewPrefs).result.current
    expect([r.wordWrap, r.minimap, r.showStatus, r.fontSize]).toEqual([
      false,
      false,
      true,
      28,
    ])
    for (const [s, n] of [
      ["3", 10],
      ["abc", 13],
      ["12.5", 13],
      ["", 13],
      ["-4", 13],
    ] as const) {
      localStorage.setItem("gld.ide.view.fontSize", s)
      expect(renderHook(useViewPrefs).result.current.fontSize, s).toBe(n)
    }
  })

  it("set(fontSize) borne entre 10 et 28 ; zoomed suit le même calcul", () => {
    const { result } = renderHook(useViewPrefs)
    act(() => result.current.set("fontSize", 40))
    expect(result.current.fontSize).toBe(28)
    act(() => result.current.set("fontSize", 2))
    expect(result.current.fontSize).toBe(10)
    expect([
      zoomed(13, 1),
      zoomed(13, -1),
      zoomed(20, 0),
      zoomed(28, 1),
      zoomed(10, -1),
    ]).toEqual([14, 12, 13, 28, 10])
  })
})
