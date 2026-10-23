import { i18n } from "@/i18n"

// Accès aux routes du serveur Go (gld/main.go).

// Fournisseurs d'agents de code (provider.go) : ce que chacun sait faire ; l'interface masque le reste.
export type Caps = { hotCache: boolean; keepWarm: boolean; config: boolean; memory: boolean; tuiPrompts: boolean; resume: boolean }
export type ProviderInfo = { id: string; name: string; caps: Caps }
export type Session = { provider?: string; id: string; dir: string; title: string; named: boolean; cwd: string; mtime: number; kb: number; tok: Tokens }
export type Tokens = { in: number; out: number; cr: number; cw: number }
export type Group = { project: string; missing: boolean; sessions: Session[] }
export type Project = { name: string; dirs: string[] } // ensemble nommé de dossiers de travail
export type VGroup = { name: string; project: string; ids: string[]; fav?: boolean }
export type Data = { home: string; root: string; groups: Group[]; dirs: string[]; live: string[]; waiting: string[]; asking?: string[]; vgroups: VGroup[]; favs: string[]; tags: Record<string, string[]>; projects: Project[]; providers: ProviderInfo[]; lang?: string }
// Snippet = commande Claude Code. key : u/<chemin> (personnelle) ou p/<dossier>/<chemin> (d'un projet).
export type Snippet = { key: string; scope: string; project: string; path: string; command: string; description: string; kb: number; mtime: number }
export type SnipScope = { dir: string; project: string }
export type MemFile = {
  key: string
  name: string
  kb: number
  mtime: number
  path: string
  type?: string // metadata.type du front matter
  paths?: string[] // règle scopée : globs du champ paths:
  tokens: number // estimation calibrée, ou compte réel si exact
  exact?: boolean // tokens est un compte réel (Haiku)
  always?: boolean // chargé à chaque session
  off?: boolean // règle désactivée (.md.off)
  warn?: string // front matter invalide
}
export type LoadEvent = { at: number; session: string; file: string; type?: string; reason?: string; globs?: string[]; trigger?: string }
export type FileLoads = { sessions: number; last: number }
// Skills (skills.go)
export type Skill = {
  key: string
  name: string
  dir: string
  scope: "user" | "project" | "plugin" | "store"
  origin: "store" | "plugin" | "install" | "custom" // d'où vient le skill
  source?: string
  project?: string
  plugin?: string
  readOnly?: boolean
  description: string
  chars: number
  manual?: boolean
  hidden?: boolean
  tools?: string
  files: number
  kb: number
  mtime: number
  path: string
  warn?: string
}
export type SkillFile = { rel: string; kb: number }

// Ce que chaque projet configure en propre (configproj.go)
export type ProjectCfg = {
  dir: string
  project: string
  files: { id: string; label: string; rows: { path: string; value: string }[] }[]
  perms: Record<string, { rule: string; scope: string }[]>
  hooks: HookInfo[]
  mcp: McpServer[]
  rules: { name: string; paths?: string[]; warn?: string }[]
  skills: string[]
  docs: string[]
  specific: number
}

