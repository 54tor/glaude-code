import { i18n } from "@/i18n"
import { authFetch } from "@/lib/auth"

// Accès aux routes du serveur Go (gld/main.go).

// Fournisseurs d'agents de code (provider.go) : ce que chacun sait faire ; l'interface masque le reste.
export type Caps = { hotCache: boolean; keepWarm: boolean; config: boolean; memory: boolean; tuiPrompts: boolean; resume: boolean }
export type ProviderInfo = { id: string; name: string; caps: Caps }
export type Session = { provider?: string; id: string; dir: string; title: string; named: boolean; cwd: string; mtime: number; kb: number; tok: Tokens; parent?: string }
export type Tokens = { in: number; out: number; cr: number; cw: number }
export type Group = { project: string; missing: boolean; sessions: Session[] }
export type Project = { name: string; dirs: string[] } // ensemble nommé de dossiers de travail
export type VGroup = { name: string; project: string; ids: string[]; fav?: boolean }
export type Data = { home: string; root: string; groups: Group[]; dirs: string[]; live: string[]; background?: string[]; waiting: string[]; asking?: string[]; vgroups: VGroup[]; favs: string[]; projects: Project[]; providers: ProviderInfo[]; lang?: string }
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
export type PluginInst = { id: string; version: string; scope: string; enabled: boolean; project?: string }
export type PluginAvail = { id: string; name: string; description: string; marketplace: string; installs: number; installed: boolean }
export type Marketplace = { name: string; source: string; repo?: string; url?: string }
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
  descTokens: number
  tokens: number
  manual?: boolean
  hidden?: boolean
  tools?: string
  files: number
  kb: number
  mtime: number
  path: string
  warn?: string
  lock?: SkillLock // posé par npx skills
}
export type SkillLock = { source: string; sourceType: string; skillPath: string; hash: string; installedAt: string; updatedAt: string; link: boolean; global: boolean; managed: boolean }
export type SkillHit = { ref: string; source: string; skill: string; installs: string; url: string }
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
export type BgTask = { id: string; tool?: string; desc: string; status: string; summary?: string; start?: string; end?: string; hasOutput: boolean }
export type LiveState = { said?: string; open: boolean; status: string; activity: string; thinking: boolean; generic?: boolean; exec?: boolean; since: number; context: number; out: number; model: string; tool?: string; toolRunning?: boolean; subs: SubLive[]; tasks?: BgTask[]; verbs?: string[]; queue?: { t: string; ts: string }[]; pending?: QItem[] }
// File de messages côté serveur (queue.go) : auteur, et état d'envoi (uncertain : parti sans réponse, à trancher).
export type QItem = { id: string; text: string; author: "host" | "guest"; state: "queued" | "sending" | "uncertain"; at: number; sentAt?: number }
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
export type Msg = { r: "user" | "assistant" | "tool" | "doc"; t: string; ts?: string; o?: number; n?: number; img?: string; k?: number; cut?: number; len?: number }
export type Ls = { path: string; parent: string; dirs: string[]; sep: string; error?: string }
export type Result = { ok: boolean; out: string; net?: boolean; n?: number } // net : pas de réponse (réseau, délai) ; l'action a pu avoir lieu
// État git d'un dossier (gitstatus.go)
export type GitState = { branch: string; detached?: boolean; upstream: boolean; ahead: number; behind: number; changed: number; untracked: number }
// Maintien au chaud d'une session (keepwarm.go) : pings automatiques, comptés et plafonnés.
export type WarmState = { dir: string; on: boolean; pings: number; max: number; lastPing: number; note?: string }
export type GitRemote = { name: string; url: string }
export type GitChange = { path: string; status: string; from?: string } // status : « XY » comme git status --short
// Dossiers de configuration Claude (homes.go) : current "" = celui du processus principal (main).
// main "" et mainId : mode multi-utilisateur, le principal sans droit dessus ; id : clé des droits, connue du serveur.
// Appareil jumelé par QR code en mode jeton (pair.go) ; seen : dernière visite, en secondes Unix.
export type Device = { id: string; name: string; ip: string; created: number; seen: number }
export type HomesInfo = { current: string; main: string; mainId?: string; homes: { name: string; dir: string; id?: string; running: boolean }[] }

const enc = encodeURIComponent

