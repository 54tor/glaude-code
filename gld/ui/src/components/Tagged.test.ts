import { describe, expect, it } from "vitest"
import { isTagged } from "./Tagged"

describe("isTagged", () => {
  it("détecte une balise connue après un tiret de liste", () => {
    expect(isTagged('- lien\n- <teammate-message teammate_id="a">go</teammate-message> suite')).toBe(true)
  })
  it("ignore une balise inconnue en milieu de texte", () => {
    expect(isTagged("voir <div>x</div> ici")).toBe(false)
  })
  it("garde les messages commençant par une balise", () => {
    expect(isTagged("<task-notification><status>ok</status></task-notification>")).toBe(true)
  })
})
