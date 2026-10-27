// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { setLang } from "@/i18n"
import { afterEach, describe, expect, it, vi } from "vitest"
import * as teams from "@/lib/teams"
import { TeamPicker } from "./TeamPicker"

setLang("fr")
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
})

const ag = (ID: string, Name: string, Role: string): teams.Agent => ({
  ID,
  Name,
  Role,
  Access: "",
  Prompt: "",
  Model: "",
  Builtin: true,
  Base: "",
})
const chef = ag("chef", "Chef de projet", "chef de projet")
const backend = ag("backend", "Développement backend", "backend")
const testeur = ag("testeur", "Testeur", "testeur")
const catalog: teams.Catalog = {
  agents: [chef, testeur, backend],
  teams: [
    {
      ID: "feat",
      Name: "Fonctionnalité",
      Members: ["backend", "testeur"],
      Builtin: true,
      Base: "",
    },
    {
      ID: "bug",
      Name: "Correctif de bug",
      Members: ["testeur"],
      Builtin: true,
      Base: "",
    },
  ],
}
const mockCompose = (tooMany = false) =>
  vi.spyOn(teams, "teamsCompose").mockImplementation(async (ids) => ({
    ok: true,
    lead: chef,
    mates: ids.length ? [backend, testeur] : [],
    tooMany,
  }))
const click = async (e: HTMLElement) => void fireEvent.click(e)
const type = async (e: HTMLElement, v: string) =>
  void fireEvent.change(e, { target: { value: v } })
const pickDir = vi.fn(async () => "/work")
const setup = (onLaunch = vi.fn(async () => ({ ok: true }) as never)) => {
  render(<TeamPicker catalog={catalog} onLaunch={onLaunch} pickDir={pickDir} />)
  return onLaunch
}

describe("TeamPicker", () => {
  it("deux teams : chaque coéquipier une fois, compteur", async () => {
    mockCompose()
    setup()
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await click(screen.getByRole("checkbox", { name: /Correctif de bug/ }))
    await screen.findByText("2 team(s) · 2 coéquipier(s)")
    expect(screen.getAllByText("Testeur", { selector: "b" })).toHaveLength(1)
    expect(screen.getAllByText("Chef de projet", { selector: "b" })).toHaveLength(1)
  })
  it("masque la description qui répète le libellé, garde les autres", async () => {
    mockCompose()
    setup()
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await screen.findByText("Développement backend", { selector: "b" })
    expect(screen.queryByText("· testeur")).toBeNull()
    expect(screen.queryByText(/chef de projet/)).toBeNull()
    expect(screen.getByText("· backend")).toBeTruthy()
  })
  it("tooMany avertit sans bloquer Lancer", async () => {
    mockCompose(true)
    setup()
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await type(screen.getByRole("textbox"), "x")
    await click(screen.getByRole("button", { name: "Choisir" }))
    await screen.findByText(/Plus de 5 coéquipiers/)
    expect(
      (screen.getByRole("button", { name: "Lancer" }) as HTMLButtonElement)
        .disabled
    ).toBe(false)
  })
  it("Lancer désactivé sans team, objectif ou dossier", async () => {
    mockCompose()
    setup()
    const btn = () =>
      screen.getByRole("button", { name: "Lancer" }) as HTMLButtonElement
    expect(btn().disabled).toBe(true)
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await type(screen.getByRole("textbox"), "x")
    expect(btn().disabled).toBe(true)
    await click(screen.getByRole("button", { name: "Choisir" }))
    await waitFor(() => expect(btn().disabled).toBe(false))
  })
  it("Lancer appelle onLaunch avec ids, objectif, dossier", async () => {
    mockCompose()
    const onLaunch = setup()
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await type(screen.getByRole("textbox"), "fais ça")
    await click(screen.getByRole("button", { name: "Choisir" }))
    await click(await screen.findByRole("button", { name: "Lancer" }))
    expect(onLaunch).toHaveBeenCalledWith(["feat"], "fais ça", "/work")
  })
  it("affiche out et Conflicts en texte", async () => {
    mockCompose()
    const onLaunch = vi.fn(
      async () =>
        ({
          ok: false,
          out: "<b>boom</b>",
          run: {
            TeamName: "t",
            SessionID: "",
            Mates: [],
            Conflicts: ["backend.md"],
            TooMany: false,
          },
        }) as never
    )
    setup(onLaunch)
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await type(screen.getByRole("textbox"), "x")
    await click(screen.getByRole("button", { name: "Choisir" }))
    await click(await screen.findByRole("button", { name: "Lancer" }))
    expect(await screen.findByText("<b>boom</b>")).toBeTruthy()
    expect(screen.getByText("backend.md")).toBeTruthy()
  })
  it("bascule au clavier (espace)", async () => {
    mockCompose()
    setup()
    const cb = screen.getByRole("checkbox", { name: /Fonctionnalité/ })
    expect(cb.getAttribute("tabindex")).toBe("0")
    fireEvent.keyDown(cb, { key: " " })
    expect(cb.getAttribute("aria-checked")).toBe("true")
    fireEvent.keyDown(cb, { key: "Enter" })
    expect(cb.getAttribute("aria-checked")).toBe("false")
  })
  it("team retirée du catalogue : sélection purgée, composition vidée, Lancer désactivé", async () => {
    mockCompose()
    const onLaunch = vi.fn(async () => ({ ok: true }) as never)
    const { rerender } = render(
      <TeamPicker catalog={catalog} onLaunch={onLaunch} pickDir={pickDir} />
    )
    await click(screen.getByRole("checkbox", { name: /Fonctionnalité/ }))
    await screen.findByText("1 team(s) · 2 coéquipier(s)")
    await type(screen.getByRole("textbox"), "x")
    await click(screen.getByRole("button", { name: "Choisir" }))
    await waitFor(() =>
      expect(
        (screen.getByRole("button", { name: "Lancer" }) as HTMLButtonElement)
          .disabled
      ).toBe(false)
    )
    rerender(
      <TeamPicker
        catalog={{ ...catalog, teams: [catalog.teams[1]] }}
        onLaunch={onLaunch}
        pickDir={pickDir}
      />
    )
    await screen.findByText("0 team(s) · 0 coéquipier(s)")
    expect(screen.queryByText("Développement backend", { selector: "b" })).toBeNull()
    expect(
      (screen.getByRole("button", { name: "Lancer" }) as HTMLButtonElement)
        .disabled
    ).toBe(true)
  })
})
