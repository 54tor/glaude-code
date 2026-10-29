import { i18n } from "@/i18n"
import { authFetch } from "@/lib/auth"
import { http, qs, seg } from "@/lib/http"

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
export type QItem = { id: string; text: string; author: "host" | "guest"; guest?: string; label?: string; state: "queued" | "sending" | "uncertain"; at: number; sentAt?: number }
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
// Réponse textuelle d'une action de l'API /v1 (toast).
export type Said = { message: string }
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
export type InviteQueued = { id: string; text: string; state: "queued" | "sending" | "uncertain"; at: number }
export type InviteInfo = { id: string; seq: number; name: string; label: string; mode: "rw" | "ro"; state: "connected" | "away" | "pending"; lastSeen: number; redeemed: boolean; queue: InviteQueued[] }
export type ShareInfo = { id: string; dir: string; title: string; handle: string; open: boolean; invites: InviteInfo[] }
export type GuestShare = { handle: string; title: string; mode: "rw" | "ro" }
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
  data: () => http.get<Data>("/state"),
  banAlerts: (after: number) =>
    get<{ ok: boolean; cursor: number; bans: { at: number; ip: string }[]; pairings: { at: number; ip: string }[] }>(`/auth/alerts?after=${after}`),
  cleanupWarning: () => http.get<{ days: number; sessions: { dir: string; id: string; title: string; cwd: string; expires: number }[] }>("/cleanup-warning"),
  sessionsStamp: () => http.get<{ stamp: number }>("/state/stamp"),
  sessionTrash: () => http.get<{ sessions: { dir: string; id: string; title: string; cwd: string; at: number; kb: number }[]; days: number }>("/trash/sessions"),
  info: (s: Item) => http.get<Detail>(`/info${qs({ dir: s.dir, id: s.id })}`),
  // Lecture par la fin, `before` = « start » rendu par la page précédente. Avec `limit` : autant de messages, textes coupés à
  // 64 Kio (le lecteur). Sans `limit` : pages de 1000 messages aux textes entiers (l'export).
  conv: (s: Item & { sub?: string }, tools: boolean, page?: { limit?: number; before?: number; anchors?: boolean }) =>
    http.get<{ msgs: Msg[]; start?: number; more?: boolean }>(
      `/sessions/${seg(s.id)}/conversation${qs({ dir: s.dir, sub: s.sub, tools, limit: page?.limit || undefined, before: page ? (page.before ?? 0) : undefined, anchors: !!page?.anchors && !tools })}`
    ),
  // Messages d'outil des lignes [from, until) du transcript, pour déplier une ancre.
  convTools: (s: Item, from: number, until: number) => http.get<{ msgs: Msg[] }>(`/sessions/${seg(s.id)}/conversation/tools${qs({ dir: s.dir, from, until })}`),
  // Suite d'un message coupé, à partir de l'octet from ; cut = 0 : fin du texte.
  convText: (s: Item, tools: boolean, m: Msg, from: number) =>
    http.get<{ t: string; cut: number }>(`/sessions/${seg(s.id)}/conversation/text${qs({ dir: s.dir, o: m.o ?? 0, n: m.n ?? 0, from, tools })}`),
  ls: (path: string) => http.get<Ls>(`/directories${qs({ path })}`),
  homes: () => get<HomesInfo>("/homes"),
  devices: () => get<{ devices: Device[] | null }>("/devices"),
  keepWarm: () => http.get<{ items: Record<string, WarmState>; ttl: number; below: number }>("/keepwarm"),
  gitChanges: (dir: string) => http.get<{ files: GitChange[]; more?: number; prefix?: string }>(`/git/changes${qs({ dir })}`),
  notes: () => http.get<{ notes: Record<string, string> }>("/notes"),
  trustPending: () => http.get<{ dirs: string[] }>("/trust/pending"),
  gitRemotes: (dir: string) => http.get<{ remotes: GitRemote[] }>(`/git/remotes${qs({ dir })}`),
  gitStatus: () => http.get<{ status: Record<string, GitState>; repos: Record<string, string[]> }>("/git/status"),
  snippets: () => get<{ dir: string; files: Snippet[]; scopes: SnipScope[] }>("/snippet-list"),
  snippet: (key: string) => get<{ text?: string; error?: string; missing?: boolean }>(`/snippet?key=${enc(key)}`),
  memory: () => http.get<{ global: MemFile[]; projects: MemProject[]; targets: MemTarget[] }>("/memory/files"),
  memoryFile: (key: string) => http.get<{ text: string; missing?: boolean }>(`/memory/files/${seg(key)}`),
  memoryLoaded: () => http.get<{ hook: boolean; files: Record<string, FileLoads> }>("/memory/loaded"),
  memorySave: (key: string, text: string, index = false) => http.put<Said>(`/memory/files/${seg(key)}`, { text, index }),
  memoryRename: (key: string, name: string) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { name }),
  memoryMove: (key: string, to: string, copy = false) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { to, copy }),
  memoryToggle: (key: string, enabled: boolean) => http.patch<Said & { key: string }>(`/memory/files/${seg(key)}`, { enabled }),
  memoryType: (key: string, type: string) => http.patch<Said>(`/memory/files/${seg(key)}`, { type }),
  memoryPaths: (key: string, paths: string[]) => http.patch<Said>(`/memory/files/${seg(key)}`, { paths }),
  memoryRemove: (keys: string[]) => http.del<Said>(`/memory/files${qs({ item: keys })}`),
  memoryFix: (key: string) => http.post<Said>("/memory/fixes", { key }),
  memoryRepair: (dir: string) => http.post<Said>("/memory/repairs", { dir }),
  memoryDraft: (body: { model: string; instruction: string; key: string; refs: string[] }) => http.post<Said & { text: string; cost?: number }>("/memory/drafts", body),
  memoryMerge: (body: { keys: string[]; out?: string; launch: boolean; prompt?: string; args?: string }) => http.post<Said & { prompt: string }>("/memory/merges", body),
  memoryTokens: (keys: string[]) => http.post<{ counts: Record<string, number>; errors: Record<string, string>; cost: number }>("/memory/token-counts", { keys }),
  memoryLint: (scope: string) => http.post<{ findings: Finding[]; cost?: number }>("/memory/lints", { scope }),
  sessionLoaded: (id: string) => http.get<{ files: LoadEvent[] }>(`/sessions/${seg(id)}/loaded`),
  sessionLoadedFile: (id: string, file: string) => http.get<{ text?: string }>(`/sessions/${seg(id)}/loaded/files/${seg(file)}`),
  configRaw: (file: string) => get<{ ok: boolean; text?: string; out?: string }>(`/config-raw?file=${enc(file)}`),
  slashCommands: () => http.get<{ commands: { cmd: string; desc: string; origin: string }[] }>("/slash-commands"),
  skills: () => get<{ skills: Skill[]; projects: { dir: string; project: string }[] }>("/skills-list"),
  skill: (key: string) => get<{ text?: string; files?: SkillFile[]; error?: string }>(`/skill?key=${enc(key)}`),
  skillsFind: (q: string) => get<{ hits?: SkillHit[]; error?: string }>(`/skills-find?q=${enc(q)}`),
  skillsUpdates: (force = false) => get<{ states?: Record<string, string> }>(`/skills-updates${force ? "?force=1" : ""}`),
  marketplaces: () => get<{ marketplaces?: Marketplace[]; error?: string }>("/marketplaces"),
  plugins: (q = "") => get<{ installed?: PluginInst[]; available?: PluginAvail[]; error?: string }>(`/plugins?q=${enc(q)}`),
  configProjects: () => get<{ projects: ProjectCfg[] }>("/config-projects"),
  sessionIdle: (id: string) => http.get<{ idle: boolean }>(`/sessions/${seg(id)}/state`),
  taskOutput: (dir: string, id: string, task: string) => http.get<{ text?: string }>(`/sessions/${seg(id)}/tasks/${seg(task)}/output${qs({ dir })}`),
  queue: (id: string) => http.get<{ items: QItem[] }>(`/sessions/${seg(id)}/queue`),
  queueAdd: (id: string, text: string) => http.post<{ item: QItem }>(`/sessions/${seg(id)}/queue`, { text }),
  queueRm: (id: string, item: string) => http.del<{ text: string }>(`/sessions/${seg(id)}/queue/${seg(item)}`),
  queueRetry: (id: string, item: string) => http.patch(`/sessions/${seg(id)}/queue/${seg(item)}`, { state: "queued" }),
  convRewind: (dir: string, id: string, o: number) => http.del<{ n: number; message: string }>(`/sessions/${seg(id)}/conversation${qs({ dir, after: o })}`),
  sessionSend: (id: string, text: string, timeoutMs?: number) => http.post<Said>(`/sessions/${seg(id)}/messages`, { text }, timeoutMs),
  sessionKey: (id: string, key: string) => http.post<Said>(`/sessions/${seg(id)}/keystrokes`, { key }),
  sessionClose: (id: string) => http.del<Said>(`/sessions/${seg(id)}/process`),
  open: (b: { dir: string; id?: string; args?: string; prompt?: string }) =>
    b.id ? http.put<Said>(`/sessions/${seg(b.id)}/process${qs({ dir: b.dir })}`, { args: b.args, prompt: b.prompt }) : http.put<Said>(`/projects/${seg(b.dir)}/process`, { args: b.args, prompt: b.prompt }),
  newSession: (b: { dir?: string; id?: string; cwd?: string; prompt: string; args?: string }) => http.post<Said>("/sessions", b),
  rename: (s: Item, b: { title?: string; auto?: boolean }) => http.patch<Said & { title: string }>(`/sessions/${seg(s.id)}${qs({ dir: s.dir })}`, b),
  reassign: (items: Item[], to: string) => http.patch<Said & { n: number }>("/sessions", { items, to }),
  rmSessions: (items: Item[]) => http.del<Said>(`/sessions${qs({ item: items.map((i) => `${i.dir}:${i.id}`) })}`),
  trashSessionsAdd: (items: Item[]) => http.post<Said>(`/trash/sessions${qs({ close: true })}`, { items }),
  trashSessionsRestore: (ids: string[]) => http.patch<Said>(`/trash/sessions${qs({ item: ids })}`, { trashed: false }),
  trashSessionsPurge: (ids: string[]) => http.del<Said>(`/trash/sessions${qs({ item: ids })}`),
  vgroups: (b: { groups: VGroup[]; favs: string[]; projects?: Project[] }) => http.put("/vgroups", b),
  dirsAdd: (dir: string, children = false) => http.post<Said & { n: number }>("/directories", { dir, children }),
  dirsRemove: (dir: string) => http.del<Said>(`/directories${qs({ dir })}`),
  keepWarmSet: (b: { dir: string; id: string; on: boolean; max?: number; reheat?: boolean }) => http.put<Said>("/keepwarm", b),
  trustAnswer: (dir: string, yes: boolean) => http.post<Said>("/trust/answers", { dir, yes }),
  termOpen: (id: string) => http.post<{ token: string; cols: number; rows: number }>("/terminal", { id }),
  chatStop: (id: string) => http.del<Said>(`/sessions/${seg(id)}/chat`),
  notesSave: (b: { key: string; text?: string; to?: string }) => http.put<Said>("/notes", b),
  tabsSave: (name: string) => http.put<Said & { n: number }>("/tabs", { name }),
  tabsRm: (at: string) => http.del<Said>(`/tabs/${seg(at)}`),
  gitPush: (dir: string, remote: string) => http.post<Said>("/git/pushes", { dir, remote }, 90_000),
  trashRestore: (id: string) => http.patch<Said & { key: string }>(`/trash/memory/${seg(id)}`, { trashed: false }),
  trashPurge: (id?: string) => http.del<Said>(`/trash/memory${qs(id ? { item: id } : { all: true })}`),
  inviteCreate: (id: string, dir: string, title?: string, name?: string) => api.post("/invite-create", { id, dir, ...(title ? { title } : {}), ...(name ? { name } : {}) }) as Promise<Result & { token?: string; invite?: { id: string; seq: number; label: string } }>,
  inviteRegen: (id: string, invite: string) => api.post("/invite-regen", { id, invite }) as Promise<Result & { token?: string }>,
  inviteMode: (id: string, invite: string, mode: "rw" | "ro") => api.post("/invite-mode", { id, invite, mode }),
  inviteRevoke: (id: string, invite: string) => api.post("/invite-revoke", { id, invite }),
  shares: () => get<{ shares: ShareInfo[] }>("/shares"),
  guestMe: () => get<{ id: string; dir: string; mode: "rw" | "ro"; csrf: string; handle: string; title: string; invite: string; label: string }>("/guest-me"),
  // Sans X-Gld-Share : la liste concerne le navigateur entier, pas un partage.
  guestList: async (): Promise<GuestShare[]> => {
    const r = await fetch("/guest-list")
    if (!r.ok) throw new HttpError(r.status)
    return (await r.json()) as GuestShare[]
  },
  guestLeave: () => api.post("/guest-leave", {}),
  shareEnd: (id: string) => api.post("/share-end", { id }),
  sessionLive: (dir: string, id: string) => http.get<LiveState>(`/sessions/${seg(id)}/live${qs({ dir })}`),
  // refresh : relevé demandé par un POST (un GET ne doit rien déclencher), puis lecture de l'état, qui dit « refreshing ».
  account: async (refresh = false) => {
    if (refresh) await api.post("/account-refresh", {})
    return get<Account>("/account")
  },
  screen: (id: string) => http.get<{ text: string }>(`/sessions/${seg(id)}/screen`),
  termInfo: () => http.get<{ enabled: boolean }>("/terminal"),
  sendInfo: () => http.get<{ ids: string[]; why: string }>("/sessions/send-info"),
  config: () => get<CfgOverview>("/config"),
  configEffective: (dir: string) => get<{ rows: EffRow[] }>(`/config-effective?dir=${enc(dir)}`),
  configMcp: () => get<{ servers: McpServer[] }>("/config-mcp"),
  configPermSuggest: () => get<{ suggestions: PermSuggest[] }>("/config-perm-suggest"),
  memoryHealth: () => http.get<{ issues: Issue[] }>("/memory/health"),
  memoryMatches: (key: string, dir: string) => http.get<{ files?: string[]; total?: number; truncated?: boolean }>(`/memory/matches${qs({ key, dir })}`),
  memorySearch: (q: string) => http.get<{ hits: Record<string, string> }>(`/memory/files${qs({ q })}`),
  tabs: () => http.get<{ saves: TabSave[] }>("/tabs"),
  trash: () => http.get<{ items: Trashed[] }>("/trash/memory"),
  trashFile: (id: string) => http.get<{ text?: string }>(`/trash/memory/${seg(id)}/file`),
  search: (q: string) => http.get<{ hits: Hit[] }>(`/sessions${qs({ q })}`),
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
