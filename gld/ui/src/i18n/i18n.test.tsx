// @vitest-environment jsdom
import { act, cleanup, render } from "@testing-library/react"
import { afterEach, describe, expect, it } from "vitest"
import { useTranslation } from "react-i18next"
import { ago, size } from "@/lib/format"
import { catalogFiles, i18n, resolveFront, setLang } from "./index"

const flat = (object: object, path = ""): string[] =>
  Object.entries(object).flatMap(([key, value]) => (typeof value === "object" && value ? flat(value, path + key + ".") : [path + key]))
const leaves = (object: object): string[] => Object.values(object).flatMap((value) => (typeof value === "object" && value ? leaves(value) : [String(value)]))
const names = (lookup: Record<string, unknown>) => Object.keys(lookup).map((path) => path.split("/").pop()).sort()
const bundle = (lang: string) => i18n.getResourceBundle(lang, "translation") as object

afterEach(() => {
  cleanup()
  setLang("fr")
})

describe("resolveFront", () => {
  it("suit le serveur, puis le navigateur, puis le français", () => {
    expect(resolveFront("fr", "en-US")).toBe("fr")
    expect(resolveFront("en", "fr-FR")).toBe("en")
    expect(resolveFront("other", "fr-FR")).toBe("en")
    expect(resolveFront(undefined, "en-US")).toBe("en")
    expect(resolveFront("", "de")).toBe("fr")
  })
})

describe("catalogues", () => {
  it("ont les mêmes fichiers, les mêmes clés et aucune valeur vide", () => {
    expect(names(catalogFiles.en)).toEqual(names(catalogFiles.fr))
    expect(flat(bundle("en")).sort()).toEqual(flat(bundle("fr")).sort())
    for (const value of [...leaves(bundle("fr")), ...leaves(bundle("en"))]) expect(value.trim()).not.toBe("")
  })
  it("le premier niveau de chaque fichier porte son nom", async () => {
    for (const lang of ["fr", "en"] as const)
      for (const [path, load] of Object.entries(catalogFiles[lang])) {
        const value = typeof load === "function" ? await load() : load
        expect(Object.keys(value.default)).toEqual([path.split("/").pop()!.replace(".json", "")])
      }
  })
})

describe("formats", () => {
  const now = Date.now() / 1000
  it("suivent la langue", () => {
    expect(ago(now - 3 * 3600)).toBe("il y a 3 heures")
    expect(size(2048)).toBe("2 Mo")
    setLang("en")
    expect(ago(now - 3 * 3600)).toBe("3 hours ago")
    expect(ago(now - 5)).toBe("just now")
    expect(size(2048)).toBe("2 MB")
  })
})

function Probe() {
  const { t } = useTranslation()
  return <p>{t("format.now")}</p>
}

describe("rendu", () => {
  it("change de langue sans remontage et n'affiche aucun identifiant brut", () => {
    const { container } = render(<Probe />)
    expect(container.textContent).toBe("à l'instant")
    act(() => setLang("en"))
    expect(container.textContent).toBe("just now")
    expect(container.textContent).not.toMatch(/\b[a-z]+(\.[a-zA-Z]+)+\b/)
    expect(document.documentElement.lang).toBe("en")
  })
})
