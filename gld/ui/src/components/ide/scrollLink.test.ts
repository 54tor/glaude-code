import { describe, expect, it, vi } from "vitest"
import { createScrollLink } from "./scrollLink"

describe("scrollLink", () => {
  it("relaie chaque côté vers l'autre, et plus rien après désinscription", () => {
    const link = createScrollLink()
    const applyEditor = vi.fn()
    const applyPreview = vi.fn()
    const detachEditor = link.attachEditor(applyEditor)
    link.attachPreview(applyPreview)
    link.fromEditor(0.25)
    link.fromPreview(0.75)
    expect(applyPreview).toHaveBeenCalledWith(0.25)
    expect(applyEditor).toHaveBeenCalledWith(0.75)
    detachEditor()
    link.fromPreview(0.5)
    expect(applyEditor).toHaveBeenCalledTimes(1)
  })
})
