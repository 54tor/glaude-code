// Accès aux routes du serveur Go (gld-web/main.go).

export type Session = { id: string; dir: string; title: string; named: boolean; cwd: string; mtime: number; kb: number; tok: Tokens }
export type Tokens = { in: number; out: number; cr: number; cw: number }
export type Group = { project: string; missing: boolean; sessions: Session[] }
export type VGroup = { name: string; project: string; ids: string[]; fav?: boolean }
export type Data = { home: string; root: string; groups: Group[]; live: string[]; waiting: string[]; vgroups: VGroup[]; favs: string[]; tags: Record<string, string[]> }
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
export type Msg = { r: "user" | "assistant" | "tool"; t: string; ts?: string; o?: number; n?: number }
export type Ls = { path: string; parent: string; dirs: string[]; sep: string; error?: string }
export type Result = { ok: boolean; out: string }

const enc = encodeURIComponent

async function get<T>(url: string): Promise<T> {
  const r = await fetch(url)
  return (await r.json()) as T
}

export const api = {
  data: () => get<Data>("/api"),
  info: (s: Item) => get<Detail>(`/info?dir=${enc(s.dir)}&id=${s.id}`),
  // Sans page : toute la conversation. Avec page : `limit` messages par la fin ; `before` = « start » de la page suivante.
  conv: (s: Item, tools: boolean, page?: { limit: number; before?: number }) =>
    get<{ msgs: Msg[]; start?: number; more?: boolean }>(
      `/conv?dir=${enc(s.dir)}&id=${s.id}${tools ? "&tools=1" : ""}${page ? `&limit=${page.limit}&before=${page.before ?? 0}` : ""}`
    ),
  ls: (path: string) => get<Ls>(`/ls?path=${enc(path)}`),
  snippets: () => get<{ dir: string; files: Snippet[]; scopes: SnipScope[] }>("/snippet-list"),
  snippet: (key: string) => get<{ text?: string; error?: string; missing?: boolean }>(`/snippet?key=${enc(key)}`),
  memory: () => get<{ global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }>("/memory-list"),
  memoryFile: (key: string) => get<{ text?: string; error?: string }>(`/memory-file?key=${enc(key)}`),
  memoryLoaded: () => get<{ hook: boolean; files: Record<string, FileLoads> }>("/memory-loaded"),
  sessionLoaded: (id: string) => get<{ files: LoadEvent[] }>(`/session-loaded?id=${id}`),
  configRaw: (file: string) => get<{ ok: boolean; text?: string; out?: string }>(`/config-raw?file=${enc(file)}`),
  skills: () => get<{ skills: Skill[]; projects: { dir: string; project: string }[] }>("/skills-list"),
  skill: (key: string) => get<{ text?: string; files?: SkillFile[]; error?: string }>(`/skill?key=${enc(key)}`),
  configProjects: () => get<{ projects: ProjectCfg[] }>("/config-projects"),
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
  async post(url: string, body: unknown): Promise<Result> {
    try {
      const r = await fetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
      return (await r.json()) as Result
    } catch (e) {
      return { ok: false, out: `Erreur : ${e instanceof Error ? e.message : String(e)}` }
    }
  },
}
