// @vitest-environment jsdom
import {
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react"
import { setLang } from "@/i18n"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"
import * as teams from "@/lib/teams"
import { AgentEditor } from "./AgentEditor"
import { TeamEditor } from "./TeamEditor"
import { TeamsPage } from "./TeamsPage"

const rights = vi.hoisted(() => ({ cfg: true }))
vi.mock("@/lib/auth", async (orig) => ({
  ...(await orig<typeof import("@/lib/auth")>()),
  useCan: () => (r: string) => (r === "config_write" ? rights.cfg : true),
}))
vi.mock("@/lib/teams")
vi.mock("@/components/actions", () => ({
  useActions: () => ({ pickDir: vi.fn() }),
}))

setLang("fr")
const ag = (
  ID: string,
  Name: string,
  o: Partial<teams.Agent> = {}
): teams.Agent => ({
  ID,
  Name,
  Role: "r",
  Access: "write",
  Prompt: "",
  Model: "",
  Builtin: true,
  Base: "",
  ...o,
})
const atlas = ag("atlas", "Atlas", { Access: "lead" })
const ada = ag("ada", "Ada")
const perso = ag("p1", "Zoé", { Builtin: false })
const bteam: teams.Team = {
  ID: "feat",
  Name: "Fonctionnalité",
  Members: ["ada"],
  Builtin: true,
  Base: "",
}
const pteam: teams.Team = {
  ID: "t1",
  Name: "Mienne",
  Members: ["p1"],
  Builtin: false,
  Base: "",
}
const cat: teams.Catalog = {
  agents: [atlas, ada, perso],
  teams: [bteam, pteam],
}
const name = () => screen.getByLabelText("Nom") as HTMLInputElement
const type = (e: HTMLElement, v: string) =>
  fireEvent.change(e, { target: { value: v } })
const saveBtn = () =>
  screen.getByRole("button", { name: "Enregistrer" }) as HTMLButtonElement

beforeEach(() => {
  rights.cfg = true
  vi.mocked(teams.teamsCatalog).mockResolvedValue({ ok: true, ...cat })
  vi.mocked(teams.teamsList).mockResolvedValue({ ok: true, teams: [] })
  vi.mocked(teams.teamsAgentSave).mockResolvedValue({ ok: true, agent: ada })
  vi.mocked(teams.teamsAgentRm).mockResolvedValue({ ok: true })
  vi.mocked(teams.teamsTeamSave).mockResolvedValue({ ok: true, team: bteam })
  vi.mocked(teams.teamsTeamRm).mockResolvedValue({ ok: true })
})
afterEach(() => {
  cleanup()
  vi.resetAllMocks()
})

describe("AgentEditor", () => {
  it("agent fourni : nom différent, id de l'original, pas de suppression", async () => {
    const onSaved = vi.fn()
    render(<AgentEditor agent={ada} onClose={vi.fn()} onSaved={onSaved} />)
    expect(name().value).not.toBe("Ada")
    expect(screen.getByText("Copie de Ada")).toBeTruthy()
    expect(screen.queryByRole("button", { name: "Supprimer" })).toBeNull()
    fireEvent.click(saveBtn())
    await waitFor(() => expect(teams.teamsAgentSave).toHaveBeenCalled())
    expect(vi.mocked(teams.teamsAgentSave).mock.calls[0][0]).toMatchObject({
      ID: "ada",
      Name: name().value,
    })
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
  })
  it("agent perso : sauvegarde sous son id, suppression confirmée", async () => {
    render(<AgentEditor agent={perso} onClose={vi.fn()} onSaved={vi.fn()} />)
    expect(name().value).toBe("Zoé")
    fireEvent.click(saveBtn())
    await waitFor(() =>
      expect(vi.mocked(teams.teamsAgentSave).mock.calls[0][0]).toMatchObject({
        ID: "p1",
      })
    )
    fireEvent.click(screen.getByRole("button", { name: "Supprimer" }))
    expect(teams.teamsAgentRm).not.toHaveBeenCalled()
    fireEvent.click(
      screen.getAllByRole("button", { name: "Supprimer" }).at(-1)!
    )
    await waitFor(() => expect(teams.teamsAgentRm).toHaveBeenCalledWith("p1"))
  })
  it("suppression refusée : texte du serveur affiché", async () => {
    vi.mocked(teams.teamsAgentRm).mockResolvedValue({
      ok: false,
      out: "utilisé par la team Mienne",
    })
    render(<AgentEditor agent={perso} onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "Supprimer" }))
    fireEvent.click(
      screen.getAllByRole("button", { name: "Supprimer" }).at(-1)!
    )
    expect(await screen.findByText("utilisé par la team Mienne")).toBeTruthy()
  })
  it("accès : read et write seulement", () => {
    render(<AgentEditor agent={null} onClose={vi.fn()} onSaved={vi.fn()} />)
    const opts = Array.from(
      (screen.getByLabelText("Accès") as HTMLSelectElement).options
    ).map((o) => o.value)
    expect(opts).toEqual(["read", "write"])
  })
  it("prompt compté en octets UTF-8 : 4001 x é = 8002 octets bloque", () => {
    render(<AgentEditor agent={null} onClose={vi.fn()} onSaved={vi.fn()} />)
    type(name(), "N")
    type(screen.getByLabelText("Rôle"), "R")
    expect(saveBtn().disabled).toBe(false)
    type(screen.getByLabelText("Prompt"), "é".repeat(4001))
    expect(screen.getByText("8002 / 8000")).toBeTruthy()
    expect(saveBtn().disabled).toBe(true)
    type(screen.getByLabelText("Prompt"), "é".repeat(4000))
    expect(saveBtn().disabled).toBe(false)
  })
  it("nom ou rôle vide : Enregistrer désactivé", () => {
    render(<AgentEditor agent={null} onClose={vi.fn()} onSaved={vi.fn()} />)
    expect(saveBtn().disabled).toBe(true)
  })
  it("erreur serveur à l'enregistrement en texte", async () => {
    vi.mocked(teams.teamsAgentSave).mockResolvedValue({
      ok: false,
      out: "<b>doublon</b>",
    })
    render(<AgentEditor agent={ada} onClose={vi.fn()} onSaved={vi.fn()} />)
    fireEvent.click(saveBtn())
    expect(await screen.findByText("<b>doublon</b>")).toBeTruthy()
  })
})

