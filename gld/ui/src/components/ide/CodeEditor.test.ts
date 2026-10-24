import { describe, expect, it, vi } from "vitest"
// @ts-expect-error classe Uri réelle de Monaco, publiée sans typings à ce chemin ; l'éditeur complet ne se charge pas sous Node
import { URI } from "monaco-editor/base/common/uri"

const ts = vi.hoisted(() => ({ addExtraLib: vi.fn() }))
vi.mock("monaco-editor/editor", () => ({ Uri: URI }))
vi.mock("monaco-editor/languages/features/typescript/register", () => ({ typescriptDefaults: ts }))
vi.mock("./monacoSetup", () => ({}))

describe("setTsLib", () => {
  it("clé = URI encodée du modèle de l'éditeur ; remplacer libère l'ancienne entrée", async () => {
    const { setTsLib, clearTsLibs } = await import("./CodeEditor")
    const dispose = vi.fn()
    ts.addExtraLib.mockReturnValue({ dispose })
    setTsLib("dir with space/é#.ts", "a")
    expect(ts.addExtraLib).toHaveBeenCalledWith("a", "file:///dir%20with%20space/%C3%A9%23.ts")
    setTsLib("dir with space/é#.ts", "b")
    expect(dispose).toHaveBeenCalledTimes(1)
    clearTsLibs()
    expect(dispose).toHaveBeenCalledTimes(2)
  })
})
