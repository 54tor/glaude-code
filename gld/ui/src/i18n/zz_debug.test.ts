import { expect, it } from "vitest"
import { i18n } from "./index"

it("tous les domaines de catalogue sont chargés dans les deux langues", () => {
  for (const lang of ["fr", "en"]) {
    const b = i18n.getResourceBundle(lang, "translation") as Record<string, object>
    expect(Object.keys(b).sort()).toEqual(["auth", "backup", "common", "config", "conversation", "extensions", "format", "ide", "memory", "shell", "skills", "teams"])
    for (const domain of Object.values(b)) expect(Object.keys(domain).length).toBeGreaterThan(0)
  }
})
