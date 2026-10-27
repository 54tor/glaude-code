// @vitest-environment jsdom
import { describe, expect, it } from "vitest"
import { draftOf, setDraft } from "./useDraft"

describe("useDraft", () => {
  it("reprend la file écrite par un autre onglet", () => {
    setDraft("s1", { queue: ["a", "b"] })
    const other = JSON.stringify({ s1: { text: "", queue: ["b"] } }) // l'autre onglet a envoyé « a »
    localStorage.setItem("gld.drafts", other)
    window.dispatchEvent(new StorageEvent("storage", { key: "gld.drafts", newValue: other }))
    expect(draftOf("s1").queue).toEqual(["b"])
  })
})
