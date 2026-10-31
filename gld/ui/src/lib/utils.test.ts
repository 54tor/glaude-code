// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest"
import { copyText, isSend, keyHint, spliceAtCaret } from "./utils"

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

describe("Entrée au clavier tactile", () => {
  afterEach(() => vi.unstubAllGlobals())
  const enter = (record: { shiftKey?: boolean; ctrlKey?: boolean } = {}) => ({ key: "Enter", shiftKey: false, nativeEvent: {}, ...record })
  const touch = (on: boolean) => vi.stubGlobal("matchMedia", (query: string) => ({ matches: on && query.includes("coarse"), media: query }))

  it("avec un clavier, Entrée envoie et Maj+Entrée va à la ligne", () => {
    touch(false)
    expect(isSend(enter())).toBe(true)
    expect(isSend(enter({ shiftKey: true }))).toBe(false)
    expect(keyHint("Écrire… (Entrée envoie)")).toBe("Écrire… (Entrée envoie)")
  })

  it("sur iPhone, Entrée va à la ligne, Ctrl+Entrée envoie encore, l'aide clavier disparaît", () => {
    touch(true)
    expect(isSend(enter())).toBe(false)
    expect(isSend(enter({ ctrlKey: true }))).toBe(true)
    expect(keyHint("Écrire à Claude… (Entrée envoie, Maj+Entrée : saut de ligne)")).toBe("Écrire à Claude…")
  })
})

describe("spliceAtCaret", () => {
  it("remplace la sélection, ajoute à la fin sans champ et respecte la limite", () => {
    const el = { selectionStart: 2, selectionEnd: 4 } as HTMLTextAreaElement
    expect(spliceAtCaret(el, "abcdef", "XY", 99)).toBe("abXYef")
    expect(spliceAtCaret(null, "abc", "XY", 99)).toBe("abcXY")
    expect(spliceAtCaret(null, "abc", "XYZ", 4)).toBe("abcX")
  })
})
