// @vitest-environment jsdom
import { cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"
import { api, type PluginInst, type ProjectCfg } from "@/lib/api"
import { ProjectsTab } from "./ConfigProjects"

afterEach(() => {
  surAjoutRegle.mockClear()
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

const surAjoutRegle = vi.fn()

async function afficherProjetOuvert(projets: ProjectCfg[] = [projetAvecRegleGlobaleEtPartagee], pluginsInstalles: PluginInst[] = []) {
  vi.spyOn(api, "plugins").mockResolvedValue({ installed: pluginsInstalles })
  vi.spyOn(api, "configProjects").mockResolvedValue({ projects: projets } as Awaited<ReturnType<typeof api.configProjects>>)
  const { container } = render(<ProjectsTab home="/home/bob" version={0} gone={() => false} onAddDir={vi.fn()} onAddRule={surAjoutRegle} suggest={() => []} onEdit={vi.fn()} onTransfer={vi.fn()} onRemove={vi.fn()} />)
  fireEvent.click(await screen.findByRole("button", { name: /home\/bob\/p|~?\/p/ }))
  return { container }
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

  it("ajoute une règle dans le fichier perso par défaut", async () => {
    await afficherProjetOuvert()

    fireEvent.change(screen.getByLabelText("Nouvelle règle de permission"), { target: { value: " Bash(git status:*) " } })
    fireEvent.click(screen.getByRole("button", { name: /Ajouter$/ }))

    expect(surAjoutRegle).toHaveBeenCalledWith("l/-p", "allow", "Bash(git status:*)")
  })

  it("n'ajoute rien quand la règle est vide", async () => {
    await afficherProjetOuvert()

    expect(screen.getByRole("button", { name: /Ajouter$/ }).hasAttribute("disabled")).toBe(true)
  })

  it("liste les plugins installés pour ce projet seulement", async () => {
    const pluginsInstalles: PluginInst[] = [
      { id: "superpowers", version: "7.0.0", scope: "project", enabled: true, project: "/home/bob/p" },
      { id: "autre", version: "1.0.0", scope: "project", enabled: true, project: "/home/bob/q" },
    ]
    await afficherProjetOuvert([projetAvecRegleGlobaleEtPartagee], pluginsInstalles)

    expect(await screen.findByText("superpowers")).toBeTruthy()
    expect(screen.queryByText("autre")).toBeNull()
  })

  it("regroupe les règles par fichier, du global au perso", async () => {
    const projetTroisCouches: ProjectCfg = {
      ...projetAvecRegleGlobaleEtPartagee,
      perms: { allow: [{ rule: "Read", scope: "l" }, { rule: "Grep", scope: "u" }, { rule: "Glob", scope: "p" }] },
    }
    const { container } = await afficherProjetOuvert([projetTroisCouches])

    const couchesObtenues = Array.from(container.querySelectorAll("[data-layer]")).map((couche) => couche.getAttribute("data-layer"))
    expect(couchesObtenues).toEqual(["u", "p", "l"])
  })

  it("relie une règle aux hooks et aux skills du projet qui touchent son outil", async () => {
    const projetAvecLiens: ProjectCfg = {
      ...projetAvecRegleGlobaleEtPartagee,
      perms: { allow: [{ rule: "Bash(make:*)", scope: "p" }] },
      hooks: [{ scope: "p", file: "p/-p", event: "PreToolUse", matcher: "Bash", type: "command", command: "garde" }],
      skills: ["deploy"],
      skillTools: { deploy: "Bash(make:*)" },
    }
    await afficherProjetOuvert([projetAvecLiens])

    expect(screen.getByText(/1 hook\(s\) lié\(s\)/)).toBeTruthy()
    expect(screen.getByText(/skill : \/deploy/)).toBeTruthy()
  })
})
