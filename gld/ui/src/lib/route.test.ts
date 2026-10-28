import fc from "fast-check"
import { describe, expect, it } from "vitest"
import { href, parse, type Route } from "./route"

const seg = fc.string({ minLength: 1 }).filter((s) => s !== "." && s !== "..")
const key = fc
  .array(seg, { minLength: 1, maxLength: 4 })
  .map((a) => a.join("/"))
const route: fc.Arbitrary<Route> = fc.oneof(
  fc.constant({ kind: "recent" as const }),
  fc.constant({ kind: "all" as const }),
  fc.constant({ kind: "active" as const }),
  fc.constant({ kind: "auto" as const }),
  fc.constant({ kind: "tabs" as const }),
  fc.record({
    kind: fc.constant("group" as const),
    name: fc.string({ minLength: 1 }),
    key,
  }),
  fc.record({ kind: fc.constant("project" as const), name: fc.string() }),
  fc.record({ kind: fc.constant("workspace" as const), dir: fc.string({ minLength: 1 }) }),
  fc.record({ kind: fc.constant("projectConfig" as const), dir: fc.string({ minLength: 1 }) }),
  fc.record({ kind: fc.constant("dir" as const), key }),
  fc.record({ kind: fc.constant("config" as const), tab: fc.string() }),
  fc.record({
    kind: fc.constant("skills" as const),
    key: fc.oneof(fc.constant(""), key),
  }),
  fc.record({
    kind: fc.constant("memory" as const),
    key: fc.oneof(fc.constant(""), key),
  }),
  fc.record({
    kind: fc.constant("snippets" as const),
    path: fc.oneof(fc.constant(""), key),
  })
)

describe("route", () => {
  it("parse(href(r)) redonne r, quels que soient les caractères (/, %, espaces, accents)", () => {
    fc.assert(
      fc.property(route, (r) => {
        expect(parse(href(r))).toEqual(r)
      }),
      { seed: 42, numRuns: 2000 }
    )
  })
  it("un chemin inconnu ou mal encodé ramène à l'accueil", () => {
    for (const loc of [
      "/",
      "/inconnu",
      "/dossier/%E0%A4%A",
      "/groupe/sans-cle",
      "/groupe//k",
      "/projet/%",
    ]) {
      expect(parse(loc)).toEqual({ kind: "recent" })
    }
  })
  it("espace de travail : aller-retour et encodage invalide", () => {
    expect(href({ kind: "workspace", dir: "/home/moi/p" })).toBe("/espace/%2Fhome%2Fmoi%2Fp")
    expect(parse("/espace/%2Fhome%2Fmoi%2Fp")).toEqual({ kind: "workspace", dir: "/home/moi/p" })
    expect(parse("/espace/%E0%A4%A")).toEqual({ kind: "recent" })
  })
  it("ne lève jamais d'exception", () => {
    fc.assert(
      fc.property(fc.string(), (s) => {
        parse(s)
        parse("/" + s)
      }),
      { seed: 7, numRuns: 2000 }
    )
  })
  it("chemins attendus", () => {
    expect(href({ kind: "dir", key: "~/Projets/a b" })).toBe(
      "/dossier/~/Projets/a%20b"
    )
    expect(href({ kind: "config", tab: "" })).toBe("/configuration")
    expect(href({ kind: "group", name: "a/b", key: "~/x" })).toBe(
      "/groupe/a%2Fb/~/x"
    )
  })
})
