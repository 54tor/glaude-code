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
    render(createElement(Tagged, { t, md: (input: string) => input, onReply }))
    fireEvent.click(screen.getByRole("button"))
    expect(onReply).toHaveBeenCalledWith("uds:/x.sock", "b7")
  })
})

describe("agent-message", () => {
  it("est reconnu et rendu en carte avec l'expéditeur", () => {
    const t = '<agent-message from="a93586">[Subagent hand-back] rapport</agent-message>'
    expect(isTagged(`texte\n${t}`)).toBe(true)
    render(createElement(Tagged, { t, md: (input: string) => input }))
    expect(screen.getByText(/a93586/)).toBeTruthy()
    expect(screen.getByText(/rapport/)).toBeTruthy()
  })
})

describe("task-notification", () => {
  const notification =
    "<task-notification><task-id>a1</task-id><status>completed</status><summary>Agent fini</summary>" +
    "<result>Lancer :\n\n- `a &amp;&amp; b`</result>" +
    "<usage><subagent_tokens>581258</subagent_tokens><tool_uses>1</tool_uses><duration_ms>14651</duration_ms></usage></task-notification>"
  it("décode les entités, met le résultat en forme et résume l'usage", () => {
    render(createElement(Tagged, { t: notification, md: (text: string) => createElement("div", { "data-testid": "md" }, text) }))
    expect(screen.getAllByTestId("md").some((element) => element.textContent?.includes("a && b"))).toBe(true)
    expect(screen.queryByText(/<subagent_tokens>/)).toBeNull()
    expect(screen.getByText(/581258/)).toBeTruthy()
    expect(screen.getByText(/15 s/)).toBeTruthy()
  })
})
