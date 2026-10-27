import { i18n } from "@/i18n"
import { authFetch } from "@/lib/auth"

// Équipes d'agents (gld/routes_teams.go). Les structs Go n'ont pas de balises json : les champs gardent leur majuscule.
export type Agent = { ID: string; Name: string; Role: string; Access: string; Prompt: string; Model: string; Builtin: boolean; Base: string }
export type Team = { ID: string; Name: string; Members: string[]; Builtin: boolean; Base: string }
export type Catalog = { agents: Agent[]; teams: Team[] }
export type Composition = { lead: Agent; mates: Agent[]; tooMany: boolean }
export type Run = { TeamName: string; SessionID: string; Mates: { ID: string; Name: string }[]; Conflicts: string[]; TooMany: boolean }
export type TeamBrief = { Name: string; LeadSessionID: string; Live: boolean; Members: number }
// Name : nom d'exécution brut (clé des actions) ; Display : à afficher.
export type AgentView = { Name: string; Display: string; AgentID: string; Role: string; Pane: string; State: string; Lead: boolean }
export type TeamTask = { ID: string; Title: string; Status: string }
export type InboxMsg = { ID: string; From: string; To: string; Text: string; Summary: string; Kind: string; At: number; Read: boolean }
export type DirectMsg = { Author: string; To: string; Text: string; At: number }
export type TeamView = {
  Name: string
  LeadSessionID: string
  Live: boolean
  Agents: AgentView[]
  Tasks: TeamTask[]
  Messages: InboxMsg[]
  Direct: DirectMsg[]
  Queued: number
  Permission: string
  Unknown: string[]
}
// ok=false : out porte le message du serveur (ou l'erreur réseau).
export type Res<T = object> = ({ ok: true } & T) | { ok: false; out: string; run?: Run }

async function call<T = object>(url: string, body?: unknown): Promise<Res<T>> {
  try {
    const r = await authFetch(url, body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
    const j = (await r.json()) as Res<T>
    return j.ok ? j : { ...j, ok: false, out: j.out || i18n.t("shell.api.error", { msg: String(r.status) }) }
  } catch (e) {
    return { ok: false, out: i18n.t("shell.api.error", { msg: e instanceof Error ? e.message : String(e) }) }
  }
}
const q = encodeURIComponent

export const teamsCatalog = () => call<Catalog>("/teams-catalog")
export const teamsCompose = (teamIds: string[]) => call<Composition>("/teams-compose", { teamIds })
export const teamsLaunch = (teamIds: string[], goal: string, dir: string) => call<{ run: Run }>("/teams-launch", { teamIds, goal, dir })
export const teamsList = () => call<{ teams: TeamBrief[] }>("/teams")
export const teamGet = (name: string) => call<{ view: TeamView }>(`/team?name=${q(name)}`)
export const teamSend = (name: string, to: string, text: string) => call<{ queued: boolean }>("/team-send", { name, to, text })
export const teamKey = (name: string, agent: string, key: string, expect = "") => call("/team-key", { name, agent, key, expect })
export const teamClose = (name: string) => call("/team-close", { name })
export const teamScreen = (name: string, agent: string) => call<{ text: string }>(`/team-screen?name=${q(name)}&agent=${q(agent)}`)
export const teamsAgentSave = (a: Partial<Agent>) => call<{ agent: Agent }>("/teams-agent-save", a)
export const teamsAgentRm = (id: string) => call("/teams-agent-rm", { id })
export const teamsTeamSave = (t: Partial<Team>) => call<{ team: Team }>("/teams-team-save", t)
export const teamsTeamRm = (id: string) => call("/teams-team-rm", { id })
