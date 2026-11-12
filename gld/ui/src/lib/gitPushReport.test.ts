import { describe, expect, it } from "vitest"
import { buildGitPushReport } from "./gitPushReport"

const baseRun = { dir: "/depot", remote: "origin", branch: "main" }

describe("buildGitPushReport", () => {
  it("contient la branche, le remote, le code de retour et la sortie du hook", () => {
    const report = buildGitPushReport({ ...baseRun, exitCode: 1 }, "hook: tests en échec")

    expect(report).toContain("main")
    expect(report).toContain("origin")
    expect(report).toContain("/depot")
    expect(report).toContain("1")
    expect(report).toContain("hook: tests en échec")
  })

  it("ne garde que les dernières lignes d'une très longue sortie", () => {
    const longOutput = Array.from({ length: 500 }, (_, lineIndex) => `ligne ${lineIndex}`).join("\n")

    const report = buildGitPushReport({ ...baseRun, exitCode: 1 }, longOutput)

    expect(report).toContain("ligne 499")
    expect(report).not.toContain("ligne 0\n")
  })

  it("rapporte le message d'erreur quand il n'y a pas de code de retour", () => {
    const report = buildGitPushReport({ ...baseRun, errorMessage: "réseau coupé" }, "")

    expect(report).toContain("réseau coupé")
  })
})
