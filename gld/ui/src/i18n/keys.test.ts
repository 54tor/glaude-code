import { describe, expect, it } from "vitest"
import { i18n } from "./index"

const sources = import.meta.glob<string>("/src/**/*.{ts,tsx}", { query: "?raw", import: "default", eager: true })

// t("a.b.c") ou i18n.t("a.b.c") : les clés littérales seulement (les clés construites avec ${…} sont ignorées).
const CALL = /\b(?:i18n\.)?(?:t|tr)\(\s*"([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)"/g
const OTHER = /\b(?:labelKey|titleKey|descKey|key)\s*:\s*"((?:common|format|shell|conversation|memory|config|skills|auth)\.[a-zA-Z0-9_.]+)"/g

describe("clés de traduction", () => {
  const used = new Map<string, string>()
  for (const [file, src] of Object.entries(sources)) {
    if (file.includes("/i18n/") || file.includes(".test.")) continue
    for (const re of [CALL, OTHER]) for (const regExpExecArray of src.matchAll(re)) used.set(regExpExecArray[1], file)
  }

  it("trouve des clés dans le code", () => {
    expect(used.size).toBeGreaterThan(300)
  })

  it("toute clé utilisée existe dans les deux langues", () => {
    const missing: string[] = []
    for (const [key, file] of used) {
      for (const lang of ["fr", "en"]) {
        const ok = i18n.exists(key, { lng: lang }) || i18n.exists(key + "_other", { lng: lang }) || i18n.exists(key + "_one", { lng: lang })
        if (!ok) missing.push(`${lang} ${key} (${file.split("/").pop()})`)
      }
    }
    expect(missing).toEqual([])
  })
})
