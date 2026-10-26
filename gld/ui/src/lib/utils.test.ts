// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"
import { copyText } from "./utils"

describe("copyText", () => {
  afterEach(() => vi.unstubAllGlobals())

  it("sans API Clipboard (HTTP sur le réseau local), copie par execCommand et ne laisse rien dans la page", async () => {
    vi.stubGlobal("navigator", { ...navigator, clipboard: undefined })
    let sel = ""
    document.execCommand = vi.fn(() => ((sel = document.querySelector("textarea")!.value), true))
    await expect(copyText("abc")).resolves.toBeUndefined()
    expect(sel).toBe("abc")
    expect(document.querySelector("textarea")).toBeNull()
  })

  it("rejette quand execCommand échoue, pour que l'appelant affiche l'erreur", async () => {
    vi.stubGlobal("navigator", { ...navigator, clipboard: undefined })
    document.execCommand = vi.fn(() => false)
    await expect(copyText("abc")).rejects.toThrow()
  })
})