// Configuration de Claude Code (config*.go)
export type ConfigFile = { id: string; scope: "user" | "project" | "local"; dir?: string; label: string; path: string; exists: boolean; error?: string; data?: unknown }
export type HookInfo = { scope: string; file: string; event: string; matcher?: string; type: string; command: string; missing?: string }
export type CfgIssue = { file: string; kind: string; msg: string }
export type EffRow = { path: string; values: Record<string, string>; winner: string; merged?: boolean }
export type McpServer = { name: string; scope: string; project?: string; type: string; command?: string; url?: string; envKeys?: string[] }
export type PermSuggest = { rule: string; count: number; readOnly: boolean; example: string }
export type BackupInfo = { name: string; at: string; file: string; kb: number }
export type CfgOverview = {
  files: ConfigFile[]
  hooks: HookInfo[]
  issues: CfgIssue[]
  backups: BackupInfo[]
  plugins: { id: string; enabled: boolean; installed: boolean }[]
  projects: { dir: string; project: string }[]
}
export type CfgEdit = { file: string; op: "set" | "delete" | "add" | "remove"; path: string[]; value?: unknown }
export type Finding = { kind: string; keys: string[]; why: string }
export type Issue = { key: string; kind: string; msg: string; dir?: string }
export type MemProject = { dir: string; project: string; missing: boolean; files: MemFile[] }
export type TabSession = { id: string; dir: string; project: string; title: string }
export type TabSave = { name: string; auto: boolean; at: string; sessions: TabSession[] }
export type MemTarget = { dir: string; project: string }
export type Trashed = { id: string; key: string; path: string; reason: string; at: number; kb: number }
export type Hit = { dir: string; id: string; n: number; snip: string }
export type Item = { dir: string; id: string }
export type Detail = { prompts: string[]; n: number; branch: string; models: string[]; t0: string; t1: string; subagents: number }
export type SubLive = { id: string; desc: string; type: string; running: boolean; activity: string; context: number; out: number; mtime: number }
export type LiveState = { said?: string; open: boolean; status: string; activity: string; thinking: boolean; generic?: boolean; exec?: boolean; since: number; context: number; out: number; model: string; tool?: string; toolRunning?: boolean; subs: SubLive[]; verbs?: string[]; queue?: { t: string; ts: string }[] }
export type UsageLimit = { key: string; label: string; percent: number; resetsAt?: string; limitDollars?: number; usedDollars?: number; lockedWhy?: string }
export type Account = {
  email: string
  name?: string
  org?: string
  orgType?: string
  tier?: string
  billing?: string
  extraUsage: boolean
  extraWhy?: string
  since?: string
  limits: UsageLimit[]
  limitsAt?: number
  refreshing?: boolean
  totalSessions: number
  totalMessages: number
  firstSession?: string
  days: { date: string; messages: number; sessions: number; tools: number; tokens?: Record<string, number> }[]
  statsAt?: string
  models: { model: string; in: number; out: number; cacheRead: number; cacheWrite: number }[]
}
// cut/len : texte coupé à l'octet cut d'un texte de len octets (message démesuré) ; la suite vient de api.convText.
export type Msg = { r: "user" | "assistant" | "tool"; t: string; ts?: string; o?: number; n?: number; img?: string; k?: number; cut?: number; len?: number }
export type Ls = { path: string; parent: string; dirs: string[]; sep: string; error?: string }
export type Result = { ok: boolean; out: string }
// État git d'un dossier (gitstatus.go)
export type GitState = { branch: string; detached?: boolean; upstream: boolean; ahead: number; behind: number; changed: number; untracked: number }
// Maintien au chaud d'une session (keepwarm.go) : pings automatiques, comptés et plafonnés.
export type WarmState = { dir: string; on: boolean; pings: number; max: number; lastPing: number; note?: string }
export type GitRemote = { name: string; url: string }
export type GitChange = { path: string; status: string; from?: string } // status : « XY » comme git status --short
// Dossiers de configuration Claude (homes.go) : current "" = celui du processus principal (main).
export type HomesInfo = { current: string; main: string; homes: { name: string; dir: string; running: boolean }[] }

const enc = encodeURIComponent

// Une réponse d'erreur (serveur arrêté, 500, jeton expiré) lève au lieu d'être prise pour des données.
async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  if (!r.ok) throw new Error(`HTTP ${r.status}`)
  return (await r.json()) as T
}

