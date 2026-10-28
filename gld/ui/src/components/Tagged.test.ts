// @vitest-environment jsdom
import { fireEvent, render, screen } from "@testing-library/react"
import { createElement } from "react"
import { describe, expect, it, vi } from "vitest"
import { isTagged, parseJsonBody, Tagged } from "./Tagged"

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
  it("le bouton Répondre transmet l'adresse et le nom de l'expéditeur", () => {
    const onReply = vi.fn()
    const t = '<cross-session-message from="uds:/x.sock" from-name="b7" from-mode="prompting">hi</cross-session-message>'
    render(createElement(Tagged, { t, md: (x: string) => x, onReply }))
    fireEvent.click(screen.getByRole("button"))
    expect(onReply).toHaveBeenCalledWith("uds:/x.sock", "b7")
  })
})
