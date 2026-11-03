import { describe, expect, it } from "vitest"
import type { ProjectCfg } from "@/lib/api"
import { overridingKind, projectsAddingDirectory } from "./permDependencies"

const projectWith = (dir: string, project: string, additionalDirectories: string[]): ProjectCfg => ({
  dir,
  project,
  files: [],
  perms: { additionalDirectories: additionalDirectories.map((rule) => ({ rule, scope: "l" })) },
  hooks: [],
  mcp: [],
  rules: [],
  skills: [],
  docs: [],
  specific: 0,
})

describe("overridingKind", () => {
  const perms = {
    allow: [{ rule: "Bash(rm:*)", scope: "p" }, { rule: "Bash(ls:*)", scope: "u" }, { rule: "Bash(cat:*)", scope: "p" }],
    ask: [{ rule: "Bash(cat:*)", scope: "l" }],
    deny: [{ rule: "Bash(rm:*)", scope: "u" }],
  }

  it("signale une règle autorisée neutralisée par un refus d'une autre couche", () => {
    expect(overridingKind(perms, "allow", "Bash(rm:*)")).toBe("deny")
  })

  it("préfère le refus à la confirmation quand les deux existent", () => {
    const permsAvecLesDeux = { ...perms, deny: [...perms.deny, { rule: "Bash(cat:*)", scope: "u" }] }
    expect(overridingKind(permsAvecLesDeux, "allow", "Bash(cat:*)")).toBe("deny")
  })

  it("signale une règle autorisée neutralisée par une confirmation", () => {
    expect(overridingKind(perms, "allow", "Bash(cat:*)")).toBe("ask")
  })

  it("ne signale rien pour une règle sans équivalent plus fort ni pour un refus", () => {
    expect(overridingKind(perms, "allow", "Bash(ls:*)")).toBeUndefined()
    expect(overridingKind(perms, "deny", "Bash(rm:*)")).toBeUndefined()
  })
})

describe("projectsAddingDirectory", () => {
  const cible = projectWith("-c", "/home/bob/lib", [])
  const ajouteParTilde = projectWith("-a", "/home/bob/app", ["~/lib"])
  const ajouteParChemin = projectWith("-b", "/home/bob/api", ["/home/bob/lib"])
  const sansLien = projectWith("-d", "/home/bob/autre", ["/home/bob/x"])

  it("liste les projets qui ajoutent le dossier, en chemin absolu ou avec ~", () => {
    const projetsObtenus = projectsAddingDirectory([cible, ajouteParTilde, ajouteParChemin, sansLien], cible, "/home/bob").map((projet) => projet.dir)
    expect(projetsObtenus).toEqual(["-a", "-b"])
  })

  it("ne compte pas le projet lui-même", () => {
    const seAjouteLuiMeme = projectWith("-c", "/home/bob/lib", ["/home/bob/lib"])
    expect(projectsAddingDirectory([seAjouteLuiMeme], seAjouteLuiMeme, "/home/bob")).toEqual([])
  })
})