// status : 403 dit un droit manquant (mode multi-utilisateur), à distinguer d'un serveur injoignable.
export class HttpError extends Error {
  status: number
  constructor(status: number) {
    super(`HTTP ${status}`)
    this.status = status
  }
}

// Une réponse d'erreur (serveur arrêté, 500, jeton expiré) lève au lieu d'être prise pour des données.
async function get<T>(url: string): Promise<T> {
  const r = await authFetch(url)
  if (!r.ok) throw new HttpError(r.status)
  return (await r.json()) as T
}

export type FsTrashItem = { id: string; path: string; mtime: number }
export type FsEntry = { name: string; type: "dir" | "file" | "symlink"; size: number; mtime: number }
// Réponse d'un appel fs-* : le statut (409, 413, 415…) arrive comme donnée, jamais comme exception ; 0 = réseau.
export type FsRes<T = object> = { status: number; ok: boolean; body: Partial<T> & { ok?: boolean; error?: string } }
async function fsCall<T = object>(url: string, body?: unknown): Promise<FsRes<T>> {
  try {
    const r = await authFetch(url, body === undefined ? {} : { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body) })
    const b = (await r.json().catch(() => ({}))) as FsRes<T>["body"]
    return { status: r.status, ok: r.ok && b.ok !== false, body: b }
  } catch {
    return { status: 0, ok: false, body: {} }
  }
}
const fsq = (dir: string, path?: string) => `dir=${enc(dir)}${path === undefined ? "" : `&path=${enc(path)}`}`

