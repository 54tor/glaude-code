import { describe, expect, it } from "vitest"
import { formatRaw, parseRaw } from "./permLint"

const msgs = (t: string, level?: string) => parseRaw(t).lint.filter((l) => !level || l.level === level).map((l) => `${l.line}:${l.level}`)

describe("permLint", () => {
  it("aller-retour texte", () => {
    const p = { allow: ["Bash(git status:*)", "mcp__x"], ask: [], deny: ["Bash(rm:*)"], additionalDirectories: ["~/Projets/x", "../autre"] }
    const r = parseRaw(formatRaw(p))
    expect(r.perms).toEqual(p)
    expect(r.lint).toEqual([])
  })
  it("dossiers supplémentaires : chemins libres, doublon signalé", () => {
    expect(parseRaw("[additionalDirectories]\n~/a b/c\n../x\n").perms.additionalDirectories).toEqual(["~/a b/c", "../x"])
    expect(msgs("[additionalDirectories]\n~/a\n~/a", "warn")).toEqual(["3:warn"])
    expect(msgs("[additionalDirectories]\n" + "a".repeat(501), "error")).toEqual(["2:error"])
  })
  it("ignore commentaires et lignes vides", () => {
    expect(parseRaw("# note\n\n[allow]\n  Read(~/x/**)  \n").perms.allow).toEqual(["Read(~/x/**)"])
  })
  it("erreurs bloquantes", () => {
    expect(msgs("Bash(ls:*)", "error")).toEqual(["1:error"]) // hors section
    expect(msgs("[autre]", "error")).toEqual(["1:error"])
    expect(msgs("[allow]\nBash(ls", "error")).toEqual(["2:error"])
    expect(msgs("[allow]\nBash(git:* foo)", "error")).toEqual(["2:error"])
    expect(msgs("[allow]\nBash(" + "a".repeat(300) + ")", "error")).toEqual(["2:error"])
  })
  it("avertissements", () => {
    expect(msgs("[allow]\nBsh(ls:*)", "warn")).toEqual(["2:warn"]) // outil inconnu
    expect(msgs("[allow]\nBash", "warn")).toEqual(["2:warn"])
    expect(msgs("[allow]\nBash()", "warn")).toEqual(["2:warn"])
    expect(msgs("[allow]\nRead(a)\nRead(a)", "warn")).toEqual(["3:warn"]) // doublon
    expect(msgs("[allow]\nRead(a)\n[deny]\nRead(a)", "warn")).toEqual(["4:warn"]) // conflit
    expect(msgs("[allow]\nmcp__srv__outil\nWebFetch(domain:x.com)", "warn")).toEqual([])
  })
})
