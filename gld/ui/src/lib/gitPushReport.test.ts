import { describe, expect, it } from "vitest"
import { buildGitPushReport } from "./gitPushReport"

const baseRun = { dir: "/depot", remote: "origin", branch: "main" }

describe("buildGitPushReport", () => {
  it("contient la branche, le remote, le code de retour et la mention du fichier joint", () => {
    const report = buildGitPushReport(
      { ...baseRun, exitCode: 1 },
      "@~/uploads/push-1.txt"
    )

    expect(report).toContain("main")
    expect(report).toContain("origin")
    expect(report).toContain("/depot")
    expect(report).toContain("1")
    expect(report).toContain("@~/uploads/push-1.txt")
  })

  it("demande de corriger, commiter puis repousser la même branche", () => {
    const report = buildGitPushReport({ ...baseRun, exitCode: 1 })

    expect(report).toMatch(/commit/i)
    expect(report).toContain("main")
  })

  it("rapporte le message d'erreur quand il n'y a pas de code de retour", () => {
    const report = buildGitPushReport({
      ...baseRun,
      errorMessage: "réseau coupé",
    })

    expect(report).toContain("réseau coupé")
  })
})
