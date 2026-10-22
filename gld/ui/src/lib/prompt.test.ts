import { describe, expect, it } from "vitest"
import { parsePrompt } from "./prompt"

const screens = {
  permission: `
 Bash command

   rm -rf build
   Remove the build folder

 Do you want to proceed?
 ❯ 1. Yes
   2. Yes, and don't ask again for rm commands
   3. No, and tell Claude what to do differently (esc)

 Esc to cancel · Tab to amend`,
  boxed: `
╭──────────────────────────────────╮
│ Do you want to make this edit?   │
│ ❯ 1. Yes                         │
│   2. No                          │
╰──────────────────────────────────╯
 Esc to cancel`,
  ask: `
 Côté guru, qui termine le HTTPS ?

 ❯ 1. Guru termine le TLS (Recommandé)
      Certificats gérés sur guru
   2. Un reverse proxy dédié
   3. Type something.
 ────────────────────────────────
   4. Chat about this

 Enter to select · ↑/↓ to navigate · Esc to cancel`,
  review: `
 Review your answers

 Ready to submit your answers?

 ❯ 1. Submit answers
   2. Cancel`,
  select: `
 Select a model

 ❯ Opus
   Sonnet
   Haiku

 Enter to confirm · Esc to exit`,
  busy: `
 ❯ 1. un exemple dans la réponse
   2. autre ligne

 ✻ Réfléchit… (esc to interrupt)`,
  prose: `
Voici une liste :
1. premier
2. second

Voilà.
 ❯ 
 ⏵⏵ auto mode on`,
}

describe("parsePrompt", () => {
  it("permission : question et trois options numérotées, la dernière à réponse libre", () => {
    const p = parsePrompt(screens.permission)!
    expect(p.question).toBe("Do you want to proceed?")
    expect(p.options.map((o) => o.keys)).toEqual([["1"], ["2"], ["3"]])
    expect(p.options[2].text).toBe(true)
  })
  it("dialogue encadré", () => {
    const p = parsePrompt(screens.boxed)!
    expect(p.options).toHaveLength(2)
    expect(p.options[0].label).toBe("1. Yes")
  })
  it("question à choix : « Type something » et « Chat about this » sont des réponses libres", () => {
    const p = parsePrompt(screens.ask)!
    expect(p.question).toMatch(/HTTPS/)
    expect(p.options.filter((o) => o.text).map((o) => o.keys[0])).toEqual(["3", "4"])
  })
  it("écran de relecture des réponses", () => {
    const p = parsePrompt(screens.review)!
    expect(p.question).toBe("Ready to submit your answers?")
    expect(p.options.map((o) => o.keys[0])).toEqual(["1", "2"])
  })
  it("liste à sélection : flèches puis Entrée", () => {
    const p = parsePrompt(screens.select)!
    expect(p.options.map((o) => o.keys)).toEqual([["Enter"], ["Down", "Enter"], ["Down", "Down", "Enter"]])
  })
  it("rien en pleine réflexion ni sur du texte ordinaire", () => {
    expect(parsePrompt(screens.busy)).toBeNull()
    expect(parsePrompt(screens.prose)).toBeNull()
  })
})
