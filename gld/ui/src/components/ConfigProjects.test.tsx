// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type ProjectCfg } from "@/lib/api"
import { ProjectsTab } from "./ConfigProjects"

afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const projetAvecRegleGlobaleEtPartagee: ProjectCfg = {
  dir: "-p",
  project: "/home/bob/p",
  files: [],
  perms: {
    allow: [
      { rule: "Bash(ls:*)", scope: "u" },
      { rule: "Bash(make:*)", scope: "p" },
    ],
  },
  hooks: [],
  mcp: [],
  rules: [],
  skills: [],
  docs: [],
  specific: 1,
}

async function afficherProjetOuvert(projets: ProjectCfg[] = [projetAvecRegleGlobaleEtPartagee]) {
  vi.spyOn(api, "configProjects").mockResolvedValue({ projects: projets } as Awaited<ReturnType<typeof api.configProjects>>)
  render(<ProjectsTab home="/home/bob" version={0} gone={() => false} onAddDir={vi.fn()} suggest={() => []} onEdit={vi.fn()} onTransfer={vi.fn()} onRemove={vi.fn()} />)
  fireEvent.click(await screen.findByRole("button", { name: /home\/bob\/p|~?\/p/ }))
}

describe("ProjectsTab : règles globales", () => {
  it("affiche la règle globale en lecture seule, sans bouton de suppression", async () => {
    await afficherProjetOuvert()

    expect(screen.getByText("Bash(ls:*)")).toBeTruthy()
    expect(screen.getByText("global")).toBeTruthy()
    const boutonsSuppressionObtenus = screen.getAllByRole("button", { name: /Bash\(/ })
    const reglesSupprimablesObtenues = boutonsSuppressionObtenus.map((bouton) => bouton.getAttribute("aria-label"))
    expect(reglesSupprimablesObtenues).toHaveLength(1)
    expect(reglesSupprimablesObtenues[0]).toContain("Bash(make:*)")
  })

  it("ne compte pas la règle globale dans le badge du projet", async () => {
    await afficherProjetOuvert()

    const badgeObtenu = screen.getByText(/règles de permission/)
    expect(badgeObtenu.textContent).toContain("1")
  })

  it("signale une règle autorisée neutralisée par un refus", async () => {
    const projetAvecConflit: ProjectCfg = { ...projetAvecRegleGlobaleEtPartagee, perms: { allow: [{ rule: "Bash(rm:*)", scope: "p" }], deny: [{ rule: "Bash(rm:*)", scope: "u" }] } }
    await afficherProjetOuvert([projetAvecConflit])

    expect(screen.getAllByText(/neutralisée/)).toHaveLength(1)
  })

  it("indique les projets qui ajoutent ce dossier comme dossier supplémentaire", async () => {
    const projetAjoutant: ProjectCfg = { ...projetAvecRegleGlobaleEtPartagee, dir: "-a", project: "/home/bob/app", perms: { additionalDirectories: [{ rule: "/home/bob/p", scope: "l" }] } }
    await afficherProjetOuvert([projetAvecRegleGlobaleEtPartagee, projetAjoutant])

    expect(screen.getByText(/Ajouté comme dossier supplémentaire par/).textContent).toContain("app")
  })
})
