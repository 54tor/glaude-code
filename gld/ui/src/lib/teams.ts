import { ApiError, http, qs, seg } from "@/lib/http"
import { authFetch } from "@/lib/auth"
import { netError, readResponse, v1 } from "@/lib/apiError"

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
  PermissionTruncated: boolean
  Unknown: string[]
}
// Échec d'un lancement : conflicts vient du corps 409 (agents perso homonymes).
export class LaunchError extends ApiError {
  run?: Run
}

export const teamsCatalog = () => http.get<Catalog>("/teams/catalog")
export const teamsCompose = (teamIds: string[]) => http.post<Composition>("/teams/compositions", { teamIds })
async function readConflictRun(response: Response): Promise<Run | undefined> {
  if (response.ok) return undefined
  const errorBody = (await response.clone().json().catch(() => null)) as { run?: Run } | null
  return errorBody?.run
}
export async function teamsLaunch(teamIds: string[], goal: string, dir: string): Promise<Run> {
  let response: Response
  try {
    response = await authFetch(v1(`/teams/${seg(teamIds[0] ?? "")}/runs`), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ teamIds, goal, dir }),
    })
  } catch (fetchError) {
    throw netError(fetchError)
  }
  const conflictRun = await readConflictRun(response)
  try {
    return await readResponse<Run>(response)
  } catch (responseError) {
    if (!(responseError instanceof ApiError)) throw responseError
    const launchError = new LaunchError(responseError.status, responseError.code, responseError.message)
    launchError.run = conflictRun
    throw launchError
  }
}
export const teamsList = () => http.get<{ teams: TeamBrief[] }>("/teams")
export const teamGet = (name: string) => http.get<TeamView>(`/teams/${seg(name)}/run`)
export const teamSend = (name: string, to: string, text: string) => http.post<{ queued: boolean }>(`/teams/${seg(name)}/run/messages`, { to, text })
export const teamKey = (name: string, agent: string, key: string, expect = "") => http.post(`/teams/${seg(name)}/run/keystrokes`, { agent, key, expect })
export const teamClose = (name: string) => http.del(`/teams/${seg(name)}/run`)
export const teamScreen = (name: string, agent: string) => http.get<{ text: string }>(`/teams/${seg(name)}/run/screen${qs({ agent })}`)
export const teamsAgentSave = (agent: Partial<Agent>) => http.put<Agent>(`/agents/${seg(agent.ID || "new")}`, agent)
export const teamsAgentRm = (id: string) => http.del(`/agents/${seg(id)}`)
export const teamsTeamSave = (team: Partial<Team>) => http.put<Team>(`/teams/${seg(team.ID || "new")}`, team)
export const teamsTeamRm = (id: string) => http.del(`/teams/${seg(id)}`)