export const api = {
  data: () => get<Data>("/api"),
  cleanupWarning: () => get<{ days: number; sessions: { dir: string; id: string; title: string; cwd: string; expires: number }[] }>("/cleanup-warning"),
  sessionsStamp: () => get<{ stamp: number }>("/sessions-stamp"),
  sessionTrash: () => get<{ sessions: { dir: string; id: string; title: string; cwd: string; at: number; kb: number }[]; days: number }>("/session-trash"),
  info: (s: Item) => get<Detail>(`/info?dir=${enc(s.dir)}&id=${s.id}`),
  // Lecture par la fin, `before` = « start » rendu par la page précédente. Avec `limit` : autant de messages, textes coupés à
  // 64 Kio (le lecteur). Sans `limit` : pages de 1000 messages aux textes entiers (l'export).
  conv: (s: Item & { sub?: string }, tools: boolean, page?: { limit?: number; before?: number }) =>
    get<{ msgs: Msg[]; start?: number; more?: boolean }>(
      `/conv?dir=${enc(s.dir)}&id=${s.id}${s.sub ? `&sub=${enc(s.sub)}` : ""}${tools ? "&tools=1" : ""}${page?.limit ? `&limit=${page.limit}` : ""}${page ? `&before=${page.before ?? 0}` : ""}`
    ),
  // Suite d'un message coupé, à partir de l'octet from ; cut = 0 : fin du texte.
  convText: (s: Item, tools: boolean, m: Msg, from: number) =>
    get<{ t: string; cut: number }>(`/conv-text?dir=${enc(s.dir)}&id=${s.id}&o=${m.o ?? 0}&n=${m.n ?? 0}&from=${from}${tools ? "&tools=1" : ""}`),
  ls: (path: string) => get<Ls>(`/ls?path=${enc(path)}`),
  homes: () => get<HomesInfo>("/homes"),
  keepWarm: () => get<{ items: Record<string, WarmState>; ttl: number; below: number }>("/keepwarm"),
  gitChanges: (dir: string) => get<{ files: GitChange[]; more?: number }>(`/git-changes?dir=${enc(dir)}`),
  notes: () => get<{ notes: Record<string, string> }>("/notes"),
  trustPending: () => get<{ dirs: string[] }>("/trust-pending"),
  gitRemotes: (dir: string) => get<{ remotes: GitRemote[] }>(`/git-remotes?dir=${enc(dir)}`),
  gitStatus: () => get<{ status: Record<string, GitState>; repos: Record<string, string[]> }>("/git-status"),
  snippets: () => get<{ dir: string; files: Snippet[]; scopes: SnipScope[] }>("/snippet-list"),
  snippet: (key: string) => get<{ text?: string; error?: string; missing?: boolean }>(`/snippet?key=${enc(key)}`),
  memory: () => get<{ global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }>("/memory-list"),
  memoryFile: (key: string) => get<{ text?: string; error?: string }>(`/memory-file?key=${enc(key)}`),
  memoryLoaded: () => get<{ hook: boolean; files: Record<string, FileLoads> }>("/memory-loaded"),
  sessionLoaded: (id: string) => get<{ files: LoadEvent[] }>(`/session-loaded?id=${id}`),
  configRaw: (file: string) => get<{ ok: boolean; text?: string; out?: string }>(`/config-raw?file=${enc(file)}`),
  slashCommands: () => get<{ commands: { cmd: string; desc: string; origin: string }[] }>("/slash-commands"),
  skills: () => get<{ skills: Skill[]; projects: { dir: string; project: string }[] }>("/skills-list"),
  skill: (key: string) => get<{ text?: string; files?: SkillFile[]; error?: string }>(`/skill?key=${enc(key)}`),
  configProjects: () => get<{ projects: ProjectCfg[] }>("/config-projects"),
  sessionIdle: (id: string) => get<{ idle: boolean }>(`/session-state?id=${enc(id)}`),
  sessionLive: (dir: string, id: string) => get<LiveState>(`/session-live?dir=${enc(dir)}&id=${enc(id)}`),
  // refresh : relevé demandé par un POST (un GET ne doit rien déclencher), puis lecture de l'état, qui dit « refreshing ».
  account: async (refresh = false) => {
    if (refresh) await api.post("/account-refresh", {})
    return get<Account>("/account")
  },
  screen: (id: string) => get<{ ok: boolean; out?: string; text?: string }>(`/session-screen?id=${enc(id)}`),
  termInfo: () => get<{ enabled: boolean }>("/term-info"),
  sendInfo: () => get<{ ids: string[]; why: string }>("/session-send-info"),
  config: () => get<CfgOverview>("/config"),
  configEffective: (dir: string) => get<{ rows: EffRow[] }>(`/config-effective?dir=${enc(dir)}`),
  configMcp: () => get<{ servers: McpServer[] }>("/config-mcp"),
  configPermSuggest: () => get<{ suggestions: PermSuggest[] }>("/config-perm-suggest"),
  memoryHealth: () => get<{ issues: Issue[] }>("/memory-health"),
  memoryMatches: (key: string, dir: string) => get<{ files?: string[]; total?: number; truncated?: boolean; error?: string }>(`/memory-matches?key=${enc(key)}&dir=${enc(dir)}`),
  memorySearch: (q: string) => get<{ hits: Record<string, string> }>(`/memory-search?q=${enc(q)}`),
  tabs: () => get<{ saves: TabSave[] }>("/tabs"),
  trash: () => get<{ items: Trashed[] }>("/trash"),
  trashFile: (id: string) => get<{ text?: string; error?: string }>(`/trash-file?id=${enc(id)}`),
  search: (q: string) => get<{ hits: Hit[] }>(`/search?q=${enc(q)}`),
  // Une action ne lève jamais : toute erreur (réseau, HTTP) devient un Result visible.
  // timeoutMs : abandonne la requête au bout de ce délai (sinon elle attend le serveur indéfiniment).
  async post(url: string, body: unknown, timeoutMs?: number): Promise<Result> {
    try {
      const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), signal: timeoutMs ? AbortSignal.timeout(timeoutMs) : undefined })
      return (await r.json()) as Result
    } catch (e) {
      return { ok: false, out: i18n.t("shell.api.error", { msg: e instanceof Error ? e.message : String(e) }) }
    }
  },
}