describe("TeamEditor", () => {
  it("exclut le chef, exige un membre, copie d'une team fournie", async () => {
    const onSaved = vi.fn()
    render(
      <TeamEditor
        team={bteam}
        agents={cat.agents}
        onClose={vi.fn()}
        onSaved={onSaved}
      />
    )
    expect(screen.queryByRole("checkbox", { name: /Atlas/ })).toBeNull()
    expect(screen.queryByRole("button", { name: "Supprimer" })).toBeNull()
    expect(name().value).not.toBe("Fonctionnalité")
    fireEvent.click(screen.getByRole("checkbox", { name: /Ada/ }))
    expect(saveBtn().disabled).toBe(true)
    fireEvent.click(screen.getByRole("checkbox", { name: /Ada/ }))
    fireEvent.click(saveBtn())
    await waitFor(() => expect(teams.teamsTeamSave).toHaveBeenCalled())
    expect(vi.mocked(teams.teamsTeamSave).mock.calls[0][0]).toMatchObject({
      ID: "feat",
      Members: ["ada"],
    })
    await waitFor(() => expect(onSaved).toHaveBeenCalled())
  })
  it("team perso : suppression confirmée", async () => {
    render(
      <TeamEditor
        team={pteam}
        agents={cat.agents}
        onClose={vi.fn()}
        onSaved={vi.fn()}
      />
    )
    fireEvent.click(screen.getByRole("button", { name: "Supprimer" }))
    fireEvent.click(
      screen.getAllByRole("button", { name: "Supprimer" }).at(-1)!
    )
    await waitFor(() => expect(teams.teamsTeamRm).toHaveBeenCalledWith("t1"))
  })
})

describe("TeamsPage", () => {
  it("sans config_write : aucun contrôle d'édition", async () => {
    rights.cfg = false
    render(<TeamsPage home="/h" />)
    await screen.findByText("Fonctionnalité")
    expect(
      screen.queryByRole("button", {
        name: /Modifier|Nouvelle team|Nouvel agent/,
      })
    ).toBeNull()
    expect(screen.queryByText("perso")).toBeNull()
  })
  it("Modifier ne bascule pas la carte ; après enregistrement le catalogue est rechargé", async () => {
    render(<TeamsPage home="/h" />)
    await screen.findByText("Fonctionnalité")
    const card = screen.getByRole("checkbox", { name: /Fonctionnalité/ })
    fireEvent.click(
      screen.getByRole("button", { name: "Modifier Fonctionnalité" })
    )
    expect(card.getAttribute("aria-checked")).toBe("false")
    await screen.findByText("Copie de Fonctionnalité")
    const n = vi.mocked(teams.teamsCatalog).mock.calls.length
    fireEvent.click(saveBtn())
    await waitFor(() =>
      expect(vi.mocked(teams.teamsCatalog).mock.calls.length).toBe(n + 1)
    )
  })
  it("liste des agents, Nouvel agent, agents chef non modifiables", async () => {
    render(<TeamsPage home="/h" />)
    await screen.findByText("perso")
    expect(screen.queryByRole("button", { name: "Modifier Atlas" })).toBeNull()
    expect(screen.getByRole("button", { name: "Nouvelle team" })).toBeTruthy()
    fireEvent.click(screen.getByRole("button", { name: "Nouvel agent" }))
    expect(await screen.findByLabelText("Nom")).toBeTruthy()
  })
})
