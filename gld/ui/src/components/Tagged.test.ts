import { describe, expect, it } from "vitest"
import { isTagged, parseJsonBody } from "./Tagged"

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

describe("parseJsonBody", () => {
  it("décode un objet JSON", () => {
    expect(parseJsonBody('{"type":"idle_notification","result":"a\\nb"}')).toEqual({ type: "idle_notification", result: "a\nb" })
  })
  it("ignore le texte libre et le JSON invalide", () => {
    expect(parseJsonBody("go")).toBeNull()
    expect(parseJsonBody("{pas du json")).toBeNull()
  })
})

describe("cross-session-message", () => {
  it("est reconnu même sans être en tête du message", () => {
    expect(isTagged('le support\n<cross-session-message from="uds:/x.sock" from-name="b7" from-mode="prompting">hi</cross-session-message>')).toBe(true)
  })
})