export const api = {
  fsTree: (dir: string, path: string) => get<{ entries: FsEntry[]; truncated: boolean }>(`/fs-tree?${fsq(dir, path)}`),
  fsFile: (dir: string, path: string) => fsCall<{ content: string; etag: string; size: number; mtime: number }>(`/fs-file?${fsq(dir, path)}`),
  fsSave: (dir: string, path: string, content: string, etag: string, force?: boolean) => fsCall<{ etag: string; conflict: boolean; deleted: boolean }>("/fs-save", { dir, path, content, etag, force }),
  fsNew: (dir: string, path: string, kind: "file" | "dir") => fsCall("/fs-new", { dir, path, kind }),
  fsRename: (dir: string, from: string, to: string) => fsCall("/fs-rename", { dir, from, to }),
  fsRm: (dir: string, path: string) => fsCall("/fs-rm", { dir, path }),
  fsTrash: (dir: string) => fsCall<{ items: FsTrashItem[] }>(`/fs-trash?${fsq(dir)}`),
  fsTrashRestore: (dir: string, id: string) => fsCall("/fs-trash-restore", { dir, id }),
  fsTrashPurge: (dir: string, id: string) => fsCall("/fs-trash-purge", { dir, id }),
  fsIndex: (dir: string) => fsCall<{ state: string; files: { path: string; size: number; mtime: number }[] }>(`/fs-index?${fsq(dir)}`),
  fsFiles: (dir: string, paths: string[]) => fsCall<{ files: { path: string; content: string }[] }>("/fs-files", { dir, paths }),
  data: () => get<Data>("/api"),
  banAlerts: (after: number) =>
    get<{ ok: boolean; cursor: number; bans: { at: number; ip: string }[]; pairings: { at: number; ip: string }[] }>(`/auth/alerts?after=${after}`),
  cleanupWarning: () => get<{ days: number; sessions: { dir: string; id: string; title: string; cwd: string; expires: number }[] }>("/cleanup-warning"),
  sessionsStamp: () => get<{ stamp: number }>("/sessions-stamp"),
  sessionTrash: () => get<{ sessions: { dir: string; id: string; title: string; cwd: string; at: number; kb: number }[]; days: number }>("/session-trash"),
  info: (s: Item) => get<Detail>(`/info?dir=${enc(s.dir)}&id=${s.id}`),
  // Lecture par la fin, `before` = « start » rendu par la page précédente. Avec `limit` : autant de messages, textes coupés à
  // 64 Kio (le lecteur). Sans `limit` : pages de 1000 messages aux textes entiers (l'export).
  conv: (s: Item & { sub?: string }, tools: boolean, page?: { limit?: number; before?: number; anchors?: boolean }) =>
    get<{ msgs: Msg[]; start?: number; more?: boolean }>(
      `/conv?dir=${enc(s.dir)}&id=${s.id}${s.sub ? `&sub=${enc(s.sub)}` : ""}${tools ? "&tools=1" : ""}${page?.limit ? `&limit=${page.limit}` : ""}${page ? `&before=${page.before ?? 0}` : ""}${page?.anchors && !tools ? "&anchors=1" : ""}`
    ),
  // Messages d'outil des lignes [from, until) du transcript, pour déplier une ancre.
  convTools: (s: Item, from: number, until: number) => get<{ msgs: Msg[] }>(`/conv-tools?dir=${enc(s.dir)}&id=${s.id}&from=${from}&until=${until}`),
  // Suite d'un message coupé, à partir de l'octet from ; cut = 0 : fin du texte.
  convText: (s: Item, tools: boolean, m: Msg, from: number) =>
    get<{ t: string; cut: number }>(`/conv-text?dir=${enc(s.dir)}&id=${s.id}&o=${m.o ?? 0}&n=${m.n ?? 0}&from=${from}${tools ? "&tools=1" : ""}`),
  ls: (path: string) => get<Ls>(`/ls?path=${enc(path)}`),
  homes: () => get<HomesInfo>("/homes"),
  devices: () => get<{ devices: Device[] | null }>("/devices"),
  keepWarm: () => get<{ items: Record<string, WarmState>; ttl: number; below: number }>("/keepwarm"),
  gitChanges: (dir: string) => get<{ files: GitChange[]; more?: number; prefix?: string }>(`/git-changes?dir=${enc(dir)}`),
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
  sessionLoadedFile: (id: string, file: string) => get<{ text?: string }>(`/session-loaded-file?id=${id}&file=${enc(file)}`),
  configRaw: (file: string) => get<{ ok: boolean; text?: string; out?: string }>(`/config-raw?file=${enc(file)}`),
  slashCommands: () => get<{ commands: { cmd: string; desc: string; origin: string }[] }>("/slash-commands"),
  skills: () => get<{ skills: Skill[]; projects: { dir: string; project: string }[] }>("/skills-list"),
  skill: (key: string) => get<{ text?: string; files?: SkillFile[]; error?: string }>(`/skill?key=${enc(key)}`),
  skillsFind: (q: string) => get<{ hits?: SkillHit[]; error?: string }>(`/skills-find?q=${enc(q)}`),
  skillsUpdates: (force = false) => get<{ states?: Record<string, string> }>(`/skills-updates${force ? "?force=1" : ""}`),
  marketplaces: () => get<{ marketplaces?: Marketplace[]; error?: string }>("/marketplaces"),
  plugins: (q = "") => get<{ installed?: PluginInst[]; available?: PluginAvail[]; error?: string }>(`/plugins?q=${enc(q)}`),
  configProjects: () => get<{ projects: ProjectCfg[] }>("/config-projects"),
  sessionIdle: (id: string) => get<{ idle: boolean }>(`/session-state?id=${enc(id)}`),
  taskOutput: (dir: string, id: string, task: string) => get<{ ok: boolean; text?: string }>(`/session-task-output?dir=${enc(dir)}&id=${enc(id)}&task=${enc(task)}`),
  queue: (id: string) => get<{ items: QItem[] }>(`/queue?id=${enc(id)}`),
  queueAdd: (id: string, text: string) => api.post("/queue-add", { id, text }),
  queueRm: (id: string, item: string) => api.post("/queue-rm", { id, item }) as Promise<Result & { text?: string }>,
  queueRetry: (id: string, item: string) => api.post("/queue-retry", { id, item }),
  shareCreate: (id: string, dir: string) => api.post("/share-create", { id, dir }) as Promise<Result & { token?: string }>,
  shareInfo: (id: string) => get<{ shared: boolean; mode?: "rw" | "ro"; connected: boolean }>(`/share-info?id=${enc(id)}`),
  shareMode: (id: string, mode: "rw" | "ro") => api.post("/share-mode", { id, mode }),
  shareEnd: (id: string) => api.post("/share-end", { id }),
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
      const r = await authFetch(url, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify(body), signal: timeoutMs ? AbortSignal.timeout(timeoutMs) : undefined })
      return (await r.json()) as Result
    } catch (e) {
      return { ok: false, net: true, out: i18n.t("shell.api.error", { msg: e instanceof Error ? e.message : String(e) }) }
    }
  },
}
